package gocraft

import (
	"log/slog"
	"testing"
)

// The same layout the emitting tests use — player, tiers, price — because it is
// the same event seen from the other end.
func anIncomingPurchase() *CustomDispatch {
	return &CustomDispatch{
		eventType: "fr.oreo.shop/purchase",
		fields:    aPurchase().Fields(),
	}
}

func TestCustomDispatchReadsByKind(t *testing.T) {
	event := anIncomingPurchase()
	if player, ok := event.Text(0); !ok || player != "oreo" {
		t.Fatalf("Text(0) = %q, %v", player, ok)
	}
	if _, ok := event.Decimal(0); ok {
		t.Fatal("Decimal(0) read a string as a decimal")
	}
	if _, ok := event.Text(7); ok {
		t.Fatal("Text(7) read past the end of the payload")
	}
	if event.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", event.Len())
	}
}

// A handler that writes and then reads has to see its own write: what it does
// next may well depend on the discount it just applied.
func TestSetIsVisibleToTheHandlerThatWroteIt(t *testing.T) {
	event := anIncomingPurchase()
	if err := event.Set(2, Double(1350)); err != nil {
		t.Fatalf("Set(2, 1350) = %v", err)
	}
	if price, ok := event.Decimal(2); !ok || price != 1350 {
		t.Fatalf("Decimal(2) after Set = %v, %v", price, ok)
	}
}

// Writes are recorded, not diffed. The verdict carries exactly what the handler
// asked for, in the order it asked, which is what the host replays into the
// emitter's own object.
func TestSetRecordsOneMutationEach(t *testing.T) {
	event := anIncomingPurchase()
	if err := event.Set(2, Double(1350)); err != nil {
		t.Fatal(err)
	}
	if err := event.SetAt([]uint32{1, 1}, Double(3.50)); err != nil {
		t.Fatalf("SetAt([1 1], 3.50) = %v", err)
	}
	mutations := event.mutations
	if len(mutations) != 2 {
		t.Fatalf("%d mutations recorded, want 2", len(mutations))
	}
	if got := mutations[1].Path; len(got) != 2 || got[0] != 1 || got[1] != 1 {
		t.Fatalf("second mutation path = %v, want [1 1]", got)
	}
}

// A list element is written without replacing the list, which is what makes a
// field declared immutable still useful: nobody swaps the list, its elements
// stay the subscriber's to change.
func TestSetAtReachesInsideAField(t *testing.T) {
	event := anIncomingPurchase()
	if err := event.SetAt([]uint32{1, 0}, Double(9)); err != nil {
		t.Fatal(err)
	}
	tiers, ok := event.Field(1)
	if !ok || len(tiers.List) != 2 {
		t.Fatalf("Field(1) = %+v, %v", tiers, ok)
	}
	if tiers.List[0].Double != 9 || tiers.List[1].Double != 4.50 {
		t.Fatalf("tiers = %v, want the first replaced and the second untouched", tiers.List)
	}
}

// Refused here rather than at the far end, where the plugin that wrote it is
// only a name in a log line.
func TestSetRefusesAPathThePayloadCannotTake(t *testing.T) {
	event := anIncomingPurchase()
	if err := event.Set(9, Double(1)); err == nil {
		t.Fatal("a write past the declared layout was accepted")
	}
	if err := event.Set(-1, Double(1)); err == nil {
		t.Fatal("a negative index was accepted")
	}
	if err := event.SetAt(nil, Double(1)); err == nil {
		t.Fatal("a write with no path was accepted")
	}
	if len(event.mutations) != 0 {
		t.Fatal("a refused write was recorded anyway")
	}
}

// Into hands the payload to the same struct the emitting side publishes, with
// whatever this handler and the ones before it changed already applied.
func TestIntoReadsThePayloadIntoAStruct(t *testing.T) {
	event := anIncomingPurchase()
	if err := event.Set(2, Double(1350)); err != nil {
		t.Fatal(err)
	}
	var view purchase
	if err := event.Into(&view); err != nil {
		t.Fatalf("Into(&view) = %v", err)
	}
	if view.Player != "oreo" || view.Price != 1350 {
		t.Fatalf("view = %+v, want the payload with the write applied", view)
	}
}

func TestIntoRefusesAnotherEventsType(t *testing.T) {
	refund := &CustomDispatch{eventType: "fr.oreo.shop/refund", fields: aPurchase().Fields()}
	if err := refund.Into(&purchase{}); err == nil {
		t.Fatal("a refund was read into a purchase")
	}
	if err := anIncomingPurchase().Into(nil); err == nil {
		t.Fatal("a nil target was accepted")
	}
}

func TestOnCustomReceivesTheDispatchedEvent(t *testing.T) {
	events := newEvents(slog.Default(), "fr.oreo.shop", nil, nil)
	var seen *CustomDispatch
	if err := events.OnCustom("fr.oreo.shop/purchase", func(event *CustomDispatch, control EventControl) {
		seen = event
		control.Cancel()
	}); err != nil {
		t.Fatalf("OnCustom = %v", err)
	}
	if err := events.OnCustom("fr.oreo.shop/purchase", nil); err == nil {
		t.Fatal("a subscription with no handler was accepted")
	}
	event := anIncomingPurchase()
	answer := &control{}
	events.dispatch(event, answer)
	if seen != event {
		t.Fatalf("handler saw %p, dispatched %p", seen, event)
	}
	if !answer.verdict().Cancelled {
		t.Fatal("Cancel did not reach the verdict")
	}
}

// An effect belongs to the thing it happens to: the handle carries the verb,
// and the control is only where a handle comes from when the event did not
// hand one over.
func TestAPlayerHandleSendsThroughItsDispatch(t *testing.T) {
	answer := &control{}
	uuid := make([]byte, 16)
	for index := range uuid {
		uuid[index] = byte(index)
	}
	player := answer.Player(uuid)
	if player == nil {
		t.Fatal("a 16-byte uuid produced no handle")
	}
	if err := player.SendMessage("10% off applied."); err != nil {
		t.Fatalf("SendMessage = %v", err)
	}
	if err := player.SendMessage("and a second line"); err != nil {
		t.Fatal(err)
	}
	verdict := answer.verdict()
	if len(verdict.Effects) != 2 {
		t.Fatalf("verdict carries %d effects, want 2", len(verdict.Effects))
	}
	first := verdict.Effects[0]
	if first.Type != EffectMessage || len(first.Fields) != 2 {
		t.Fatalf("effect = %+v", first)
	}
	if first.Fields[1].String != "10% off applied." {
		t.Fatalf("message = %q", first.Fields[1].String)
	}
}

func TestControlRefusesSomethingThatIsNotAUUID(t *testing.T) {
	answer := &control{}
	if handle := answer.Player([]byte("oreo")); handle != nil {
		t.Fatal("a four-byte uuid produced a handle")
	}
	if err := answer.Player([]byte("oreo")).SendMessage("hello"); err == nil {
		t.Fatal("sending through a nil handle was accepted")
	}
	if len(answer.verdict().Effects) != 0 {
		t.Fatal("a refused effect was queued anyway")
	}
}

// A handle kept past its dispatch writes into a verdict already sent. Saying so
// beats an effect that goes nowhere, which is the failure mode nothing reveals.
func TestAStaleHandleSaysSo(t *testing.T) {
	answer := &control{}
	player := answer.Player(make([]byte, 16))
	answer.verdict()
	if err := player.SendMessage("too late"); err == nil {
		t.Fatal("a handle kept past its dispatch still queued an effect")
	}
}
