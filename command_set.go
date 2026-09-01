package gocraft

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/GoCraft-MC/gocraft-abi/command"
)

// Declaring a plugin's commands, once, as code.
//
// A command has two halves that must agree: a shape the host renders into a
// client's autocomplete, and a function that runs when someone reaches the end
// of it. Written separately they agree on the day they are written. Written
// here they are the same statement:
//
//	set := gocraft.NewCommandSet()
//	shop := set.Command("shop").Permission("shop.use")
//	shop.Sub("sell").Decimal("price", gocraft.Min(0.01)).Runs(sell)
//	shop.Sub("admin", "reload").Permission("shop.admin").Runs(reload)
//
// The build asks the plugin for the shape and the host asks it for the
// functions, so a path with no handler and a handler on no path both stop being
// possible to write rather than being caught later.
//
// Nothing here touches the wire format. The set hands over the same neutral
// file gocraft-apt hands over from javac, and gocraft-cli turns that into the
// commands.pb both runtimes ship — one writer for the wire tree, however many
// ways there are to declare one.

// CommandSet is a plugin's commands: their shape and what runs them.
type CommandSet struct {
	roots    []*commandNode
	handlers map[string]CommandHandler
	failures []error
}

// NewCommandSet starts an empty declaration.
func NewCommandSet() *CommandSet {
	return &CommandSet{handlers: make(map[string]CommandHandler)}
}

// Command begins a top-level command.
func (s *CommandSet) Command(name string) *CommandBuilder {
	node := s.child(&s.roots, name, false)
	return &CommandBuilder{set: s, node: node, path: []string{name}}
}

// Tree is the shape, validated.
//
// Every mistake made while declaring is reported here rather than at the call
// that made it: a builder that returned an error per step would bury the
// declaration it exists to keep readable.
func (s *CommandSet) Tree() (command.Root, error) {
	if err := errors.Join(s.failures...); err != nil {
		return command.Root{}, err
	}
	root := command.Root{}
	for _, node := range s.roots {
		root.Children = append(root.Children, node.convert())
	}
	if err := command.Validate(&root); err != nil {
		return command.Root{}, err
	}
	return root, nil
}

// Intermediate is what a build reads: the same neutral form every other runtime
// hands over, carrying no executor ids because those are the build's to mint.
func (s *CommandSet) Intermediate() ([]byte, error) {
	root, err := s.Tree()
	if err != nil {
		return nil, err
	}
	return command.EncodeIntermediate(root)
}

