package git

import (
	"fmt"
	"strings"

	"github.com/itssoumit/trimdown/internal/ir"
)

const (
	logFormat       = "--pretty=format:%h %s (%cr) <%an>"
	logDisplayCap   = 20
	logTruncateCols = 100
)

// buildLogArgs prepends "log" and injects a compact one-line format unless the
// user already chose an output format.
func buildLogArgs(args []string) []string {
	out := []string{"log", "--no-color"}
	if !hasLogFormat(args) {
		out = append(out, logFormat)
	}
	return append(out, args...)
}

func hasLogFormat(args []string) bool {
	for _, a := range args {
		if a == "--oneline" || a == "--pretty" || a == "--format" ||
			strings.HasPrefix(a, "--pretty=") || strings.HasPrefix(a, "--format=") ||
			strings.HasPrefix(a, "--graph") {
			return true
		}
	}
	return false
}

// parseLog caps the number of commits shown and truncates long subject lines.
func parseLog(stdout string, _ invocation) ir.Report {
	lines := splitLines(stdout)
	// Drop trailing empties that some formats add.
	total := len(lines)
	if total == 0 {
		return ir.Report{Summary: "no commits", Status: ir.StatusOK}
	}

	shown := lines
	noted := ""
	if total > logDisplayCap {
		shown = lines[:logDisplayCap]
		noted = fmt.Sprintf("… +%d more commits", total-logDisplayCap)
	}
	for i, l := range shown {
		shown[i] = truncateRunes(l, logTruncateCols)
	}

	text := strings.Join(shown, "\n")
	if noted != "" {
		text += "\n" + noted
	}
	return ir.Report{
		Summary: fmt.Sprintf("%d commits", total),
		Status:  ir.StatusOK,
		Text:    text,
	}
}
