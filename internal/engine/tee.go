package engine

import (
	"io"
	"os"
)

// IsStdoutTerminal reports whether stdout is a terminal (character device)
// rather than a pipe/file. We use it to tell apart "an agent is capturing this
// output" (a pipe → safe to measure and the case analytics care about) from "a
// human is watching" (a TTY → leave interactive tools fully alone). Stdlib only,
// no dependency.
func IsStdoutTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

// TeePassthrough runs a command, streaming its output live to this process's
// stdout/stderr (lossless, with correct ordering within each stream) while also
// accumulating a bounded copy. The copy lets the caller measure how many tokens
// flowed through a command we didn't filter — sizing the savings opportunity of
// tools we don't yet cover. Stdout and stderr buffer separately (each drained by
// its own os/exec goroutine), so there is no shared-state race.
func TeePassthrough(name string, args []string) (captured string, exitCode int) {
	cmd := ResolvedCommand(name, args...)
	cmd.Stdin = os.Stdin
	outBuf := &cappedBuffer{limit: MaxCapture}
	errBuf := &cappedBuffer{limit: MaxCapture}
	cmd.Stdout = io.MultiWriter(os.Stdout, outBuf)
	cmd.Stderr = io.MultiWriter(os.Stderr, errBuf)
	err := cmd.Run()
	// Order between the two streams doesn't matter for a token count.
	return outBuf.buf.String() + errBuf.buf.String(), ExitCode(err)
}
