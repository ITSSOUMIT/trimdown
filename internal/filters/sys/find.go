package sys

import (
	"fmt"
	"path"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

const (
	maxFindDirs   = 50  // cap directory groups
	maxFindPerDir = 20  // sample paths shown per directory before collapsing
	findNameWidth = 160 // truncate very long paths
)

// findFilter parses `find` output (a flat list of paths) into a directory-
// grouped rollup: one Item per parent directory with a match count and a capped
// sample of basenames, instead of a blind line cap. The grouping is the
// structure inherent to find's output.
type findFilter struct{}

func (findFilter) Tool() string       { return "find" }
func (findFilter) Subcommand() string { return "" }

func (findFilter) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand("find", o.Args...))
}

func (findFilter) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	if c.ExitCode != 0 {
		if msg, ok := compactError(c.Stderr); ok {
			return ir.Report{
				Tool:     "find",
				Summary:  msg,
				Status:   ir.StatusFail,
				Filtered: true,
				Raw:      c.Stdout + c.Stderr,
				ExitCode: c.ExitCode,
			}, nil
		}
		// find commonly exits non-zero on permission errors while still printing
		// valid results on stdout; only passthrough when there's nothing usable.
		if strings.TrimSpace(c.Stdout) == "" {
			return ir.RawReport("find", c.Stdout+c.Stderr, c.ExitCode), nil
		}
	}

	var paths []string
	for _, p := range splitLines(c.Stdout) {
		if strings.TrimSpace(p) == "" {
			continue
		}
		paths = append(paths, p)
	}
	if len(paths) == 0 {
		return ir.Report{Tool: "find", Summary: "0 paths", Status: ir.StatusOK, Filtered: true, Raw: c.Stdout}, nil
	}

	// Group basenames by parent directory, preserving first-seen order.
	members := map[string][]string{}
	var order []string
	for _, p := range paths {
		dir := path.Dir(p)
		if _, ok := members[dir]; !ok {
			order = append(order, dir)
		}
		members[dir] = append(members[dir], path.Base(p))
	}

	var items []ir.Item
	var notes []string
	for gi, dir := range order {
		if gi >= maxFindDirs {
			notes = append(notes, fmt.Sprintf("… +%d more directories", len(order)-maxFindDirs))
			break
		}
		ms := members[dir]
		items = append(items, ir.Item{
			Key: truncateRunes(dir, findNameWidth) + "/",
			Val: fmt.Sprintf("(%d)", len(ms)),
		})
		for mi, name := range ms {
			if mi >= maxFindPerDir {
				items = append(items, ir.Item{Key: fmt.Sprintf("  … +%d more", len(ms)-maxFindPerDir)})
				break
			}
			items = append(items, ir.Item{Key: "  " + truncateRunes(name, findNameWidth)})
		}
	}

	return ir.Report{
		Tool:     "find",
		Summary:  fmt.Sprintf("%d paths in %d directories", len(paths), len(order)),
		Status:   ir.StatusOK,
		Items:    items,
		Notes:    notes,
		Filtered: true,
		Raw:      c.Stdout,
	}, nil
}
