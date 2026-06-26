package golang

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

// golangciRE matches the default text format: "file:line:col: message (linter)".
// We parse text (not JSON) to stay robust across golangci-lint v1/v2 flag churn.
var golangciRE = regexp.MustCompile(`^(.+?):(\d+):(\d+): (.+?) \((\S+)\)\s*$`)

const maxGolangciIssues = 40

type golangci struct{}

func (golangci) Tool() string       { return "golangci-lint" }
func (golangci) Subcommand() string { return "run" }

func (golangci) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand("golangci-lint", o.Args...))
}

func (golangci) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	src := c.Stdout + "\n" + c.Stderr
	var diags []ir.Diagnostic
	byLinter := map[string]int{}
	overflow := 0

	engine.ScanLines(src, func(line string) {
		m := golangciRE.FindStringSubmatch(line)
		if m == nil {
			return
		}
		byLinter[m[5]]++
		if len(diags) >= maxGolangciIssues {
			overflow++
			return
		}
		ln, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		diags = append(diags, ir.Diagnostic{
			File: m[1], Line: ln, Col: col, Severity: ir.SevWarning, Rule: m[5], Message: m[4],
		})
	})

	if len(diags) == 0 {
		return ir.Report{Tool: "golangci-lint", Subcommand: "run", Summary: "ok (no issues)", Status: ir.StatusOK, Filtered: true, Raw: rawOf(c)}, nil
	}

	var notes []string
	notes = append(notes, "by linter: "+topLinters(byLinter))
	if overflow > 0 {
		notes = append(notes, fmt.Sprintf("… +%d more issues", overflow))
	}
	return ir.Report{
		Tool:        "golangci-lint",
		Subcommand:  "run",
		Summary:     fmt.Sprintf("%d issues", len(diags)+overflow),
		Status:      ir.StatusWarn,
		Diagnostics: diags,
		Notes:       notes,
		Filtered:    true,
		Raw:         rawOf(c),
	}, nil
}

func topLinters(m map[string]int) string {
	type kv struct {
		k string
		v int
	}
	var s []kv
	for k, v := range m {
		s = append(s, kv{k, v})
	}
	// simple insertion sort by count desc (small maps)
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j].v > s[j-1].v; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
	var parts []string
	for i, e := range s {
		if i >= 5 {
			break
		}
		parts = append(parts, fmt.Sprintf("%s ×%d", e.k, e.v))
	}
	return strings.Join(parts, ", ")
}
