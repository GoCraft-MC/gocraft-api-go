package gocraft

import (
	"fmt"
	"sync"
	"sync/atomic"

	abi "github.com/GoCraft-MC/gocraft-abi/abi/v1"
	wire "github.com/GoCraft-MC/gocraft-abi/abi/v1/wire"
	"github.com/GoCraft-MC/gocraft-abi/ipc"
)

// CustomEvent is a plugin-defined event this plugin can publish.
//
// The author writes an ordinary struct and three small methods. There is no
// generated superclass and no annotation: what the host needs is the layout,
// and the layout is already in the manifest under [[events.provides]].
//
// Fields and SetFields are the same list in the same order, which is the order
// the manifest declares. That order is the contract — appending a field is
// safe, reordering one shifts every index for everyone who compiled against the
// previous version.
type CustomEvent interface {
	// EventType is the namespaced name, exactly as [[events.provides]] spells
	// it: "fr.oreo.shop/purchase".
	EventType() string

	// Fields writes this event's values, in declaration order.
	Fields() []Value

	// SetFields receives them back once every subscriber has run, in the same
	// order, with whatever they changed already applied.
	//
	// This is what makes a cross-process, cross-language dispatch feel
	// in-process: the object the author emitted is the object they read
	// afterwards. A field nobody touched arrives unchanged.
	SetFields([]Value) error
}

// Emit publishes a plugin-defined event and blocks until its subscribers have
// run, reporting whether the event may proceed.
//
// False means a subscriber cancelled it, or that a fail-closed event lost one.
// The caller is expected to abandon whatever it was about to do; the host has
// applied nothing on its behalf.
//
// The event's own object is updated before this returns, so a caller reads the
// price a discount plugin set by reading the field it emitted.
func (e *Events) Emit(event CustomEvent) (bool, error) {
	if event == nil {
		return false, fmt.Errorf("gocraft: no event to emit")
	}
	eventType := event.EventType()
	e.mu.RLock()
	typeID, bound := e.bindings[eventType]
	emitter, active := e.emitter, e.active
	e.mu.RUnlock()
	if !active {
		return false, fmt.Errorf("gocraft: plugin is not enabled")
	}
	if !bound {
		// The host builds the table from the manifests it scanned, so an
		// unbound name means this plugin's own manifest never declared the
		// event — not that the host lost it.
		return false, fmt.Errorf("gocraft: %s is not declared in [[events.provides]]", eventType)
	}
	if emitter == nil {
		return false, fmt.Errorf("gocraft: no connection to the host")
	}
	fields := event.Fields()
	result, err := emitter.emit(e.pluginID, typeID, fields)
	if err != nil {
		return false, err
	}
	if result.Error != "" {
		return false, fmt.Errorf("gocraft: host refused %s: %s", eventType, result.Error)
	}
	if err := replay(event, fields, result.Mutations); err != nil {
		// The dispatch happened; only the author's copy is stale. Reporting it
		// as an error rather than swallowing it means they find out now, not
		// when a later read returns the price nobody discounted.
		return !result.Cancelled, err
	}
	return !result.Cancelled, nil
}

// replay applies what the subscribers changed to the values the author emitted,
// then hands them back.
//
// Applied here rather than by the host: the host answered with the mutations
// precisely so the emitter's own object can be updated without shipping the
// whole event back.
func replay(event CustomEvent, fields []Value, mutations []abi.Mutation) error {
	if len(mutations) == 0 {
		return nil
	}
	updated := fields
	for _, mutation := range mutations {
		applied, err := abi.ApplyPath(updated, mutation)
		if err != nil {
			return fmt.Errorf("gocraft: replay %s: %w", event.EventType(), err)
		}
		updated = applied
	}
	return event.SetFields(updated)
}

// emitter is the plugin's half of the split sequence space.
//
// The host numbers the exchanges it starts odd; this numbers the one a plugin
// starts even. Two counters over one space would eventually put the same number
// on a request in flight in each direction, and each side would answer the
// other's question.
type emitter struct {
	codec *ipc.Codec
	seq   atomic.Uint64

	mu      sync.Mutex
	pending map[uint64]chan *wire.Emitted
	closed  bool
}

func newEmitter(codec *ipc.Codec) *emitter {
	return &emitter{codec: codec, pending: make(map[uint64]chan *wire.Emitted)}
}

func (e *emitter) emit(pluginID string, typeID uint32, fields []Value) (abi.EmissionResult, error) {
	request, err := ipc.EncodeEmission(abi.Emission{
		PluginID: pluginID, TypeID: typeID, Fields: fields,
	})
	if err != nil {
		return abi.EmissionResult{}, err
	}
	// Even, from 2. Zero stays "correlated with nothing".
	seq := e.seq.Add(2)
	replies, err := e.expect(seq)
	if err != nil {
		return abi.EmissionResult{}, err
	}
	defer e.forget(seq)

	if err := e.codec.Send(&wire.Envelope{Seq: seq, Body: &wire.Envelope_Emit{Emit: request}}); err != nil {
		return abi.EmissionResult{}, fmt.Errorf("gocraft: emit: %w", err)
	}
	// No timeout of its own. The host bounds the dispatch with the shared event
	// budget and always answers; if it dies instead, the read loop closes this
	// channel rather than leaving the caller parked forever.
	emitted, ok := <-replies
	if !ok {
		return abi.EmissionResult{}, fmt.Errorf("gocraft: host closed while emitting")
	}
	return ipc.DecodeEmissionResult(emitted)
}

// deliver wakes whoever sent the emission this answers. It runs on the read
// loop, so it never blocks: the channel is buffered and the entry is removed on
// delivery, which also means a host answering twice cannot have its second
// answer taken for another emission's.
func (e *emitter) deliver(seq uint64, emitted *wire.Emitted) {
	e.mu.Lock()
	replies, waiting := e.pending[seq]
	if waiting {
		delete(e.pending, seq)
	}
	e.mu.Unlock()
	if waiting {
		replies <- emitted
	}
}

// shutdown wakes every caller still waiting, so a plugin blocked on an answer
// that will never come stops rather than holding the process open.
func (e *emitter) shutdown() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return
	}
	e.closed = true
	for seq, replies := range e.pending {
		close(replies)
		delete(e.pending, seq)
	}
}

func (e *emitter) expect(seq uint64) (chan *wire.Emitted, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.closed {
		return nil, fmt.Errorf("gocraft: host closed while emitting")
	}
	replies := make(chan *wire.Emitted, 1)
	e.pending[seq] = replies
	return replies, nil
}

func (e *emitter) forget(seq uint64) {
	e.mu.Lock()
	delete(e.pending, seq)
	e.mu.Unlock()
}
