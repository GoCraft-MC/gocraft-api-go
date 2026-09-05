package gocraft

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func testLogger(output *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(output, nil))
}

func TestEventsDispatchInOrderAndRecoverPanics(t *testing.T) {
	var output bytes.Buffer
	events := newEvents(testLogger(&output), "fr.oreo.hello", nil, nil)
	called := 0
	if err := events.OnBlockBreak(func(*BlockBreakEvent, EventControl) {
		called++
		panic("broken listener")
	}); err != nil {
		t.Fatal(err)
	}
	if err := events.OnBlockBreak(func(event *BlockBreakEvent, control EventControl) {
		called++
		control.Cancel()
	}); err != nil {
		t.Fatal(err)
	}

	event := &BlockBreakEvent{}
	answer := &control{}
	events.dispatch(event, answer)
	if called != 2 || !answer.Cancelled() {
		t.Fatalf("dispatch called %d listeners, cancelled=%v", called, answer.Cancelled())
	}
	if log := output.String(); !strings.Contains(log, "broken listener") || !strings.Contains(log, "stack") {
		t.Fatalf("panic log = %q", log)
	}
	if registered := events.registeredTypes(); len(registered) != 1 || registered[0] != EventBlockBreak {
		t.Fatalf("registeredTypes() = %v", registered)
	}
}

func TestEventsClearPreventsFutureCallbacks(t *testing.T) {
	events := newEvents(slog.Default(), "fr.oreo.hello", nil, nil)
	called := false
	if err := events.OnPlayerJoin(func(*PlayerJoinEvent, EventControl) { called = true }); err != nil {
		t.Fatal(err)
	}
	events.clear()
	events.dispatch(&PlayerJoinEvent{}, &control{})
	if called {
		t.Fatal("disabled registry invoked a listener")
	}
	if err := events.OnPlayerJoin(func(*PlayerJoinEvent, EventControl) {}); err == nil {
		t.Fatal("disabled registry accepted a listener")
	}
}
