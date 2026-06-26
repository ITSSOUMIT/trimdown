package python

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func init() {
	registry.Register(pyInterp{tool: "python"})
	registry.Register(pyInterp{tool: "python3"})
}

// pyInterp is the whole-tool dispatcher for the `python` / `python3`
// interpreters. It inspects the invocation (o.Args = everything after the
// interpreter name) and routes each mode to a parser tuned to ITS real output:
// `-m pytest/mypy/pip/ruff` reuse the dedicated toolchain filters; `-m unittest`
// and `-m venv` get bespoke parsers; tracebacks in a plain `script.py` run are
// compacted; genuine program output and interactive modes pass through.
type pyInterp struct{ tool string }

func (p pyInterp) Tool() string     { return p.tool }
func (pyInterp) Subcommand() string { return "" }

// moduleOf returns the module name M for `python -m M ...`, plus the args that
// follow M, and ok=false if this isn't a `-m` invocation. It honors both
// `-m mod` and `-mmod` forms and stops at the first non-flag otherwise.
func moduleOf(args []string) (mod string, rest []string, ok bool) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "-m" {
			if i+1 < len(args) {
				return args[i+1], args[i+2:], true
			}
			return "", nil, false
		}
		if strings.HasPrefix(a, "-m") && len(a) > 2 {
			return a[2:], args[i+1:], true
		}
		if a == "" || a[0] == '-' {
			continue // other interpreter flag (-O, -B, -u, ...)
		}
		// First positional is a script path, not a module.
		return "", nil, false
	}
	return "", nil, false
}

// subOpts builds an Opts that the dedicated toolchain filters expect: their
// Args is the module's own args (the `-m <module>` prefix removed).
func subOpts(o registry.Opts, tool string, rest []string) registry.Opts {
	return registry.Opts{
		Tool:    tool,
		Args:    rest,
		Verbose: o.Verbose,
		Quiet:   o.Quiet,
		JSON:    o.JSON,
		Raw:     o.Raw,
	}
}

func (p pyInterp) Exec(o registry.Opts) engine.CaptureResult {
	if mod, rest, ok := moduleOf(o.Args); ok {
		switch mod {
		case "pytest":
			return pytest{}.Exec(subOpts(o, "pytest", rest))
		case "mypy":
			return mypy{}.Exec(subOpts(o, "mypy", rest))
		case "pip":
			return pip{}.Exec(subOpts(o, "pip", rest))
		case "ruff":
			return ruff{}.Exec(subOpts(o, "ruff", rest))
		}
	}
	// Everything else (unittest, venv, scripts, -c, --version, servers, unknown
	// -m): run the interpreter verbatim and let Parse decide how to compact.
	return engine.Capture(engine.ResolvedCommand(p.tool, o.Args...))
}

func (p pyInterp) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	// Routed toolchain modules: reuse the dedicated parsers (with their own Opts).
	if mod, rest, ok := moduleOf(o.Args); ok {
		switch mod {
		case "pytest":
			return pytest{}.Parse(c, subOpts(o, "pytest", rest))
		case "mypy":
			return mypy{}.Parse(c, subOpts(o, "mypy", rest))
		case "pip":
			return pip{}.Parse(c, subOpts(o, "pip", rest))
		case "ruff":
			return ruff{}.Parse(c, subOpts(o, "ruff", rest))
		case "unittest":
			return parseUnittest(c), nil
		case "venv":
			return parseVenv(c, rest), nil
		default:
			// http.server and other long-running / unknown modules: passthrough.
			return ir.RawReport(p.tool, rawOf(c), c.ExitCode), nil
		}
	}

	// `python -c "..."` — inline code; the IGNORE class, pass through.
	if hasFlag(o.Args, "-c") {
		return ir.RawReport(p.tool, rawOf(c), c.ExitCode), nil
	}

	// `python --version` / `-V`.
	if hasFlag(o.Args, "--version", "-V") {
		if v := versionString(c); v != "" {
			return ir.Report{Tool: p.tool, Summary: v, Status: ir.StatusOK, Filtered: true, Raw: rawOf(c)}, nil
		}
		return ir.RawReport(p.tool, rawOf(c), c.ExitCode), nil
	}

	// `python script.py [...]` (or REPL): genuine program output. Passthrough,
	// EXCEPT compact any Python traceback it produced.
	if tb := extractTraceback(c.Stdout + "\n" + c.Stderr); tb != nil {
		return tracebackReport(p.tool, tb, c), nil
	}
	return ir.RawReport(p.tool, rawOf(c), c.ExitCode), nil
}

// hasFlag reports whether args contains any of the given exact flag tokens.
func hasFlag(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f {
				return true
			}
		}
	}
	return false
}

// versionString returns the trimmed `Python X.Y.Z` line from either stream.
func versionString(c engine.CaptureResult) string {
	for _, s := range []string{c.Stdout, c.Stderr} {
		for _, l := range splitLines(s) {
			t := strings.TrimSpace(l)
			if strings.HasPrefix(t, "Python ") {
				return t
			}
		}
	}
	return ""
}

