package gocraft

import (
	"fmt"

	abi "github.com/GoCraft-MC/gocraft-abi/abi/v1"
)

// CustomDispatch is a plugin-defined event this plugin subscribed to.
//
// The emitting side works with a struct, because the emitter owns the type and
// wrote it. A subscriber does not: it knows the layout the provider's manifest
// declares and nothing more, and the index is what the manifest guarantees. So
// this reads positionally, and says so, rather than pretending to a type it
// would be decoding on faith.
//
// Writes are recorded rather than diffed. A subscriber changes at most a field
// or two out of an event that may carry a list of a thousand records; asking
// what changed by comparing the whole payload afterwards would pay for the
// whole event on every dispatch, and lose the deep-path granularity that lets
// a list element be written without replacing the list.
//
// Into reads the payload into a struct for an author who would rather have
// one. It is deliberately read-only: a struct the SDK handed out has no way to
// tell it was written to, and the mutation is what the host applies.
type CustomDispatch struct {
	eventType string
	fields    []Value
	mutations []abi.Mutation

	// sink is the dispatch a handle read out of this payload acts through. A
	// plugin-defined event may carry a PlayerRef, and a subscriber handed one
	// it cannot answer would be handed an identity rather than a handle.
	sink *effects
}

// customFrom reads a dispatched event this build has no generated layout for.
//
// Called by the generated eventFrom for anything that is not native. The type
// is carried rather than resolved: the host numbered it from the manifests it
// scanned, and a subscriber matches on the name it subscribed with.
func customFrom(incoming *abi.Event, sink *effects) (Event, error) {
	if incoming.Type == "" {
		return nil, fmt.Errorf("gocraft: dispatched event has no type")
	}
	return &CustomDispatch{eventType: incoming.Type, fields: incoming.Fields, sink: sink}, nil
}

// Type is the namespaced name, as [[events.provides]] spells it.
func (e *CustomDispatch) Type() string { return e.eventType }

// Len is how many fields arrived, which is how many the provider declared.
func (e *CustomDispatch) Len() int { return len(e.fields) }

// Field reads one value, with whatever earlier subscribers changed applied.
func (e *CustomDispatch) Field(index int) (Value, bool) {
	if index < 0 || index >= len(e.fields) {
		return Value{}, false
	}
	return e.fields[index], true
}

// Bool, Int, Decimal, Text and Bytes read a field of that kind.
//
// The second result is false for an index that does not exist and for a field
// that holds another kind. One answer for both, because a subscriber can do
// nothing useful with the difference: either way it compiled against a layout
// that is not the one being sent.
func (e *CustomDispatch) Bool(index int) (bool, bool) {
	value, ok := e.Field(index)
	if !ok || value.Kind != abi.ValueBool {
		return false, false
	}
	return value.Bool, true
}

func (e *CustomDispatch) Int(index int) (int64, bool) {
	value, ok := e.Field(index)
	if !ok || value.Kind != abi.ValueInt64 {
		return 0, false
	}
	return value.Int64, true
}

func (e *CustomDispatch) Decimal(index int) (float64, bool) {
	value, ok := e.Field(index)
	if !ok || value.Kind != abi.ValueDouble {
		return 0, false
	}
	return value.Double, true
}

func (e *CustomDispatch) Text(index int) (string, bool) {
	value, ok := e.Field(index)
	if !ok || value.Kind != abi.ValueString {
		return "", false
	}
	return value.String, true
}

func (e *CustomDispatch) Bytes(index int) ([]byte, bool) {
	value, ok := e.Field(index)
	if !ok || value.Kind != abi.ValueBytes {
		return nil, false
	}
	return value.Bytes, true
}

// Player reads a field the provider declared as a PlayerRef, bound to this
// dispatch.
//
// The point of PlayerRef being in the manifest vocabulary rather than being
// sixteen bytes: a subscriber is handed somebody it can answer. Generated code
// reads a field this way; by hand it is the same call.
func (e *CustomDispatch) Player(index int) (*PlayerRef, bool) {
	value, ok := e.Field(index)
	if !ok {
		return nil, false
	}
	player, err := playerFrom(value, e.sink)
	if err != nil {
		return nil, false
	}
	return player, true
}

// Set replaces a whole field.
//
// Whether the provider allows it is the host's answer, not this one: the
// manifest says which fields are mutable and only the host has read it. A write
// it refuses is logged against this plugin and dropped, so the event carries on
// rather than being cancelled by a subscriber writing where it may not.
func (e *CustomDispatch) Set(index int, value Value) error {
	if index < 0 {
		return fmt.Errorf("gocraft: %s: negative field index %d", e.eventType, index)
	}
	return e.SetAt([]uint32{uint32(index)}, value)
}

// SetAt replaces a value at a positional path, which is how an element inside a
// field is written without replacing the field.
//
// A list of records declared immutable is the ordinary case, not an exotic one:
// nobody may swap the list, and its elements are still the subscriber's to
// change. The host answers a path of length one from the field's own
// mutability and anything deeper from the field existing at all.
func (e *CustomDispatch) SetAt(path []uint32, value Value) error {
	if len(path) == 0 {
		return fmt.Errorf("gocraft: %s: a mutation needs a path", e.eventType)
	}
	mutation := abi.Mutation{Path: append([]uint32(nil), path...), Value: value}
	updated, err := abi.ApplyPath(e.fields, mutation)
	if err != nil {
		// Applied locally before it is recorded, so a handler that writes and
		// then reads sees its own write, and so a path this payload cannot
		// take is refused here rather than at the far end where the plugin
		// that wrote it is only a name in a log line.
		return fmt.Errorf("gocraft: %s: %w", e.eventType, err)
	}
	e.fields = updated
	e.mutations = append(e.mutations, mutation)
	return nil
}

// Into reads the payload into a struct, using the same SetFields an emitter
// implements. Read-only: writes go through Set and SetAt.
func (e *CustomDispatch) Into(target CustomEvent) error {
	if target == nil {
		return fmt.Errorf("gocraft: %s: no target to read into", e.eventType)
	}
	if declared := target.EventType(); declared != e.eventType {
		return fmt.Errorf("gocraft: %s cannot be read into %s", e.eventType, declared)
	}
	return target.SetFields(e.fields)
}

// EffectMessage is the only host call a plugin can make today.
//
// Re-exported from the contract, like Value, so an author never imports it and
// the string a subscriber asks with is the string the host dispatches on.
const EffectMessage = abi.EffectMessage

// OnCustom registers a handler for a plugin-defined event.
//
// The type must be one this plugin's manifest subscribes to. It does not have
// to be one it provides — subscribing to another plugin's event is the point —
// but the host refuses at boot a subscription to a type no scanned manifest
// provides, so a typo is a startup failure rather than a handler that never
// fires.
func (e *Events) OnCustom(eventType string, handler func(*CustomDispatch, EventControl)) error {
	if handler == nil {
		return fmt.Errorf("gocraft: %s needs a handler", eventType)
	}
	return e.On(eventType, func(event Event, answer EventControl) {
		custom, ok := event.(*CustomDispatch)
		if !ok {
			// A native event registered under a plugin-defined name, which the
			// manifest cannot express: native names have no slash. Reaching
			// here means the generated router changed shape.
			e.logger.Error("plugin event is not a custom dispatch",
				"event", event.Type())
			return
		}
		handler(custom, answer)
	})
}
