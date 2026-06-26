package git

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
)

const listCap = 40

// isListArgs reports whether the subcommand args request a listing (no
// non-flag operand, or an explicit list verb).
func hasOperand(args []string) bool {
	for _, a := range args {
		if a != "" && !strings.HasPrefix(a, "-") {
			return true
		}
	}
	return false
}

// parseTag: bare `git tag` lists tags → Items; create/delete → ok summary.
func parseTag(c engine.CaptureResult, inv invocation) ir.Report {
	// Delete / create / verify produce small/empty output.
	if hasFlag(inv.args, "-d", "--delete") {
		return ir.Report{Summary: "ok tag deleted", Status: ir.StatusOK}
	}
	lines := nonEmptyLines(c.Stdout)
	// A create (`git tag v1`) yields no stdout.
	if len(lines) == 0 {
		if hasOperand(inv.args) {
			return ir.Report{Summary: "ok tag created", Status: ir.StatusOK}
		}
		return ir.Report{Summary: "no tags", Status: ir.StatusOK}
	}
	rep := ir.Report{Summary: fmt.Sprintf("%d tags", len(lines)), Status: ir.StatusOK}
	for _, l := range capList(lines, listCap) {
		rep.Items = append(rep.Items, ir.Item{Key: l})
	}
	return rep
}

// parseRemote: `git remote` / `git remote -v` → Items; add/remove → ok.
func parseRemote(c engine.CaptureResult, inv invocation) ir.Report {
	first := firstArg(inv.args)
	switch first {
	case "add":
		return ir.Report{Summary: "ok remote added", Status: ir.StatusOK}
	case "remove", "rm":
		return ir.Report{Summary: "ok remote removed", Status: ir.StatusOK}
	case "rename":
		return ir.Report{Summary: "ok remote renamed", Status: ir.StatusOK}
	case "set-url":
		return ir.Report{Summary: "ok remote url set", Status: ir.StatusOK}
	}
	lines := nonEmptyLines(c.Stdout)
	if len(lines) == 0 {
		return ir.Report{Summary: "no remotes", Status: ir.StatusOK}
	}
	verbose := hasFlag(inv.args, "-v", "--verbose")
	rep := ir.Report{Status: ir.StatusOK}
	if verbose {
		// "origin\tgit@…(fetch)" / "origin\tgit@…(push)" — keep fetch URLs only.
		names := map[string]bool{}
		for _, l := range lines {
			fields := strings.Fields(l)
			if len(fields) >= 3 && fields[len(fields)-1] == "(fetch)" {
				name := fields[0]
				url := strings.Join(fields[1:len(fields)-1], " ")
				names[name] = true
				rep.Items = append(rep.Items, ir.Item{Key: name, Val: url + " (fetch)"})
			}
		}
		rep.Summary = fmt.Sprintf("%d remotes", len(names))
	} else {
		for _, l := range capList(lines, listCap) {
			rep.Items = append(rep.Items, ir.Item{Key: strings.TrimSpace(l)})
		}
		rep.Summary = fmt.Sprintf("%d remotes", len(lines))
	}
	return rep
}

var cleanRe = regexp.MustCompile(`^(?:Would remove|Removing|Would skip repository|Skipping repository) (.+)$`)

// parseClean: dry-run ("Would remove …") or real removal ("Removing …") →
// a capped file list + count.
func parseClean(c engine.CaptureResult) ir.Report {
	var files []string
	for _, l := range splitLines(c.Stdout) {
		if m := cleanRe.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			files = append(files, strings.TrimSpace(m[1]))
		}
	}
	if len(files) == 0 {
		return ir.Report{Summary: "nothing to clean", Status: ir.StatusOK}
	}
	dryRun := strings.Contains(c.Stdout, "Would remove") || strings.Contains(c.Stdout, "Would skip")
	verb := "removed"
	status := ir.StatusOK
	if dryRun {
		verb = "would remove"
		status = ir.StatusWarn
	}
	rep := ir.Report{Summary: fmt.Sprintf("%s %d files", verb, len(files)), Status: status}
	for _, f := range capList(files, listCap) {
		rep.Items = append(rep.Items, ir.Item{Key: f})
	}
	return rep
}

