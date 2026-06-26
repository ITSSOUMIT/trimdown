package js

import (
	"strings"
	"testing"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func TestTscParse(t *testing.T) {
	out := "src/a.ts(12,5): error TS2304: Cannot find name 'x'.\nsrc/b.ts(3,1): error TS1005: ';' expected."
	rep, _ := tsc{}.Parse(engine.CaptureResult{Stdout: out, ExitCode: 1}, registry.Opts{})
	if rep.Summary != "2 errors" || rep.Status != ir.StatusFail {
		t.Fatalf("summary=%q status=%v", rep.Summary, rep.Status)
	}
	if rep.Diagnostics[0].Rule != "TS2304" || rep.Diagnostics[0].Line != 12 {
		t.Fatalf("diag0=%+v", rep.Diagnostics[0])
	}
}

func TestTscClean(t *testing.T) {
	rep, _ := tsc{}.Parse(engine.CaptureResult{Stdout: ""}, registry.Opts{})
	if rep.Status != ir.StatusOK {
		t.Fatalf("clean status=%v", rep.Status)
	}
}

func TestEslintJSON(t *testing.T) {
	js := `[{"filePath":"/proj/src/a.js","messages":[
	  {"ruleId":"no-unused-vars","severity":2,"message":"'x' is defined but never used","line":1,"column":7},
	  {"ruleId":"semi","severity":1,"message":"Missing semicolon","line":2,"column":10}]}]`
	rep, _ := eslint{}.Parse(engine.CaptureResult{Stdout: js, ExitCode: 1}, registry.Opts{})
	if rep.Summary != "1 errors, 1 warnings" || rep.Status != ir.StatusFail {
		t.Fatalf("summary=%q status=%v", rep.Summary, rep.Status)
	}
	if rep.Diagnostics[0].File != "src/a.js" || rep.Diagnostics[0].Rule != "no-unused-vars" {
		t.Fatalf("diag0=%+v", rep.Diagnostics[0])
	}
}

func TestEslintClean(t *testing.T) {
	rep, _ := eslint{}.Parse(engine.CaptureResult{Stdout: "[]"}, registry.Opts{})
	if rep.Status != ir.StatusOK || !strings.Contains(rep.Summary, "no problems") {
		t.Fatalf("clean: %+v", rep)
	}
}

func TestJSTestVitest(t *testing.T) {
	// vitest-style summary
	out := " Test Files  1 failed | 2 passed (3)\n      Tests  2 failed | 18 passed (20)\n  ✗ math.test.ts > adds numbers"
	rep := parseJSTest("vitest", out, 1, out)
	if rep.Status != ir.StatusFail {
		t.Fatalf("status=%v", rep.Status)
	}
	if rep.Summary != "18 passed, 2 failed" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if len(rep.Tests) != 1 {
		t.Fatalf("tests=%d", len(rep.Tests))
	}
}

func TestJSTestJestPass(t *testing.T) {
	out := "Tests:       8 passed, 8 total\nSnapshots:   0 total"
	rep := parseJSTest("jest", out, 0, out)
	if rep.Status != ir.StatusOK || rep.Summary != "8 passed" {
		t.Fatalf("summary=%q status=%v", rep.Summary, rep.Status)
	}
}

func TestPrettierCheck(t *testing.T) {
	out := "Checking formatting...\n[warn] src/a.ts\n[warn] src/b.ts\n[warn] Code style issues found in 2 files."
	rep, _ := prettier{}.Parse(engine.CaptureResult{Stdout: out, ExitCode: 1}, registry.Opts{})
	if rep.Summary != "2 files need formatting" || len(rep.Items) != 2 {
		t.Fatalf("summary=%q items=%d", rep.Summary, len(rep.Items))
	}
}
