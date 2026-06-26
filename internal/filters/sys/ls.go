package sys

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

const (
	maxLsEntries = 100 // cap entries in plain / long listings
	maxLsDirs    = 50  // cap directory groups in recursive listings
	maxLsPerDir  = 40  // cap entries per directory in recursive listings
	lsNameWidth  = 120 // truncate very long names
)

// lsPermsRE matches the leading type+permission field of an `ls -l` row
// (e.g. "drwxr-xr-x", "-rw-r--r--@"). Field-based parsing of the row is more
// robust than one big regex across BSD/GNU date-format differences.
var lsPermsRE = regexp.MustCompile(`^[dlbcps-][rwxsStT-]{9}[.+@]?$`)

// lsTotalRE matches the "total N" header emitted by `ls -l`.
var lsTotalRE = regexp.MustCompile(`^total\s+\d+$`)

// lsFilter parses ls output (plain, -l/-la long, -R recursive) into a structured
// entry list with directory/file counts, instead of a blind line cap.
type lsFilter struct{}

func (lsFilter) Tool() string       { return "ls" }
func (lsFilter) Subcommand() string { return "" }

func (lsFilter) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand("ls", o.Args...))
}

func (lsFilter) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	if c.ExitCode != 0 {
		if msg, ok := compactError(c.Stderr); ok {
			return ir.Report{
				Tool:     "ls",
				Summary:  msg,
				Status:   ir.StatusFail,
				Filtered: true,
				Raw:      c.Stdout + c.Stderr,
				ExitCode: c.ExitCode,
			}, nil
		}
		return ir.RawReport("ls", c.Stdout+c.Stderr, c.ExitCode), nil
	}

	long := lsHasFlag(o.Args, 'l')
	recursive := lsHasFlag(o.Args, 'R')

	if recursive {
		return lsParseRecursive(c.Stdout), nil
	}
	if long {
		return lsParseLong(splitLines(c.Stdout), c.Stdout), nil
	}
	return lsParsePlain(splitLines(c.Stdout), c.Stdout), nil
}

// lsEntry is a single directory entry with a derived dir/file classification.
type lsEntry struct {
	name  string
	size  string // present for -l rows
	isDir bool
}

// classifyName uses -F style suffixes (/ for dir, * exec, @ symlink, etc.) to
// decide dir vs file, then strips the indicator from the displayed name.
func classifyName(name string) lsEntry {
	if name == "" {
		return lsEntry{name: name}
	}
	last := name[len(name)-1]
	switch last {
	case '/':
		return lsEntry{name: name[:len(name)-1], isDir: true}
	case '*', '@', '=', '|':
		return lsEntry{name: name[:len(name)-1]}
	}
	return lsEntry{name: name}
}

func lsParsePlain(lines []string, raw string) ir.Report {
	var entries []lsEntry
	for _, l := range lines {
		l = strings.TrimRight(l, " ")
		if l == "" {
			continue
		}
		// Plain ls may be columnar (multiple names per line, space-padded).
		for _, f := range strings.Fields(l) {
			entries = append(entries, classifyName(f))
		}
	}
	return lsBuildReport(entries, raw, false)
}

func lsParseLong(lines []string, raw string) ir.Report {
	var entries []lsEntry
	for _, l := range lines {
		if l == "" || lsTotalRE.MatchString(l) {
			continue
		}
		// Standard `ls -l` layout: perms links owner group size  month day time  name
		// (9+ fields; the name is everything from field 8 on, allowing spaces).
		fields := strings.Fields(l)
		if len(fields) < 9 || !lsPermsRE.MatchString(fields[0]) {
			// Unparseable long row (unusual locale/format); fall back to name.
			if len(fields) > 0 {
				entries = append(entries, classifyName(fields[len(fields)-1]))
			}
			continue
		}
		name := strings.Join(fields[8:], " ")
		// `-l` shows symlinks as "name -> target"; keep just the link name.
		if i := strings.Index(name, " -> "); i >= 0 {
			name = name[:i]
		}
		e := classifyName(name)
		e.size = humanSize(fields[4])
		if fields[0][0] == 'd' {
			e.isDir = true
		}
		entries = append(entries, e)
	}
	return lsBuildReport(entries, raw, true)
}