// Paths lists what the set binds, in a stable order.
func (s *CommandSet) Paths() []string {
	paths := make([]string, 0, len(s.handlers))
	for path := range s.handlers {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func (s *CommandSet) fail(format string, args ...any) {
	s.failures = append(s.failures, fmt.Errorf("gocraft: "+format, args...))
}

// child finds or creates a node, so two paths through one command are two paths
// and not two commands.
func (s *CommandSet) child(into *[]*commandNode, name string, argument bool) *commandNode {
	for _, existing := range *into {
		if existing.name != name {
			continue
		}
		if existing.argument != argument {
			s.fail("%q is declared as both a name and a value", name)
		}
		return existing
	}
	node := &commandNode{name: name, argument: argument}
	*into = append(*into, node)
	return node
}

// CommandBuilder points at one node of a set being declared.
type CommandBuilder struct {
	set  *CommandSet
	node *commandNode
	path []string
}

// Permission guards this node and everything under it.
func (b *CommandBuilder) Permission(node string) *CommandBuilder {
	if b.node.argument {
		b.set.fail("a permission guards a name, and %q is a value", b.node.name)
		return b
	}
	if b.node.permission != "" && b.node.permission != node {
		b.set.fail("%q is guarded by both %q and %q", b.spell(), b.node.permission, node)
	}
	b.node.permission = node
	return b
}

// Sub descends through literals, creating what is not there yet.
func (b *CommandBuilder) Sub(names ...string) *CommandBuilder {
	if len(names) == 0 {
		b.set.fail("%q: Sub needs a name", b.spell())
		return b
	}
	current := b
	for _, name := range names {
		node := b.set.child(&current.node.children, name, false)
		current = &CommandBuilder{set: b.set, node: node, path: append(current.pathTo(), name)}
	}
	return current
}

// Runs binds the function that answers when someone reaches this node.
func (b *CommandBuilder) Runs(handler CommandHandler) *CommandBuilder {
	if handler == nil {
		b.set.fail("%q: a handler is required", b.spell())
		return b
	}
	path := b.spell()
	if _, taken := b.set.handlers[path]; taken {
		b.set.fail("%q is bound twice", path)
		return b
	}
	b.node.runs = true
	b.set.handlers[path] = handler
	return b
}

// The value vocabulary, one method per type §03 names. A renderer knows every
// one of these; a plugin that wanted a sixteenth would be asking two editions
// to invent the same widget.

func (b *CommandBuilder) Integer(name string, bounds ...Bound) *CommandBuilder {
	return b.argument(name, command.ArgInteger, bounds...)
}

func (b *CommandBuilder) Decimal(name string, bounds ...Bound) *CommandBuilder {
	return b.argument(name, command.ArgDecimal, bounds...)
}

func (b *CommandBuilder) Text(name string) *CommandBuilder {
	return b.argument(name, command.ArgString)
}

func (b *CommandBuilder) Greedy(name string) *CommandBuilder {
	return b.argument(name, command.ArgGreedy)
}

func (b *CommandBuilder) Player(name string) *CommandBuilder {
	return b.argument(name, command.ArgPlayer)
}

func (b *CommandBuilder) BlockPos(name string) *CommandBuilder {
	return b.argument(name, command.ArgBlockPos)
}

func (b *CommandBuilder) BlockState(name string) *CommandBuilder {
	return b.argument(name, command.ArgBlockState)
}

func (b *CommandBuilder) Item(name string) *CommandBuilder {
	return b.argument(name, command.ArgItem)
}

func (b *CommandBuilder) Duration(name string) *CommandBuilder {
	return b.argument(name, command.ArgDuration)
}

// OneOf is a value from a fixed list, which the client completes for itself.
func (b *CommandBuilder) OneOf(name string, values ...string) *CommandBuilder {
	next := b.argument(name, command.ArgEnum)
	if len(values) == 0 {
		b.set.fail("%q: a choice needs values", next.spell())
		return next
	}
	next.node.enum = values
	return next
}

// Custom is a value only this plugin knows how to complete. The host asks the
// plugin at completion time, which is why it costs a round trip and the others
// do not.
func (b *CommandBuilder) Custom(name, kind string) *CommandBuilder {
	next := b.argument(name, command.ArgCustom)
	if kind == "" {
		b.set.fail("%q: a custom value needs a type name", next.spell())
		return next
	}
	next.node.custom = kind
	return next
}

func (b *CommandBuilder) argument(name string, kind command.ArgType, bounds ...Bound) *CommandBuilder {
	node := b.set.child(&b.node.children, name, true)
	if node.kind != 0 && node.kind != kind {
		b.set.fail("%q is declared as two different types", name)
	}
	node.kind = kind
	// Only Integer and Decimal accept bounds, so a range on a player name is
	// not a mistake this has to catch — it is one the signatures refuse.
	for _, bound := range bounds {
		bound(node)
	}
	return &CommandBuilder{set: b.set, node: node, path: append(b.pathTo(), "<"+name+">")}
}

// pathTo copies, because a builder handed out earlier must not see a sibling
// declared later appended to its own path.
func (b *CommandBuilder) pathTo() []string {
	return append([]string(nil), b.path...)
}

// spell is the path as Register writes it: literals as written, values in
// angle brackets. One vocabulary across runtimes, or an author moving between
// them learns it twice.
func (b *CommandBuilder) spell() string {
	return strings.Join(b.path, " ")
}

// Bound narrows a numeric value.
type Bound func(*commandNode)

// Min refuses anything below value.
func Min(value float64) Bound {
	return func(node *commandNode) { node.minimum = &value }
}

// Max refuses anything above value.
func Max(value float64) Bound {
	return func(node *commandNode) { node.maximum = &value }
}

// commandNode is the mutable half of the tree, which the sealed one cannot be.
type commandNode struct {
	name       string
	argument   bool
	kind       command.ArgType
	permission string
	enum       []string
	custom     string
	minimum    *float64
	maximum    *float64
	runs       bool
	children   []*commandNode
}

func (n *commandNode) convert() command.Node {
	children := make([]command.Node, 0, len(n.children))
	for _, child := range n.children {
		children = append(children, child.convert())
	}
	if len(children) == 0 {
		children = nil
	}
	// The executor id stays zero for a node that runs nothing, and one for a
	// node that does. The real ids are minted by whoever reads the neutral
	// form; this only has to say which nodes are executable.
	var exec command.ExecID
	if n.runs {
		exec = 1
	}
	if !n.argument {
		return command.Literal{
			Name: n.name, Permission: n.permission, Exec: exec, Children: children,
		}
	}
	argument := command.Argument{
		Name: n.name, Type: n.kind, Enum: n.enum, CustomType: n.custom,
		Exec: exec, Children: children,
	}
	switch n.kind {
	case command.ArgInteger:
		argument.IntegerMin, argument.IntegerMax = whole(n.minimum), whole(n.maximum)
	case command.ArgDecimal:
		argument.DecimalMin, argument.DecimalMax = n.minimum, n.maximum
	}
	return argument
}

func whole(value *float64) *int64 {
	if value == nil {
		return nil
	}
	rounded := int64(*value)
	return &rounded
}
