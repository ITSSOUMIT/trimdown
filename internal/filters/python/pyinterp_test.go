package python

import (
	"strings"
	"testing"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/registry"
)

func opts(tool string, args ...string) registry.Opts {
	return registry.Opts{Tool: tool, Args: args}
}

func TestModuleOf(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantMod string
		wantOk  bool
		rest    []string
	}{
		{"dash-m space", []string{"-m", "pytest", "tests/"}, "pytest", true, []string{"tests/"}},
		{"dash-m joined", []string{"-mpytest", "tests/"}, "pytest", true, []string{"tests/"}},
		{"interp flag then -m", []string{"-O", "-m", "venv", "env"}, "venv", true, []string{"env"}},
		{"script not module", []string{"script.py", "--flag"}, "", false, nil},
		{"-c not module", []string{"-c", "print(1)"}, "", false, nil},
		{"bare", nil, "", false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mod, rest, ok := moduleOf(tt.args)
			if mod != tt.wantMod || ok != tt.wantOk {
				t.Fatalf("got mod=%q ok=%v; want %q %v", mod, ok, tt.wantMod, tt.wantOk)
			}
			if ok && strings.Join(rest, ",") != strings.Join(tt.rest, ",") {
				t.Fatalf("rest=%v want %v", rest, tt.rest)
			}
		})
	}
}

func TestPyInterpRoutesPytest(t *testing.T) {
	out := `..F.
=================================== FAILURES ===================================
_________________________________ test_login __________________________________
    def test_login():
>       assert do_login() == 200
E       assert 401 == 200
auth_test.py:12: AssertionError
========================= 1 failed, 3 passed in 0.12s ==========================`
	c := engine.CaptureResult{Stdout: out, ExitCode: 1}
	rep, err := pyInterp{tool: "python"}.Parse(c, opts("python", "-m", "pytest", "tests/"))
	if err != nil {
		t.Fatal(err)
	}
	if rep.Tool != "pytest" {
		t.Fatalf("tool=%q want pytest", rep.Tool)
	}
	if rep.Summary != "3 passed, 1 failed" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if !rep.Filtered || len(rep.Tests) != 1 {
		t.Fatalf("filtered=%v tests=%d", rep.Filtered, len(rep.Tests))
	}
}

func TestPyInterpUnittestPass(t *testing.T) {
	out := `...
----------------------------------------------------------------------
Ran 3 tests in 0.001s

OK`
	c := engine.CaptureResult{Stderr: out, ExitCode: 0}
	rep, _ := pyInterp{tool: "python"}.Parse(c, opts("python", "-m", "unittest"))
	if rep.Tool != "unittest" {
		t.Fatalf("tool=%q", rep.Tool)
	}
	if rep.Status != 0 { // StatusOK
		t.Fatalf("status=%v", rep.Status)
	}
	if rep.Summary != "3 tests, all passed" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if !rep.Filtered {
		t.Fatalf("expected filtered")
	}
}

func TestPyInterpUnittestFail(t *testing.T) {
	out := `.F.E
======================================================================
FAIL: test_add (tests.test_math.MathTest)
----------------------------------------------------------------------
Traceback (most recent call last):
  File "tests/test_math.py", line 8, in test_add
    self.assertEqual(add(1, 1), 3)
AssertionError: 2 != 3
======================================================================
ERROR: test_div (tests.test_math.MathTest)
----------------------------------------------------------------------
Traceback (most recent call last):
  File "tests/test_math.py", line 12, in test_div
    div(1, 0)
ZeroDivisionError: division by zero
----------------------------------------------------------------------
Ran 4 tests in 0.002s

FAILED (failures=1, errors=1)`
	c := engine.CaptureResult{Stderr: out, ExitCode: 1}
	rep, _ := pyInterp{tool: "python"}.Parse(c, opts("python", "-m", "unittest"))
	if rep.Summary != "4 tests, 2 failed" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if rep.Status == 0 {
		t.Fatalf("expected fail status")
	}
	if len(rep.Tests) != 2 {
		t.Fatalf("tests=%d want 2", len(rep.Tests))
	}
	if rep.Tests[0].Name != "test_add (tests.test_math.MathTest)" {
		t.Fatalf("name=%q", rep.Tests[0].Name)
	}
	joined := strings.Join(rep.Tests[0].Detail, "\n")
	if !strings.Contains(joined, "AssertionError: 2 != 3") {
		t.Fatalf("detail missing exception:\n%s", joined)
	}
}

