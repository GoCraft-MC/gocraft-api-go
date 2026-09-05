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
	verdict := event.verdict()
	if len(verdict.Mutations) != 2 {
		t.Fatalf("verdict has %d mutations, want 2", len(verdict.Mutations))
	}
	if got := verdict.Mutations[1].Path; len(got) != 2 || got[0] != 1 || got[1] != 1 {
		t.Fatalf("second mutation path = %v, want [1 1]", got)
	}
	if verdict.Cancelled {
		t.Fatal("verdict is cancelled and nothing cancelled it")
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
	if len(event.verdict().Mutations) != 0 {
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
	if err := events.OnCustom("fr.oreo.shop/purchase", func(event *CustomDispatch) {
		seen = event
		event.Cancel()
	}); err != nil {
		t.Fatalf("OnCustom = %v", err)
	}
	if err := events.OnCustom("fr.oreo.shop/purchase", nil); err == nil {
		t.Fatal("a subscription with no handler was accepted")
	}
	event := anIncomingPurchase()
	events.dispatch(event)
	if seen != event {
		t.Fatalf("handler saw %p, dispatched %p", seen, event)
	}
	if !event.verdict().Cancelled {
		t.Fatal("Cancel did not reach the verdict")
	}
}
