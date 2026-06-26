package ruby

import (
	"encoding/json"
	"fmt"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

const maxRubocopDiags = 50

type rubocop struct{}

func (rubocop) Tool() string       { return "rubocop" }
func (rubocop) Subcommand() string { return "" }

func (rubocop) Exec(o registry.Opts) engine.CaptureResult {
	args := o.Args
	// In autocorrect mode JSON isn't meaningful; pass through.
	if !hasAny(args, "-a", "-A", "--auto-correct", "--autocorrect", "--autocorrect-all") && !hasFormat(args) {
		args = append(append([]string{}, args...), "--format", "json")
	}
	return engine.Capture(rubyExec("rubocop", args))
}

type rubocopJSON struct {
	Files []struct {
		Path     string `json:"path"`
		Offenses []struct {
			CopName  string `json:"cop_name"`
			Severity string `json:"severity"`
			Message  string `json:"message"`
			Location struct {
				StartLine   int `json:"start_line"`
				StartColumn int `json:"start_column"`
			} `json:"location"`
		} `json:"offenses"`
	} `json:"files"`
	Summary struct {
		OffenseCount    int `json:"offense_count"`
		TargetFileCount int `json:"target_file_count"`
	} `json:"summary"`
}

func (rubocop) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	if hasAny(o.Args, "-a", "-A", "--auto-correct", "--autocorrect", "--autocorrect-all") {
		return ir.Report{Filtered: false, Raw: rawOf(c), ExitCode: c.ExitCode}, nil
	}
	js := extractJSON(c.Stdout)
	var data rubocopJSON
	if js == "" || json.Unmarshal([]byte(js), &data) != nil {
		return ir.Report{Filtered: false, Raw: rawOf(c), ExitCode: c.ExitCode}, nil
	}

	if data.Summary.OffenseCount == 0 {
		return ir.Report{
			Tool: "rubocop", Summary: fmt.Sprintf("ok (%d files)", data.Summary.TargetFileCount),
			Status: ir.StatusOK, Filtered: true, Raw: rawOf(c),
		}, nil
	}

	var diags []ir.Diagnostic
	for _, f := range data.Files {
		for _, off := range f.Offenses {
			if len(diags) >= maxRubocopDiags {
				break
			}
			diags = append(diags, ir.Diagnostic{
				File: f.Path, Line: off.Location.StartLine, Col: off.Location.StartColumn,
				Severity: rubocopSeverity(off.Severity), Rule: off.CopName, Message: off.Message,
			})
		}
	}
	return ir.Report{
		Tool: "rubocop", Summary: fmt.Sprintf("%d offenses (%d files)", data.Summary.OffenseCount, data.Summary.TargetFileCount),
		Status: ir.StatusWarn, Diagnostics: diags, Filtered: true, Raw: rawOf(c),
	}, nil
}

func rubocopSeverity(s string) ir.Severity {
	switch s {
	case "error", "fatal":
		return ir.SevError
	case "warning":
		return ir.SevWarning
	default:
		return ir.SevInfo
	}
}
