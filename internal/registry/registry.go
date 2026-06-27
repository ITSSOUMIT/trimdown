// Package registry holds the Filter interface and a self-registration table.
// Adding a tool means implementing Filter and calling Register in an init() —
// no central switch statement to edit (the key scalability win over rtk).
package registry

import (
	"sort"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
)

// Opts carries the parsed invocation and global flags down to a filter.
type Opts struct {
	Tool    string   // the tool name, e.g. "git"
	Args    []string // args after the tool name (subcommand + its args/flags)
	Verbose int      // -v count
	Quiet   bool     // -q: ultra-compact output
	JSON    bool     // --json: structured output
	Raw     bool     // --raw: skip filtering
}

// Filter parses one tool (or one subcommand) into the IR. Exec and Parse are
// split so Parse stays a pure, process-free function that golden tests can call
// directly, while Exec owns command construction (flag injection, binary swap
// like `bundle exec`, multi-exec, etc.).
type Filter interface {
	Tool() string       // e.g. "git", "pytest"
	Subcommand() string // "" matches the whole tool; else a specific subcommand
	Exec(o Opts) engine.CaptureResult
	Parse(c engine.CaptureResult, o Opts) (ir.Report, error)
}

var filters = map[string][]Filter{}

// Register adds a filter to the table. Call from init().
func Register(f Filter) {
	filters[f.Tool()] = append(filters[f.Tool()], f)
}

// RegisteredTools returns the sorted names of every tool that has at least one
// registered filter. The command rewriter uses this to decide which first-word
// of a shell command is worth wrapping with trimdown.
func RegisteredTools() []string {
	tools := make([]string, 0, len(filters))
	for t := range filters {
		tools = append(tools, t)
	}
	sort.Strings(tools)
	return tools
}

// Lookup finds the best filter for a tool invocation: a subcommand-specific
// filter wins over a whole-tool filter; if neither matches, ok is false and the
// caller should pass the command through unfiltered.
func Lookup(tool string, args []string) (Filter, bool) {
	fs := filters[tool]
	if len(fs) == 0 {
		return nil, false
	}
	sub := firstNonFlag(args)
	var whole Filter
	for _, f := range fs {
		switch f.Subcommand() {
		case "":
			whole = f
		case sub:
			return f, true
		}
	}
	if whole != nil {
		return whole, true
	}
	return nil, false
}

func firstNonFlag(args []string) string {
	for _, a := range args {
		if a == "" || a[0] == '-' {
			continue
		}
		return a
	}
	return ""
}