// parseConfig: --list → key=value Items; --get → the value; set → ok.
func parseConfig(c engine.CaptureResult, inv invocation) ir.Report {
	switch {
	case hasFlag(inv.args, "-l", "--list"):
		lines := nonEmptyLines(c.Stdout)
		rep := ir.Report{Summary: fmt.Sprintf("%d settings", len(lines)), Status: ir.StatusOK}
		for _, l := range capList(lines, listCap) {
			if i := strings.Index(l, "="); i >= 0 {
				rep.Items = append(rep.Items, ir.Item{Key: l[:i], Val: l[i+1:]})
			} else {
				rep.Items = append(rep.Items, ir.Item{Key: l})
			}
		}
		return rep
	case hasFlag(inv.args, "--get", "--get-all", "--get-regexp"):
		val := strings.TrimSpace(c.Stdout)
		if val == "" {
			return ir.Report{Summary: "(unset)", Status: ir.StatusOK}
		}
		return ir.Report{Summary: val, Status: ir.StatusOK}
	default:
		// A set (`git config user.name x`) yields no output.
		out := strings.TrimSpace(c.Stdout)
		if out == "" {
			return ir.Report{Summary: "ok", Status: ir.StatusOK}
		}
		return ir.Report{Summary: out, Status: ir.StatusOK}
	}
}

var reflogRe = regexp.MustCompile(`^([0-9a-f]{4,40}) \S+@\{\d+\}: (.+)$`)

const reflogCap = 20

// parseReflog compacts reflog entries to "sha action: subject".
func parseReflog(c engine.CaptureResult) ir.Report {
	lines := splitLines(c.Stdout)
	if len(lines) == 0 {
		return ir.Report{Summary: "empty reflog", Status: ir.StatusOK}
	}
	var entries []string
	for _, l := range lines {
		if m := reflogRe.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			entries = append(entries, m[1]+" "+truncateRunes(m[2], logTruncateCols))
		} else if t := strings.TrimSpace(l); t != "" {
			entries = append(entries, truncateRunes(t, logTruncateCols))
		}
	}
	total := len(entries)
	noted := ""
	if total > reflogCap {
		entries = entries[:reflogCap]
		noted = fmt.Sprintf("… +%d more", total-reflogCap)
	}
	text := strings.Join(entries, "\n")
	if noted != "" {
		text += "\n" + noted
	}
	return ir.Report{Summary: fmt.Sprintf("%d reflog entries", total), Status: ir.StatusOK, Text: text}
}

var shortlogRe = regexp.MustCompile(`^\s*(\d+)\s+(.+?):?\s*$`)

// parseShortlog turns "<count>\t<author>" rows into author→count Items.
func parseShortlog(c engine.CaptureResult) ir.Report {
	var items []ir.Item
	total := 0
	for _, l := range splitLines(c.Stdout) {
		if m := shortlogRe.FindStringSubmatch(l); m != nil {
			n := leadingInt(m[1])
			total += n
			items = append(items, ir.Item{Key: strings.TrimSpace(m[2]), Val: m[1]})
		}
	}
	if len(items) == 0 {
		// Non-numbered shortlog (grouped by author with subjects) — pass trimmed.
		return ir.Report{Summary: "shortlog", Status: ir.StatusOK, Text: strings.TrimRight(c.Stdout, "\n")}
	}
	return ir.Report{
		Summary: fmt.Sprintf("%d authors, %d commits", len(items), total),
		Status:  ir.StatusOK,
		Items:   capItems(items, listCap),
	}
}

// parseOneValue surfaces a short single-line output (describe, rev-parse) as the
// Summary instead of passing through. Multi-line output (e.g. rev-parse of many
// refs) is kept as Text.
func parseOneValue(c engine.CaptureResult, sub string) ir.Report {
	lines := nonEmptyLines(c.Stdout)
	switch len(lines) {
	case 0:
		return ir.Report{Summary: "ok " + sub, Status: ir.StatusOK}
	case 1:
		return ir.Report{Summary: lines[0], Status: ir.StatusOK}
	default:
		return ir.Report{
			Summary: fmt.Sprintf("%d values", len(lines)),
			Status:  ir.StatusOK,
			Text:    strings.Join(capList(lines, listCap), "\n"),
		}
	}
}

var blameRe = regexp.MustCompile(`^\^?([0-9a-f]{4,40})\s+\(([^)]*)\)\s?(.*)$`)

const blameCap = 200

// parseBlame compacts blame lines to "sha (author date) line".
func parseBlame(c engine.CaptureResult) ir.Report {
	lines := splitLines(c.Stdout)
	var out []string
	for _, l := range lines {
		if m := blameRe.FindStringSubmatch(l); m != nil {
			sha := m[1]
			if len(sha) > 8 {
				sha = sha[:8]
			}
			out = append(out, fmt.Sprintf("%s (%s) %s", sha, compactBlameMeta(m[2]), m[3]))
		} else {
			out = append(out, l)
		}
	}
	total := len(out)
	noted := ""
	if total > blameCap {
		out = out[:blameCap]
		noted = fmt.Sprintf("… +%d more lines", total-blameCap)
	}
	text := strings.Join(out, "\n")
	if noted != "" {
		text += "\n" + noted
	}
	return ir.Report{Summary: fmt.Sprintf("%d lines", total), Status: ir.StatusOK, Text: text}
}

