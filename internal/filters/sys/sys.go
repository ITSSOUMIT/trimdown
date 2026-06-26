// Package sys holds native filters for language-agnostic system/file tools:
// read (cat/head/tail), grep, log, env, json, and compaction for ls/tree/find/
// diff. Note these are lower-priority than git/test/lint tools because an agent
// like Claude Code often uses its built-in Read/Grep/Glob instead of the shell.
package sys

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func init() {
	registry.Register(read{})
	registry.Register(logFilter{})
	registry.Register(envFilter{})
	registry.Register(jsonFilter{})
	registry.Register(grep{})
	for _, t := range []string{"ls", "tree", "find"} {
		registry.Register(compactor{tool: t})
	}
	registry.Register(diffFilter{})
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

// readArgsAndFlags splits positional file paths from -n/--max-lines/--tail flags.
type fileOpts struct {
	files    []string
	maxLines int
	tail     int
}

func parseFileOpts(args []string) fileOpts {
	var o fileOpts
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-n" || a == "--max-lines":
			if i+1 < len(args) {
				i++
				o.maxLines = atoiSafe(args[i])
			}
		case a == "--tail":
			if i+1 < len(args) {
				i++
				o.tail = atoiSafe(args[i])
			}
		case strings.HasPrefix(a, "-"):
			// ignore unknown flags
		default:
			o.files = append(o.files, a)
		}
	}
	return o
}

// readFilesOrStdin returns the combined contents of files, or stdin if none.
func readFilesOrStdin(files []string) string {
	if len(files) == 0 {
		b, _ := io.ReadAll(os.Stdin)
		return string(b)
	}
	var sb strings.Builder
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			sb.WriteString("trimdown: " + err.Error() + "\n")
			continue
		}
		sb.Write(b)
		if !strings.HasSuffix(string(b), "\n") {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

func atoiSafe(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// compactor exec's a real tool (ls/tree/find) and strips/caps its output.
type compactor struct{ tool string }

func (c compactor) Tool() string     { return c.tool }
func (compactor) Subcommand() string { return "" }
func (c compactor) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand(c.tool, o.Args...))
}
func (c compactor) Parse(cr engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	if cr.ExitCode != 0 {
		return ir.Report{Filtered: false, Raw: cr.Stdout + cr.Stderr, ExitCode: cr.ExitCode}, nil
	}
	lines := splitLines(cr.Stdout)
	const limit = 200
	var notes []string
	if len(lines) > limit {
		notes = append(notes, fmt.Sprintf("… +%d more", len(lines)-limit))
		lines = lines[:limit]
	}
	for i := range lines {
		lines[i] = truncateRunes(lines[i], 200)
	}
	return ir.Report{Tool: c.tool, Status: ir.StatusOK, Text: strings.Join(lines, "\n"), Notes: notes, Filtered: true, Raw: cr.Stdout}, nil
}
