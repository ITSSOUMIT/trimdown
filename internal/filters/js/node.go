package js

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

// node is a whole-tool filter for the Node.js runtime. It special-cases
// --version/--check/--test and otherwise passes program output through,
// compacting only an uncaught error + stack trace if one is present.
type node struct{}

func (node) Tool() string       { return "node" }
func (node) Subcommand() string { return "" }

func (node) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand("node", o.Args...))
}

var (
	// A stack frame: "    at fn (file:line:col)" or "    at file:line:col".
	nodeFrameRE = regexp.MustCompile(`^\s*at\s+`)
	// node_modules / internal node frames we drop from the kept trace.
	nodeNoiseFrameRE = regexp.MustCompile(`node_modules|node:internal|\(internal/|\(node:`)
	// Error header: "TypeError: x is not a function" / "ReferenceError: ...".
	nodeErrHeaderRE = regexp.MustCompile(`^([A-Z][A-Za-z]*(?:Error|Exception)):\s*(.*)$`)
	// TAP markers for `node --test`.
	tapNotOkRE = regexp.MustCompile(`^not ok (\d+)\s*-?\s*(.*)$`)
	tapPlanRE  = regexp.MustCompile(`^# (?:tests|pass|fail) (\d+)$`)
	tapFailRE  = regexp.MustCompile(`^# fail (\d+)$`)
	tapTestsRE = regexp.MustCompile(`^# tests (\d+)$`)
)

const maxNodeFrames = 5

func (node) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	raw := rawOf(c)
	args := o.Args

	switch {
	case hasFlag(args, "--version", "-v"):
		return nodeVersionReport(engine.StripANSI(c.Stdout+c.Stderr), raw, c.ExitCode), nil
	case hasFlag(args, "--check", "-c"):
		return nodeCheckReport(c, raw), nil
	case hasFlag(args, "--test"):
		return nodeTestReport(engine.StripANSI(c.Stdout+"\n"+c.Stderr), c.ExitCode, raw), nil
	case hasFlag(args, "-e", "--eval", "-p", "--print"):
		// Inline scripts: the output is the user's; pass through.
		return ir.RawReport("node", raw, c.ExitCode), nil
	}

	// Default: `node script.js`. Passthrough unless there's an uncaught error.
	return nodeScriptReport(c, raw), nil
}

func nodeVersionReport(out, raw string, exit int) ir.Report {
	for _, l := range splitLines(out) {
		t := strings.TrimSpace(l)
		if t != "" {
			return ir.Report{Tool: "node", Summary: t, Status: ir.StatusOK, Filtered: true, Raw: raw}
		}
	}
	return ir.RawReport("node", raw, exit)
}

func nodeCheckReport(c engine.CaptureResult, raw string) ir.Report {
	if c.ExitCode == 0 {
		return ir.Report{Tool: "node", Summary: "syntax ok", Status: ir.StatusOK, Filtered: true, Raw: raw}
	}
	// SyntaxError text is on stderr; keep the first meaningful line.
	for _, l := range splitLines(engine.StripANSI(c.Stderr)) {
		t := strings.TrimSpace(l)
		if t == "" || nodeFrameRE.MatchString(l) {
			continue
		}
		if strings.Contains(t, "SyntaxError") || strings.Contains(t, "Error") {
			return ir.Report{Tool: "node", Summary: "syntax error", Status: ir.StatusFail, Text: truncateRunes(t, 200), Filtered: true, Raw: raw}
		}
	}
	return ir.Report{Tool: "node", Summary: "syntax error", Status: ir.StatusFail, Filtered: true, Raw: raw}
}

// nodeTestReport parses TAP from `node --test`, failures-only.
func nodeTestReport(out string, exit int, raw string) ir.Report {
	tests, failed := -1, -1
	var failLines []string

	for _, l := range splitLines(out) {
		t := strings.TrimSpace(l)
		if m := tapTestsRE.FindStringSubmatch(t); m != nil {
			tests, _ = strconv.Atoi(m[1])
			continue
		}
		if m := tapFailRE.FindStringSubmatch(t); m != nil {
			failed, _ = strconv.Atoi(m[1])
			continue
		}
		if tapPlanRE.MatchString(t) {
			continue
		}
		if m := tapNotOkRE.FindStringSubmatch(t); m != nil {
			if len(failLines) < 15 {
				name := strings.TrimSpace(m[2])
				if name == "" {
					name = "test " + m[1]
				}
				failLines = append(failLines, truncateRunes(name, 160))
			}
		}
	}

	if tests < 0 && failed < 0 && len(failLines) == 0 {
		return ir.RawReport("node", raw, exit)
	}
	if failed < 0 {
		failed = len(failLines)
	}
	if tests < 0 {
		tests = 0
	}

	status := ir.StatusOK
	if failed > 0 || exit != 0 {
		status = ir.StatusFail
	}

	var results []ir.TestResult
	for _, fl := range failLines {
		results = append(results, ir.TestResult{Name: fl, Status: ir.StatusFail})
	}
	return ir.Report{
		Tool: "node", Subcommand: "--test",
		Summary: fmt.Sprintf("%d tests, %d failed", tests, failed),
		Status:  status, Tests: results, Filtered: true, Raw: raw,
	}
}

// nodeScriptReport passes program output through unless an uncaught Node error
// with a stack trace is present, in which case it compacts to error + top frames.
func nodeScriptReport(c engine.CaptureResult, raw string) ir.Report {
	stderr := engine.StripANSI(c.Stderr)
	header, msg, frames, ok := extractNodeTrace(stderr)
	if !ok {
		// Clean (or no recognizable trace) — passthrough.
		return ir.RawReport("node", raw, c.ExitCode)
	}

	var b strings.Builder
	b.WriteString(header)
	if msg != "" {
		b.WriteString(": ")
		b.WriteString(msg)
	}
	for _, f := range frames {
		b.WriteString("\n")
		b.WriteString(f)
	}

	diag := ir.Diagnostic{Severity: ir.SevError, Rule: header, Message: msg, Context: frames}
	return ir.Report{
		Tool: "node", Summary: header, Status: ir.StatusFail,
		Diagnostics: []ir.Diagnostic{diag}, Text: b.String(), Filtered: true, Raw: raw,
	}
}

// extractNodeTrace finds the error header + message and the top non-noise
// frames (capped at maxNodeFrames). ok is false when no trace is recognizable.
func extractNodeTrace(stderr string) (header, msg string, frames []string, ok bool) {
	lines := splitLines(stderr)
	for i, l := range lines {
		m := nodeErrHeaderRE.FindStringSubmatch(strings.TrimSpace(l))
		if m == nil {
			continue
		}
		header, msg = m[1], strings.TrimSpace(m[2])
		// Collect frames following the header, skipping node_modules/internal.
		for _, fl := range lines[i+1:] {
			if !nodeFrameRE.MatchString(fl) {
				continue
			}
			t := strings.TrimSpace(fl)
			if nodeNoiseFrameRE.MatchString(t) {
				continue
			}
			if len(frames) >= maxNodeFrames {
				break
			}
			frames = append(frames, truncateRunes(t, 160))
		}
		return header, msg, frames, true
	}
	return "", "", nil, false
}
