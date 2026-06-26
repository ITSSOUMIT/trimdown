package git

import (
	"fmt"
	"strings"

	"github.com/itssoumit/trimdown/internal/ir"
)

const (
	diffHunkCap  = 80  // max changed lines shown per hunk
	diffTotalCap = 300 // max body lines overall
)

type fileDiff struct {
	path    string
	added   int
	removed int
	body    []string // compacted hunk lines for this file
}

// parseDiff compacts a unified diff: a per-file +/- stat, then capped hunks.
func parseDiff(stdout string) ir.Report {
	if strings.TrimSpace(stdout) == "" {
		return ir.Report{Summary: "no changes", Status: ir.StatusOK}
	}
	files := splitDiffFiles(stdout)
	if len(files) == 0 {
		// Not a recognizable diff (e.g. --stat output); pass through trimmed.
		return ir.Report{Summary: "diff", Status: ir.StatusOK, Text: strings.TrimRight(stdout, "\n")}
	}

	var totalAdd, totalDel int
	var stat strings.Builder
	for _, f := range files {
		totalAdd += f.added
		totalDel += f.removed
		fmt.Fprintf(&stat, " %s | +%d -%d\n", f.path, f.added, f.removed)
	}

	var body strings.Builder
	emitted := 0
	truncated := false
	for _, f := range files {
		if emitted >= diffTotalCap {
			truncated = true
			break
		}
		for _, line := range f.body {
			if emitted >= diffTotalCap {
				truncated = true
				break
			}
			body.WriteString(line)
			body.WriteByte('\n')
			emitted++
		}
	}

	var b strings.Builder
	b.WriteString(strings.TrimRight(stat.String(), "\n"))
	if body.Len() > 0 {
		b.WriteString("\n\n")
		b.WriteString(strings.TrimRight(body.String(), "\n"))
	}
	if truncated {
		b.WriteString("\n… diff truncated (use --raw for full)")
	}

	return ir.Report{
		Summary: fmt.Sprintf("%d files, +%d -%d", len(files), totalAdd, totalDel),
		Status:  ir.StatusOK,
		Text:    b.String(),
	}
}

// splitDiffFiles parses a unified diff into per-file records with +/- counts
// and per-hunk-capped bodies.
func splitDiffFiles(diff string) []fileDiff {
	var files []fileDiff
	var cur *fileDiff
	hunkShown := 0

	flush := func() {
		if cur != nil {
			files = append(files, *cur)
		}
	}

	for _, line := range splitLines(diff) {
		switch {
		case strings.HasPrefix(line, "diff --git "):
			flush()
			f := fileDiff{path: parseDiffPath(line)}
			cur = &f
			hunkShown = 0
		case cur == nil:
			// preamble before any file (rare) — ignore
		case strings.HasPrefix(line, "@@"):
			hunkShown = 0
			cur.body = append(cur.body, hunkHeader(line))
		case strings.HasPrefix(line, "+++") || strings.HasPrefix(line, "---") ||
			strings.HasPrefix(line, "index ") || strings.HasPrefix(line, "new file") ||
			strings.HasPrefix(line, "deleted file") || strings.HasPrefix(line, "similarity ") ||
			strings.HasPrefix(line, "rename ") || strings.HasPrefix(line, "old mode") ||
			strings.HasPrefix(line, "new mode") || strings.HasPrefix(line, "Binary files"):
			// metadata: don't show, don't count
		case strings.HasPrefix(line, "+"):
			cur.added++
			if hunkShown < diffHunkCap {
				cur.body = append(cur.body, line)
				hunkShown++
			}
		case strings.HasPrefix(line, "-"):
			cur.removed++
			if hunkShown < diffHunkCap {
				cur.body = append(cur.body, line)
				hunkShown++
			}
		default:
			// context line — only keep within an already-started hunk
			if hunkShown > 0 && hunkShown < diffHunkCap {
				cur.body = append(cur.body, line)
				hunkShown++
			}
		}
	}
	flush()
	return files
}

func parseDiffPath(line string) string {
	// "diff --git a/path b/path" → "path"
	fields := strings.Fields(line)
	if len(fields) >= 4 {
		return strings.TrimPrefix(fields[2], "a/")
	}
	return "?"
}

func hunkHeader(line string) string {
	// Keep "@@ -a,b +c,d @@ context" but drop nothing — it's already compact.
	return truncateRunes(line, 100)
}

// parseShow keeps the commit metadata, then compacts the diff portion.
func parseShow(stdout string) ir.Report {
	idx := strings.Index(stdout, "\ndiff --git ")
	if idx < 0 {
		// No diff (e.g. a tag/blob) — pass through trimmed.
		return ir.Report{Summary: "show", Status: ir.StatusOK, Text: strings.TrimRight(stdout, "\n")}
	}
	head := strings.TrimRight(stdout[:idx], "\n")
	diffRep := parseDiff(stdout[idx+1:])
	text := head
	if diffRep.Text != "" {
		text += "\n\n" + diffRep.Text
	}
	return ir.Report{Summary: diffRep.Summary, Status: ir.StatusOK, Text: text}
}
