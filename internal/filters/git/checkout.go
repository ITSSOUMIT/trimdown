package git

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
)

var (
	switchedToRe = regexp.MustCompile(`^Switched to (?:a new )?branch '([^']+)'`)
	resetHeadRe  = regexp.MustCompile(`HEAD is now at ([0-9a-f]{4,40}) (.*)`)
)

// parseCheckout handles `git checkout` and `git switch`: branch switches,
// up-to-date notices, and the "local changes would be overwritten" error.
func parseCheckout(c engine.CaptureResult, inv invocation) ir.Report {
	all := c.Stdout + "\n" + c.Stderr
	if c.ExitCode != 0 {
		return checkoutError(all)
	}
	for _, l := range splitLines(all) {
		l = strings.TrimSpace(l)
		if m := switchedToRe.FindStringSubmatch(l); m != nil {
			return ir.Report{Summary: "ok → " + m[1], Status: ir.StatusOK}
		}
		if strings.HasPrefix(l, "Already on '") {
			name := strings.TrimSuffix(strings.TrimPrefix(l, "Already on '"), "'")
			return ir.Report{Summary: "ok (already on " + name + ")", Status: ir.StatusOK}
		}
		if strings.HasPrefix(l, "HEAD is now at ") {
			return ir.Report{Summary: "ok (detached " + strings.TrimPrefix(l, "HEAD is now at ") + ")", Status: ir.StatusOK}
		}
	}
	// File restores: "Updated N paths from the index" or quiet success.
	if n := updatedPaths(all); n > 0 {
		return ir.Report{Summary: fmt.Sprintf("ok restored %d files", n), Status: ir.StatusOK}
	}
	return ir.Report{Summary: "ok", Status: ir.StatusOK}
}

// parseRestore handles `git restore`: usually silent on success.
func parseRestore(c engine.CaptureResult, _ invocation) ir.Report {
	all := c.Stdout + "\n" + c.Stderr
	if c.ExitCode != 0 {
		return checkoutError(all)
	}
	if n := updatedPaths(all); n > 0 {
		return ir.Report{Summary: fmt.Sprintf("ok restored %d files", n), Status: ir.StatusOK}
	}
	return ir.Report{Summary: "ok", Status: ir.StatusOK}
}

// checkoutError compacts the common "local changes would be overwritten" error
// to its offending file list; unrecognized errors → zero Report (raw fallback).
func checkoutError(all string) ir.Report {
	if !strings.Contains(all, "would be overwritten") {
		return ir.Report{}
	}
	var files []string
	for _, l := range splitLines(all) {
		t := strings.TrimSpace(l)
		// git lists each blocking path indented as "\t<path>".
		if strings.HasPrefix(l, "\t") && t != "" {
			files = append(files, t)
		}
	}
	rep := ir.Report{
		Summary: "✗ local changes would be overwritten",
		Status:  ir.StatusFail,
	}
	for _, f := range capList(files, conflictCap) {
		rep.Items = append(rep.Items, ir.Item{Key: f})
	}
	rep.Notes = append(rep.Notes, "commit or stash before switching")
	return rep
}

var updatedPathsRe = regexp.MustCompile(`Updated (\d+) paths? from`)

func updatedPaths(s string) int {
	if m := updatedPathsRe.FindStringSubmatch(s); m != nil {
		return leadingInt(m[1])
	}
	return 0
}

// parseReset compacts `git reset`: "HEAD is now at <sha> <subj>" and the
// "Unstaged changes after reset" file list → a one-line summary.
func parseReset(c engine.CaptureResult) ir.Report {
	all := c.Stdout + "\n" + c.Stderr
	if m := resetHeadRe.FindStringSubmatch(all); m != nil {
		sha := m[1]
		if len(sha) > 7 {
			sha = sha[:7]
		}
		return ir.Report{
			Summary: "ok HEAD→" + sha + " " + truncateRunes(strings.TrimSpace(m[2]), 60),
			Status:  ir.StatusOK,
		}
	}
	// Mixed reset prints "Unstaged changes after reset:" + paths.
	n := 0
	for _, l := range splitLines(c.Stdout) {
		if strings.HasPrefix(l, "M\t") || strings.HasPrefix(l, "D\t") || strings.HasPrefix(l, "A\t") {
			n++
		}
	}
	if n > 0 {
		return ir.Report{Summary: fmt.Sprintf("ok reset (%d unstaged)", n), Status: ir.StatusOK}
	}
	return ir.Report{Summary: "ok reset", Status: ir.StatusOK}
}

// parseApply compacts `git apply`: silent success or a compacted error.
func parseApply(c engine.CaptureResult) ir.Report {
	if c.ExitCode != 0 {
		errs := errorLines(c.Stderr)
		if len(errs) == 0 {
			return ir.Report{}
		}
		rep := ir.Report{Summary: fmt.Sprintf("✗ apply: %d errors", len(errs)), Status: ir.StatusFail}
		rep.Text = strings.Join(capList(errs, conflictCap), "\n")
		return rep
	}
	return ir.Report{Summary: "ok apply", Status: ir.StatusOK}
}

// parseAm compacts `git am`: success → applied count; conflict/error → compacted.
func parseAm(c engine.CaptureResult) ir.Report {
	all := c.Stdout + "\n" + c.Stderr
	if c.ExitCode != 0 {
		if files := conflictedFiles(all); len(files) > 0 {
			return conflictReport("am", files)
		}
		errs := errorLines(all)
		if len(errs) == 0 {
			return ir.Report{}
		}
		return ir.Report{Summary: "✗ am failed", Status: ir.StatusFail, Text: strings.Join(capList(errs, conflictCap), "\n")}
	}
	n := 0
	for _, l := range splitLines(c.Stdout) {
		if strings.HasPrefix(strings.TrimSpace(l), "Applying:") {
			n++
		}
	}
	if n > 0 {
		return ir.Report{Summary: fmt.Sprintf("ok am (%d applied)", n), Status: ir.StatusOK}
	}
	return ir.Report{Summary: "ok am", Status: ir.StatusOK}
}

// errorLines collects lines beginning with "error:" or "fatal:".
func errorLines(s string) []string {
	var out []string
	for _, l := range splitLines(s) {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "error:") || strings.HasPrefix(t, "fatal:") {
			out = append(out, t)
		}
	}
	return out
}
