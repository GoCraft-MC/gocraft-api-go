package gocraft

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// How a build asks a plugin what commands it has.
//
// Java has javac, so an annotation processor can read the source while it is
// being compiled. Go has no such seam, so the plugin answers for itself: built
// and run with the flag below, it writes what it declares and exits without
// connecting to anything.
//
//	go run . -gocraft-dump-commands commands.json
//	gocraft-cli build -commands commands.json -o my-plugin.gcpkg .
//
// `go run` rather than the shipped binary, so this works when the plugin is
// cross-compiled for a server that is not this machine.
//
// It writes the neutral form, never a commands.pb. One program encodes the wire
// tree, and it is the one that builds bundles — otherwise "identical for every
// runtime" would hold right up until the two encoders disagreed.

// dumpFlag is prefixed because the binary belongs to the plugin author, who may
// well parse flags of their own.
const dumpFlag = "-gocraft-dump-commands"

// CommandPlugin is implemented by a plugin that declares commands.
//
// The method is called twice in a plugin's life and never by the author: once
// by a build asking what to put in the bundle, and once at load to bind the
// handlers. That is the whole point — the shape and the functions come from one
// statement, so a path with no handler is not something that can be written.
type CommandPlugin interface {
	Commands() *CommandSet
}

// dumpTarget reads the flag by hand rather than through the runner's flag set,
// which requires -sock: there is no host here, and demanding a socket to
// describe oneself would be a strange thing to ask.
func dumpTarget(arguments []string) (string, bool) {
	for index, argument := range arguments {
		switch {
		case argument == dumpFlag || argument == "-"+dumpFlag:
			if index+1 < len(arguments) {
				return arguments[index+1], true
			}
			return "", true
		case strings.HasPrefix(argument, dumpFlag+"="):
			return strings.TrimPrefix(argument, dumpFlag+"="), true
		case strings.HasPrefix(argument, "-"+dumpFlag+"="):
			return strings.TrimPrefix(argument, "-"+dumpFlag+"="), true
		}
	}
	return "", false
}

func dumpCommands(implementation Plugin, target string) error {
	if target == "" {
		return fmt.Errorf("gocraft: %s needs a file to write", dumpFlag)
	}
	declaring, ok := implementation.(CommandPlugin)
	if !ok {
		return fmt.Errorf("gocraft: this plugin declares no commands; "+
			"implement Commands() *gocraft.CommandSet to use %s", dumpFlag)
	}
	set := declaring.Commands()
	if set == nil {
		return fmt.Errorf("gocraft: Commands() returned nothing")
	}
	encoded, err := set.Intermediate()
	if err != nil {
		return err
	}
	if directory := filepath.Dir(target); directory != "." {
		if err := os.MkdirAll(directory, 0o755); err != nil {
			return fmt.Errorf("gocraft: %s: %w", target, err)
		}
	}
	if err := os.WriteFile(target, encoded, 0o644); err != nil {
		return fmt.Errorf("gocraft: %s: %w", target, err)
	}
	return nil
}
