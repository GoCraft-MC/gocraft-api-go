# gocraft-api-go

What a native Go plugin for [GoCraft](https://github.com/GoCraft-MC/GoCraft) is
written against.

```go
import gocraft "github.com/GoCraft-MC/gocraft-api-go"
```

It depends on the ABI and on nothing else. Not on the server: a plugin that had
to compile the thing it plugs into would be a cycle, and would tie every plugin
to the version of the server that happened to be checked out.

## What you have to write

Four things, and the build turns them into one `.gcpkg` file the server reads:

| | |
| --- | --- |
| `plugin.toml` | who you are, what you subscribe to, where your binary is |
| a type implementing `Plugin` | `OnLoad`, `OnEnable`, `OnDisable` |
| `Commands()`, if you have any | the shape and the handlers, in one statement |
| a `main` calling `gocraft.Run` | the plugin is its own program |

### plugin.toml

```toml
id      = "example-go"      # unique; namespace it if you publish
version = "1.0.0"
api     = 1                 # gocraft.CurrentVersion
runtime = "go"
entry   = "bin/example-go"  # your compiled binary, inside the bundle

[subscribe]
events = ["player.join", "block.break"]
perms  = ["example.greet"]  # permissions answered inside each event

[commands]
tree = "commands.pb"        # only if you declare commands
```

`[subscribe] events` is not decoration: the host sends you nothing you did not
ask for, so an event missing here is an event your handler never sees. And
`perms` is what makes `event.Can("example.greet")` a map lookup instead of a
round trip while the tick waits.

### Building it

```sh
go run . -gocraft-dump-commands .gocraft/commands.json   # if you have commands
go build -o bin/example-go .
go run github.com/GoCraft-MC/gocraft-cli@latest \
    build -commands .gocraft/commands.json -o my-plugin.gcpkg .
```

Drop `my-plugin.gcpkg` in the server's `plugins/` directory. The binary is
platform-specific, so build it for the server's OS and architecture —
`GOOS=linux GOARCH=amd64 go build …` if you are not on it.

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
	return nil
}

// Declared once. A build asks for this to put the shape in the bundle, and the
// host asks for it to bind the handlers — so a command with no handler, or a
// handler on no command, is not something that can be written.
func (p *plugin) Commands() *gocraft.CommandSet {
	set := gocraft.NewCommandSet()
	set.Command("greet").Permission("example.greet").Runs(func(call *gocraft.CommandContext) error {
		call.Reply("Hello, " + call.SenderName + "!")
		return nil
	})
	return set
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

## Commands

```sh
go run . -gocraft-dump-commands .gocraft/commands.json
gocraft-cli build -commands .gocraft/commands.json -o my-plugin.gcpkg .
```

A dot directory because `gocraft-cli` skips those when it packs, the way it
skips `.git`: the dump is a build artefact, not something the server reads.

`go run` rather than the binary you ship, so the dump still works when the
plugin is cross-compiled for a server that is not this machine.

The dump writes the same neutral file `gocraft-apt` writes from javac, and
`gocraft-cli` turns it into the `commands.pb` every runtime ships. One program
encodes the wire tree, however many ways there are to declare one — a plugin
never touches it.

Executor ids are minted by that one program, in declaration order. Nothing here
has an opinion about them, which is why handlers bind to paths (`shop sell
<price>`) rather than to numbers.

### The value vocabulary

| | what the client is asked for |
| --- | --- |
| `Integer(name, Min(…), Max(…))` | a whole number, optionally bounded |
| `Decimal(name, Min(…), Max(…))` | a number with a fractional part |
| `Text(name)` | one word |
| `Greedy(name)` | the rest of the line; nothing may follow it |
| `Player(name)` | a player, completed by the client |
| `BlockPos(name)` | a position, with the client's coordinate helpers |
| `BlockState(name)` | a block, completed from the edition's registry |
| `Item(name)` | an item, likewise |
| `Duration(name)` | `30s`, `5m`, `2h` |
| `OneOf(name, "a", "b")` | one of a fixed list, completed by the client |
| `Custom(name, "your/type")` | only you can complete it, so it costs a round trip |

Eleven types, and no twelfth. Every one of them renders on Java *and*
Bedrock without either edition inventing a widget the other lacks — which is why
adding one is a schema change rather than a plugin's decision.

A range on a type that has none does not compile: only `Integer` and `Decimal`
take bounds.

### What the builder refuses

Mistakes are collected while you declare and reported together, so a typo does
not hide the three problems after it. Binding one path twice, guarding a value
with a permission, declaring one name as two types, a command that runs nothing
— all of them fail your build, not the server's startup.

## Values

Everything crossing the boundary is positional. The wire carries no field names,
so a handler reads `field(0)`, not `field("player")` — which is why the schema
may add fields at the end and may never reorder them.
