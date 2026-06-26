// Package git is trimdown's flagship native filter. git is the highest-value
// proxy target: it's Bash-only (no native agent tool), called constantly, and
// very noisy. One registry.Filter handles all subcommands, threading git's
// global flags and dispatching to per-subcommand parsers built on the IR.
package git

import (
	"os/exec"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func init() { registry.Register(Filter{}) }

// Filter implements registry.Filter for git as a whole; it parses git's global
// flags itself (so `git -C path status` still routes to the status handler,
// which the generic firstNonFlag matcher couldn't do).
type Filter struct{}

func (Filter) Tool() string       { return "git" }
func (Filter) Subcommand() string { return "" }

// invocation is the parsed `git [global] <sub> [args]` split.
type invocation struct {
	global []string
	sub    string
	args   []string
}

func (Filter) Exec(o registry.Opts) engine.CaptureResult {
	inv := parseInvocation(o.Args)
	switch inv.sub {
	case "status":
		// Single-exec: porcelain carries branch + entries; no second plain call.
		return capture(inv.global, "status", "--porcelain", "-b")
	case "log":
		return capture(inv.global, buildLogArgs(inv.args)...)
	default:
		return capture(inv.global, append([]string{inv.sub}, inv.args...)...)
	}
}

func (Filter) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	inv := parseInvocation(o.Args)
	h := handlers[inv.sub]
	if h == nil {
		// Unsupported subcommand → render raw, preserve exit code.
		return ir.RawReport("git", rawOf(c), c.ExitCode), nil
	}
	if c.ExitCode != 0 {
		// Failures: show the real error unchanged.
		return ir.RawReport("git", rawOf(c), c.ExitCode), nil
	}
	rep := h(c, inv)
	rep.Tool = "git"
	rep.Subcommand = inv.sub
	rep.Filtered = true
	rep.Raw = rawOf(c)
	return rep, nil
}

// handlers maps a git subcommand to its parser. Each parser is pure (takes
// captured output, returns an IR), so golden tests call them directly.
var handlers = map[string]func(engine.CaptureResult, invocation) ir.Report{
	"status": func(c engine.CaptureResult, inv invocation) ir.Report {
		rep := parseStatus(c.Stdout)
		if st := inProgressState(inv.global); st != "" {
			rep.Text = st + "\n" + rep.Text
		}
		return rep
	},
	"log":      func(c engine.CaptureResult, inv invocation) ir.Report { return parseLog(c.Stdout, inv) },
	"diff":     func(c engine.CaptureResult, _ invocation) ir.Report { return parseDiff(c.Stdout) },
	"show":     func(c engine.CaptureResult, _ invocation) ir.Report { return parseShow(c.Stdout) },
	"add":      func(c engine.CaptureResult, _ invocation) ir.Report { return okReport(c) },
	"commit":   func(c engine.CaptureResult, _ invocation) ir.Report { return parseCommit(c.Stdout) },
	"push":     func(c engine.CaptureResult, _ invocation) ir.Report { return parsePush(c.Stderr) },
	"pull":     func(c engine.CaptureResult, _ invocation) ir.Report { return parsePull(c.Stdout, c.Stderr) },
	"fetch":    func(c engine.CaptureResult, _ invocation) ir.Report { return parseFetch(c.Stderr) },
	"branch":   func(c engine.CaptureResult, _ invocation) ir.Report { return parseBranch(c.Stdout) },
	"stash":    func(c engine.CaptureResult, inv invocation) ir.Report { return parseStash(c.Stdout, inv) },
	"worktree": func(c engine.CaptureResult, _ invocation) ir.Report { return parseWorktree(c.Stdout) },
}

// parseInvocation splits git's global flags from the subcommand. Global flags
// that take a value (-C, -c, --git-dir, --work-tree, --namespace) consume the
// next token; boolean globals are passed through; the first remaining
// non-flag token is the subcommand.
func parseInvocation(args []string) invocation {
	var inv invocation
	valueGlobals := map[string]bool{"-C": true, "-c": true, "--git-dir": true, "--work-tree": true, "--namespace": true}
	boolGlobals := map[string]bool{
		"-p": true, "-P": true, "--paginate": true, "--no-pager": true, "--bare": true,
		"--no-optional-locks": true, "--literal-pathspecs": true, "--no-replace-objects": true,
		"--glob-pathspecs": true, "--icase-pathspecs": true,
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case valueGlobals[a]:
			inv.global = append(inv.global, a)
			if i+1 < len(args) {
				i++
				inv.global = append(inv.global, args[i])
			}
		case boolGlobals[a]:
			inv.global = append(inv.global, a)
		case strings.HasPrefix(a, "--git-dir=") || strings.HasPrefix(a, "--work-tree=") ||
			strings.HasPrefix(a, "--namespace=") || strings.HasPrefix(a, "-c"):
			inv.global = append(inv.global, a)
		case strings.HasPrefix(a, "-"):
			// Unknown leading flag — treat as a global passthrough.
			inv.global = append(inv.global, a)
		default:
			inv.sub = a
			inv.args = args[i+1:]
			return inv
		}
	}
	return inv
}

func capture(global []string, args ...string) engine.CaptureResult {
	full := make([]string, 0, len(global)+len(args))
	full = append(full, global...)
	full = append(full, args...)
	return engine.Capture(gitCmd(full...))
}

func gitCmd(args ...string) *exec.Cmd {
	cmd := engine.ResolvedCommand("git", args...)
	// Locale-stable parsing: we depend on git's English porcelain/status phrases.
	cmd.Env = append(cmd.Environ(), "LC_ALL=C")
	return cmd
}

func rawOf(c engine.CaptureResult) string {
	switch {
	case c.Stderr == "":
		return c.Stdout
	case c.Stdout == "":
		return c.Stderr
	default:
		return c.Stdout + c.Stderr
	}
}

// okReport summarizes a no-meaningful-output success (e.g. git add) as "ok".
func okReport(c engine.CaptureResult) ir.Report {
	body := strings.TrimSpace(c.Stdout)
	if body == "" {
		return ir.Report{Summary: "ok", Status: ir.StatusOK}
	}
	return ir.Report{Summary: "ok", Status: ir.StatusOK, Text: body}
}
