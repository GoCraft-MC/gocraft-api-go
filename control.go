package gocraft

import (
	"fmt"

	abi "github.com/GoCraft-MC/gocraft-abi/abi/v1"
)

// EventControl is what a handler answers one event with.
//
// It carries the verdict — whether the event may proceed — and hands out the
// handles a handler acts through. It deliberately carries no verb of its own:
// an effect belongs to the thing it happens to, so a message is
// player.SendMessage and not control.SendMessageTo. That is what keeps this
// interface the same size as the vocabulary grows.
//
// A handler that only watches never names it. One that refuses, or that answers
// somebody, takes it as its second parameter.
//
// One control serves every handler for one event, in priority order, so a later
// one sees what an earlier one decided. It is not thread-safe and does not need
// to be: they run one after another.
type EventControl interface {
	// Cancel prevents the action the event announced.
	//
	// The host decides the outcome once every subscriber has answered or the
	// budget has run out. It has the event definition and arbitrates, so
	// cancelling something declared uncancellable is logged there rather than
	// refused here.
	Cancel()

	// Cancelled reports whether this handler, or one before it, cancelled.
	Cancelled() bool

	// Player is a handle for acting on somebody the event did not hand over.
	//
	// A native event carries a *PlayerRef already bound to this dispatch, and a
	// handler uses that. A plugin-defined event carries whatever its author
	// declared — primitives, a string, a byte slice — so the player it is about
	// arrives as a bare uuid and becomes actionable here.
	//
	// Sixteen bytes. Anything else is a nil handle, and calling through one
	// says so rather than losing the effect quietly.
	Player(uuid []byte) *PlayerRef
}

// effects is the queue one dispatch fills and the verdict empties.
//
// A handle holds a pointer to it rather than to the event, so a handle outlives
// nothing: once the verdict is sent the queue is sealed, and acting through a
// handle kept past that point is an error rather than an effect nobody
// receives.
type effects struct {
	queue  []abi.HostCall
	sealed bool
}

func (e *effects) add(call abi.HostCall) error {
	if e == nil {
		return fmt.Errorf("gocraft: this handle belongs to no dispatch")
	}
	if e.sealed {
		return fmt.Errorf("gocraft: this handle belongs to a dispatch that has already been answered")
	}
	e.queue = append(e.queue, call)
	return nil
}

// control is the SDK's own EventControl, one per dispatched event.
type control struct {
	effects   effects
	cancelled bool
}

func (c *control) Cancel() { c.cancelled = true }

func (c *control) Cancelled() bool { return c.cancelled }

func (c *control) Player(uuid []byte) *PlayerRef {
	if len(uuid) != 16 {
		return nil
	}
	player := &PlayerRef{sink: &c.effects}
	copy(player.UUID[:], uuid)
	return player
}

// verdict seals the queue and reports what the handlers decided.
//
// Sealing here rather than leaving it open is what turns "a handler kept a
// handle and used it next tick" from an effect that silently goes nowhere into
// an error naming what happened.
func (c *control) verdict() abi.Verdict {
	c.effects.sealed = true
	return abi.Verdict{Cancelled: c.cancelled, Effects: c.effects.queue}
}
