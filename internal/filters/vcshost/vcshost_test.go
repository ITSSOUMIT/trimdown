package vcshost

import (
	"strings"
	"testing"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func TestGHPRList(t *testing.T) {
	js := `[
      {"number":1,"title":"Add feature","headRefName":"feat","state":"OPEN","isDraft":false,"author":{"login":"a"}},
      {"number":2,"title":"Fix bug","headRefName":"fix","state":"OPEN","isDraft":true,"author":{"login":"b"}}
    ]`
	rep, _ := gh{}.Parse(engine.CaptureResult{Stdout: js, ExitCode: 0}, registry.Opts{Args: []string{"pr", "list"}})
	if !rep.Filtered || rep.Summary != "2 prs" {
		t.Fatalf("summary=%q filtered=%v", rep.Summary, rep.Filtered)
	}
	if len(rep.Items) != 2 || rep.Items[0].Key != "#1" {
		t.Fatalf("items=%+v", rep.Items)
	}
	if !strings.Contains(rep.Items[1].Val, "DRAFT") && !strings.Contains(rep.Items[1].Val, "draft") {
		t.Fatalf("draft PR not marked: %q", rep.Items[1].Val)
	}
}

func TestGHPRView(t *testing.T) {
	js := `{"number":7,"title":"Big change","state":"OPEN","author":{"login":"me"},
	        "headRefName":"feature","baseRefName":"main","additions":10,"deletions":2,
	        "mergeable":"MERGEABLE","reviewDecision":"APPROVED","isDraft":false}`
	rep, _ := gh{}.Parse(engine.CaptureResult{Stdout: js, ExitCode: 0}, registry.Opts{Args: []string{"pr", "view", "7"}})
	if !rep.Filtered || !strings.HasPrefix(rep.Summary, "#7 ") {
		t.Fatalf("summary=%q", rep.Summary)
	}
	got := map[string]string{}
	for _, it := range rep.Items {
		got[it.Key] = it.Val
	}
	if got["branch"] != "feature → main" {
		t.Fatalf("branch=%q", got["branch"])
	}
	if got["changes"] != "+10 -2" {
		t.Fatalf("changes=%q", got["changes"])
	}
}

func TestGHRunListFailure(t *testing.T) {
	js := `[
	  {"status":"completed","conclusion":"success","workflowName":"CI","headBranch":"main","displayTitle":"ok"},
	  {"status":"completed","conclusion":"failure","workflowName":"CI","headBranch":"feat","displayTitle":"boom"}
	]`
	rep, _ := gh{}.Parse(engine.CaptureResult{Stdout: js, ExitCode: 0}, registry.Opts{Args: []string{"run", "list"}})
	if rep.Status != ir.StatusFail {
		t.Fatalf("status=%v, want fail", rep.Status)
	}
	if !strings.Contains(rep.Summary, "1 failed") {
		t.Fatalf("summary=%q", rep.Summary)
	}
}

func TestGHApiMinifiesJSON(t *testing.T) {
	rep, _ := gh{}.Parse(engine.CaptureResult{Stdout: "{\n  \"a\": 1\n}", ExitCode: 0}, registry.Opts{Args: []string{"api", "user"}})
	if !rep.Filtered || strings.Contains(rep.Text, "\n") {
		t.Fatalf("api not minified: %q", rep.Text)
	}
}

func TestGHListFallbackToTable(t *testing.T) {
	// Non-JSON stdout (e.g. older gh / no --json support) → compact the table.
	out := "Showing 1 of 1 open pull request\n\n#3  title  feat\n"
	rep, _ := gh{}.Parse(engine.CaptureResult{Stdout: out, ExitCode: 0}, registry.Opts{Args: []string{"pr", "list"}})
	if !rep.Filtered {
		t.Fatalf("table fallback should still filter: %+v", rep)
	}
	if strings.Contains(rep.Text, "\n\n") {
		t.Fatalf("blank lines not stripped: %q", rep.Text)
	}
}

func TestGlabMRList(t *testing.T) {
	js := `[{"iid":5,"title":"Fix it","source_branch":"feat","state":"opened","author":{"username":"x"}}]`
	rep, _ := glab{}.Parse(engine.CaptureResult{Stdout: js, ExitCode: 0}, registry.Opts{Args: []string{"mr", "list"}})
	if !rep.Filtered || len(rep.Items) != 1 || rep.Items[0].Key != "!5" {
		t.Fatalf("items=%+v summary=%q", rep.Items, rep.Summary)
	}
}

func TestGTLogStack(t *testing.T) {
	out := "◉ feat-b #1234 (current)\n◯ feat-a #1230\n◯ main\n"
	rep, _ := gt{}.Parse(engine.CaptureResult{Stdout: out, ExitCode: 0}, registry.Opts{Args: []string{"log"}})
	if !rep.Filtered || rep.Summary != "3 branches" {
		t.Fatalf("summary=%q", rep.Summary)
	}
	if rep.Items[0].Key != "feat-b" || !strings.Contains(rep.Items[0].Val, "#1234") {
		t.Fatalf("first branch=%+v", rep.Items[0])
	}
}

func TestGTSubmit(t *testing.T) {
	out := "Submitting stack...\nSubmitted #1234 feat-b\nCreated #1235 feat-c\n"
	rep, _ := gt{}.Parse(engine.CaptureResult{Stdout: out, ExitCode: 0}, registry.Opts{Args: []string{"submit"}})
	if !rep.Filtered || rep.Summary != "submitted 2" {
		t.Fatalf("summary=%q text=%q", rep.Summary, rep.Text)
	}
}

func TestVCSPassthrough(t *testing.T) {
	// Unknown subcommand → passthrough.
	rep, _ := gh{}.Parse(engine.CaptureResult{Stdout: "x", ExitCode: 0}, registry.Opts{Args: []string{"pr", "diff"}})
	if rep.Filtered {
		t.Fatal("unknown gh subcommand should pass through")
	}
	// Non-zero exit → passthrough (auth/permission errors stay visible).
	rep2, _ := gh{}.Parse(engine.CaptureResult{Stderr: "gh: not authenticated", ExitCode: 1}, registry.Opts{Args: []string{"pr", "list"}})
	if rep2.Filtered {
		t.Fatal("error exit should pass through")
	}
}
