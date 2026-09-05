package gocraft

import (
	"fmt"

	abi "github.com/GoCraft-MC/gocraft-abi/abi/v1"
)

// invokeCommand runs one executor and reports what it asked for.
//
// The replies become chat.message effects rather than a result message of their
// own. The host queues them and delivers them on the tick, which is the one
// path a plugin has to the world — a command handler writing to a player
// directly would race the simulation reading it, exactly as an event handler
// would.
func (s *runtimeState) invokeCommand(invocation abi.CommandInvocation) abi.CommandResult {
	if !s.enabled || s.context == nil {
		return abi.CommandResult{Error: "gocraft: plugin is not enabled"}
	}
	// The sender's handle is bound to this invocation, so call.Sender.SendMessage
	// reaches them the same way an event handler reaches a player. Without it
	// the method would compile on a command sender and fail at runtime, which
	// is the shape of trap this SDK exists to remove.
	answer := &effects{}
	call, err := commandContextFrom(invocation, answer)
	if err != nil {
		return abi.CommandResult{Error: err.Error()}
	}
	replies, callErr := s.context.commands.invoke(invocation.Executor, call)
	answer.sealed = true
	result := abi.CommandResult{}
	for _, reply := range replies {
		result.Effects = append(result.Effects, abi.HostCall{
			Type:   abi.EffectMessage,
			Fields: []abi.Value{invocation.Sender.Player, abi.String(reply)},
		})
	}
	result.Effects = append(result.Effects, answer.queue...)
	// The replies travel even when the handler failed: one that refused and
	// said why has already queued that sentence, and dropping it would leave
	// the sender with nothing but a generic failure.
	if callErr != nil {
		result.Error = callErr.Error()
	}
	return result
}

func commandContextFrom(invocation abi.CommandInvocation, sink *effects) (*CommandContext, error) {
	var sender *PlayerRef
	if invocation.Sender.Player.Kind != abi.ValueList || len(invocation.Sender.Player.List) != 0 {
		decoded, err := playerFrom(invocation.Sender.Player, sink)
		if err != nil {
			return nil, err
		}
		sender = decoded
	}
	arguments, err := commandValuesFrom(invocation.Arguments, sink)
	if err != nil {
		return nil, err
	}
	return &CommandContext{
		Sender:      sender,
		SenderName:  invocation.Sender.Name,
		Args:        arguments,
		permissions: invocation.Sender,
	}, nil
}

func commandValuesFrom(arguments []abi.CommandArgument, sink *effects) (CommandValues, error) {
	values := make(CommandValues, len(arguments))
	for _, argument := range arguments {
		if _, duplicate := values[argument.Name]; duplicate {
			return nil, fmt.Errorf("gocraft: duplicate command argument %s", argument.Name)
		}
		decoded, err := commandValueFrom(CommandValueKind(argument.Type), argument.Value, sink)
		if err != nil {
			return nil, fmt.Errorf("gocraft: command argument %s: %w", argument.Name, err)
		}
		values[argument.Name] = decoded
	}
	return values, nil
}
