package gocraft

import (
	"fmt"

	abi "github.com/GoCraft-MC/gocraft-abi/abi/v1"
)

func (s *runtimeState) dispatch(incoming *abi.Event) (abi.Verdict, error) {
	if !s.enabled || s.context == nil {
		return abi.Verdict{}, fmt.Errorf("gocraft: plugin is not enabled")
	}
	// The control exists before the event does, because decoding binds every
	// handle in the payload to it: a *PlayerRef a handler receives can already
	// be acted on, without being handed a channel to act through.
	answer := &control{}
	event, err := eventFrom(incoming, &answer.effects)
	if err != nil {
		return abi.Verdict{}, err
	}
	s.context.events.dispatch(event, answer)

	verdict := answer.verdict()
	// Both paths use the same positional Mutation transport.
	if custom, ok := event.(*CustomDispatch); ok {
		verdict.Mutations = custom.mutations
	} else {
		verdict.Mutations = nativeMutations(event, incoming.Fields)
		verdict.Cancelled = verdict.Cancelled && nativeCancellable(incoming.Type)
	}
	return verdict, nil
}
