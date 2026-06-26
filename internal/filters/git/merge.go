package git

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
)

// conflictRe matches git's per-file conflict notice, e.g.
// "CONFLICT (content): Merge conflict in src/foo.go".
var conflictRe = regexp.MustCompile(`^CONFLICT \(([^)]+)\): .*? in (.+)$`)

// stoppedAtRe matches a rebase "Could not apply <sha>... <subject>" stop point.
var stoppedAtRe = regexp.MustCompile(`Could not apply ([0-9a-f]{4,40})\.*\s*(.*)`)

// conflictedFiles scans output for CONFLICT lines and returns the file paths
// (deduped, order-preserving) plus the conflict types seen.
func conflictedFiles(s string) []string {
	seen := map[string]bool{}
	var files []string
	for _, l := range splitLines(s) {
		if m := conflictRe.FindStringSubmatch(strings.TrimSpace(l)); m != nil {
			path := strings.TrimSpace(m[2])
			if !seen[path] {
				seen[path] = true
				files = append(files, path)
			}
		}
	}
	return files
}

const conflictCap = 40

// conflictReport builds the shared "N conflicts" failure IR for merge/rebase/
// cherry-pick/revert. Returns a zero Report if no CONFLICT lines are present
// (so the caller can fall back to raw passthrough).
func conflictReport(verb string, files []string) ir.Report {
	if len(files) == 0 {
		return ir.Report{}
	}
	items := make([]ir.Item, 0, len(files))
	for _, f := range capList(files, conflictCap) {
		items = append(items, ir.Item{Key: f})
	}
	return ir.Report{
		Summary: fmt.Sprintf("✗ %s: %d conflicts", verb, len(files)),
		Status:  ir.StatusFail,
		Items:   items,
	}
}

// parseMerge compacts a merge: success → diffstat summary; conflict → file list.
func parseMerge(c engine.CaptureResult) ir.Report {
	all := c.Stdout + "\n" + c.Stderr
	if c.ExitCode != 0 {
		return conflictReport("merge", conflictedFiles(all))
	}
	if strings.Contains(all, "Already up to date") || strings.Contains(all, "Already up-to-date") {
		return ir.Report{Summary: "ok merge (up-to-date)", Status: ir.StatusOK}
	}
	ff := strings.Contains(all, "Fast-forward")
	files, ins, del := parseShortstat(all)
	summary := "ok merge"
	if ff {
		summary = "ok merge (fast-forward)"
	}
	if files > 0 {
		summary = fmt.Sprintf("%s: %d files +%d -%d", summary, files, ins, del)
	}
	return ir.Report{Summary: summary, Status: ir.StatusOK}
}

// parseRebase compacts a rebase: success → "Successfully rebased"; conflict →
// conflicted files + the sha it stopped at.
func parseRebase(c engine.CaptureResult, inv invocation) ir.Report {
	if hasFlag(inv.args, "-i", "--interactive") {
		// Interactive rebase: hand back raw (editor-driven, not compactable).
		return ir.Report{}
	}
	all := c.Stdout + "\n" + c.Stderr
	if c.ExitCode != 0 {
		rep := conflictReport("rebase", conflictedFiles(all))
		if rep.Summary == "" {
			return ir.Report{}
		}
		if m := stoppedAtRe.FindStringSubmatch(all); m != nil {
			sha := m[1]
			if len(sha) > 7 {
				sha = sha[:7]
			}
			subj := strings.TrimSpace(m[2])
			rep.Notes = append(rep.Notes, strings.TrimSpace("stopped at "+sha+" "+subj))
		}
		return rep
	}
	if strings.Contains(all, "is up to date") || strings.Contains(all, "up to date") {
		return ir.Report{Summary: "ok rebase (up-to-date)", Status: ir.StatusOK}
	}
	return ir.Report{Summary: "ok rebase", Status: ir.StatusOK}
}

// parseCherryPick handles cherry-pick and revert (same output shape): success →
// short summary; conflict → conflicted files.
func parseCherryPick(c engine.CaptureResult, verb string) ir.Report {
	all := c.Stdout + "\n" + c.Stderr
	if c.ExitCode != 0 {
		return conflictReport(verb, conflictedFiles(all))
	}
	// On success git prints "[branch sha] subject" like a commit.
	if rep := parseCommit(c.Stdout); rep.Summary != "ok" {
		return ir.Report{Summary: "ok " + verb + " " + strings.TrimPrefix(rep.Summary, "ok "), Status: ir.StatusOK}
	}
	return ir.Report{Summary: "ok " + verb, Status: ir.StatusOK}
}
