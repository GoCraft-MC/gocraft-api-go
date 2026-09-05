package gocraft

import (
	"testing"

	abi "github.com/GoCraft-MC/gocraft-abi/abi/v1"
)

// The event carries the payload and nothing else. Refusing what it announced
// is the control's, so that one shape serves a generated event and a struct a
// plugin author wrote — nothing can add a method to the latter.
func TestAnEventCarriesOnlyItsPayload(t *testing.T) {
	event := &BlockBreakEvent{
		Player: &PlayerRef{Username: "Elias"},
		Pos:    BlockPos{X: 4, Y: 64, Z: -2},
		Block:  Block{ID: "minecraft:stone"},
	}
	if event.Type() != EventBlockBreak {
		t.Fatalf("Type() = %q", event.Type())
	}
	answer := &control{}
	if answer.Cancelled() {
		t.Fatal("a fresh control is cancelled")
	}
	answer.Cancel()
	if !answer.Cancelled() {
		t.Fatal("Cancel() did not persist")
	}
}

// Decoding is what proves the layout: the payload carries no field names, so a
// reader that disagreed with the host about index 3 would produce a plausible
// event rather than an error.
func TestBlockBreakDecodesItsPositionalPayload(t *testing.T) {
	uuid := make([]byte, 16)
	uuid[0] = 7
	fields := []abi.Value{
		abi.List(abi.Bytes(uuid), abi.String("Elias"), abi.String("bedrock")),
		abi.List(abi.Int64(4), abi.Int64(64), abi.Int64(-2)),
		abi.List(abi.String("minecraft:stone"), abi.List()),
		abi.String("minecraft:diamond_pickaxe"),
		abi.List(abi.List(abi.String("spawn.bypass"), abi.Bool(true))),
	}
	decoded, err := eventFrom(&abi.Event{Type: EventBlockBreak, Fields: fields}, &effects{})
	if err != nil {
		t.Fatal(err)
	}
	event, ok := decoded.(*BlockBreakEvent)
	if !ok {
		t.Fatalf("decoded %T", decoded)
	}
	if event.Player.Username != "Elias" || event.Player.Edition != "bedrock" {
		t.Fatalf("player = %+v", event.Player)
	}
	if event.Pos != (BlockPos{X: 4, Y: 64, Z: -2}) {
		t.Fatalf("pos = %+v", event.Pos)
	}
	if event.Block.ID != "minecraft:stone" {
		t.Fatalf("block = %+v", event.Block)
	}
	if event.Tool != "minecraft:diamond_pickaxe" {
		t.Fatalf("tool = %q", event.Tool)
	}
	// The injected map is answered through a query, never handed over.
	if !event.Can("spawn.bypass") || event.Can("never.declared") {
		t.Fatal("resolved permissions did not arrive")
	}
}

// A coordinate the schema declares as sint64 has to survive as one. Narrowing
// it would be a silent truncation nothing in the build would catch.
func TestBlockPositionKeepsSixtyFourBits(t *testing.T) {
	const far = int64(1) << 40
	position, err := positionFrom(abi.List(abi.Int64(far), abi.Int64(-far), abi.Int64(far)))
	if err != nil {
		t.Fatal(err)
	}
	if position.X != far || position.Y != -far || position.Z != far {
		t.Fatalf("position = %+v", position)
	}
}

func TestEventFromRefusesWhatItCannotRead(t *testing.T) {
	if _, err := eventFrom(nil, &effects{}); err == nil {
		t.Fatal("a missing event decoded")
	}
	if _, err := eventFrom(&abi.Event{}, &effects{}); err == nil {
		t.Fatal("an event with no type decoded")
	}
	if _, err := eventFrom(&abi.Event{Type: EventPlayerJoin}, &effects{}); err == nil {
		t.Fatal("a payload of the wrong length decoded")
	}
}

// Anything that is not native is plugin-defined, and read positionally rather
// than refused. The name is carried through unresolved: which layout it should
// be read against belongs to the manifest of the plugin that provides it, not
// to this build.
func TestEventFromReadsAnUnknownTypePositionally(t *testing.T) {
	event, err := eventFrom(&abi.Event{
		Type:   "fr.oreo.shop/purchase",
		TypeID: 3,
		Fields: []abi.Value{abi.String("mika"), abi.Double(1500)},
	}, &effects{})
	if err != nil {
		t.Fatalf("eventFrom(a plugin-defined event, &effects{}) = %v", err)
	}
	custom, ok := event.(*CustomDispatch)
	if !ok {
		t.Fatalf("eventFrom returned %T, want *CustomDispatch", event)
	}
	if custom.Type() != "fr.oreo.shop/purchase" {
		t.Fatalf("Type() = %q", custom.Type())
	}
	if price, ok := custom.Decimal(1); !ok || price != 1500 {
		t.Fatalf("Decimal(1) = %v, %v", price, ok)
	}
}
