# gocraft-api-go

What a native Go plugin for [GoCraft](https://github.com/GoCraft-MC/GoCraft) is
written against.

```go
import gocraft "github.com/GoCraft-MC/gocraft-api-go"
```

It depends on the ABI and on nothing else. Not on the server: a plugin that had
to compile the thing it plugs into would be a cycle, and would tie every plugin
to the version of the server that happened to be checked out.

## What a plugin looks like

A plugin is its own program. The host starts it, hands it a socket, and speaks
the ABI over it; there is no shared address space and no way for a panic here to
take the server down.

```go
type plugin struct{ context gocraft.Context }

func (p *plugin) OnLoad(context gocraft.Context) error {
	p.context = context
	if err := context.Events().OnPlayerJoin(func(event *gocraft.PlayerJoinEvent) {
		context.Logger().Info("player joined", "player", event.Player.Username)
	}); err != nil {
		return err
	}
	// The path, as commands.pb spells it. The executor id the tree assigns is
	// the tree's business, not this file's.
	return context.Commands().Register("greet", func(call *gocraft.CommandContext) error {
		call.Reply("Hello, " + call.SenderName + "!")
		return nil
	})
}

func (p *plugin) OnEnable() error  { return nil }
func (p *plugin) OnDisable() error { return nil }

func main() {
	metadata := gocraft.Metadata{
		ID: "example", Version: "1.0.0", APIVersion: gocraft.CurrentVersion,
	}
	if err := gocraft.Run(metadata, &plugin{}); err != nil {
		slog.Error("plugin stopped", "err", err)
		os.Exit(1)
	}
}
```

## Events

`events.gen.go` is generated from `abi/v1/events.proto` by `protoc-gen-gocraft`,
which lives in the GoCraft repository and emits the host side and this side from
the same schema in one command. That is the point: the two used to be written
separately, and the SDK drifted — a field the host wrote as a 64-bit position
was read back here as a 32-bit one, and nothing failed until the coordinate was
large enough to matter.

Do not edit it. A hand-written event is a second definition of the wire format.

## Values

Everything crossing the boundary is positional. The wire carries no field names,
so a handler reads `field(0)`, not `field("player")` — which is why the schema
may add fields at the end and may never reorder them.