func lsBuildReport(entries []lsEntry, raw string, withSize bool) ir.Report {
	dirs, files := 0, 0
	for _, e := range entries {
		if e.isDir {
			dirs++
		} else {
			files++
		}
	}

	var items []ir.Item
	var notes []string
	for i, e := range entries {
		if i >= maxLsEntries {
			notes = append(notes, fmt.Sprintf("… +%d more", len(entries)-maxLsEntries))
			break
		}
		key := truncateRunes(e.name, lsNameWidth)
		if e.isDir {
			key += "/"
		}
		val := ""
		if withSize {
			val = e.size
		}
		items = append(items, ir.Item{Key: key, Val: val})
	}

	return ir.Report{
		Tool:     "ls",
		Summary:  fmt.Sprintf("%d entries (%d dirs, %d files)", len(entries), dirs, files),
		Status:   ir.StatusOK,
		Items:    items,
		Notes:    notes,
		Filtered: true,
		Raw:      raw,
	}
}

func lsParseRecursive(raw string) ir.Report {
	lines := splitLines(raw)
	type group struct {
		header  string
		entries []lsEntry
	}
	var groups []group
	var cur *group

	flush := func() {
		if cur != nil {
			groups = append(groups, *cur)
			cur = nil
		}
	}

	totalEntries := 0
	for _, l := range lines {
		trimmed := strings.TrimRight(l, " ")
		if trimmed == "" {
			continue
		}
		// A directory header line ends with ':' (e.g. "./sub:").
		if strings.HasSuffix(trimmed, ":") {
			flush()
			h := strings.TrimSuffix(trimmed, ":")
			cur = &group{header: h}
			continue
		}
		if lsTotalRE.MatchString(trimmed) {
			continue
		}
		if cur == nil {
			// Output before any header (single dir with no headers): synthesize.
			cur = &group{header: "."}
		}
		// Recursive output without -l: one or more names per line.
		for _, f := range strings.Fields(trimmed) {
			cur.entries = append(cur.entries, classifyName(f))
			totalEntries++
		}
	}
	flush()

	var items []ir.Item
	var notes []string
	for gi, g := range groups {
		if gi >= maxLsDirs {
			notes = append(notes, fmt.Sprintf("… +%d more directories", len(groups)-maxLsDirs))
			break
		}
		items = append(items, ir.Item{
			Key: g.header + "/",
			Val: fmt.Sprintf("(%d entries)", len(g.entries)),
		})
		for ei, e := range g.entries {
			if ei >= maxLsPerDir {
				items = append(items, ir.Item{Key: fmt.Sprintf("  … +%d more", len(g.entries)-maxLsPerDir)})
				break
			}
			key := "  " + truncateRunes(e.name, lsNameWidth)
			if e.isDir {
				key += "/"
			}
			items = append(items, ir.Item{Key: key})
		}
	}

	return ir.Report{
		Tool:     "ls",
		Summary:  fmt.Sprintf("%d entries in %d directories", totalEntries, len(groups)),
		Status:   ir.StatusOK,
		Items:    items,
		Notes:    notes,
		Filtered: true,
		Raw:      raw,
	}
}

// lsHasFlag reports whether any clustered short-flag argument (e.g. "-la")
// contains the given flag letter.
func lsHasFlag(args []string, flag byte) bool {
	for _, a := range args {
		if len(a) < 2 || a[0] != '-' || a[1] == '-' {
			continue
		}
		for i := 1; i < len(a); i++ {
			if a[i] == flag {
				return true
			}
		}
	}
	return false
}

// humanSize converts a byte count string to a compact human-readable form.
func humanSize(s string) string {
	n := atoiSafe(s)
	if n == 0 && s != "0" {
		return s
	}
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	v := int64(n)
	for v/div >= unit && exp < 3 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(v)/float64(div), "KMGT"[exp])
}
