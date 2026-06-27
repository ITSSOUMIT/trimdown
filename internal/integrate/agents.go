// Package integrate wires trimdown into agentic coding tools that expose a
// pre-tool command hook (the only deterministic, context-aware interception
// point). Each supported agent is one entry in a small registry; install,
// uninstall, agents, and doctor all read from it, so adding an agent is one
// entry plus its hook adapter. v0.4.0 ships Claude Code only.
package integrate

import (
	"fmt"
	"os"
	"sort"
)

// Agent describes one supported agentic tool and how to wire/unwire its hook.
type Agent struct {
	Name    string // CLI identifier, e.g. "claude-code"
	Display string // human name, e.g. "Claude Code"

	// ConfigPath returns the settings file to edit for the given scope
	// (global = user-wide; otherwise project-local).
	ConfigPath func(global bool) (string, error)

	// Install merges the trimdown hook into the file; reports whether it changed.
	Install func(path string) (bool, error)
	// Uninstall removes the trimdown hook; reports whether it changed.
	Uninstall func(path string) (bool, error)
	// Status returns a one-line doctor report for the given scope.
	Status func(global bool) string
	// Hook runs the agent's hook adapter (reads the event on stdin, writes the
	// decision to stdout) and returns an exit code.
	Hook func() int
}

var agents = map[string]*Agent{}

func registerAgent(a *Agent) { agents[a.Name] = a }

// LookupAgent finds a supported agent by name.
func LookupAgent(name string) (*Agent, bool) {
	a, ok := agents[name]
	return a, ok
}

// allAgents returns the agents sorted by name.
func allAgents() []*Agent {
	out := make([]*Agent, 0, len(agents))
	for _, a := range agents {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Install handles `trimdown install [<agent>] [--global]`. With no agent it
// lists the supported ones.
func Install(args []string) int {
	global, rest := parseScope(args)
	if len(rest) == 0 {
		return ListAgents()
	}
	a, ok := LookupAgent(rest[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "trimdown: unknown agent %q\n\n", rest[0])
		ListAgents()
		return 1
	}
	path, err := a.ConfigPath(global)
	if err != nil {
		fmt.Fprintln(os.Stderr, "trimdown:", err)
		return 1
	}
	changed, err := a.Install(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "trimdown:", err)
		return 1
	}
	if changed {
		fmt.Printf("✓ installed %s hook → %s\n", a.Display, path)
	} else {
		fmt.Printf("• %s hook already present → %s\n", a.Display, path)
	}
	fmt.Println("  trimdown will now compact covered commands automatically.")
	fmt.Println("  Disable anytime with TRIMDOWN_DISABLE=1, or `trimdown uninstall " + a.Name + "`.")
	return 0
}

// Uninstall handles `trimdown uninstall <agent> [--global]`.
func Uninstall(args []string) int {
	global, rest := parseScope(args)
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "trimdown uninstall: name an agent (see `trimdown agents`)")
		return 1
	}
	a, ok := LookupAgent(rest[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "trimdown: unknown agent %q\n", rest[0])
		return 1
	}
	path, err := a.ConfigPath(global)
	if err != nil {
		fmt.Fprintln(os.Stderr, "trimdown:", err)
		return 1
	}
	changed, err := a.Uninstall(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "trimdown:", err)
		return 1
	}
	if changed {
		fmt.Printf("✓ removed %s hook from %s\n", a.Display, path)
	} else {
		fmt.Printf("• no %s hook found in %s\n", a.Display, path)
	}
	return 0
}

// ListAgents prints the supported agents and basic usage.
func ListAgents() int {
	fmt.Println("Supported agents:")
	for _, a := range allAgents() {
		fmt.Printf("  %-14s %s\n", a.Name, a.Display)
	}
	fmt.Println()
	fmt.Println("Usage:")
	fmt.Println("  trimdown install <agent>            install into ./.claude (project-local)")
	fmt.Println("  trimdown install <agent> --global   install user-wide")
	fmt.Println("  trimdown uninstall <agent> [--global]")
	fmt.Println("  trimdown doctor")
	return 0
}

// Doctor reports the status of every agent in both scopes.
func Doctor() int {
	fmt.Println("trimdown doctor")
	if os.Getenv("TRIMDOWN_DISABLE") != "" {
		fmt.Println("  ⚠ TRIMDOWN_DISABLE is set — all rewriting/compaction is OFF")
	}
	for _, a := range allAgents() {
		fmt.Printf("\n%s\n", a.Display)
		fmt.Println("  project:", a.Status(false))
		fmt.Println("  global: ", a.Status(true))
	}
	return 0
}

// Hook handles `trimdown hook <agent>` — runs that agent's hook adapter.
func Hook(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "trimdown hook: name an agent (e.g. claude-code)")
		return 1
	}
	a, ok := LookupAgent(args[0])
	if !ok || a.Hook == nil {
		fmt.Fprintf(os.Stderr, "trimdown hook: unknown agent %q\n", args[0])
		return 1
	}
	return a.Hook()
}

// parseScope pulls --global/-g out of args, returning the flag and the rest.
func parseScope(args []string) (global bool, rest []string) {
	for _, a := range args {
		switch a {
		case "--global", "-g":
			global = true
		default:
			rest = append(rest, a)
		}
	}
	return global, rest
}
