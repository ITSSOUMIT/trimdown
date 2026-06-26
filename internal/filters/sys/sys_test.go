package sys

import (
	"strings"
	"testing"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func TestLogDedup(t *testing.T) {
	in := "GET /a 200\nGET /a 200\nGET /a 200\nERROR boom\nGET /b 404\n"
	rep, _ := logFilter{}.Parse(engine.CaptureResult{Stdout: in}, registry.Opts{})
	if !strings.Contains(rep.Text, "(×3)") {
		t.Fatalf("expected dedup count:\n%s", rep.Text)
	}
	// numbers normalized → /a and /b collapse to same pattern
	if !strings.Contains(rep.Summary, "patterns") {
		t.Fatalf("summary=%q", rep.Summary)
	}
}

func TestEnvMasksSecrets(t *testing.T) {
	in := "PATH=/usr/bin\nAWS_SECRET_ACCESS_KEY=abcd1234\nHOME=/root\n"
	rep, _ := envFilter{}.Parse(engine.CaptureResult{Stdout: in}, registry.Opts{})
	for _, it := range rep.Items {
		if strings.Contains(it.Key, "SECRET") && it.Val != "***" {
			t.Fatalf("secret not masked: %+v", it)
		}
		if it.Key == "PATH" && it.Val != "/usr/bin" {
			t.Fatalf("non-secret altered: %+v", it)
		}
	}
}

func TestEnvFilter(t *testing.T) {
	in := "PATH=/usr/bin\nAWS_REGION=us-east-1\nHOME=/root\n"
	rep, _ := envFilter{}.Parse(engine.CaptureResult{Stdout: in}, registry.Opts{Args: []string{"-f", "aws"}})
	if len(rep.Items) != 1 || rep.Items[0].Key != "AWS_REGION" {
		t.Fatalf("filter failed: %+v", rep.Items)
	}
}

func TestJSONSchema(t *testing.T) {
	in := `{"user":{"name":"x","age":30},"tags":["a","b"],"active":true}`
	rep, _ := jsonFilter{}.Parse(engine.CaptureResult{Stdout: in}, registry.Opts{})
	if !strings.Contains(rep.Text, "name: string") || !strings.Contains(rep.Text, "age: number") {
		t.Fatalf("schema missing fields:\n%s", rep.Text)
	}
	if !strings.Contains(rep.Text, "active: bool") {
		t.Fatalf("bool type missing:\n%s", rep.Text)
	}
}

func TestJSONCompact(t *testing.T) {
	in := "{\n  \"a\": 1,\n  \"b\": 2\n}"
	rep, _ := jsonFilter{}.Parse(engine.CaptureResult{Stdout: in}, registry.Opts{Args: []string{"--compact"}})
	if rep.Text != `{"a":1,"b":2}` {
		t.Fatalf("compact=%q", rep.Text)
	}
}

func TestGrepGroupByFile(t *testing.T) {
	in := "a.go:10:func foo\na.go:20:func bar\nb.go:5:var x\n"
	rep, _ := grep{}.Parse(engine.CaptureResult{Stdout: in, ExitCode: 0}, registry.Opts{})
	if rep.Summary != "3 matches in 2 files" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if !strings.Contains(rep.Text, "a.go (2)") {
		t.Fatalf("grouping missing:\n%s", rep.Text)
	}
}

func TestReadTail(t *testing.T) {
	in := "l1\nl2\nl3\nl4\nl5\n"
	rep, _ := read{}.Parse(engine.CaptureResult{Stdout: in}, registry.Opts{Args: []string{"--tail", "2"}})
	if rep.Text != "l4\nl5" {
		t.Fatalf("tail=%q", rep.Text)
	}
}

func TestDiffParse(t *testing.T) {
	in := "3c3\n< old line\n---\n> new line\n"
	rep, _ := diffFilter{}.Parse(engine.CaptureResult{Stdout: in, ExitCode: 1}, registry.Opts{})
	if rep.Summary != "+1 -1" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	_ = ir.StatusOK
}