func TestPyInterpVenv(t *testing.T) {
	c := engine.CaptureResult{ExitCode: 0}
	rep, _ := pyInterp{tool: "python3"}.Parse(c, opts("python3", "-m", "venv", ".venv"))
	if rep.Summary != "ok venv .venv" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if !rep.Filtered || rep.Status != 0 {
		t.Fatalf("filtered=%v status=%v", rep.Filtered, rep.Status)
	}
}

func TestPyInterpScriptTraceback(t *testing.T) {
	stderr := `Traceback (most recent call last):
  File "/app/main.py", line 42, in <module>
    main()
  File "/app/main.py", line 30, in main
    process(data)
  File "/usr/lib/python3.11/json/__init__.py", line 346, in loads
    return _default_decoder.decode(s)
ValueError: invalid literal for int() with base 10: 'abc'`
	c := engine.CaptureResult{Stdout: "starting...\n", Stderr: stderr, ExitCode: 1}
	rep, _ := pyInterp{tool: "python"}.Parse(c, opts("python", "main.py"))
	if !rep.Filtered {
		t.Fatalf("expected traceback to be compacted (filtered)")
	}
	if !strings.HasPrefix(rep.Summary, "ValueError: ") {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if rep.Status == 0 {
		t.Fatalf("expected fail status")
	}
	// site-packages/stdlib frame should be dropped in favor of app frames.
	if strings.Contains(rep.Text, "json/__init__.py") {
		t.Fatalf("noisy stdlib frame not dropped:\n%s", rep.Text)
	}
	if !strings.Contains(rep.Text, "main.py:30") || !strings.Contains(rep.Text, "in main") {
		t.Fatalf("app frame missing:\n%s", rep.Text)
	}
	if !strings.Contains(rep.Text, "process(data)") {
		t.Fatalf("source line missing:\n%s", rep.Text)
	}
}

func TestPyInterpScriptClean(t *testing.T) {
	c := engine.CaptureResult{Stdout: "hello world\nresult: 42\n", ExitCode: 0}
	rep, _ := pyInterp{tool: "python"}.Parse(c, opts("python", "script.py"))
	if rep.Filtered {
		t.Fatalf("clean script run must pass through, got filtered")
	}
	if rep.Raw != "hello world\nresult: 42\n" {
		t.Fatalf("raw=%q", rep.Raw)
	}
}

func TestPyInterpVersion(t *testing.T) {
	c := engine.CaptureResult{Stdout: "Python 3.11.6\n", ExitCode: 0}
	rep, _ := pyInterp{tool: "python"}.Parse(c, opts("python", "--version"))
	if !rep.Filtered || rep.Summary != "Python 3.11.6" {
		t.Fatalf("filtered=%v summary=%q", rep.Filtered, rep.Summary)
	}
}

func TestPyInterpInlineCodePassthrough(t *testing.T) {
	c := engine.CaptureResult{Stdout: "42\n", ExitCode: 0}
	rep, _ := pyInterp{tool: "python"}.Parse(c, opts("python", "-c", "print(6*7)"))
	if rep.Filtered {
		t.Fatalf("-c inline code must pass through")
	}
}

func TestPyInterpHTTPServerPassthrough(t *testing.T) {
	c := engine.CaptureResult{Stderr: "Serving HTTP on 0.0.0.0 port 8000 ...\n", ExitCode: 0}
	rep, _ := pyInterp{tool: "python"}.Parse(c, opts("python", "-m", "http.server"))
	if rep.Filtered {
		t.Fatalf("http.server must pass through")
	}
}

func TestPyInterpRegistered(t *testing.T) {
	for _, tool := range []string{"python", "python3"} {
		if _, ok := registry.Lookup(tool, []string{"-m", "pytest"}); !ok {
			t.Fatalf("%s not registered", tool)
		}
	}
}