// ---------------------------------------------------------------------------
// unittest
// ---------------------------------------------------------------------------

var (
	unittestRanRE  = regexp.MustCompile(`^Ran (\d+) tests? in `)
	unittestFailRE = regexp.MustCompile(`^FAILED \((.+)\)$`)
	unittestKVRE   = regexp.MustCompile(`(failures|errors|skipped|expected failures|unexpected successes)=(\d+)`)
	unittestCaseRE = regexp.MustCompile(`^(FAIL|ERROR): (.+)$`)
	unittestSepRE  = regexp.MustCompile(`^={10,}$`)
	unittestDashRE = regexp.MustCompile(`^-{10,}$`)
)

const (
	maxUnittestCases  = 10
	maxUnittestDetail = 6
)

// parseUnittest compacts `python -m unittest` text output: the trailing
// "Ran N tests" line, the OK / FAILED(...) verdict, and the FAIL:/ERROR: case
// blocks with their tracebacks (failures-only, capped).
func parseUnittest(c engine.CaptureResult) ir.Report {
	out := c.Stdout + "\n" + c.Stderr
	lines := splitLines(out)

	total := -1
	failures, errors, skipped := 0, 0, 0
	sawVerdict := false
	ok := false

	var cases []ir.TestResult
	var cur *ir.TestResult
	inBlock := false

	flush := func() {
		if cur != nil && len(cases) < maxUnittestCases {
			cases = append(cases, *cur)
		}
		cur = nil
	}

	for _, l := range lines {
		if m := unittestRanRE.FindStringSubmatch(l); m != nil {
			total, _ = strconv.Atoi(m[1])
			continue
		}
		if strings.TrimSpace(l) == "OK" || strings.HasPrefix(l, "OK (") {
			sawVerdict, ok = true, true
			continue
		}
		if m := unittestFailRE.FindStringSubmatch(l); m != nil {
			sawVerdict, ok = true, false
			for _, kv := range unittestKVRE.FindAllStringSubmatch(m[1], -1) {
				n, _ := strconv.Atoi(kv[2])
				switch kv[1] {
				case "failures":
					failures = n
				case "errors":
					errors = n
				case "skipped":
					skipped = n
				}
			}
			continue
		}

		// Case blocks are delimited by lines of '='. A "FAIL:/ERROR:" header
		// names the test; the following indented lines are its traceback.
		if unittestSepRE.MatchString(l) {
			flush()
			inBlock = true
			continue
		}
		if m := unittestCaseRE.FindStringSubmatch(l); m != nil && inBlock {
			flush()
			st := ir.StatusFail
			cur = &ir.TestResult{Name: strings.TrimSpace(m[2]), Status: st}
			if m[1] == "ERROR" {
				cur.Detail = append(cur.Detail, "(error)")
			}
			continue
		}
		if unittestDashRE.MatchString(l) {
			continue // the '----' under a case header
		}
		if cur != nil {
			t := strings.TrimSpace(l)
			if t == "" {
				continue
			}
			if len(cur.Detail) < maxUnittestDetail {
				cur.Detail = append(cur.Detail, strings.TrimRight(l, " "))
			}
		}
	}
	flush()

	if total < 0 && !sawVerdict {
		// Not recognizable unittest output (e.g. import error before run).
		return ir.RawReport("python", rawOf(c), c.ExitCode)
	}

	failed := failures + errors
	status := ir.StatusOK
	if !ok || failed > 0 {
		status = ir.StatusFail
	}

	var sb strings.Builder
	if total >= 0 {
		fmt.Fprintf(&sb, "%d tests", total)
	} else {
		sb.WriteString("tests")
	}
	if failed > 0 {
		fmt.Fprintf(&sb, ", %d failed", failed)
	} else if ok {
		sb.WriteString(", all passed")
	}
	if skipped > 0 {
		fmt.Fprintf(&sb, ", %d skipped", skipped)
	}

	return ir.Report{
		Tool:     "unittest",
		Summary:  sb.String(),
		Status:   status,
		Tests:    cases,
		Filtered: true,
		Raw:      rawOf(c),
	}
}

// ---------------------------------------------------------------------------
// venv
// ---------------------------------------------------------------------------

// parseVenv reports near-silent `python -m venv DIR` success/failure.
func parseVenv(c engine.CaptureResult, rest []string) ir.Report {
	dir := firstArg(rest)
	if dir == "" {
		dir = "(env)"
	}
	if c.ExitCode != 0 {
		return ir.Report{
			Tool:     "venv",
			Summary:  "venv failed: " + dir,
			Status:   ir.StatusFail,
			Filtered: true,
			Raw:      rawOf(c),
		}
	}
	return ir.Report{
		Tool:     "venv",
		Summary:  "ok venv " + dir,
		Status:   ir.StatusOK,
		Filtered: true,
		Raw:      rawOf(c),
	}
}

// ---------------------------------------------------------------------------
// traceback compaction
// ---------------------------------------------------------------------------

