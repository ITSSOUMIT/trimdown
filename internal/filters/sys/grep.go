package sys

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

const (
	maxGrepMatches = 200
	grepLineWidth  = 160
)

// grep runs ripgrep (or grep) and groups matches by file.
type grep struct{}

func (grep) Tool() string       { return "grep" }
func (grep) Subcommand() string { return "" }

func (grep) Exec(o registry.Opts) engine.CaptureResult {
	bin := "grep"
	args := o.Args
	if _, err := exec.LookPath("rg"); err == nil {
		bin = "rg"
	} else {
		// ensure grep emits filenames for grouping
		args = append([]string{"-Hn"}, args...)
	}
	return engine.Capture(engine.ResolvedCommand(bin, args...))
}

func (grep) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	// grep/rg exit 1 means "no matches" (not an error).
	if c.ExitCode > 1 {
		return ir.Report{Filtered: false, Raw: c.Stdout + c.Stderr, ExitCode: c.ExitCode}, nil
	}
	order := []string{}
	byFile := map[string][]string{}
	total := 0

	for _, l := range splitLines(c.Stdout) {
		file, rest := splitGrepLine(l)
		if file == "" {
			continue
		}
		if _, ok := byFile[file]; !ok {
			order = append(order, file)
		}
		if total < maxGrepMatches {
			byFile[file] = append(byFile[file], truncateRunes(strings.TrimSpace(rest), grepLineWidth))
		}
		total++
	}

	if total == 0 {
		return ir.Report{Tool: "grep", Summary: "no matches", Status: ir.StatusOK, Filtered: true, Raw: c.Stdout}, nil
	}

	var b strings.Builder
	for _, f := range order {
		ms := byFile[f]
		fmt.Fprintf(&b, "%s (%d)\n", f, len(ms))
		for _, m := range ms {
			b.WriteString("  ")
			b.WriteString(m)
			b.WriteByte('\n')
		}
	}
	var notes []string
	if total > maxGrepMatches {
		notes = append(notes, fmt.Sprintf("… +%d more matches", total-maxGrepMatches))
	}
	return ir.Report{
		Tool:     "grep",
		Summary:  fmt.Sprintf("%d matches in %d files", total, len(order)),
		Status:   ir.StatusOK,
		Text:     strings.TrimRight(b.String(), "\n"),
		Notes:    notes,
		Filtered: true,
		Raw:      c.Stdout,
	}, nil
}

// splitGrepLine parses "file:line:content" (rg/grep -Hn) into (file, content).
func splitGrepLine(l string) (file, rest string) {
	a := strings.SplitN(l, ":", 3)
	if len(a) < 3 {
		return "", ""
	}
	return a[0], a[1] + ": " + a[2]
}
