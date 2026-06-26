package js

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

const maxEslintDiags = 50

type eslint struct{}

func (eslint) Tool() string       { return "eslint" }
func (eslint) Subcommand() string { return "" }

func (eslint) Exec(o registry.Opts) engine.CaptureResult {
	args := o.Args
	if !hasFlag(args, "--format", "-f") {
		args = append(append([]string{}, args...), "--format", "json")
	}
	return engine.Capture(pkgExec("eslint", args))
}

type eslintFile struct {
	FilePath string `json:"filePath"`
	Messages []struct {
		RuleID   string `json:"ruleId"`
		Severity int    `json:"severity"`
		Message  string `json:"message"`
		Line     int    `json:"line"`
		Column   int    `json:"column"`
	} `json:"messages"`
}

func (eslint) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	js := strings.TrimSpace(c.Stdout)
	if i := strings.IndexByte(js, '['); i > 0 {
		js = js[i:] // skip any leading noise
	}
	var files []eslintFile
	if js == "" || json.Unmarshal([]byte(js), &files) != nil {
		return ir.Report{Filtered: false, Raw: rawOf(c), ExitCode: c.ExitCode}, nil
	}

	var diags []ir.Diagnostic
	errors, warnings := 0, 0
	for _, f := range files {
		for _, m := range f.Messages {
			if m.Severity == 2 {
				errors++
			} else {
				warnings++
			}
			if len(diags) >= maxEslintDiags {
				continue
			}
			sev := ir.SevWarning
			if m.Severity == 2 {
				sev = ir.SevError
			}
			diags = append(diags, ir.Diagnostic{
				File: shortenPath(f.FilePath), Line: m.Line, Col: m.Column,
				Severity: sev, Rule: m.RuleID, Message: m.Message,
			})
		}
	}

	if errors == 0 && warnings == 0 {
		return ir.Report{Tool: "eslint", Summary: "ok (no problems)", Status: ir.StatusOK, Filtered: true, Raw: rawOf(c)}, nil
	}
	status := ir.StatusWarn
	if errors > 0 {
		status = ir.StatusFail
	}
	return ir.Report{
		Tool: "eslint", Summary: fmt.Sprintf("%d errors, %d warnings", errors, warnings),
		Status: status, Diagnostics: diags, Filtered: true, Raw: rawOf(c),
	}, nil
}

func hasFlag(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n || strings.HasPrefix(a, n+"=") {
				return true
			}
		}
	}
	return false
}

func shortenPath(p string) string {
	for _, root := range []string{"/src/", "/app/", "/lib/", "/tests/", "/test/"} {
		if i := strings.LastIndex(p, root); i >= 0 {
			return p[i+1:]
		}
	}
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
