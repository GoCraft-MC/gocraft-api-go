package gocraft

import (
	"testing"

	abi "github.com/GoCraft-MC/gocraft-abi/abi/v1"
)

func TestNativeMutationAndCancellationRoundTrip(t *testing.T) {
	state := newRuntimeState(Metadata{ID: "events"}, &eventPlugin{})
	if _, err := state.load(loadRequest{pluginID: "events", dataDirectory: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.disable() })
	calls := 0
	if err := state.context.events.OnPlayerChat(func(event *PlayerChatEvent, control EventControl) {
		calls++
		event.Message = "rewritten"
		event.Player.Username = "not returned"
		control.Cancel()
	}); err != nil {
		t.Fatal(err)
	}
	if err := state.context.events.OnPlayerChat(func(event *PlayerChatEvent, control EventControl) {
		calls++
		if event.Message != "rewritten" || !control.Cancelled() {
			t.Fatal("handlers did not share state")
		}
	}); err != nil {
		t.Fatal(err)
	}
	fields := []abi.Value{abi.List(abi.Bytes(make([]byte, 16)), abi.String("Alex"), abi.String("java")), abi.String("original"), abi.List()}
	verdict, err := state.dispatch(&abi.Event{Type: EventPlayerChat, Fields: fields})
	if err != nil || !verdict.Cancelled || calls != 2 || len(verdict.Mutations) != 1 {
		t.Fatalf("verdict=%+v calls=%d err=%v", verdict, calls, err)
	}
	after, err := abi.ApplyPath(fields, verdict.Mutations[0])
	if err != nil || after[1].String != "rewritten" || after[0].List[1].String != "Alex" {
		t.Fatalf("round trip = %+v, %v", after, err)
	}
}

func TestObservationalEventsCannotReturnCancellation(t *testing.T) {
	state := newRuntimeState(Metadata{ID: "events"}, &eventPlugin{})
	if _, err := state.load(loadRequest{pluginID: "events", dataDirectory: t.TempDir()}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { state.disable() })
	// The dynamic fallback cannot defeat the typed observational API.
	if err := state.context.events.On(EventPlayerJoin, func(_ Event, control EventControl) { control.Cancel() }); err != nil {
		t.Fatal(err)
	}
	fields := []abi.Value{abi.List(abi.Bytes(make([]byte, 16)), abi.String("Alex"), abi.String("java")), abi.List()}
	verdict, err := state.dispatch(&abi.Event{Type: EventPlayerJoin, Fields: fields})
	if err != nil || verdict.Cancelled {
		t.Fatalf("observational verdict = %+v, %v", verdict, err)
	}
	if err := state.context.events.OnPlayerChat(nil); err == nil {
		t.Fatal("typed nil handler accepted")
	}
}
