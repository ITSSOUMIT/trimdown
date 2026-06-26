package ruby

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

const maxRspecFailures = 10

type rspec struct{}

func (rspec) Tool() string       { return "rspec" }
func (rspec) Subcommand() string { return "" }

func (rspec) Exec(o registry.Opts) engine.CaptureResult {
	args := o.Args
	if !hasFormat(args) {
		args = append(append([]string{}, args...), "--format", "json")
	}
	return engine.Capture(rubyExec("rspec", args))
}

func hasFormat(args []string) bool {
	for i, a := range args {
		if a == "--format" || a == "-f" {
			return true
		}
		if strings.HasPrefix(a, "--format=") || (strings.HasPrefix(a, "-f") && len(a) > 2) {
			return true
		}
		_ = i
	}
	return false
}

type rspecJSON struct {
	Examples []struct {
		FullDescription string `json:"full_description"`
		Status          string `json:"status"`
		FilePath        string `json:"file_path"`
		LineNumber      int    `json:"line_number"`
		Exception       *struct {
			Class   string `json:"class"`
			Message string `json:"message"`
		} `json:"exception"`
	} `json:"examples"`
	Summary struct {
		ExampleCount int `json:"example_count"`
		FailureCount int `json:"failure_count"`
		PendingCount int `json:"pending_count"`
	} `json:"summary"`
}

func (rspec) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	js := extractJSON(c.Stdout)
	var data rspecJSON
	if js == "" || json.Unmarshal([]byte(js), &data) != nil {
		return ir.Report{Filtered: false, Raw: rawOf(c), ExitCode: c.ExitCode}, nil
	}

	var failures []ir.TestResult
	for _, e := range data.Examples {
		if e.Status != "failed" {
			continue
		}
		if len(failures) >= maxRspecFailures {
			break
		}
		detail := []string{fmt.Sprintf("%s:%d", e.FilePath, e.LineNumber)}
		if e.Exception != nil {
			detail = append(detail, shortClass(e.Exception.Class)+": "+firstLine(e.Exception.Message))
		}
		failures = append(failures, ir.TestResult{Name: e.FullDescription, Status: ir.StatusFail, Detail: detail})
	}

	passed := data.Summary.ExampleCount - data.Summary.FailureCount - data.Summary.PendingCount
	status := ir.StatusOK
	if data.Summary.FailureCount > 0 {
		status = ir.StatusFail
	}
	summary := fmt.Sprintf("%d passed", passed)
	if data.Summary.FailureCount > 0 {
		summary += fmt.Sprintf(", %d failed", data.Summary.FailureCount)
	}
	if data.Summary.PendingCount > 0 {
		summary += fmt.Sprintf(", %d pending", data.Summary.PendingCount)
	}
	return ir.Report{
		Tool: "rspec", Summary: summary, Status: status, Tests: failures,
		Filtered: true, Raw: rawOf(c),
	}, nil
}

func shortClass(c string) string {
	if i := strings.LastIndex(c, "::"); i >= 0 {
		return c[i+2:]
	}
	return c
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return strings.TrimSpace(s[:i])
	}
	return s
}
