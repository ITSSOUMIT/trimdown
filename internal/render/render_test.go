package render

import (
	"strings"
	"testing"

	"github.com/itssoumit/trimdown/internal/ir"
)

func TestRenderRawWhenNotFiltered(t *testing.T) {
	r := ir.RawReport("git", "raw output here", 0)
	if got := Render(r, Opts{}); got != "raw output here" {
		t.Fatalf("got %q, want raw passthrough", got)
	}
}

func TestRenderRawFlagOverridesFiltered(t *testing.T) {
	r := ir.Report{Summary: "compact", Filtered: true, Raw: "the raw"}
	if got := Render(r, Opts{Raw: true}); got != "the raw" {
		t.Fatalf("--raw should emit raw, got %q", got)
	}
}

func TestRenderDiagnosticsGroupedByFile(t *testing.T) {
	r := ir.Report{
		Summary:  "2 issues in 1 file",
		Filtered: true,
		Status:   ir.StatusFail,
		Diagnostics: []ir.Diagnostic{
			{File: "main.py", Line: 1, Col: 8, Rule: "F401", Message: "unused import"},
			{File: "main.py", Line: 2, Col: 8, Rule: "F401", Message: "unused import sys"},
		},
	}
	out := Render(r, Opts{})
	if !strings.Contains(out, "main.py (2)") {
		t.Fatalf("expected grouped file header, got:\n%s", out)
	}
	if !strings.Contains(out, ":1:8 F401 unused import") {
		t.Fatalf("expected diagnostic line, got:\n%s", out)
	}
}

func TestRenderJSON(t *testing.T) {
	r := ir.Report{Tool: "git", Summary: "clean", Filtered: true}
	out := Render(r, Opts{JSON: true})
	if !strings.Contains(out, `"tool": "git"`) || !strings.Contains(out, `"summary": "clean"`) {
		t.Fatalf("unexpected json:\n%s", out)
	}
}
