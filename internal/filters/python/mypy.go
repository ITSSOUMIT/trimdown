package python

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

// mypyRE matches "file:line[:col]: severity: message [code]".
var mypyRE = regexp.MustCompile(`^(.+?):(\d+)(?::\d+)?: (error|warning|note): (.+?)(?:\s+\[([^\]]+)\])?$`)

const maxMypyDiags = 50

type mypy struct{}

func (mypy) Tool() string       { return "mypy" }
func (mypy) Subcommand() string { return "" }

func (mypy) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(pyCommand("mypy", []string{"-m", "mypy"}, o.Args))
}

func (mypy) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	var diags []ir.Diagnostic
	errors, warnings := 0, 0

	for _, l := range splitLines(engine.StripANSI(c.Stdout)) {
		m := mypyRE.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		sev := m[3]
		if sev == "note" {
			// Attach notes to the previous diagnostic as context.
			if n := len(diags); n > 0 {
				diags[n-1].Context = append(diags[n-1].Context, m[4])
			}
			continue
		}
		if sev == "error" {
			errors++
		} else {
			warnings++
		}
		if len(diags) >= maxMypyDiags {
			continue
		}
		ln, _ := strconv.Atoi(m[2])
		diags = append(diags, ir.Diagnostic{
			File: m[1], Line: ln, Severity: severityOf(sev), Rule: m[5], Message: m[4],
		})
	}

	if errors == 0 && warnings == 0 {
		return ir.Report{Tool: "mypy", Summary: "ok", Status: ir.StatusOK, Filtered: true, Raw: rawOf(c)}, nil
	}
	status := ir.StatusWarn
	if errors > 0 {
		status = ir.StatusFail
	}
	return ir.Report{
		Tool:        "mypy",
		Summary:     fmt.Sprintf("%d errors, %d warnings", errors, warnings),
		Status:      status,
		Diagnostics: diags,
		Filtered:    true,
		Raw:         rawOf(c),
	}, nil
}

func severityOf(s string) ir.Severity {
	if s == "warning" {
		return ir.SevWarning
	}
	return ir.SevError
}
