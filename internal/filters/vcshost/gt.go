package vcshost

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

// gt is the Graphite CLI dispatcher. Graphite is TEXT only (no JSON), so every
// parser compacts the human output.
type gt struct{}

func (gt) Tool() string       { return "gt" }
func (gt) Subcommand() string { return "" }

func (gt) Exec(o registry.Opts) engine.CaptureResult {
	return resolveExec("gt", o.Args)
}

var (
	// A PR reference within gt log/submit output, e.g. "#1234" or
	// "https://app.graphite.dev/.../pull/1234".
	gtPRNumRE = regexp.MustCompile(`#(\d+)\b`)
	// Branch-tree drawing chars at the start of a gt log line.
	gtTreePrefixRE = regexp.MustCompile(`^[\s│─├└┘┐◯◉●○*|\\/+-]+`)
	// "Submitted" / "Created" PR lines.
	gtSubmittedRE = regexp.MustCompile(`(?i)\b(submitted|created|updated)\b`)
	gtConflictRE  = regexp.MustCompile(`(?i)conflict`)
)

func (gt) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	raw := rawOf(c)
	sub := firstNonFlag(o.Args)

	if c.ExitCode != 0 {
		return ir.RawReport("gt", raw, c.ExitCode), nil
	}

	switch sub {
	case "log", "ls", "l":
		return gtLog(c, raw), nil
	case "submit", "s":
		return gtSubmit(c, raw), nil
	case "sync":
		return gtSync(c, raw), nil
	case "restack", "fix", "r":
		return gtRestack(c, raw), nil
	case "create", "c", "branch", "b", "modify", "m":
		return gtCreate(sub, c, raw), nil
	case "status", "st":
		return gtStatus(c, raw), nil
	default:
		return ir.RawReport("gt", raw, c.ExitCode), nil
	}
}

// gtLog compacts the stack view into one line per branch, keeping the branch
// name and any PR number/status marker.
func gtLog(c engine.CaptureResult, raw string) ir.Report {
	src := engine.StripANSI(c.Stdout)
	var items []ir.Item
	for _, l := range splitLines(src) {
		stripped := gtTreePrefixRE.ReplaceAllString(l, "")
		stripped = strings.TrimSpace(stripped)
		if stripped == "" {
			continue
		}
		// The first token is the branch name; PR info follows.
		fields := strings.Fields(stripped)
		branch := fields[0]
		var extras []string
		if m := gtPRNumRE.FindStringSubmatch(stripped); m != nil {
			extras = append(extras, "#"+m[1])
		}
		// Markers Graphite adds, e.g. "(current)", "(needs restack)".
		for _, marker := range []string{"current", "needs restack", "merged", "closed"} {
			if strings.Contains(strings.ToLower(stripped), marker) {
				extras = append(extras, marker)
			}
		}
		items = append(items, ir.Item{Key: truncateRunes(branch, 80), Val: strings.Join(extras, " ")})
	}
	total := len(items)
	items, note := capItems(items, maxVCSItems)
	rep := keyValReport("gt", "log", fmt.Sprintf("%d branches", total), ir.StatusOK, items, raw)
	if note != "" {
		rep.Notes = []string{note}
	}
	return rep
}

func gtSubmit(c engine.CaptureResult, raw string) ir.Report {
	src := engine.StripANSI(c.Stdout + "\n" + c.Stderr)
	var lines []string
	for _, l := range splitLines(src) {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if gtSubmittedRE.MatchString(t) || gtPRNumRE.MatchString(t) {
			lines = append(lines, truncateRunes(t, vcsTruncateCol))
		}
	}
	lines, note := capStrings(lines, maxVCSLines)
	summary := fmt.Sprintf("submitted %d", len(lines))
	rep := ir.Report{
		Tool: "gt", Subcommand: "submit", Summary: summary, Status: ir.StatusOK,
		Text: strings.Join(lines, "\n"), Filtered: true, Raw: raw,
	}
	if note != "" {
		rep.Notes = []string{note}
	}
	return rep
}

func gtSync(c engine.CaptureResult, raw string) ir.Report {
	src := engine.StripANSI(c.Stdout + "\n" + c.Stderr)
	var lines []string
	conflict := false
	for _, l := range splitLines(src) {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if gtConflictRE.MatchString(t) {
			conflict = true
		}
		lines = append(lines, truncateRunes(t, vcsTruncateCol))
	}
	lines, note := capStrings(lines, maxVCSLines)
	status := ir.StatusOK
	summary := "synced"
	if conflict {
		status = ir.StatusWarn
		summary = "synced (conflicts)"
	}
	rep := ir.Report{
		Tool: "gt", Subcommand: "sync", Summary: summary, Status: status,
		Text: strings.Join(lines, "\n"), Filtered: true, Raw: raw,
	}
	if note != "" {
		rep.Notes = []string{note}
	}
	return rep
}

func gtRestack(c engine.CaptureResult, raw string) ir.Report {
	src := engine.StripANSI(c.Stdout + "\n" + c.Stderr)
	conflict := gtConflictRE.MatchString(src)
	status := ir.StatusOK
	summary := "restacked"
	var text string
	if conflict {
		status = ir.StatusFail
		summary = "restack conflict"
		var lines []string
		for _, l := range splitLines(src) {
			t := strings.TrimSpace(l)
			if t != "" {
				lines = append(lines, truncateRunes(t, vcsTruncateCol))
			}
		}
		lines, _ = capStrings(lines, maxVCSLines)
		text = strings.Join(lines, "\n")
	}
	return ir.Report{
		Tool: "gt", Subcommand: "restack", Summary: summary, Status: status,
		Text: text, Filtered: true, Raw: raw,
	}
}

func gtCreate(sub string, c engine.CaptureResult, raw string) ir.Report {
	src := engine.StripANSI(c.Stdout + "\n" + c.Stderr)
	// Keep the first meaningful line as a summary (e.g. created branch name).
	for _, l := range splitLines(src) {
		t := strings.TrimSpace(l)
		if t != "" {
			return ir.Report{
				Tool: "gt", Subcommand: sub, Summary: truncateRunes(t, vcsTruncateCol),
				Status: ir.StatusOK, Filtered: true, Raw: raw,
			}
		}
	}
	return ir.Report{Tool: "gt", Subcommand: sub, Summary: "ok", Status: ir.StatusOK, Filtered: true, Raw: raw}
}

func gtStatus(c engine.CaptureResult, raw string) ir.Report {
	return compactTable("gt", "status", c)
}

// capStrings caps a string slice and returns an overflow note ("" if none).
func capStrings(lines []string, n int) ([]string, string) {
	if len(lines) <= n {
		return lines, ""
	}
	return lines[:n], fmt.Sprintf("… +%d more lines", len(lines)-n)
}
