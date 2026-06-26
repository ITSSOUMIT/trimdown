package js

import (
	"fmt"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

type prettier struct{}

func (prettier) Tool() string       { return "prettier" }
func (prettier) Subcommand() string { return "" }

func (prettier) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(pkgExec("prettier", o.Args))
}

// Parse extracts the files prettier --check reports as needing formatting.
func (prettier) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	var files []string
	for _, l := range splitLines(engine.StripANSI(c.Stdout + "\n" + c.Stderr)) {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "Checking formatting") ||
			strings.HasPrefix(t, "All matched files") {
			continue
		}
		if strings.HasPrefix(t, "[warn]") {
			t = strings.TrimSpace(strings.TrimPrefix(t, "[warn]"))
			if t == "" || strings.Contains(t, "Code style issues") || strings.Contains(t, "Forgot to run") {
				continue
			}
		}
		files = append(files, t)
	}

	if len(files) == 0 {
		return ir.Report{Tool: "prettier", Summary: "ok (formatted)", Status: ir.StatusOK, Filtered: true, Raw: rawOf(c)}, nil
	}
	items := make([]ir.Item, 0, len(files))
	for _, f := range files {
		items = append(items, ir.Item{Key: f})
	}
	return ir.Report{
		Tool: "prettier", Summary: fmt.Sprintf("%d files need formatting", len(files)),
		Status: ir.StatusWarn, Items: items, Filtered: true, Raw: rawOf(c),
	}, nil
}
