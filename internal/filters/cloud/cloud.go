// Package cloud holds native filters for cloud/DB CLIs: aws (force + compact
// JSON), docker/kubectl/oc (table compaction + log dedup), psql (strip table
// chrome), and curl (compact JSON bodies).
package cloud

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func init() {
	registry.Register(aws{})
	registry.Register(psql{})
	registry.Register(curl{})
	for _, t := range []string{"docker", "kubectl", "oc"} {
		registry.Register(orchestrator{tool: t})
	}
}

const maxCloudLines = 80

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

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

func capLines(lines []string, n int) ([]string, []string) {
	if len(lines) <= n {
		return lines, nil
	}
	return lines[:n], []string{fmt.Sprintf("… +%d more lines", len(lines)-n)}
}

// dedupLines collapses consecutive identical lines into "(×N)".
func dedupLines(lines []string) []string {
	var out []string
	for i := 0; i < len(lines); {
		j := i + 1
		for j < len(lines) && lines[j] == lines[i] {
			j++
		}
		if n := j - i; n > 1 {
			out = append(out, fmt.Sprintf("%s (×%d)", lines[i], n))
		} else {
			out = append(out, lines[i])
		}
		i = j
	}
	return out
}

// orchestrator compacts docker/kubectl/oc output: dedup for logs, strip+cap
// for everything else.
type orchestrator struct{ tool string }

func (o orchestrator) Tool() string     { return o.tool }
func (orchestrator) Subcommand() string { return "" }

func (o orchestrator) Exec(opts registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand(o.tool, opts.Args...))
}

func (o orchestrator) Parse(c engine.CaptureResult, opts registry.Opts) (ir.Report, error) {
	if c.ExitCode != 0 {
		return ir.Report{Filtered: false, Raw: rawOf(c), ExitCode: c.ExitCode}, nil
	}
	lines := splitLines(engine.StripANSI(c.Stdout))
	if isLogsCommand(opts.Args) {
		lines = dedupLines(lines)
	}
	for i := range lines {
		lines[i] = truncateRunes(lines[i], 200)
	}
	lines, notes := capLines(lines, maxCloudLines)
	return ir.Report{
		Tool: o.tool, Status: ir.StatusOK, Text: strings.Join(lines, "\n"),
		Notes: notes, Filtered: true, Raw: rawOf(c),
	}, nil
}

func isLogsCommand(args []string) bool {
	for _, a := range args {
		if a == "logs" {
			return true
		}
	}
	return false
}

// --- aws ---

type aws struct{}

func (aws) Tool() string       { return "aws" }
func (aws) Subcommand() string { return "" }

func (aws) Exec(o registry.Opts) engine.CaptureResult {
	args := o.Args
	if !hasOutputFlag(args) {
		args = append(append([]string{}, args...), "--output", "json")
	}
	return engine.Capture(engine.ResolvedCommand("aws", args...))
}

func (aws) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	if c.ExitCode != 0 {
		return ir.Report{Filtered: false, Raw: rawOf(c), ExitCode: c.ExitCode}, nil
	}
	var v any
	if json.Unmarshal([]byte(strings.TrimSpace(c.Stdout)), &v) != nil {
		// Non-JSON (e.g. text/table) — strip + cap.
		lines, notes := capLines(splitLines(c.Stdout), maxCloudLines)
		return ir.Report{Tool: "aws", Status: ir.StatusOK, Text: strings.Join(lines, "\n"), Notes: notes, Filtered: true, Raw: rawOf(c)}, nil
	}
	b, _ := json.Marshal(v) // minified
	text := truncateRunes(string(b), 4000)
	return ir.Report{Tool: "aws", Status: ir.StatusOK, Text: text, Filtered: true, Raw: rawOf(c)}, nil
}

func hasOutputFlag(args []string) bool {
	for _, a := range args {
		if a == "--output" || strings.HasPrefix(a, "--output=") {
			return true
		}
	}
	return false
}

// --- psql ---

var psqlBorderRE = regexp.MustCompile(`^[\s+|'-]+$`)

type psql struct{}

func (psql) Tool() string       { return "psql" }
func (psql) Subcommand() string { return "" }
func (psql) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand("psql", o.Args...))
}
func (psql) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	if c.ExitCode != 0 {
		return ir.Report{Filtered: false, Raw: rawOf(c), ExitCode: c.ExitCode}, nil
	}
	var kept []string
	for _, l := range splitLines(c.Stdout) {
		if psqlBorderRE.MatchString(l) || strings.TrimSpace(l) == "" {
			continue
		}
		kept = append(kept, strings.TrimSpace(strings.Trim(l, "|")))
	}
	kept, notes := capLines(kept, maxCloudLines)
	return ir.Report{Tool: "psql", Status: ir.StatusOK, Text: strings.Join(kept, "\n"), Notes: notes, Filtered: true, Raw: rawOf(c)}, nil
}

// --- curl ---

type curl struct{}

func (curl) Tool() string       { return "curl" }
func (curl) Subcommand() string { return "" }
func (curl) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand("curl", o.Args...))
}
func (curl) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	body := strings.TrimSpace(c.Stdout)
	var v any
	if body != "" && json.Unmarshal([]byte(body), &v) == nil {
		b, _ := json.Marshal(v)
		return ir.Report{Tool: "curl", Status: ir.StatusOK, Text: truncateRunes(string(b), 4000), Filtered: true, Raw: rawOf(c)}, nil
	}
	// Non-JSON — pass raw (curl is often used for downloads/headers).
	return ir.Report{Filtered: false, Raw: rawOf(c), ExitCode: c.ExitCode}, nil
}
