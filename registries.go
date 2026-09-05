package gocraft

import (
	"context"
	"log/slog"
	"sync"
)

// Events owns listeners registered by one plugin, and publishes the events the
// plugin declares it provides.
type Events struct {
	mu        sync.RWMutex
	logger    *slog.Logger
	listeners map[string][]EventHandler
	active    bool

	// pluginID travels with every emission: the host skips this plugin's own
	// subscribers, so it has to know who is publishing.
	pluginID string
	// bindings is the id table the host sent with LOAD, name to id. Only this
	// plugin's own events are in it.
	bindings map[string]uint32
	// emitter is nil in a plugin that has no connection — a command dump, or a
	// test that builds a registry on its own.
	emitter *emitter
}

func newEvents(logger *slog.Logger, pluginID string, bindings map[string]uint32, publisher *emitter) *Events {
	return &Events{
		logger: logger, listeners: make(map[string][]EventHandler), active: true,
		pluginID: pluginID, bindings: bindings, emitter: publisher,
	}
}

// Commands owns command callbacks registered by one plugin.
//
// tree is what the bundle declared, so a handler is registered against the path
// an author wrote in their command tree rather than against the executor id
// that tree happened to assign it.
type Commands struct {
	mu       sync.RWMutex
	logger   *slog.Logger
	tree     *commandTree
	handlers map[uint32]CommandHandler
	active   bool
}

func newCommands(logger *slog.Logger, tree *commandTree) *Commands {
	if tree == nil {
		tree = &commandTree{executors: map[string]uint32{}}
	}
	return &Commands{logger: logger, tree: tree,
		handlers: make(map[uint32]CommandHandler), active: true}
}

// Scheduler owns asynchronous tasks registered by one plugin.
type Scheduler struct {
	mu      sync.Mutex
	logger  *slog.Logger
	next    TaskID
	active  bool
	cancels map[TaskID]context.CancelFunc
	wait    sync.WaitGroup
}

func newScheduler(logger *slog.Logger) *Scheduler {
	return &Scheduler{logger: logger, active: true, cancels: make(map[TaskID]context.CancelFunc)}
}
