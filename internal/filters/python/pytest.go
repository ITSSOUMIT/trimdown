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

var (
	pytestCountRE   = regexp.MustCompile(`(\d+) (passed|failed|skipped|errors?|xfailed|xpassed|deselected|warnings?)`)
	pytestFailHdrRE = regexp.MustCompile(`^_{3,} (.+?) _{3,}$`)
)

const (
	maxPytestFailures = 10
	maxFailDetail     = 4
)

type pytest struct{}

func (pytest) Tool() string       { return "pytest" }
func (pytest) Subcommand() string { return "" }

func (pytest) Exec(o registry.Opts) engine.CaptureResult {
	args := o.Args
	if len(args) > 0 && args[0] == "pytest" { // when invoked as `pytest pytest ...` (rare)
		args = args[1:]
	}
	inject := []string{}
	if !hasAnyPrefix(args, "-q", "--quiet") {
		inject = append(inject, "-q")
	}
	if !hasAnyPrefix(args, "--tb") {
		inject = append(inject, "--tb=short")
	}
	full := append(inject, args...)
	return engine.Capture(pyCommand("pytest", []string{"-m", "pytest"}, full))
}

func (pytest) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	return parsePytest(c.Stdout+"\n"+c.Stderr, c.ExitCode, rawOf(c)), nil
}

func parsePytest(out string, exitCode int, raw string) ir.Report {
	counts := map[string]int{}
	inFailures := false
	var failures []ir.TestResult
	var cur *ir.TestResult

	flush := func() {
		if cur != nil && len(failures) < maxPytestFailures {
			failures = append(failures, *cur)
		}
		cur = nil
	}

	for _, l := range splitLines(out) {
		switch {
		case strings.Contains(l, "= FAILURES ="):
			inFailures = true
			continue
		case strings.Contains(l, "= short test summary") || strings.Contains(l, "= ERRORS ="):
			flush()
			inFailures = false
			continue
		case isSummaryLine(l):
			for _, m := range pytestCountRE.FindAllStringSubmatch(l, -1) {
				n, _ := strconv.Atoi(m[1])
				counts[normalizeOutcome(m[2])] = n
			}
			flush()
			inFailures = false
			continue
		}

		if inFailures {
			if m := pytestFailHdrRE.FindStringSubmatch(l); m != nil {
				flush()
				cur = &ir.TestResult{Name: m[1], Status: ir.StatusFail}
				continue
			}
			if cur != nil && len(cur.Detail) < maxFailDetail {
				t := strings.TrimSpace(l)
				if strings.HasPrefix(t, "E ") || strings.HasPrefix(t, ">") || strings.Contains(l, ".py:") {
					cur.Detail = append(cur.Detail, strings.TrimRight(l, " "))
				}
			}
		}
	}
	flush()

	// Quiet runs may print only "2 failed, 6 passed in 0.1s" without "=" banners.
	if len(counts) == 0 {
		for _, l := range splitLines(out) {
			if strings.Contains(l, " in ") && pytestCountRE.MatchString(l) {
				for _, m := range pytestCountRE.FindAllStringSubmatch(l, -1) {
					n, _ := strconv.Atoi(m[1])
					counts[normalizeOutcome(m[2])] = n
				}
			}
		}
	}

	failed := counts["failed"] + counts["error"]
	if counts["passed"]+failed+counts["skipped"] == 0 {
		// Couldn't parse — fall back to raw.
		return ir.Report{Filtered: false, Raw: raw, ExitCode: exitCode}
	}

	status := ir.StatusOK
	if failed > 0 {
		status = ir.StatusFail
	}
	return ir.Report{
		Tool:     "pytest",
		Summary:  pytestSummary(counts),
		Status:   status,
		Tests:    failures,
		Filtered: true,
		Raw:      raw,
	}
}

func isSummaryLine(l string) bool {
	return strings.HasPrefix(l, "=") &&
		(strings.Contains(l, "passed") || strings.Contains(l, "failed") || strings.Contains(l, "error")) &&
		strings.Contains(l, " in ")
}

func normalizeOutcome(s string) string {
	switch s {
	case "errors":
		return "error"
	case "warnings", "warning":
		return "warning"
	default:
		return s
	}
}

func pytestSummary(counts map[string]int) string {
	var parts []string
	for _, k := range []string{"passed", "failed", "error", "skipped", "xfailed", "xpassed"} {
		if counts[k] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", counts[k], k))
		}
	}
	if len(parts) == 0 {
		return "no tests"
	}
	return strings.Join(parts, ", ")
}
