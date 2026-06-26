package git

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/itssoumit/trimdown/internal/ir"
)

// parseCommit extracts "[branch hash] subject" → "ok <hash7>".
func parseCommit(stdout string) ir.Report {
	for _, l := range splitLines(stdout) {
		l = strings.TrimSpace(l)
		if strings.HasPrefix(l, "[") {
			if end := strings.Index(l, "]"); end > 0 {
				fields := strings.Fields(l[1:end])
				if len(fields) > 0 {
					hash := fields[len(fields)-1]
					if len(hash) >= 7 {
						return ir.Report{Summary: "ok " + hash[:7], Status: ir.StatusOK}
					}
				}
			}
		}
	}
	return ir.Report{Summary: "ok", Status: ir.StatusOK}
}

// parsePush summarizes push (output is on stderr).
func parsePush(stderr string) ir.Report {
	if strings.Contains(stderr, "Everything up-to-date") {
		return ir.Report{Summary: "ok (up-to-date)", Status: ir.StatusOK}
	}
	for _, l := range splitLines(stderr) {
		if strings.Contains(l, " -> ") {
			after := strings.TrimSpace(l)
			if i := strings.Index(after, " -> "); i >= 0 {
				dest := strings.Fields(after[i+4:])
				if len(dest) > 0 {
					return ir.Report{Summary: "ok " + dest[0], Status: ir.StatusOK}
				}
			}
		}
	}
	return ir.Report{Summary: "ok", Status: ir.StatusOK}
}

// parsePull summarizes pull from its stat output.
func parsePull(stdout, stderr string) ir.Report {
	all := stdout + "\n" + stderr
	if strings.Contains(all, "Already up to date") || strings.Contains(all, "Already up-to-date") {
		return ir.Report{Summary: "ok (up-to-date)", Status: ir.StatusOK}
	}
	files, ins, del := parseShortstat(all)
	if files > 0 {
		return ir.Report{
			Summary: fmt.Sprintf("ok %d files +%d -%d", files, ins, del),
			Status:  ir.StatusOK,
		}
	}
	return ir.Report{Summary: "ok", Status: ir.StatusOK}
}

// parseFetch counts updated refs (output on stderr).
func parseFetch(stderr string) ir.Report {
	n := 0
	for _, l := range splitLines(stderr) {
		if strings.Contains(l, " -> ") || strings.Contains(l, "[new") {
			n++
		}
	}
	if n > 0 {
		return ir.Report{Summary: fmt.Sprintf("ok fetched (%d refs)", n), Status: ir.StatusOK}
	}
	return ir.Report{Summary: "ok fetched", Status: ir.StatusOK}
}

// parseBranch compacts a branch listing into current/local/remote groups.
func parseBranch(stdout string) ir.Report {
	var current string
	var locals, remotes []string
	for _, l := range splitLines(stdout) {
		if l == "" {
			continue
		}
		switch {
		case strings.HasPrefix(l, "* "):
			current = strings.TrimSpace(l[2:])
		case strings.HasPrefix(strings.TrimSpace(l), "remotes/"):
			name := strings.TrimSpace(l)
			name = strings.TrimPrefix(name, "remotes/")
			if !strings.HasPrefix(name, "origin/HEAD") {
				remotes = append(remotes, name)
			}
		default:
			locals = append(locals, strings.TrimSpace(l))
		}
	}

	var b strings.Builder
	if current != "" {
		b.WriteString("* " + current + "\n")
	}
	for _, l := range locals {
		b.WriteString("  " + l + "\n")
	}
	if len(remotes) > 0 {
		fmt.Fprintf(&b, "remotes (%d): %s\n", len(remotes), strings.Join(capList(remotes, 8), ", "))
	}
	summary := fmt.Sprintf("%d local", len(locals)+boolToInt(current != ""))
	if len(remotes) > 0 {
		summary += fmt.Sprintf(", %d remote", len(remotes))
	}
	return ir.Report{Summary: summary, Status: ir.StatusOK, Text: strings.TrimRight(b.String(), "\n")}
}

// parseStash compacts `git stash list`; other stash ops → "ok".
func parseStash(stdout string, inv invocation) ir.Report {
	sub := ""
	if len(inv.args) > 0 {
		sub = inv.args[0]
	}
	if sub != "list" {
		body := strings.TrimSpace(stdout)
		if body == "" || strings.Contains(body, "No local changes") {
			return ir.Report{Summary: "ok", Status: ir.StatusOK}
		}
		return ir.Report{Summary: "ok", Status: ir.StatusOK, Text: body}
	}
	var lines []string
	for _, l := range splitLines(stdout) {
		// "stash@{0}: WIP on main: abc123 msg" → "stash@{0}: abc123 msg"
		l = strings.Replace(l, "WIP on ", "", 1)
		lines = append(lines, l)
	}
	return ir.Report{
		Summary: fmt.Sprintf("%d stashes", len(lines)),
		Status:  ir.StatusOK,
		Text:    strings.Join(lines, "\n"),
	}
}

// parseWorktree compacts `git worktree list`, abbreviating $HOME to ~.
func parseWorktree(stdout string) ir.Report {
	home, _ := os.UserHomeDir()
	var lines []string
	for _, l := range splitLines(stdout) {
		if home != "" {
			l = strings.Replace(l, home, "~", 1)
		}
		lines = append(lines, l)
	}
	return ir.Report{
		Summary: fmt.Sprintf("%d worktrees", len(lines)),
		Status:  ir.StatusOK,
		Text:    strings.Join(lines, "\n"),
	}
}

// parseShortstat extracts "N files changed, X insertions(+), Y deletions(-)".
func parseShortstat(s string) (files, ins, del int) {
	for _, l := range splitLines(s) {
		if !strings.Contains(l, "changed") {
			continue
		}
		for _, part := range strings.Split(l, ",") {
			part = strings.TrimSpace(part)
			switch {
			case strings.Contains(part, "file"):
				files = leadingInt(part)
			case strings.Contains(part, "insertion"):
				ins = leadingInt(part)
			case strings.Contains(part, "deletion"):
				del = leadingInt(part)
			}
		}
		if files > 0 {
			return
		}
	}
	return
}

// leadingInt parses the first whitespace-separated token of s as an int,
// e.g. "3 files changed" → 3. Returns 0 if not a number.
func leadingInt(s string) int {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(fields[0])
	return n
}

func capList(items []string, n int) []string {
	if len(items) <= n {
		return items
	}
	out := append([]string{}, items[:n]...)
	return append(out, fmt.Sprintf("… +%d", len(items)-n))
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
