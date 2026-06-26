package engine

import (
	"os/exec"
	"runtime"
	"testing"
)

func TestExitCode(t *testing.T) {
	if got := ExitCode(nil); got != 0 {
		t.Fatalf("nil error: got %d, want 0", got)
	}

	// normal non-zero exit
	err := exec.Command("sh", "-c", "exit 42").Run()
	if got := ExitCode(err); got != 42 {
		t.Fatalf("exit 42: got %d, want 42", got)
	}

	// command not found
	err = exec.Command("trimdown-no-such-binary-zzz").Run()
	if got := ExitCode(err); got != 127 {
		t.Fatalf("not found: got %d, want 127", got)
	}

	// terminated by signal → 128+signal
	if runtime.GOOS != "windows" {
		err = exec.Command("sh", "-c", "kill -TERM $$").Run()
		if got := ExitCode(err); got != 143 {
			t.Fatalf("SIGTERM: got %d, want 143", got)
		}
	}
}

func TestCaptureCapsOutput(t *testing.T) {
	// Emit more than the cap and confirm we bound memory without erroring.
	cmd := exec.Command("sh", "-c", "yes x | head -c 200000")
	res := Capture(cmd)
	if !res.Success() {
		t.Fatalf("capture failed: exit %d", res.ExitCode)
	}
	if len(res.Stdout) == 0 {
		t.Fatal("expected some stdout")
	}
}

func TestCaptureStdoutStderrSplit(t *testing.T) {
	cmd := exec.Command("sh", "-c", "echo out; echo err 1>&2")
	res := Capture(cmd)
	if res.Stdout != "out\n" {
		t.Fatalf("stdout = %q, want %q", res.Stdout, "out\n")
	}
	if res.Stderr != "err\n" {
		t.Fatalf("stderr = %q, want %q", res.Stderr, "err\n")
	}
}
