package cloud

import (
	"strings"
	"testing"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/registry"
)

func TestAwsCompactsJSON(t *testing.T) {
	in := "{\n  \"Account\": \"123\",\n  \"UserId\": \"AID\"\n}"
	rep, _ := aws{}.Parse(engine.CaptureResult{Stdout: in, ExitCode: 0}, registry.Opts{})
	if !rep.Filtered {
		t.Fatal("should filter JSON")
	}
	if strings.Contains(rep.Text, "\n") {
		t.Fatalf("expected minified JSON, got:\n%s", rep.Text)
	}
}

func TestDedupLines(t *testing.T) {
	got := dedupLines([]string{"a", "a", "a", "b", "c", "c"})
	want := []string{"a (×3)", "b", "c (×2)"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestKubeLogsDedup(t *testing.T) {
	in := "starting\nheartbeat\nheartbeat\nheartbeat\ndone\n"
	rep, _ := kube{tool: "kubectl"}.Parse(
		engine.CaptureResult{Stdout: in, ExitCode: 0},
		registry.Opts{Args: []string{"logs", "mypod"}},
	)
	if !strings.Contains(rep.Text, "heartbeat (×3)") {
		t.Fatalf("logs not deduped:\n%s", rep.Text)
	}
}

func TestPsqlStripsBorders(t *testing.T) {
	in := " id | name \n----+------\n  1 | alice\n  2 | bob\n(2 rows)\n"
	rep, _ := psql{}.Parse(engine.CaptureResult{Stdout: in, ExitCode: 0}, registry.Opts{})
	if strings.Contains(rep.Text, "----") {
		t.Fatalf("border not stripped:\n%s", rep.Text)
	}
	if !strings.Contains(rep.Text, "alice") {
		t.Fatalf("data lost:\n%s", rep.Text)
	}
}

func TestCurlNonJSONPassthrough(t *testing.T) {
	rep, _ := curl{}.Parse(engine.CaptureResult{Stdout: "<html>...</html>", ExitCode: 0}, registry.Opts{})
	if rep.Filtered {
		t.Fatal("non-JSON curl should pass through")
	}
}
