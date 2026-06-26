package ruby

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

// minitestSummaryRE matches "N runs, N assertions, N failures, N errors, N skips".
var minitestSummaryRE = regexp.MustCompile(`(\d+) (runs?|assertions?|failures?|errors?|skips?)`)

// minitestFailHdrRE matches "  1) Failure:" / "  2) Error:".
var minitestFailHdrRE = regexp.MustCompile(`^\s*\d+\) (Failure|Error):`)

const maxRakeFailures = 10

type rake struct{}

func (rake) Tool() string       { return "rake" }
func (rake) Subcommand() string { return "" }

func (rake) Exec(o registry.Opts) engine.CaptureResult {
	// Use `rails test` when explicit test file paths are given, else `rake`.
	tool, args := "rake", o.Args
	if firstNonFlag(o.Args) == "test" && hasTestPath(o.Args) {
		tool = "rails"
	}
	return engine.Capture(rubyExec(tool, args))
}

func (rake) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	return parseMinitest(engine.StripANSI(c.Stdout+"\n"+c.Stderr), c.ExitCode, rawOf(c)), nil
}

func parseMinitest(out string, exitCode int, raw string) ir.Report {
	counts := map[string]int{}
	var failures []ir.TestResult
	var cur *ir.TestResult
	inFailures := false

	flush := func() {
		if cur != nil && len(failures) < maxRakeFailures {
			failures = append(failures, *cur)
		}
		cur = nil
	}

	for _, l := range splitLines(out) {
		if strings.HasPrefix(strings.TrimSpace(l), "Finished in ") {
			inFailures = true
		}
		if isMinitestSummary(l) {
			for _, m := range minitestSummaryRE.FindAllStringSubmatch(l, -1) {
				n, _ := strconv.Atoi(m[1])
				counts[singular(m[2])] = n
			}
			continue
		}
		if inFailures {
			if minitestFailHdrRE.MatchString(l) {
				flush()
				cur = &ir.TestResult{Name: strings.TrimSpace(l), Status: ir.StatusFail}
				continue
			}
			if cur != nil && strings.TrimSpace(l) != "" && len(cur.Detail) < 4 {
				cur.Detail = append(cur.Detail, strings.TrimRight(l, " "))
			}
		}
	}
	flush()

	runs := counts["run"]
	failed := counts["failure"] + counts["error"]
	if runs == 0 && failed == 0 {
		return ir.Report{Filtered: false, Raw: raw, ExitCode: exitCode}
	}

	status := ir.StatusOK
	summary := fmt.Sprintf("%d runs, %d failures", runs, counts["failure"])
	if failed > 0 {
		status = ir.StatusFail
	}
	if counts["error"] > 0 {
		summary += fmt.Sprintf(", %d errors", counts["error"])
	}
	if counts["skip"] > 0 {
		summary += fmt.Sprintf(", %d skips", counts["skip"])
	}
	return ir.Report{
		Tool: "rake", Summary: summary, Status: status, Tests: failures,
		Filtered: true, Raw: raw,
	}
}

func isMinitestSummary(l string) bool {
	return (strings.Contains(l, " runs,") || strings.Contains(l, " run,") ||
		strings.Contains(l, " tests,")) && strings.Contains(l, "assertion")
}

func singular(s string) string {
	switch s {
	case "runs", "run":
		return "run"
	case "assertions", "assertion":
		return "assertion"
	case "failures", "failure":
		return "failure"
	case "errors", "error":
		return "error"
	case "skips", "skip":
		return "skip"
	}
	return s
}

func firstNonFlag(args []string) string {
	for _, a := range args {
		if a != "" && a[0] != '-' {
			return a
		}
	}
	return ""
}

func hasTestPath(args []string) bool {
	for _, a := range args {
		if a == "test" || strings.HasPrefix(a, "-") || strings.Contains(a, "=") {
			continue
		}
		if strings.HasSuffix(a, ".rb") || strings.HasPrefix(a, "test/") || strings.HasPrefix(a, "spec/") {
			return true
		}
	}
	return false
}
