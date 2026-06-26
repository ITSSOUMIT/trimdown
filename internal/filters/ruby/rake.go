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

// rakeTaskRE matches a `rake -T` listing row: "rake db:migrate  # Migrate...".
var rakeTaskRE = regexp.MustCompile(`^rake\s+(\S+)\s*#\s*(.*)$`)

const maxRakeFailures = 10

// dbTaskPrefixes are rake/rails db:* tasks routed through the migration parser.
var dbTaskPrefixes = []string{
	"db:migrate", "db:rollback", "db:seed", "db:create", "db:drop",
	"db:setup", "db:prepare", "db:reset", "db:schema:load", "db:schema:dump",
	"db:version",
}

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

func (rake) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	out := engine.StripANSI(c.Stdout + "\n" + c.Stderr)
	raw := rawOf(c)
	sub := firstNonFlag(o.Args)

	switch {
	case hasAny(o.Args, "-T", "--tasks", "-AT", "-A"):
		return parseRakeTasks(out, c.ExitCode, raw), nil
	case sub == "test" || sub == "":
		// Default task and `rake test` → minitest.
		return parseMinitest(out, c.ExitCode, raw), nil
	case isDBTask(sub):
		return parseMigration(out, sub, "rake", c.ExitCode, raw), nil
	case sub == "assets:precompile":
		return parseAssetsPrecompile(out, "rake", c.ExitCode, raw), nil
	case sub == "routes":
		return parseRoutes(out, "rake", c.ExitCode, raw), nil
	default:
		return ir.RawReport("rake", raw, c.ExitCode), nil
	}
}

func isDBTask(sub string) bool {
	for _, p := range dbTaskPrefixes {
		if sub == p {
			return true
		}
	}
	return false
}

// parseRakeTasks compacts `rake -T` task listings into task→description items.
func parseRakeTasks(out string, exitCode int, raw string) ir.Report {
	var items []ir.Item
	total := 0
	for _, l := range splitLines(out) {
		m := rakeTaskRE.FindStringSubmatch(strings.TrimRight(l, " "))
		if m == nil {
			continue
		}
		total++
		if len(items) >= maxTasks {
			continue
		}
		items = append(items, ir.Item{Key: m[1], Val: strings.TrimSpace(m[2])})
	}
	if total == 0 {
		return ir.RawReport("rake", raw, exitCode)
	}
	if total > len(items) {
		items = append(items, ir.Item{Key: fmt.Sprintf("+%d more", total-len(items))})
	}
	noun := "task"
	if total != 1 {
		noun = "tasks"
	}
	return ir.Report{
		Tool: "rake", Summary: fmt.Sprintf("%d %s", total, noun), Status: ir.StatusOK,
		Items: items, Filtered: true, Raw: raw, ExitCode: exitCode,
	}
}

// railsTest handles `rails test` directly (the way Rails devs invoke it),
// reusing the minitest parser. Other `rails` subcommands (server, db:migrate,
// console, …) aren't registered, so they pass through unfiltered.
type railsTest struct{}

func (railsTest) Tool() string       { return "rails" }
func (railsTest) Subcommand() string { return "test" }

func (railsTest) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(rubyExec("rails", o.Args))
}

func (railsTest) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
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
