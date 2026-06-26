package sys

import (
	"regexp"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

const (
	maxTreeDepth  = 3   // keep this many indent levels; deeper is summarized
	maxTreePerDir = 20  // entries shown per directory before collapsing
	maxTreeLines  = 200 // overall safety cap on emitted body lines
	treeLineWidth = 160
)

// treeIndentUnitRE counts a single indent unit at the start of a line.
var treeIndentUnitRE = regexp.MustCompile(`^([│ ]   |    )`)

// treeSummaryRE matches the trailing "N directories, M files" footer.
var treeSummaryRE = regexp.MustCompile(`^\d+ director(?:y|ies)(?:, \d+ files?)?$`)

// treeFilter parses `tree` output, capping depth and per-directory entries in a
// structure-aware way (top levels kept, deep/large subtrees summarized) while
// preserving the connectors and the trailing summary line.
type treeFilter struct{}

func (treeFilter) Tool() string       { return "tree" }
func (treeFilter) Subcommand() string { return "" }

func (treeFilter) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand("tree", o.Args...))
}

func (treeFilter) Parse(c engine.CaptureResult, _ registry.Opts) (ir.Report, error) {
	if c.ExitCode != 0 {
		if msg, ok := compactError(c.Stderr); ok {
			return ir.Report{
				Tool:     "tree",
				Summary:  msg,
				Status:   ir.StatusFail,
				Filtered: true,
				Raw:      c.Stdout + c.Stderr,
				ExitCode: c.ExitCode,
			}, nil
		}
		return ir.RawReport("tree", c.Stdout+c.Stderr, c.ExitCode), nil
	}

	lines := splitLines(engine.StripANSI(c.Stdout))
	summary := ""

	var body []string
	// depthOver tracks, per indent depth, how many entries we've shown in the
	// current run at that depth so we can collapse over-long directories.
	shown := map[int]int{}
	skippedDeep := 0
	collapsed := 0
	var prevDepth int

	for _, l := range lines {
		if l == "" {
			continue
		}
		if treeSummaryRE.MatchString(strings.TrimSpace(l)) {
			summary = strings.TrimSpace(l)
			continue
		}

		depth := treeDepth(l)

		// Reset deeper counters when we ascend (a new sibling subtree begins).
		if depth <= prevDepth {
			for d := range shown {
				if d > depth {
					delete(shown, d)
				}
			}
		}
		prevDepth = depth

		// Drop entries deeper than the allowed depth; count them for a note.
		if depth > maxTreeDepth {
			skippedDeep++
			continue
		}

		// Collapse over-long directory listings at a given depth.
		if shown[depth] >= maxTreePerDir {
			collapsed++
			continue
		}
		shown[depth]++

		if len(body) >= maxTreeLines {
			collapsed++
			continue
		}
		body = append(body, truncateRunes(l, treeLineWidth))
	}

	var notes []string
	if skippedDeep > 0 {
		notes = append(notes, sprintfDeep(skippedDeep))
	}
	if collapsed > 0 {
		notes = append(notes, sprintfCollapsed(collapsed))
	}

	return ir.Report{
		Tool:     "tree",
		Summary:  summary,
		Status:   ir.StatusOK,
		Text:     strings.Join(body, "\n"),
		Notes:    notes,
		Filtered: true,
		Raw:      c.Stdout,
	}, nil
}

// treeDepth returns the indentation depth (0 = root entry) of a tree line by
// counting indent units before its connector. The root line (no connector,
// e.g. ".") is depth 0.
func treeDepth(l string) int {
	if !strings.Contains(l, "── ") {
		return 0
	}
	depth := 1
	rest := l
	for {
		m := treeIndentUnitRE.FindString(rest)
		if m == "" {
			break
		}
		// Stop once we reach the connector segment.
		if strings.HasPrefix(rest, "├── ") || strings.HasPrefix(rest, "└── ") {
			break
		}
		depth++
		rest = rest[len(m):]
	}
	return depth
}

func sprintfDeep(n int) string {
	return "… +" + itoaSys(n) + " deeper entries (use --raw)"
}

func sprintfCollapsed(n int) string {
	return "… +" + itoaSys(n) + " collapsed entries"
}

func itoaSys(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
