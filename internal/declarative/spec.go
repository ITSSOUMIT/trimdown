// Package declarative is a single Filter implementation that runs data-driven
// "specs" — embedded YAML files describing a line-strip pipeline. It covers the
// ~40 simple strip-class tools (terraform, pulumi, helm, ssh, jq, df, ...) with
// no bespoke Go and lets users add filters without recompiling. This unifies
// rtk's separate TOML-filter code path into the same Filter interface as the
// native parsers.
package declarative

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
)

// Spec is the YAML schema for one declarative filter.
type Spec struct {
	Tool         string        `yaml:"tool"`
	Subcommand   string        `yaml:"subcommand"`
	Description  string        `yaml:"description"`
	StripANSI    bool          `yaml:"strip_ansi"`
	FilterStderr bool          `yaml:"filter_stderr"`
	Replace      []ReplaceRule `yaml:"replace"`
	MatchOutput  []MatchRule   `yaml:"match_output"`
	StripLines   []string      `yaml:"strip_lines"`
	KeepLines    []string      `yaml:"keep_lines"`
	TruncateAt   int           `yaml:"truncate_at"`
	Head         int           `yaml:"head"`
	Tail         int           `yaml:"tail"`
	MaxLines     int           `yaml:"max_lines"`
	OnEmpty      string        `yaml:"on_empty"`
	Tests        []SpecTest    `yaml:"tests"`

	compiled compiled
}

// ReplaceRule is a per-line regex substitution (chained in order).
type ReplaceRule struct {
	Pattern     string `yaml:"pattern"`
	Replacement string `yaml:"replacement"`
}

// MatchRule short-circuits: if Pattern matches the whole blob (and Unless, if
// set, does not), the filter returns Message immediately.
type MatchRule struct {
	Pattern string `yaml:"pattern"`
	Message string `yaml:"message"`
	Unless  string `yaml:"unless"`
}

// SpecTest is an inline golden case feeding the test harness.
type SpecTest struct {
	Name     string `yaml:"name"`
	Input    string `yaml:"input"`
	Expected string `yaml:"expected"`
}

type compiled struct {
	replace  []compiledReplace
	matchOut []compiledMatch
	strip    []*regexp.Regexp
	keep     []*regexp.Regexp
}

type compiledReplace struct {
	re   *regexp.Regexp
	repl string
}

type compiledMatch struct {
	re      *regexp.Regexp
	unless  *regexp.Regexp
	message string
}

// Compile validates and precompiles all regexes in the spec.
func (s *Spec) Compile() error {
	if s.Tool == "" {
		return fmt.Errorf("spec has no tool")
	}
	var c compiled
	for _, r := range s.Replace {
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			return fmt.Errorf("%s: replace pattern %q: %w", s.Tool, r.Pattern, err)
		}
		c.replace = append(c.replace, compiledReplace{re: re, repl: r.Replacement})
	}
	for _, m := range s.MatchOutput {
		re, err := regexp.Compile(m.Pattern)
		if err != nil {
			return fmt.Errorf("%s: match_output pattern %q: %w", s.Tool, m.Pattern, err)
		}
		cm := compiledMatch{re: re, message: m.Message}
		if m.Unless != "" {
			u, err := regexp.Compile(m.Unless)
			if err != nil {
				return fmt.Errorf("%s: match_output unless %q: %w", s.Tool, m.Unless, err)
			}
			cm.unless = u
		}
		c.matchOut = append(c.matchOut, cm)
	}
	var err error
	if c.strip, err = compileAll(s.StripLines); err != nil {
		return fmt.Errorf("%s: strip_lines: %w", s.Tool, err)
	}
	if c.keep, err = compileAll(s.KeepLines); err != nil {
		return fmt.Errorf("%s: keep_lines: %w", s.Tool, err)
	}
	s.compiled = c
	return nil
}

func compileAll(pats []string) ([]*regexp.Regexp, error) {
	out := make([]*regexp.Regexp, 0, len(pats))
	for _, p := range pats {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("pattern %q: %w", p, err)
		}
		out = append(out, re)
	}
	return out, nil
}

// Apply runs the spec pipeline over raw output and returns the compacted text.
// Stage order: strip_ansi → replace → match_output → strip/keep → truncate →
// head/tail → max_lines → on_empty.
func (s *Spec) Apply(raw string) string {
	text := raw
	if s.StripANSI {
		text = engine.StripANSI(text)
	}

	if len(s.compiled.replace) > 0 {
		text = mapLines(text, func(line string) string {
			for _, r := range s.compiled.replace {
				line = r.re.ReplaceAllString(line, r.repl)
			}
			return line
		})
	}

	for _, m := range s.compiled.matchOut {
		if m.re.MatchString(text) && (m.unless == nil || !m.unless.MatchString(text)) {
			return m.message
		}
	}

	lines := splitLines(text)
	if len(s.compiled.strip) > 0 {
		lines = keepIf(lines, func(l string) bool { return !anyMatch(s.compiled.strip, l) })
	}
	if len(s.compiled.keep) > 0 {
		lines = keepIf(lines, func(l string) bool { return anyMatch(s.compiled.keep, l) })
	}
	if s.TruncateAt > 0 {
		for i, l := range lines {
			lines[i] = truncateRunes(l, s.TruncateAt)
		}
	}
	if s.Head > 0 && len(lines) > s.Head {
		lines = lines[:s.Head]
	}
	if s.Tail > 0 && len(lines) > s.Tail {
		lines = lines[len(lines)-s.Tail:]
	}
	if s.MaxLines > 0 && len(lines) > s.MaxLines {
		omitted := len(lines) - s.MaxLines
		lines = append(lines[:s.MaxLines:s.MaxLines], fmt.Sprintf("… +%d lines", omitted))
	}

	out := strings.TrimRight(strings.Join(lines, "\n"), "\n")
	if strings.TrimSpace(out) == "" && s.OnEmpty != "" {
		return s.OnEmpty
	}
	return out
}

func mapLines(s string, fn func(string) string) string {
	lines := splitLines(s)
	for i, l := range lines {
		lines[i] = fn(l)
	}
	return strings.Join(lines, "\n")
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimSuffix(l, "\r")
	}
	return lines
}

func keepIf(lines []string, pred func(string) bool) []string {
	out := lines[:0]
	for _, l := range lines {
		if pred(l) {
			out = append(out, l)
		}
	}
	return out
}

func anyMatch(res []*regexp.Regexp, s string) bool {
	for _, re := range res {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}
