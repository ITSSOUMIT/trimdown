package python

import (
	"strings"
	"testing"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func TestParsePytestFailures(t *testing.T) {
	out := `..F.
=================================== FAILURES ===================================
_________________________________ test_login __________________________________
    def test_login():
>       assert do_login() == 200
E       assert 401 == 200
auth_test.py:12: AssertionError
=========================== short test summary info ============================
FAILED auth_test.py::test_login - assert 401 == 200
========================= 1 failed, 3 passed in 0.12s ==========================`
	rep := parsePytest(out, 1, out)
	if rep.Summary != "3 passed, 1 failed" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if rep.Status != ir.StatusFail || len(rep.Tests) != 1 {
		t.Fatalf("status=%v tests=%d", rep.Status, len(rep.Tests))
	}
	if rep.Tests[0].Name != "test_login" {
		t.Fatalf("name=%q", rep.Tests[0].Name)
	}
	joined := strings.Join(rep.Tests[0].Detail, "\n")
	if !strings.Contains(joined, "assert 401 == 200") {
		t.Fatalf("detail missing:\n%s", joined)
	}
}

func TestParsePytestAllPass(t *testing.T) {
	out := "........\n======================= 8 passed in 0.30s ========================"
	rep := parsePytest(out, 0, out)
	if rep.Summary != "8 passed" || rep.Status != ir.StatusOK {
		t.Fatalf("summary=%q status=%v", rep.Summary, rep.Status)
	}
}

func TestRuffCheckJSON(t *testing.T) {
	js := `[{"code":"F401","message":"'os' imported but unused","filename":"/proj/src/a.py","location":{"row":1,"column":8},"fix":{"applicability":"safe"}},
	{"code":"E501","message":"line too long","filename":"/proj/src/b.py","location":{"row":4,"column":80},"fix":null}]`
	rep, _ := ruff{}.Parse(engine.CaptureResult{Stdout: js}, registry.Opts{Args: []string{"check"}})
	if rep.Summary != "2 issues" || len(rep.Diagnostics) != 2 {
		t.Fatalf("summary=%q diags=%d", rep.Summary, len(rep.Diagnostics))
	}
	if rep.Diagnostics[0].File != "src/a.py" || rep.Diagnostics[0].Rule != "F401" {
		t.Fatalf("diag0=%+v", rep.Diagnostics[0])
	}
	if !strings.Contains(strings.Join(rep.Notes, " "), "1 fixable") {
		t.Fatalf("fixable note missing: %v", rep.Notes)
	}
}

func TestRuffClean(t *testing.T) {
	rep, _ := ruff{}.Parse(engine.CaptureResult{Stdout: "[]"}, registry.Opts{Args: []string{"check"}})
	if rep.Status != ir.StatusOK || !strings.Contains(rep.Summary, "no issues") {
		t.Fatalf("clean: %+v", rep)
	}
}

func TestMypyGrouped(t *testing.T) {
	out := `src/a.py:10: error: Incompatible return value type [return-value]
src/a.py:11: note: Expected int
src/b.py:3: warning: unused thing [misc]
Found 2 errors in 2 files`
	rep, _ := mypy{}.Parse(engine.CaptureResult{Stdout: out}, registry.Opts{})
	if rep.Status != ir.StatusFail {
		t.Fatalf("status=%v", rep.Status)
	}
	if len(rep.Diagnostics) != 2 {
		t.Fatalf("diags=%d (note should attach, not add)", len(rep.Diagnostics))
	}
	if len(rep.Diagnostics[0].Context) != 1 {
		t.Fatalf("note not attached: %+v", rep.Diagnostics[0])
	}
}

func TestPipOutdated(t *testing.T) {
	js := `[{"name":"requests","version":"2.28.1","latest_version":"2.31.0"}]`
	rep, _ := pip{}.Parse(engine.CaptureResult{Stdout: js}, registry.Opts{Args: []string{"outdated"}})
	if rep.Summary != "1 outdated" || len(rep.Items) != 1 {
		t.Fatalf("summary=%q items=%d", rep.Summary, len(rep.Items))
	}
	if rep.Items[0].Val != "2.28.1 → 2.31.0" {
		t.Fatalf("item=%+v", rep.Items[0])
	}
}

func TestPipInstallPassthrough(t *testing.T) {
	rep, _ := pip{}.Parse(engine.CaptureResult{Stdout: "Successfully installed x", ExitCode: 0}, registry.Opts{Args: []string{"install", "x"}})
	if rep.Filtered {
		t.Fatalf("install should pass through unfiltered")
	}
}
