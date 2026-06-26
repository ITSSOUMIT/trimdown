// Package vcshost holds native filters for code-host CLIs: gh (GitHub), glab
// (GitLab), and gt (Graphite). They compact verbose view/list output by
// stripping ANSI/blank noise, truncating wide lines, and capping length —
// robust across the tools' frequent output-format changes. (JSON-structured
// parsing is a future enhancement.)
package vcshost

import (
	"fmt"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func init() {
	for _, tool := range []string{"gh", "glab", "gt"} {
		registry.Register(compactor{tool: tool})
	}
}

const (
	maxVCSLines = 60
	truncateCol = 200
)

// compactor is a generic strip/cap filter parameterized by tool name.
type compactor struct{ tool string }

func (c compactor) Tool() string     { return c.tool }
func (compactor) Subcommand() string { return "" }

func (c compactor) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand(c.tool, o.Args...))
}

func (c compactor) Parse(cr engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	if cr.ExitCode != 0 {
		return ir.Report{Filtered: false, Raw: rawOf(cr), ExitCode: cr.ExitCode}, nil
	}
	src := engine.StripANSI(cr.Stdout)
	var kept []string
	for _, l := range splitLines(src) {
		if strings.TrimSpace(l) == "" {
			continue
		}
		kept = append(kept, truncateRunes(l, truncateCol))
	}

	var notes []string
	if len(kept) > maxVCSLines {
		notes = append(notes, fmt.Sprintf("… +%d more lines", len(kept)-maxVCSLines))
		kept = kept[:maxVCSLines]
	}
	return ir.Report{
		Tool:     c.tool,
		Status:   ir.StatusOK,
		Text:     strings.Join(kept, "\n"),
		Notes:    notes,
		Filtered: true,
		Raw:      rawOf(cr),
	}, nil
}

func rawOf(c engine.CaptureResult) string {
	switch {
	case c.Stderr == "":
		return c.Stdout
	case c.Stdout == "":
		return c.Stderr
	default:
		return c.Stdout + c.Stderr
	}
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}
