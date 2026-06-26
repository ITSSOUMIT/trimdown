package sys

import (
	"fmt"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

const defaultReadCap = 400

// read replaces cat/head/tail: prints file (or stdin) content with line caps.
type read struct{}

func (read) Tool() string       { return "read" }
func (read) Subcommand() string { return "" }

func (read) Exec(o registry.Opts) engine.CaptureResult {
	fo := parseFileOpts(o.Args)
	return engine.CaptureResult{Stdout: readFilesOrStdin(fo.files), ExitCode: 0}
}

func (read) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	fo := parseFileOpts(o.Args)
	lines := splitLines(c.Stdout)
	total := len(lines)

	var notes []string
	switch {
	case fo.tail > 0 && total > fo.tail:
		lines = lines[total-fo.tail:]
		notes = append(notes, fmt.Sprintf("(tail %d of %d lines)", fo.tail, total))
	case fo.maxLines > 0 && total > fo.maxLines:
		lines = lines[:fo.maxLines]
		notes = append(notes, fmt.Sprintf("… +%d more lines", total-fo.maxLines))
	case fo.maxLines == 0 && fo.tail == 0 && total > defaultReadCap:
		lines = lines[:defaultReadCap]
		notes = append(notes, fmt.Sprintf("… +%d more lines (use --max-lines or --tail)", total-defaultReadCap))
	}

	return ir.Report{
		Tool:     "read",
		Status:   ir.StatusOK,
		Text:     strings.Join(lines, "\n"),
		Notes:    notes,
		Filtered: true,
		Raw:      c.Stdout,
	}, nil
}
