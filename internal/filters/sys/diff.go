package sys

import (
	"fmt"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

const maxDiffLines = 300

// diffFilter runs `diff` and keeps only changed lines and hunk headers.
type diffFilter struct{}

func (diffFilter) Tool() string       { return "diff" }
func (diffFilter) Subcommand() string { return "" }

func (diffFilter) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand("diff", o.Args...))
}

func (diffFilter) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	if c.ExitCode == 0 {
		return ir.Report{Tool: "diff", Summary: "identical", Status: ir.StatusOK, Filtered: true, Raw: c.Stdout}, nil
	}
	added, removed := 0, 0
	var kept []string
	truncated := false
	for _, l := range splitLines(c.Stdout) {
		switch {
		case strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---"):
			// file headers — keep
		case strings.HasPrefix(l, "+") || strings.HasPrefix(l, ">"):
			added++
		case strings.HasPrefix(l, "-") || strings.HasPrefix(l, "<"):
			removed++
		}
		if len(kept) >= maxDiffLines {
			truncated = true
			continue
		}
		kept = append(kept, truncateRunes(l, 200))
	}
	var notes []string
	if truncated {
		notes = append(notes, "… diff truncated (use --raw)")
	}
	return ir.Report{
		Tool:     "diff",
		Summary:  fmt.Sprintf("+%d -%d", added, removed),
		Status:   ir.StatusWarn,
		Text:     strings.Join(kept, "\n"),
		Notes:    notes,
		Filtered: true,
		Raw:      c.Stdout,
	}, nil
}
