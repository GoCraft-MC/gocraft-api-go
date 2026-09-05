// Package gocraft is the public API used by native Go plugins.
package gocraft

import (
	"fmt"
	"log/slog"

	abi "github.com/GoCraft-MC/gocraft-abi/abi/v1"
)

// CurrentVersion is the Go Plugin API version supported by this server.
const CurrentVersion uint32 = 1

// Metadata identifies a compiled plugin. The bundle manifest remains the
// host's authoritative copy and is checked before plugin code starts.
type Metadata struct {
	ID         string
	Version    string
	APIVersion uint32
}

// Plugin is the lifecycle implemented by a native Go plugin.
type Plugin interface {
	OnLoad(Context) error
	OnEnable() error
	OnDisable() error
}

// Context exposes resources owned by one plugin.
type Context interface {
	Metadata() Metadata
	DataDirectory() string
	Logger() *slog.Logger
	Events() *Events
	Commands() *Commands
	Scheduler() *Scheduler
}

// PlayerRef is a player, and what a handler acts on them through.
//
// Identity and verbs on one type, because an effect belongs to the thing it
// happens to. The alternative — a plain value plus a channel that takes it as
// an argument — puts every future verb on the channel, and the channel grows
// with the vocabulary instead of the vocabulary growing on its own.
//
// **Held by pointer, always.** It carries a link to the dispatch it came from,
// so comparing two of them with == compares that link as well: two references
// to the same player from two events would come out unequal, silently, and Go
// has no way to say otherwise. Compare UUID, which is what identity means here.
//
// Do not keep one. The fields are a snapshot the server has already moved past,
// and acting through a handle after its event was answered is an error rather
// than an effect nobody receives.
type PlayerRef struct {
	UUID     [16]byte
	Username string
	Edition  string

	// sink is the dispatch this handle can act through, nil for one decoded
	// outside a dispatch.
	sink *effects
}

// Value is this player as an event carries one: uuid, username, edition.
//
// Here rather than written out by whoever needs it: the shape has to match what
// the host reads back, and code generators were spelling it out themselves. The
// Java handle answers the same question the same way.
//
// A nil handle is an empty list, which is what the host writes when there is no
// acting player — the wire has no null and a fixed layout would have to
// special-case one anyway.
func (p *PlayerRef) Value() Value {
	if p == nil {
		return List()
	}
	return List(Bytes(p.UUID[:]), String(p.Username), String(p.Edition))
}

// SendMessage delivers one line to this player.
//
// Batched into the verdict with every other effect, so a handler that sends
// three lines still costs one round trip, and applied by the host on its own
// tick — never from the goroutine a handler runs on, which is in another
// process from the world it would be writing to.
//
// A player who logged out between the event and that tick is dropped without a
// word, which is common enough not to be worth reporting.
func (p *PlayerRef) SendMessage(message string) error {
	if p == nil {
		return fmt.Errorf("gocraft: no player to send %q to", message)
	}
	return p.sink.add(abi.HostCall{
		Type:   EffectMessage,
		Fields: []Value{Bytes(p.UUID[:]), String(message)},
	})
}

// BlockPos is an integer position in a world.
type BlockPos struct {
	X int64
	Y int64
	Z int64
}

// Block is a namespaced block state snapshot.
type Block struct {
	ID         string
	Properties map[string]string
}
