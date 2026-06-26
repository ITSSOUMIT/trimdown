package ruby

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

const maxBacktraceFrames = 5

// rubyExcRE matches the head of a Ruby exception:
//
//	app/foo.rb:10:in 'bar': something went wrong (RuntimeError)
//	-e:1:in '<main>': boom (RuntimeError)
//
// Group 1 = location, 2 = message, 3 = exception class.
var rubyExcRE = regexp.MustCompile(`^(\S+:\d+(?::in\s+[^:]+)?):\s+(.*?)\s+\(([A-Z]\w*(?:::\w+)*(?:Error|Exception)?)\)\s*$`)

// rubyFrameRE matches a backtrace continuation frame: "\tfrom app/baz.rb:5:in 'qux'".
var rubyFrameRE = regexp.MustCompile(`^\s*from\s+(.+)$`)

// rubySyntaxErrRE matches a ruby -c / load-time syntax error line.
var rubySyntaxErrRE = regexp.MustCompile(`SyntaxError|syntax error`)

type rubyInterp struct{}

func (rubyInterp) Tool() string       { return "ruby" }
func (rubyInterp) Subcommand() string { return "" }

func (rubyInterp) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(rubyExec("ruby", o.Args))
}

func (rubyInterp) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	out := engine.StripANSI(c.Stdout + "\n" + c.Stderr)
	raw := rawOf(c)

	switch {
	case hasAny(o.Args, "-c"):
		return parseRubySyntaxCheck(out, c.ExitCode, raw), nil
	case hasAny(o.Args, "-v", "--version"):
		v := firstNonEmptyLine(out)
		if v == "" {
			return ir.RawReport("ruby", raw, c.ExitCode), nil
		}
		return ir.Report{
			Tool: "ruby", Summary: v, Status: ir.StatusOK,
			Filtered: true, Raw: raw, ExitCode: c.ExitCode,
		}, nil
	case hasAny(o.Args, "-e"):
		// Inline code: arbitrary output → passthrough (but still surface a
		// crash if one occurred).
		if rep, ok := compactRubyBacktrace(out, c.ExitCode, raw); ok {
			return rep, nil
		}
		return ir.RawReport("ruby", raw, c.ExitCode), nil
	default:
		// script.rb run (or flags-only): passthrough unless it crashed.
		if rep, ok := compactRubyBacktrace(out, c.ExitCode, raw); ok {
			return rep, nil
		}
		return ir.RawReport("ruby", raw, c.ExitCode), nil
	}
}

func parseRubySyntaxCheck(out string, exitCode int, raw string) ir.Report {
	if exitCode == 0 && strings.Contains(out, "Syntax OK") {
		return ir.Report{
			Tool: "ruby", Summary: "syntax OK", Status: ir.StatusOK,
			Filtered: true, Raw: raw, ExitCode: exitCode,
		}
	}
	var diags []ir.Diagnostic
	for _, l := range splitLines(out) {
		t := strings.TrimSpace(l)
		if t == "" || t == "Syntax OK" {
			continue
		}
		if rubySyntaxErrRE.MatchString(t) || strings.Contains(t, ".rb:") || strings.HasPrefix(t, "-:") {
			if len(diags) < 20 {
				diags = append(diags, ir.Diagnostic{Severity: ir.SevError, Message: t})
			}
		}
	}
	return ir.Report{
		Tool: "ruby", Summary: "syntax error", Status: ir.StatusFail,
		Diagnostics: diags, Filtered: true, Raw: raw, ExitCode: exitCode,
	}
}

// compactRubyBacktrace looks for a Ruby exception + backtrace. If found, it
// returns a compacted report (class + message + top frames) and ok=true.
// Otherwise ok=false and the caller should pass through.
func compactRubyBacktrace(out string, exitCode int, raw string) (ir.Report, bool) {
	lines := splitLines(out)
	idx := -1
	var m []string
	for i, l := range lines {
		if mm := rubyExcRE.FindStringSubmatch(l); mm != nil {
			idx = i
			m = mm
			break
		}
	}
	if idx < 0 {
		return ir.Report{}, false
	}

	loc, msg, class := m[1], m[2], m[3]

	var frames []string
	frames = append(frames, loc) // the raising location is the first frame
	for _, l := range lines[idx+1:] {
		fm := rubyFrameRE.FindStringSubmatch(l)
		if fm == nil {
			// Backtrace ends at the first non-"from" line.
			if strings.TrimSpace(l) == "" {
				continue
			}
			break
		}
		if len(frames) >= maxBacktraceFrames {
			break
		}
		frames = append(frames, strings.TrimSpace(fm[1]))
	}

	summary := fmt.Sprintf("%s: %s", class, msg)

	diag := ir.Diagnostic{Severity: ir.SevError, Rule: class, Message: msg, Context: frames}
	return ir.Report{
		Tool: "ruby", Summary: summary, Status: ir.StatusFail,
		Diagnostics: []ir.Diagnostic{diag},
		Filtered:    true, Raw: raw, ExitCode: exitCode,
	}, true
}
