package gocraft

import (
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	abi "github.com/GoCraft-MC/gocraft-abi/abi/v1"
	wire "github.com/GoCraft-MC/gocraft-abi/abi/v1/wire"
	"github.com/GoCraft-MC/gocraft-abi/ipc"
)

// purchase is what a plugin author writes: an ordinary struct plus the three
// methods that say what its fields are.
type purchase struct {
	Player string
	Tiers  []float64
	Price  float64
}

func (*purchase) EventType() string { return "fr.oreo.shop/purchase" }

func (p *purchase) Fields() []Value {
	tiers := make([]Value, 0, len(p.Tiers))
	for _, tier := range p.Tiers {
		tiers = append(tiers, Double(tier))
	}
	return []Value{String(p.Player), List(tiers...), Double(p.Price)}
}

func (p *purchase) SetFields(fields []Value) error {
	p.Player = fields[0].String
	p.Tiers = p.Tiers[:0]
	for _, tier := range fields[1].List {
		p.Tiers = append(p.Tiers, tier.Double)
	}
	p.Price = fields[2].Double
	return nil
}

// fakeHost answers every emission with answer, and records what it received.
type fakeHost struct {
	mu       sync.Mutex
	received []*wire.Emit
	seqs     []uint64
}

func hosted(t *testing.T, answer func(*wire.Emit) *wire.Emitted) (*Events, *fakeHost) {
	t.Helper()
	pluginSide, hostSide := net.Pipe()
	t.Cleanup(func() { pluginSide.Close(); hostSide.Close() })

	// One codec per side. Two on the same stream would each buffer their own
	// reads and take turns losing frames.
	pluginCodec := ipc.NewCodec(pluginSide)
	publisher := newEmitter(pluginCodec)
	host := &fakeHost{}
	hostCodec := ipc.NewCodec(hostSide)
	go func() {
		for {
			envelope, err := hostCodec.Receive()
			if err != nil {
				return
			}
			emit := envelope.GetEmit()
			if emit == nil {
				continue
			}
			host.mu.Lock()
			host.received = append(host.received, emit)
			host.seqs = append(host.seqs, envelope.GetSeq())
			host.mu.Unlock()
			hostCodec.Send(&wire.Envelope{Seq: envelope.GetSeq(),
				Body: &wire.Envelope_Emitted{Emitted: answer(emit)}})
		}
	}()
	// The read loop the real session runs, reduced to the one frame this needs.
	go func() {
		for {
			envelope, err := pluginCodec.Receive()
			if err != nil {
				publisher.shutdown()
				return
			}
			if emitted := envelope.GetEmitted(); emitted != nil {
				publisher.deliver(envelope.GetSeq(), emitted)
			}
		}
	}()
	events := newEvents(testLogger(nil), "fr.oreo.shop",
		map[string]uint32{"fr.oreo.shop/purchase": 7}, publisher)
	return events, host
}

func aPurchase() *purchase {
	return &purchase{Player: "oreo", Tiers: []float64{19.99, 4.50}, Price: 1500}
}

