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
	if c.ExitCode != 0 && !failureAware[inv.sub] {
		// Failures we can't structure: show the real error unchanged.
		return ir.RawReport("git", rawOf(c), c.ExitCode), nil
	}
	rep := h(c, inv)
	if !rep.Filtered && rep.Summary == "" && rep.Text == "" {
		// Failure-aware handler declined to compact this error (e.g. an
		// unrecognized failure shape) → fall back to raw passthrough.
		return ir.RawReport("git", rawOf(c), c.ExitCode), nil
	}
	rep.Tool = "git"
	rep.Subcommand = inv.sub
	rep.Filtered = true
	rep.Raw = rawOf(c)
	rep.ExitCode = c.ExitCode
	return rep, nil
}

// failureAware lists subcommands whose handler is invoked even on a non-zero
// exit, because it can compact a structured failure (e.g. merge/rebase
// conflicts, checkout local-changes errors). When such a handler cannot
// recognize the failure it returns a zero Report and Parse falls back to raw.
var failureAware = map[string]bool{
	"merge":       true,
	"rebase":      true,
	"cherry-pick": true,
	"revert":      true,
	"checkout":    true,
	"switch":      true,
	"restore":     true,
	"apply":       true,
	"am":          true,
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

	// --- refs / history-moving subcommands ---
	"checkout":    func(c engine.CaptureResult, inv invocation) ir.Report { return parseCheckout(c, inv) },
	"switch":      func(c engine.CaptureResult, inv invocation) ir.Report { return parseCheckout(c, inv) },
	"restore":     func(c engine.CaptureResult, inv invocation) ir.Report { return parseRestore(c, inv) },
	"merge":       func(c engine.CaptureResult, _ invocation) ir.Report { return parseMerge(c) },
	"rebase":      func(c engine.CaptureResult, inv invocation) ir.Report { return parseRebase(c, inv) },
	"cherry-pick": func(c engine.CaptureResult, _ invocation) ir.Report { return parseCherryPick(c, "cherry-pick") },
	"revert":      func(c engine.CaptureResult, _ invocation) ir.Report { return parseCherryPick(c, "revert") },
	"reset":       func(c engine.CaptureResult, _ invocation) ir.Report { return parseReset(c) },
	"apply":       func(c engine.CaptureResult, _ invocation) ir.Report { return parseApply(c) },
	"am":          func(c engine.CaptureResult, _ invocation) ir.Report { return parseAm(c) },

	// --- listing / lookup subcommands ---
	"tag":      func(c engine.CaptureResult, inv invocation) ir.Report { return parseTag(c, inv) },
	"remote":   func(c engine.CaptureResult, inv invocation) ir.Report { return parseRemote(c, inv) },
	"clean":    func(c engine.CaptureResult, _ invocation) ir.Report { return parseClean(c) },
	"config":   func(c engine.CaptureResult, inv invocation) ir.Report { return parseConfig(c, inv) },
	"reflog":   func(c engine.CaptureResult, _ invocation) ir.Report { return parseReflog(c) },
	"shortlog": func(c engine.CaptureResult, _ invocation) ir.Report { return parseShortlog(c) },
	"describe": func(c engine.CaptureResult, _ invocation) ir.Report { return parseOneValue(c, "describe") },
	"rev-parse": func(c engine.CaptureResult, _ invocation) ir.Report {
		return parseOneValue(c, "rev-parse")
	},
	"blame":    func(c engine.CaptureResult, _ invocation) ir.Report { return parseBlame(c) },
	"bisect":   func(c engine.CaptureResult, _ invocation) ir.Report { return parseBisect(c) },
	"ls-files": func(c engine.CaptureResult, _ invocation) ir.Report { return parseLsFiles(c) },
	"grep":     func(c engine.CaptureResult, _ invocation) ir.Report { return parseGrep(c) },
	"rm":       func(c engine.CaptureResult, _ invocation) ir.Report { return parseRmMv(c, "rm") },
	"mv":       func(c engine.CaptureResult, _ invocation) ir.Report { return parseRmMv(c, "mv") },
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
