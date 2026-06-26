package golang

import (
	"strings"
	"testing"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func TestGoTestParsePassFail(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"Action":"run","Package":"x/a","Test":"TestA"}`,
		`{"Action":"pass","Package":"x/a","Test":"TestA"}`,
		`{"Action":"run","Package":"x/a","Test":"TestB"}`,
		`{"Action":"output","Package":"x/a","Test":"TestB","Output":"=== RUN   TestB\n"}`,
		`{"Action":"output","Package":"x/a","Test":"TestB","Output":"    m_test.go:9: boom\n"}`,
		`{"Action":"fail","Package":"x/a","Test":"TestB"}`,
	}, "\n")
	rep, _ := goTest{}.Parse(engine.CaptureResult{Stdout: ndjson, ExitCode: 1}, registry.Opts{})
	if rep.Summary != "1 passed, 1 failed in 1 package(s)" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if rep.Status != ir.StatusFail || len(rep.Tests) != 1 {
		t.Fatalf("status=%v tests=%d", rep.Status, len(rep.Tests))
	}
	if rep.Tests[0].Name != "TestB" || len(rep.Tests[0].Detail) == 0 {
		t.Fatalf("failure detail missing: %+v", rep.Tests[0])
	}
	if strings.Contains(strings.Join(rep.Tests[0].Detail, "\n"), "=== RUN") {
		t.Fatalf("framing not stripped: %v", rep.Tests[0].Detail)
	}
}

func TestGoTestBuildFailure(t *testing.T) {
	ndjson := strings.Join([]string{
		`{"ImportPath":"x/a","Action":"build-output","Output":"# x/a\n"}`,
		`{"ImportPath":"x/a","Action":"build-output","Output":"./m.go:3:1: syntax error\n"}`,
		`{"ImportPath":"x/a","Action":"build-fail"}`,
	}, "\n")
	rep, _ := goTest{}.Parse(engine.CaptureResult{Stdout: ndjson, ExitCode: 1}, registry.Opts{})
	if rep.Summary != "build failed" || rep.Status != ir.StatusFail {
		t.Fatalf("summary=%q status=%v", rep.Summary, rep.Status)
	}
	if !strings.Contains(strings.Join(rep.Notes, "\n"), "syntax error") {
		t.Fatalf("build error missing: %v", rep.Notes)
	}
}

func TestParseGoErrors(t *testing.T) {
	stderr := "# example/pkg\n./main.go:10:6: undefined: Foo\n./util.go:3:2: imported and not used: \"os\"\n"
	rep, _ := parseGoErrors("build", engine.CaptureResult{Stderr: stderr, ExitCode: 1})
	if len(rep.Diagnostics) != 2 || rep.Status != ir.StatusFail {
		t.Fatalf("diags=%d status=%v", len(rep.Diagnostics), rep.Status)
	}
	if rep.Diagnostics[0].File != "./main.go" || rep.Diagnostics[0].Line != 10 {
		t.Fatalf("diag0=%+v", rep.Diagnostics[0])
	}
}

func TestGolangciParse(t *testing.T) {
	out := "main.go:10:5: error returned is not checked (errcheck)\nutil.go:3:1: var x is unused (unused)\nmain.go:20:1: should not use dot imports (golint)\n"
	rep, _ := golangci{}.Parse(engine.CaptureResult{Stdout: out}, registry.Opts{})
	if len(rep.Diagnostics) != 3 {
		t.Fatalf("diags=%d", len(rep.Diagnostics))
	}
	if rep.Diagnostics[0].Rule != "errcheck" {
		t.Fatalf("rule=%q", rep.Diagnostics[0].Rule)
	}
	if !strings.Contains(strings.Join(rep.Notes, " "), "by linter") {
		t.Fatalf("missing linter breakdown: %v", rep.Notes)
	}
}

func TestGolangciClean(t *testing.T) {
	rep, _ := golangci{}.Parse(engine.CaptureResult{Stdout: ""}, registry.Opts{})
	if rep.Status != ir.StatusOK || !strings.Contains(rep.Summary, "no issues") {
		t.Fatalf("clean: %+v", rep)
	}
}
