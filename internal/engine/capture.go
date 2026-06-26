package engine

import (
	"bytes"
	"os"
	"os/exec"
)

// MaxCapture bounds how much of each stream we buffer, so a runaway-verbose
// tool can't exhaust memory. Excess is silently dropped.
const MaxCapture = 10 << 20 // 10 MiB

// CaptureResult holds a finished command's output and exit code.
type CaptureResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// cappedBuffer is an io.Writer that stops storing past Limit but keeps
// reporting full writes so the child process never sees a short write / EPIPE.
type cappedBuffer struct {
	buf     bytes.Buffer
	limit   int
	dropped bool
}

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if rem := c.limit - c.buf.Len(); rem > 0 {
		if len(p) <= rem {
			return c.buf.Write(p)
		}
		_, _ = c.buf.Write(p[:rem])
	}
	c.dropped = true
	return len(p), nil
}

// Capture runs cmd, draining stdout and stderr into bounded buffers. os/exec
// copies each stream in its own goroutine, so the two buffers are never written
// concurrently and need no locking. Stdin is inherited.
func Capture(cmd *exec.Cmd) CaptureResult {
	out := &cappedBuffer{limit: MaxCapture}
	errb := &cappedBuffer{limit: MaxCapture}
	cmd.Stdin = os.Stdin
	cmd.Stdout = out
	cmd.Stderr = errb
	err := cmd.Run()
	return CaptureResult{
		Stdout:   out.buf.String(),
		Stderr:   errb.buf.String(),
		ExitCode: ExitCode(err),
	}
}

// Success reports whether the command exited 0.
func (c CaptureResult) Success() bool { return c.ExitCode == 0 }
