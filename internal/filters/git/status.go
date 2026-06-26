package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/itssoumit/trimdown/internal/ir"
)

// parseStatus turns `git status --porcelain -b` output into a compact report.
// Pure: filesystem state (merge/rebase in progress) is added by the handler.
func parseStatus(stdout string) ir.Report {
	var branch string
	var entries []string
	for _, l := range splitLines(stdout) {
		if strings.HasPrefix(l, "## ") {
			branch = formatBranch(strings.TrimPrefix(l, "## "))
		} else if l != "" {
			entries = append(entries, l)
		}
	}

	header := "* " + branch
	if branch == "" {
		header = "*"
	}

	if len(entries) == 0 {
		return ir.Report{Summary: "clean", Status: ir.StatusOK, Text: header}
	}

	untracked, changed := 0, 0
	for _, e := range entries {
		if strings.HasPrefix(e, "?? ") {
			untracked++
		} else {
			changed++
		}
	}
	summary := fmt.Sprintf("%d changed", changed)
	if untracked > 0 {
		summary += fmt.Sprintf(", %d untracked", untracked)
	}

	var b strings.Builder
	b.WriteString(header)
	b.WriteByte('\n')
	b.WriteString(strings.Join(entries, "\n"))
	return ir.Report{Summary: summary, Status: ir.StatusWarn, Text: b.String()}
}

// formatBranch compresses the porcelain branch header, e.g.
// "main...origin/main [ahead 1, behind 2]" → "main ↑1 ↓2".
func formatBranch(s string) string {
	if strings.HasPrefix(s, "HEAD (no branch)") {
		return "(detached)"
	}
	if strings.HasPrefix(s, "No commits yet on ") {
		return strings.TrimPrefix(s, "No commits yet on ") + " (no commits yet)"
	}
	name := s
	ab := ""
	if i := strings.Index(s, " ["); i >= 0 {
		name = s[:i]
		ab = strings.TrimSuffix(s[i+2:], "]")
	}
	if i := strings.Index(name, "..."); i >= 0 {
		name = name[:i]
	}
	arrows := ""
	for _, part := range strings.Split(ab, ", ") {
		switch {
		case strings.HasPrefix(part, "ahead "):
			arrows += " ↑" + strings.TrimPrefix(part, "ahead ")
		case strings.HasPrefix(part, "behind "):
			arrows += " ↓" + strings.TrimPrefix(part, "behind ")
		}
	}
	return name + arrows
}

// inProgressState reports a merge/rebase/cherry-pick/revert in progress by
// looking at .git marker files (no extra exec). Skipped when -C/--git-dir is
// used, since those redirect the repo away from cwd.
func inProgressState(global []string) string {
	for _, g := range global {
		if g == "-C" || strings.HasPrefix(g, "--git-dir") {
			return ""
		}
	}
	fi, err := os.Stat(".git")
	if err != nil || !fi.IsDir() {
		return ""
	}
	exists := func(p string) bool { _, err := os.Stat(filepath.Join(".git", p)); return err == nil }
	switch {
	case exists("MERGE_HEAD"):
		return "merge in progress"
	case exists("rebase-merge"), exists("rebase-apply"):
		return "rebase in progress"
	case exists("CHERRY_PICK_HEAD"):
		return "cherry-pick in progress"
	case exists("REVERT_HEAD"):
		return "revert in progress"
	}
	return ""
}

// --- shared helpers for the git package ---

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
	if max <= 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}