func TestEmitCarriesTheDeclaredLayout(t *testing.T) {
	events, host := hosted(t, func(*wire.Emit) *wire.Emitted { return &wire.Emitted{} })

	allowed, err := events.Emit(aPurchase())
	if err != nil {
		t.Fatal(err)
	}
	if !allowed {
		t.Fatal("Emit() reported a cancellation nobody made")
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	if len(host.received) != 1 {
		t.Fatalf("the host received %d emissions", len(host.received))
	}
	emission := host.received[0]
	if emission.GetPluginId() != "fr.oreo.shop" || emission.GetTypeId() != 7 {
		t.Fatalf("the host received %+v", emission)
	}
	if len(emission.GetFields()) != 3 {
		t.Fatalf("the host received %d fields, want the three declared", len(emission.GetFields()))
	}
}

// The plugin numbers the one exchange it starts even, so it cannot collide with
// a host request in flight.
func TestEmitNumbersItsRequestsEven(t *testing.T) {
	events, host := hosted(t, func(*wire.Emit) *wire.Emitted { return &wire.Emitted{} })

	for range 3 {
		if _, err := events.Emit(aPurchase()); err != nil {
			t.Fatal(err)
		}
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	for _, seq := range host.seqs {
		if seq == 0 || seq%2 != 0 {
			t.Fatalf("Emit() used seq %d, want an even number above zero", seq)
		}
	}
}

// §10's promise: the emitter's own object reflects what the subscribers did,
// once emit returns.
func TestEmitReplaysMutationsIntoTheAuthorsObject(t *testing.T) {
	events, _ := hosted(t, func(*wire.Emit) *wire.Emitted {
		return &wire.Emitted{Mutations: []*wire.Mutation{
			{Path: []uint32{2}, Value: &wire.Value{Kind: &wire.Value_DoubleValue{DoubleValue: 1200}}},
			{Path: []uint32{1, 0}, Value: &wire.Value{Kind: &wire.Value_DoubleValue{DoubleValue: 15.99}}},
		}}
	})

	event := aPurchase()
	if _, err := events.Emit(event); err != nil {
		t.Fatal(err)
	}
	if event.Price != 1200 {
		t.Fatalf("the price came back as %v, want the subscriber's 1200", event.Price)
	}
	if event.Tiers[0] != 15.99 {
		t.Fatalf("the nested tier came back as %v", event.Tiers[0])
	}
	if event.Tiers[1] != 4.50 {
		t.Fatalf("a tier nobody touched changed to %v", event.Tiers[1])
	}
	if event.Player != "oreo" {
		t.Fatalf("a read-only field changed to %q", event.Player)
	}
}

func TestEmitReportsACancellation(t *testing.T) {
	events, _ := hosted(t, func(*wire.Emit) *wire.Emitted {
		return &wire.Emitted{Cancelled: true}
	})

	allowed, err := events.Emit(aPurchase())
	if err != nil {
		t.Fatal(err)
	}
	if allowed {
		t.Fatal("Emit() let a cancelled event through")
	}
}

func TestEmitReportsWhatTheHostRefused(t *testing.T) {
	events, _ := hosted(t, func(*wire.Emit) *wire.Emitted {
		return &wire.Emitted{Error: "unknown event type id 7"}
	})

	allowed, err := events.Emit(aPurchase())
	if err == nil {
		t.Fatal("Emit() hid the host's refusal")
	}
	if !strings.Contains(err.Error(), "unknown event type id 7") {
		t.Fatalf("Emit() error = %v, want the host's own words", err)
	}
	if allowed {
		t.Fatal("Emit() reported a refused event as allowed")
	}
}

// An event the manifest never declared has no id, and the message says which
// file to fix rather than blaming the host.
func TestEmitRefusesAnUndeclaredEvent(t *testing.T) {
	events := newEvents(testLogger(nil), "fr.oreo.shop", nil, nil)
	_, err := events.Emit(aPurchase())
	if err == nil {
		t.Fatal("Emit() accepted an event the manifest never declared")
	}
	if !strings.Contains(err.Error(), "events.provides") {
		t.Fatalf("Emit() error = %v, want it to name the manifest section", err)
	}
}

func TestEmitRefusesAfterTheRegistryIsCleared(t *testing.T) {
	events, _ := hosted(t, func(*wire.Emit) *wire.Emitted { return &wire.Emitted{} })
	events.clear()
	if _, err := events.Emit(aPurchase()); err == nil {
		t.Fatal("Emit() published from a disabled plugin")
	}
}

// A plugin parked on an answer that will never come would hold the process open
// after the host died.
func TestEmitWakesWhenTheHostGoesAway(t *testing.T) {
	pluginSide, hostSide := net.Pipe()
	t.Cleanup(func() { pluginSide.Close() })
	publisher := newEmitter(ipc.NewCodec(pluginSide))
	events := newEvents(testLogger(nil), "fr.oreo.shop",
		map[string]uint32{"fr.oreo.shop/purchase": 7}, publisher)

	go func() {
		codec := ipc.NewCodec(hostSide)
		codec.Receive() // take the emission, answer nothing
		hostSide.Close()
		publisher.shutdown()
	}()

	done := make(chan error, 1)
	go func() {
		_, err := events.Emit(aPurchase())
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Emit() succeeded without an answer")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Emit() never returned after the host went away")
	}
}

func TestEventBindingsDropAnUnusableTable(t *testing.T) {
	// Zero is the native type id: a plugin-defined event bound to it could not
	// be told apart from a native one.
	if table := eventBindings([]*wire.EventBinding{{TypeId: 0, Type: "fr.oreo.shop/purchase"}}); table != nil {
		t.Fatalf("eventBindings() = %+v, want the table refused", table)
	}
	table := eventBindings([]*wire.EventBinding{{TypeId: 3, Type: "fr.oreo.shop/purchase"}})
	if table["fr.oreo.shop/purchase"] != 3 {
		t.Fatalf("eventBindings() = %+v", table)
	}
}

func TestReplayReportsAMutationItCannotApply(t *testing.T) {
	event := aPurchase()
	err := replay(event, event.Fields(), []abi.Mutation{{Path: []uint32{9}, Value: Double(1)}})
	if err == nil {
		t.Fatal("replay() accepted a path past the declared layout")
	}
}
