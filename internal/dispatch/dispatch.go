// Package dispatch is the lean router: it parses global flags, then sends meta
// commands to their handlers and tool invocations to a registered filter (or
// passthrough). No heavyweight CLI framework on the hot path → fast startup.
package dispatch

import (
	"fmt"
	"os"

	"github.com/itssoumit/trimdown/internal/integrate"
	"github.com/itssoumit/trimdown/internal/meta"
	"github.com/itssoumit/trimdown/internal/registry"
	"github.com/itssoumit/trimdown/internal/rewrite"
	"github.com/itssoumit/trimdown/internal/run"
)

const usage = `trimdown — compress tool output to cut LLM token use

Usage:
  trimdown [global flags] <tool> [args...]     run a tool with compacted output
  trimdown passthrough <cmd> [args...]         run unfiltered, but record usage
  trimdown savings [--all] [--json] [-p]       show token savings + analytics

Agent integration:
  trimdown install <agent> [--global]          wire trimdown into an agent (auto)
  trimdown uninstall <agent> [--global]        remove it
  trimdown agents                              list supported agents
  trimdown doctor                              check integration status

Global flags (before the tool):
  -v        increase verbosity (repeatable)
  -q        ultra-compact output
  --json    structured (JSON) output
  --raw     skip filtering for this run

Env:
  TRIMDOWN_DISABLE=1   pass every command through unchanged (kill switch)`

// Main is the entry point. Returns the process exit code.
func Main(args []string) int {
	o, rest := parseGlobals(args)
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 1
	}

	switch rest[0] {
	case "version", "--version":
		return meta.ShowVersion()
	case "help", "--help", "-h":
		fmt.Println(usage)
		return 0
	case "passthrough":
		return meta.Passthrough(rest[1:])
	case "savings":
		return meta.Savings(rest[1:])
	case "install":
		return integrate.Install(rest[1:])
	case "uninstall":
		return integrate.Uninstall(rest[1:])
	case "agents":
		return integrate.ListAgents()
	case "doctor":
		return integrate.Doctor()
	case "hook":
		return integrate.Hook(rest[1:])
	case "rewrite":
		return integrate.RewriteCmd(rest[1:])
	}

	o.Tool = rest[0]
	o.Args = rest[1:]

	// Escape hatch + interactive guard: never capture a command that needs a
	// TTY/editor; run it raw with inherited stdio (still recorded as passthrough).
	if os.Getenv("TRIMDOWN_DISABLE") != "" || rewrite.IsInteractive(o.Tool, o.Args) {
		return meta.Passthrough(rest)
	}

	if f, ok := registry.Lookup(o.Tool, o.Args); ok {
		return run.Execute(f, o)
	}
	// Unknown tool/subcommand → run unfiltered (and record as passthrough).
	return meta.Passthrough(rest)
}

// parseGlobals consumes leading global flags and returns the remaining args
// (starting at the tool/meta-command).
func parseGlobals(args []string) (registry.Opts, []string) {
	var o registry.Opts
	i := 0
	for ; i < len(args); i++ {
		switch args[i] {
		case "-v", "--verbose":
			o.Verbose++
		case "-vv":
			o.Verbose += 2
		case "-vvv":
			o.Verbose += 3
		case "-q", "--quiet":
			o.Quiet = true
		case "--json":
			o.JSON = true
		case "--raw":
			o.Raw = true
		default:
			return o, args[i:]
		}
	}
	return o, args[i:]
}
