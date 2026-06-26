// Package sys holds native filters for language-agnostic system/file tools:
// read (cat/head/tail), grep, log, env, json, diff, and structured compaction
// for ls/tree/find. Note these are lower-priority than git/test/lint tools
// because an agent like Claude Code often uses its built-in Read/Grep/Glob
// instead of the shell.
package sys

import (
	"io"
	"os"
	"strings"

	"github.com/itssoumit/trimdown/internal/registry"
)

func init() {
	registry.Register(read{})
	registry.Register(logFilter{})
	registry.Register(envFilter{})
	registry.Register(jsonFilter{})
	registry.Register(grep{})
	registry.Register(lsFilter{})
	registry.Register(treeFilter{})
	registry.Register(findFilter{})
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

// compactError recognizes common filesystem failures (No such file, Permission
// denied) so ls/tree/find can compact a clean error message instead of either
// dumping the raw stderr verbosely or passing through. Returns ("", false) when
// the error is not recognizable, signaling the caller to raw-passthrough.
func compactError(stderr string) (string, bool) {
	low := strings.ToLower(stderr)
	switch {
	case strings.Contains(low, "no such file or directory"):
		return "no such file or directory", true
	case strings.Contains(low, "permission denied"):
		return "permission denied", true
	case strings.Contains(low, "not a directory"):
		return "not a directory", true
	}
	return "", false
}
