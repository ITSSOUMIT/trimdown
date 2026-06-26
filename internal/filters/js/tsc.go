package js

import (
	"fmt"
	"regexp"
	"strconv"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

// tscRE matches "path(line,col): error TS1234: message".
var tscRE = regexp.MustCompile(`^(.+?)\((\d+),(\d+)\): (error|warning) (TS\d+): (.+)$`)

const maxTscDiags = 50

type tsc struct{}

func (tsc) Tool() string       { return "tsc" }
func (tsc) Subcommand() string { return "" }

func (tsc) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(pkgExec("tsc", o.Args))
}

func (tsc) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	var diags []ir.Diagnostic
	overflow := 0
	engine.ScanLines(engine.StripANSI(c.Stdout), func(line string) {
		m := tscRE.FindStringSubmatch(line)
		if m == nil {
			return
		}
		if len(diags) >= maxTscDiags {
			overflow++
			return
		}
		ln, _ := strconv.Atoi(m[2])
		col, _ := strconv.Atoi(m[3])
		sev := ir.SevError
		if m[4] == "warning" {
			sev = ir.SevWarning
		}
		diags = append(diags, ir.Diagnostic{
			File: m[1], Line: ln, Col: col, Severity: sev, Rule: m[5], Message: m[6],
		})
	})

	if len(diags) == 0 {
		return ir.Report{Tool: "tsc", Summary: "ok (no errors)", Status: ir.StatusOK, Filtered: true, Raw: rawOf(c)}, nil
	}
	var notes []string
	if overflow > 0 {
		notes = append(notes, fmt.Sprintf("… +%d more", overflow))
	}
	return ir.Report{
		Tool: "tsc", Summary: fmt.Sprintf("%d errors", len(diags)+overflow),
		Status: ir.StatusFail, Diagnostics: diags, Notes: notes, Filtered: true, Raw: rawOf(c),
	}, nil
}
