package js

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

// jsTest is a failures-only filter for JS test runners (jest, vitest,
// playwright), which share similar console reporters.
type jsTest struct{ tool string }

func (j jsTest) Tool() string     { return j.tool }
func (jsTest) Subcommand() string { return "" }

func (j jsTest) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(pkgExec(j.tool, o.Args))
}

// countsRE captures "<n> failed" / "<n> passed" from a summary line.
var (
	failedRE = regexp.MustCompile(`(\d+) failed`)
	passedRE = regexp.MustCompile(`(\d+) passed`)
)

func (j jsTest) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	return parseJSTest(j.tool, engine.StripANSI(c.Stdout+"\n"+c.Stderr), c.ExitCode, rawOf(c)), nil
}

func parseJSTest(tool, out string, exitCode int, raw string) ir.Report {
	passed, failed := -1, -1
	var failLines []string

	for _, l := range splitLines(out) {
		t := strings.TrimSpace(l)
		// Summary line: contains both passed and (failed|total).
		if (strings.HasPrefix(t, "Tests") || strings.HasPrefix(t, "Tests:")) &&
			(strings.Contains(t, "passed") || strings.Contains(t, "failed")) {
			if m := passedRE.FindStringSubmatch(t); m != nil {
				passed, _ = strconv.Atoi(m[1])
			}
			if m := failedRE.FindStringSubmatch(t); m != nil {
				failed, _ = strconv.Atoi(m[1])
			}
			continue
		}
		// Failing test markers across the three runners.
		if strings.HasPrefix(t, "✕") || strings.HasPrefix(t, "✗") || strings.HasPrefix(t, "×") ||
			strings.HasPrefix(t, "●") || strings.HasPrefix(t, "FAIL ") {
			if len(failLines) < 15 {
				failLines = append(failLines, truncateRunes(t, 160))
			}
		}
	}

	if passed < 0 && failed < 0 {
		// Couldn't find a summary — fall back to raw.
		return ir.Report{Filtered: false, Raw: raw, ExitCode: exitCode}
	}
	if failed < 0 {
		failed = 0
	}
	if passed < 0 {
		passed = 0
	}

	status := ir.StatusOK
	summary := strconv.Itoa(passed) + " passed"
	if failed > 0 {
		status = ir.StatusFail
		summary += ", " + strconv.Itoa(failed) + " failed"
	}

	var tests []ir.TestResult
	for _, fl := range failLines {
		tests = append(tests, ir.TestResult{Name: fl, Status: ir.StatusFail})
	}
	return ir.Report{
		Tool: tool, Summary: summary, Status: status, Tests: tests,
		Filtered: true, Raw: raw,
	}
}