var (
	tracebackStartRE = regexp.MustCompile(`^Traceback \(most recent call last\):`)
	// "  File "path", line N, in func"
	tbFrameRE = regexp.MustCompile(`^\s+File "(.+?)", line (\d+)(?:, in (.+))?$`)
	// Final "ExceptionType: message" (allows dotted/qualified names).
	tbExcRE = regexp.MustCompile(`^([A-Za-z_][\w.]*(?:Error|Exception|Warning|Interrupt|Exit|Iteration|Stop[A-Za-z]*|Fault)[\w.]*): ?(.*)$`)
	// Bare exception with no message, e.g. "KeyboardInterrupt".
	tbExcBareRE = regexp.MustCompile(`^([A-Za-z_][\w.]*(?:Error|Exception|Warning|Interrupt|Exit))$`)
)

type tbFrame struct {
	file string
	line int
	fn   string
	code string
}

type traceback struct {
	excType string
	excMsg  string
	frames  []tbFrame
}

const maxTraceFrames = 5

// extractTraceback scans output for the LAST Python traceback and returns its
// exception type/message plus parsed frames, or nil if none is present.
func extractTraceback(out string) *traceback {
	lines := splitLines(out)

	// Find the start of the last traceback (handles chained tracebacks by
	// taking the final, outermost one the program actually died on).
	start := -1
	for i, l := range lines {
		if tracebackStartRE.MatchString(l) {
			start = i
		}
	}
	if start < 0 {
		return nil
	}

	tb := &traceback{}
	i := start + 1
	for i < len(lines) {
		l := lines[i]
		if m := tbFrameRE.FindStringSubmatch(l); m != nil {
			ln, _ := strconv.Atoi(m[2])
			fr := tbFrame{file: m[1], line: ln, fn: m[3]}
			// The next line, if more-indented than a frame header, is the
			// source line for this frame.
			if i+1 < len(lines) {
				next := lines[i+1]
				if strings.TrimSpace(next) != "" && tbFrameRE.FindStringSubmatch(next) == nil && !looksLikeException(next) {
					fr.code = strings.TrimSpace(next)
					i++
				}
			}
			tb.frames = append(tb.frames, fr)
			i++
			continue
		}
		if m := tbExcRE.FindStringSubmatch(strings.TrimRight(l, " ")); m != nil {
			tb.excType, tb.excMsg = m[1], m[2]
			break
		}
		if m := tbExcBareRE.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			tb.excType = m[1]
			break
		}
		i++
	}

	if tb.excType == "" && len(tb.frames) == 0 {
		return nil
	}
	return tb
}

func looksLikeException(l string) bool {
	t := strings.TrimRight(l, " ")
	return tbExcRE.MatchString(t) || tbExcBareRE.MatchString(strings.TrimSpace(t))
}

// isNoisyFrame reports whether a frame lives in stdlib/site-packages and can be
// dropped when we have enough application frames.
func isNoisyFrame(f tbFrame) bool {
	return strings.Contains(f.file, "site-packages") ||
		strings.Contains(f.file, "dist-packages") ||
		strings.Contains(f.file, "/lib/python")
}

// tracebackReport compacts a parsed traceback into an IR report: keep the
// exception, then the LAST ~maxTraceFrames frames, preferring application
// frames over stdlib/site-packages noise.
func tracebackReport(tool string, tb *traceback, c engine.CaptureResult) ir.Report {
	// Prefer application frames; only fall back to noisy frames if we'd
	// otherwise have nothing.
	kept := tb.frames
	if app := filterFrames(tb.frames, false); len(app) > 0 {
		kept = app
	}
	if len(kept) > maxTraceFrames {
		kept = kept[len(kept)-maxTraceFrames:]
	}

	var b strings.Builder
	for _, f := range kept {
		b.WriteString("  ")
		b.WriteString(shortenPath(f.file))
		fmt.Fprintf(&b, ":%d", f.line)
		if f.fn != "" {
			b.WriteString(" in ")
			b.WriteString(f.fn)
		}
		b.WriteByte('\n')
		if f.code != "" {
			b.WriteString("    ")
			b.WriteString(f.code)
			b.WriteByte('\n')
		}
	}

	summary := tb.excType
	if summary == "" {
		summary = "Traceback"
	}
	if tb.excMsg != "" {
		summary += ": " + truncate(tb.excMsg, 200)
	}

	status := ir.StatusFail
	if c.ExitCode == 0 {
		// A traceback that didn't kill the process (e.g. caught & printed) is
		// still worth surfacing, but as a warning.
		status = ir.StatusWarn
	}

	return ir.Report{
		Tool:     tool,
		Summary:  summary,
		Status:   status,
		Text:     strings.TrimRight(b.String(), "\n"),
		Filtered: true,
		Raw:      rawOf(c),
		ExitCode: c.ExitCode,
	}
}

// filterFrames returns frames, optionally keeping the noisy (stdlib/site-pkg)
// ones. When keepNoisy is false, noisy frames are dropped.
func filterFrames(frames []tbFrame, keepNoisy bool) []tbFrame {
	if keepNoisy {
		return frames
	}
	out := make([]tbFrame, 0, len(frames))
	for _, f := range frames {
		if !isNoisyFrame(f) {
			out = append(out, f)
		}
	}
	return out
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
