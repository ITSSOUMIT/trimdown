package python

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

type ruff struct{}

func (ruff) Tool() string       { return "ruff" }
func (ruff) Subcommand() string { return "" }

func (ruff) Exec(o registry.Opts) engine.CaptureResult {
	args := o.Args
	sub := firstArg(args)
	if sub == "format" {
		return engine.Capture(pyCommand("ruff", []string{"-m", "ruff"}, args))
	}
	// check mode (default): request JSON unless the user chose a format.
	if !hasAnyPrefix(args, "--output-format", "--format") {
		args = append(append([]string{}, args...), "--output-format=json")
	}
	return engine.Capture(pyCommand("ruff", []string{"-m", "ruff"}, args))
}

type ruffViolation struct {
	Code     string `json:"code"`
	Message  string `json:"message"`
	Filename string `json:"filename"`
	Location struct {
		Row    int `json:"row"`
		Column int `json:"column"`
	} `json:"location"`
	Fix json.RawMessage `json:"fix"`
}

func (ruff) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	if firstArg(o.Args) == "format" {
		return parseRuffFormat(c), nil
	}
	var vios []ruffViolation
	if err := json.Unmarshal([]byte(strings.TrimSpace(c.Stdout)), &vios); err != nil {
		// Not JSON (e.g. ruff error) — show raw.
		return ir.Report{Filtered: false, Raw: rawOf(c), ExitCode: c.ExitCode}, nil
	}
	if len(vios) == 0 {
		return ir.Report{Tool: "ruff", Summary: "ok (no issues)", Status: ir.StatusOK, Filtered: true, Raw: rawOf(c)}, nil
	}

	fixable := 0
	diags := make([]ir.Diagnostic, 0, len(vios))
	for _, v := range vios {
		if len(v.Fix) > 0 && string(v.Fix) != "null" {
			fixable++
		}
		diags = append(diags, ir.Diagnostic{
			File: shortenPath(v.Filename), Line: v.Location.Row, Col: v.Location.Column,
			Severity: ir.SevWarning, Rule: v.Code, Message: v.Message,
		})
	}
	var notes []string
	if fixable > 0 {
		notes = append(notes, fmt.Sprintf("%d fixable — run `ruff check --fix`", fixable))
	}
	return ir.Report{
		Tool:        "ruff",
		Summary:     fmt.Sprintf("%d issues", len(vios)),
		Status:      ir.StatusWarn,
		Diagnostics: diags,
		Notes:       notes,
		Filtered:    true,
		Raw:         rawOf(c),
	}, nil
}

func parseRuffFormat(c engine.CaptureResult) ir.Report {
	var files []string
	for _, l := range splitLines(c.Stdout) {
		if i := strings.Index(strings.ToLower(l), "would reformat:"); i >= 0 {
			files = append(files, strings.TrimSpace(l[i+len("would reformat:"):]))
		}
	}
	if len(files) == 0 {
		return ir.Report{Tool: "ruff", Summary: "format: ok", Status: ir.StatusOK, Filtered: true, Raw: rawOf(c)}
	}
	return ir.Report{
		Tool:     "ruff",
		Summary:  fmt.Sprintf("%d files need formatting", len(files)),
		Status:   ir.StatusWarn,
		Items:    toItems(files),
		Filtered: true,
		Raw:      rawOf(c),
	}
}

func toItems(ss []string) []ir.Item {
	out := make([]ir.Item, 0, len(ss))
	for _, s := range ss {
		out = append(out, ir.Item{Key: s})
	}
	return out
}

// shortenPath trims a long absolute path to the segment under a recognizable
// source root (src/, lib/, tests/), else the basename.
func shortenPath(p string) string {
	for _, root := range []string{"/src/", "/lib/", "/tests/", "/app/"} {
		if i := strings.LastIndex(p, root); i >= 0 {
			return p[i+1:]
		}
	}
	if i := strings.LastIndex(p, "/"); i >= 0 {
		return p[i+1:]
	}
	return p
}
