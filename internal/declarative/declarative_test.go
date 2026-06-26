package declarative

import (
	"strings"
	"testing"
)

// TestEmbeddedSpecsLoad asserts every embedded spec parses and compiles.
func TestEmbeddedSpecsLoad(t *testing.T) {
	specs, errs := LoadEmbedded()
	for _, err := range errs {
		t.Errorf("spec load error: %v", err)
	}
	if len(specs) == 0 {
		t.Fatal("no specs loaded")
	}
}

// TestSpecGoldenCases runs every inline `tests:` case in every spec — the
// declarative engine's golden harness. Adding a tool = a YAML file with cases.
func TestSpecGoldenCases(t *testing.T) {
	specs, _ := LoadEmbedded()
	ran := 0
	for _, s := range specs {
		for _, tc := range s.Tests {
			ran++
			name := s.Tool
			if s.Subcommand != "" {
				name += "-" + s.Subcommand
			}
			t.Run(name+"/"+tc.Name, func(t *testing.T) {
				got := s.Apply(tc.Input)
				want := strings.TrimRight(tc.Expected, "\n")
				if got != want {
					t.Errorf("\n--- got ---\n%s\n--- want ---\n%s", got, want)
				}
			})
		}
	}
	if ran == 0 {
		t.Fatal("no inline spec tests ran")
	}
	t.Logf("ran %d declarative golden cases across %d specs", ran, len(specs))
}

// TestUniqueToolSubcommand guards against two specs claiming the same key.
func TestUniqueToolSubcommand(t *testing.T) {
	specs, _ := LoadEmbedded()
	seen := map[string]bool{}
	for _, s := range specs {
		key := s.Tool + "/" + s.Subcommand
		if seen[key] {
			t.Errorf("duplicate spec for %q", key)
		}
		seen[key] = true
	}
}
