package gocraft

// The contract every generated event satisfies.
//
// The events themselves live in events.gen.go, emitted from abi/v1/events.proto
// alongside the host emitters and the Java classes. What stays here is the part
// no schema describes: what it means to be an event at all.

// Event is anything the host can dispatch to a handler.
//
// It is the payload and nothing else. Refusing what it announced, and reaching
// the players it is about, are [EventControl] — the second parameter every
// handler is offered and only some take.
//
// That split is not a preference. A plugin-defined event is a struct its author
// wrote, and nothing can add a method to it, so a verdict carried on the event
// would work for native events and be impossible for the rest. One shape for
// both beats two that differ by who wrote the event.
type Event interface {
	Type() string
}

// EventHandler receives an event by name, for a type this build does not know.
type EventHandler func(Event, EventControl)
