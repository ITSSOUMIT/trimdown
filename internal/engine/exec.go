// Package engine runs child processes, captures their output with bounded
// memory, and maps process results to faithful exit codes.
package engine

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// ResolvedCommand builds an *exec.Cmd, resolving the binary via PATH first so
// the lookup is explicit and cross-platform (Go's exec.LookPath honors PATHEXT
// on Windows). Falls back to the bare name if resolution fails.
func ResolvedCommand(name string, args ...string) *exec.Cmd {
	if path, err := exec.LookPath(name); err == nil {
		return exec.Command(path, args...)
	}
	return exec.Command(name, args...)
}

// ExitCode maps an *exec.Cmd error to a shell-faithful exit code:
//   - nil               → 0
//   - terminated by sig → 128+signal (unix)
//   - normal non-zero   → that code
//   - not found / other → 127
//
// CI fidelity depends on this being exact.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			return 128 + int(ws.Signal())
		}
		if code := ee.ExitCode(); code >= 0 {
			return code
		}
		return 1
	}
	// exec.ErrNotFound, permission denied, etc.
	return 127
}

// Passthrough runs a command with inherited stdio (no filtering) and returns
// its exit code. Used for unknown tools/subcommands and the `passthrough` meta
// command.
func Passthrough(name string, args []string) int {
	cmd := ResolvedCommand(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return ExitCode(cmd.Run())
}
