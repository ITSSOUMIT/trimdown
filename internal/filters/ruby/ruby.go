// Package ruby holds native filters for the Ruby toolchain: rake/rails test
// (minitest state machine), rspec (JSON), and rubocop (JSON). All run through
// `bundle exec` when a Gemfile is present.
package ruby

import (
	"os"
	"os/exec"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/registry"
)

func init() {
	registry.Register(rake{})
	registry.Register(railsTest{})
	registry.Register(rails{})
	registry.Register(rubyInterp{})
	registry.Register(rspec{})
	registry.Register(rubocop{})
}

// rubyExec builds a command for a Ruby tool, prefixing `bundle exec` when a
// Gemfile exists (so the project's pinned gem versions are used).
func rubyExec(tool string, args []string) *exec.Cmd {
	if _, err := os.Stat("Gemfile"); err == nil {
		if _, err := exec.LookPath("bundle"); err == nil {
			return engine.ResolvedCommand("bundle", append([]string{"exec", tool}, args...)...)
		}
	}
	return engine.ResolvedCommand(tool, args...)
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

// extractJSON returns the substring from the first '{' to the last '}' so we
// can parse JSON even when tools emit banner noise (Spring, deprecations)
// around it.
func extractJSON(s string) string {
	i := strings.IndexByte(s, '{')
	j := strings.LastIndexByte(s, '}')
	if i < 0 || j < 0 || j < i {
		return ""
	}
	return s[i : j+1]
}

func hasAny(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f {
				return true
			}
		}
	}
	return false
}
