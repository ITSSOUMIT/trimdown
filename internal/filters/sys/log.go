package sys

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

// numRE normalizes digit runs so near-identical log lines (timestamps, ids)
// collapse into one deduplicated pattern.
var numRE = regexp.MustCompile(`\d+`)

const maxLogPatterns = 40

// logFilter deduplicates log lines, collapsing repeats into "(×N)".
type logFilter struct{}

func (logFilter) Tool() string       { return "log" }
func (logFilter) Subcommand() string { return "" }

func (logFilter) Exec(o registry.Opts) engine.CaptureResult {
	fo := parseFileOpts(o.Args)
	return engine.CaptureResult{Stdout: readFilesOrStdin(fo.files), ExitCode: 0}
}

func (logFilter) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	type pat struct {
		sample string
		count  int
		order  int
	}
	patterns := map[string]*pat{}
	next := 0
	for _, l := range splitLines(engine.StripANSI(c.Stdout)) {
		if strings.TrimSpace(l) == "" {
			continue
		}
		key := numRE.ReplaceAllString(l, "#")
		p := patterns[key]
		if p == nil {
			p = &pat{sample: l, order: next}
			next++
			patterns[key] = p
		}
		p.count++
	}

	// Sort by first-seen order to preserve log flow.
	ordered := make([]*pat, len(patterns))
	for _, p := range patterns {
		ordered[p.order] = p
	}

	var lines []string
	for _, p := range ordered {
		if p == nil {
			continue
		}
		if len(lines) >= maxLogPatterns {
			break
		}
		if p.count > 1 {
			lines = append(lines, fmt.Sprintf("%s (×%d)", truncateRunes(p.sample, 180), p.count))
		} else {
			lines = append(lines, truncateRunes(p.sample, 180))
		}
	}

	return ir.Report{
		Tool:     "log",
		Summary:  fmt.Sprintf("%d unique patterns", len(patterns)),
		Status:   ir.StatusOK,
		Text:     strings.Join(lines, "\n"),
		Filtered: true,
		Raw:      c.Stdout,
	}, nil
}
