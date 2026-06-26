// Package python holds native filters for the Python toolchain: pytest (test
// state machine), ruff (JSON), mypy (grouped diagnostics), and pip (JSON, with
// uv auto-detection).
package python

import (
	"os/exec"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/registry"
)

func init() {
	registry.Register(pytest{})
	registry.Register(ruff{})
	registry.Register(mypy{})
	registry.Register(pip{})
}

// pyCommand runs a tool directly if it's on PATH, else falls back to
// `python3 -m <module>` so virtualenv-less setups still work.
func pyCommand(tool string, moduleArgs, args []string) *exec.Cmd {
	if _, err := exec.LookPath(tool); err == nil {
		return engine.ResolvedCommand(tool, args...)
	}
	py := "python3"
	if _, err := exec.LookPath(py); err != nil {
		py = "python"
	}
	full := append(append([]string{}, moduleArgs...), args...)
	return engine.ResolvedCommand(py, full...)
}

func rawOf(c engine.CaptureResult) string {
	switch {
	case c.Stderr == "":
		return c.Stdout
	case c.Stdout == "":
		return c.Stderr
	default:
		return c.Stdout + c.Stderr
	}
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines
}

func firstArg(args []string) string {
	for _, a := range args {
		if a != "" && a[0] != '-' {
			return a
		}
	}
	return ""
}

func hasAnyPrefix(args []string, prefixes ...string) bool {
	for _, a := range args {
		for _, p := range prefixes {
			if a == p || strings.HasPrefix(a, p) {
				return true
			}
		}
	}
	return false
}