// compactBlameMeta trims the "author YYYY-MM-DD HH:MM:SS +zzzz lineno" blame
// annotation to "author YYYY-MM-DD".
func compactBlameMeta(s string) string {
	fields := strings.Fields(s)
	if len(fields) == 0 {
		return s
	}
	// last field is the line number; the date is fields after the author name.
	for i, f := range fields {
		if dateRe.MatchString(f) {
			author := strings.Join(fields[:i], " ")
			return strings.TrimSpace(author + " " + f)
		}
	}
	return strings.TrimSpace(s)
}

var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// parseBisect keeps the meaningful status lines (good/bad/steps remaining).
func parseBisect(c engine.CaptureResult) ir.Report {
	all := c.Stdout + "\n" + c.Stderr
	var keep []string
	for _, l := range splitLines(all) {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if strings.Contains(t, "is the first bad commit") ||
			strings.Contains(t, "bisecting") || strings.HasPrefix(t, "Bisecting:") ||
			strings.HasPrefix(t, "[") || strings.Contains(t, "revisions left") {
			keep = append(keep, t)
		}
	}
	if len(keep) == 0 {
		return ir.Report{Summary: "ok bisect", Status: ir.StatusOK}
	}
	return ir.Report{Summary: "bisect", Status: ir.StatusOK, Text: strings.Join(keep, "\n")}
}

// parseLsFiles caps a file listing.
func parseLsFiles(c engine.CaptureResult) ir.Report {
	lines := nonEmptyLines(c.Stdout)
	if len(lines) == 0 {
		return ir.Report{Summary: "0 files", Status: ir.StatusOK}
	}
	return ir.Report{
		Summary: fmt.Sprintf("%d files", len(lines)),
		Status:  ir.StatusOK,
		Text:    strings.Join(capList(lines, listCap), "\n"),
	}
}

// parseGrep groups matches by file, capping files and per-file matches.
func parseGrep(c engine.CaptureResult) ir.Report {
	lines := nonEmptyLines(c.Stdout)
	if len(lines) == 0 {
		return ir.Report{Summary: "no matches", Status: ir.StatusOK}
	}
	type group struct {
		file  string
		lines []string
	}
	var order []string
	groups := map[string]*group{}
	for _, l := range lines {
		// "path:lineno:content" or "path:content"
		file := l
		if i := strings.Index(l, ":"); i >= 0 {
			file = l[:i]
		}
		g, ok := groups[file]
		if !ok {
			g = &group{file: file}
			groups[file] = g
			order = append(order, file)
		}
		g.lines = append(g.lines, l)
	}
	var b strings.Builder
	total := len(lines)
	for _, f := range capList(order, listCap) {
		if strings.HasPrefix(f, "… +") {
			b.WriteString(f + "\n")
			continue
		}
		for _, ml := range capList(groups[f].lines, 10) {
			b.WriteString(truncateRunes(ml, 160) + "\n")
		}
	}
	return ir.Report{
		Summary: fmt.Sprintf("%d matches in %d files", total, len(order)),
		Status:  ir.StatusOK,
		Text:    strings.TrimRight(b.String(), "\n"),
	}
}

// parseRmMv compacts `git rm`/`git mv`: "rm 'file'" lines → count.
func parseRmMv(c engine.CaptureResult, verb string) ir.Report {
	n := 0
	for _, l := range splitLines(c.Stdout) {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, verb+" '") || strings.HasPrefix(t, "rename ") {
			n++
		}
	}
	if n > 0 {
		return ir.Report{Summary: fmt.Sprintf("ok %s %d files", verb, n), Status: ir.StatusOK}
	}
	return ir.Report{Summary: "ok " + verb, Status: ir.StatusOK}
}

// --- small shared helpers ---

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range splitLines(s) {
		if strings.TrimSpace(l) != "" {
			out = append(out, l)
		}
	}
	return out
}

func hasFlag(args []string, flags ...string) bool {
	for _, a := range args {
		for _, f := range flags {
			if a == f {
				return true
			}
		}
	}
	return false
}

func firstArg(args []string) string {
	for _, a := range args {
		if a != "" && !strings.HasPrefix(a, "-") {
			return a
		}
	}
	return ""
}

func capItems(items []ir.Item, n int) []ir.Item {
	if len(items) <= n {
		return items
	}
	out := append([]ir.Item{}, items[:n]...)
	return append(out, ir.Item{Key: fmt.Sprintf("… +%d", len(items)-n)})
}
