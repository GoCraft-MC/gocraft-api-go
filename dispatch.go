package gocraft

import (
	"fmt"

	abi "github.com/GoCraft-MC/gocraft-abi/abi/v1"
)

func (s *runtimeState) dispatch(incoming *abi.Event) (abi.Verdict, error) {
	if !s.enabled || s.context == nil {
		return abi.Verdict{}, fmt.Errorf("gocraft: plugin is not enabled")
	}
	event, err := eventFrom(incoming)
	if err != nil {
		return abi.Verdict{}, err
	}
	s.context.events.dispatch(event)
	// A plugin-defined event answers for itself: it carries the writes its
	// handlers recorded as well as the cancellation, and a native one has no
	// mutations to report — nothing in the schema is writable yet.
	if custom, ok := event.(*CustomDispatch); ok {
		return custom.verdict(), nil
	}
	verdict := abi.Verdict{}
	if cancellable, ok := event.(CancellableEvent); ok {
		verdict.Cancelled = cancellable.Cancelled()
	}
	return verdict, nil
}
