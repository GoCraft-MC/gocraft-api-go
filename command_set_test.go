package gocraft

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/GoCraft-MC/gocraft-abi/command"
	"google.golang.org/protobuf/proto"

	wire "github.com/GoCraft-MC/gocraft-abi/abi/v1/wire"
)

func shopSet() *CommandSet {
	set := NewCommandSet()
	nothing := func(*CommandContext) error { return nil }

	shop := set.Command("shop").Permission("shop.use")
	shop.Sub("sell").Decimal("price", Min(0.01), Max(1000)).Runs(nothing)
	shop.Sub("give").Player("who").Integer("count", Min(1), Max(64)).Runs(nothing)
	shop.Sub("mode").OneOf("value", "buy", "sell").Runs(nothing)
	shop.Sub("admin", "reload").Permission("shop.admin").Runs(nothing)
	return set
}

// The test this whole design exists for.
//
// A declaration binds handlers to paths it spells itself. The host binds them
// to paths it reads out of the wire tree the build produced. If those two
// spellings ever disagree, every handler silently stops being reachable — so
// the pipeline is run end to end here and the two lists are compared.
func TestDeclaredPathsAreThePathsTheHostWillIndex(t *testing.T) {
	set := shopSet()

	// Exactly what a build does: the neutral form, then the wire tree.
	intermediate, err := set.Intermediate()
	if err != nil {
		t.Fatal(err)
	}
	root, err := command.DecodeIntermediate(intermediate)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := command.EncodeTree(root)
	if err != nil {
		t.Fatal(err)
	}

	// Exactly what the runtime does with the bundle it is handed.
	var decoded wire.CommandTree
	if err := proto.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	tree := &commandTree{executors: make(map[string]uint32)}
	tree.index("", decoded.GetChildren())

	declared, indexed := set.Paths(), append([]string(nil), tree.paths...)
	sortStrings(indexed)
	if !reflect.DeepEqual(declared, indexed) {
		t.Fatalf("declared %v, host indexes %v", declared, indexed)
	}
	if len(declared) != 4 {
		t.Fatalf("paths = %v", declared)
	}
	// And every one of them resolves, which is what Register does.
	for _, path := range declared {
		if _, err := tree.lookup(path); err != nil {
			t.Fatalf("%q: %v", path, err)
		}
	}
}

func TestSetSpellsPathsLikeRegisterDoes(t *testing.T) {
	want := []string{
		"shop admin reload",
		"shop give <who> <count>",
		"shop mode <value>",
		"shop sell <price>",
	}
	if got := shopSet().Paths(); !reflect.DeepEqual(got, want) {
		t.Fatalf("paths = %v, want %v", got, want)
	}
}

// Two paths sharing a prefix are two paths through one command, not two
// commands. Merging is the only reason this is a tree rather than a list.
func TestSubMergesSharedPrefixes(t *testing.T) {
	set := NewCommandSet()
	nothing := func(*CommandContext) error { return nil }
	warp := set.Command("warp")
	warp.Sub("set").Text("name").Runs(nothing)
	warp.Sub("delete").Text("name").Runs(nothing)

	root, err := set.Tree()
	if err != nil {
		t.Fatal(err)
	}
	if len(root.Children) != 1 {
		t.Fatalf("roots = %d, want one warp", len(root.Children))
	}
	if children := root.Children[0].(command.Literal).Children; len(children) != 2 {
		t.Fatalf("warp children = %d, want set and delete", len(children))
	}
}

func TestIntermediateCarriesNoExecutorIDs(t *testing.T) {
	encoded, err := shopSet().Intermediate()
	if err != nil {
		t.Fatal(err)
	}
	if text := string(encoded); strings.Contains(text, "executor") {
		t.Fatalf("executor id leaked into %s", text)
	}
}

// A builder that returned an error per call would bury the declaration it
// exists to keep readable, so mistakes are collected and reported together.
//
// What is not here is as telling: a range on a player name, or a value where a
// name belongs, are refused by the signatures rather than reported by these.
func TestSetReportsEveryMistakeAtOnce(t *testing.T) {
	cases := map[string]func(*CommandSet){
		"bound twice": func(s *CommandSet) {
			nothing := func(*CommandContext) error { return nil }
			s.Command("a").Sub("b").Runs(nothing)
			s.Command("a").Sub("b").Runs(nothing)
		},
		"permission on a value": func(s *CommandSet) {
			s.Command("a").Text("what").Permission("nope").Runs(func(*CommandContext) error { return nil })
		},
		// The same name at the same position, twice, differently. Under two
		// different parents it would be two arguments and perfectly legal.
		"one name, two types": func(s *CommandSet) {
			s.Command("a").Text("v").Runs(func(*CommandContext) error { return nil })
			s.Command("a").Integer("v")
		},
		"choice with nothing to choose": func(s *CommandSet) {
			s.Command("a").OneOf("v").Runs(func(*CommandContext) error { return nil })
		},
		"no handler": func(s *CommandSet) {
			s.Command("a").Sub("b").Runs(nil)
		},
		"runs nothing at all": func(s *CommandSet) {
			s.Command("a").Sub("b")
		},
	}
	for name, declare := range cases {
		set := NewCommandSet()
		declare(set)
		if _, err := set.Tree(); err == nil {
			t.Fatalf("%s: accepted", name)
		}
	}
}

func TestDumpTargetReadsEverySpelling(t *testing.T) {
	cases := map[string][]string{
		"here.json": {"-gocraft-dump-commands", "here.json"},
		"eq.json":   {"-gocraft-dump-commands=eq.json"},
		"long.json": {"--gocraft-dump-commands", "long.json"},
	}
	for want, arguments := range cases {
		got, dumping := dumpTarget(arguments)
		if !dumping || got != want {
			t.Fatalf("%v -> %q, %v", arguments, got, dumping)
		}
	}
	if _, dumping := dumpTarget([]string{"-sock", "/tmp/s", "-abi", "1"}); dumping {
		t.Fatal("a normal launch was read as a dump")
	}
}

type declaringPlugin struct{ Plugin }

func (declaringPlugin) Commands() *CommandSet { return shopSet() }

func TestDumpWritesWhatABuildReads(t *testing.T) {
	target := filepath.Join(t.TempDir(), "gocraft", "commands.json")
	if err := dumpCommands(declaringPlugin{}, target); err != nil {
		t.Fatal(err)
	}
	written, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	// The file is only worth anything if the build tool's reader accepts it.
	if _, err := command.DecodeIntermediate(written); err != nil {
		t.Fatalf("%v\n%s", err, written)
	}
}

func TestDumpRefusesAPluginThatDeclaresNothing(t *testing.T) {
	err := dumpCommands(struct{ Plugin }{}, filepath.Join(t.TempDir(), "commands.json"))
	if err == nil || !strings.Contains(err.Error(), "declares no commands") {
		t.Fatalf("err = %v", err)
	}
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}
