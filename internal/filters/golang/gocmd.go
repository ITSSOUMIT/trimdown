package golang

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func init() {
	registry.Register(goCmd{})
}

// Package-level regexes, compiled once.
var (
	// "go: downloading golang.org/x/text v0.3.0", "go: finding ...",
	// "go: extracting ...", "go: found ... in ...".
	goNoiseRE = regexp.MustCompile(`^go: (downloading|finding|extracting|found) `)
	// "go: added golang.org/x/text v0.3.0"
	goAddedRE = regexp.MustCompile(`^go: added (\S+) (\S+)$`)
	// "go: removed golang.org/x/text v0.3.0" (also "dropping")
	goRemovedRE = regexp.MustCompile(`^go: removed (\S+) (\S+)$`)
	// "go: upgraded golang.org/x/text v0.3.0 => v0.3.7"
	goUpgradedRE = regexp.MustCompile(`^go: upgraded (\S+) (\S+ => \S+)$`)
	// "go: downgraded golang.org/x/text v0.3.7 => v0.3.0"
	goDowngradedRE = regexp.MustCompile(`^go: downgraded (\S+) (\S+ => \S+)$`)
)

// goCmd is the whole-tool `go` dispatcher. It handles every `go` subcommand not
// claimed by a more specific filter (test/build/vet are registered separately
// and win in registry.Lookup). It switches on the first non-flag arg.
type goCmd struct{}

func (goCmd) Tool() string       { return "go" }
func (goCmd) Subcommand() string { return "" }

func (goCmd) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand("go", o.Args...))
}

func (goCmd) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	sub := firstNonFlag(o.Args)
	switch sub {
	case "mod":
		return parseGoMod(c, o)
	case "get":
		return parseGoGet(c)
	case "install":
		return parseGoInstall(c)
	case "run":
		return parseGoRun(c)
	case "fmt":
		return parseGoFmt(c)
	case "generate":
		return parseGoGenerate(c)
	case "list":
		return parseGoList(c)
	case "env":
		return parseGoEnv(c, o)
	case "clean", "fix", "version", "work":
		return parseGoShort(sub, c)
	default:
		// go doc, go tool, and anything unrecognized → passthrough.
		return ir.RawReport("go", rawOf(c), c.ExitCode), nil
	}
}

// firstNonFlag returns the first arg that isn't a flag (mirrors registry's).
func firstNonFlag(args []string) string {
	for _, a := range args {
		if a == "" || a[0] == '-' {
			continue
		}
		return a
	}
	return ""
}

// secondNonFlag returns the second non-flag arg (e.g. "tidy" in `go mod tidy`).
func secondNonFlag(args []string) string {
	seen := 0
	for _, a := range args {
		if a == "" || a[0] == '-' {
			continue
		}
		seen++
		if seen == 2 {
			return a
		}
	}
	return ""
}

// isGoNoise reports whether a line is download/find/extract progress noise.
func isGoNoise(line string) bool {
	return goNoiseRE.MatchString(line)
}

// keepLines returns the non-noise, non-empty lines from src (used for error
// surfacing). It strips "go: downloading/finding/..." chatter but keeps real
// errors and notices.
func keepLines(src string) []string {
	var out []string
	engine.ScanLines(engine.StripANSI(src), func(line string) {
		t := strings.TrimRight(line, " \t")
		if t == "" || isGoNoise(strings.TrimSpace(t)) {
			return
		}
		out = append(out, t)
	})
	return out
}

const maxKeepLines = 25

// parseGoMod handles `go mod tidy|download|verify|init|why|graph|edit`.
func parseGoMod(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	modSub := secondNonFlag(o.Args)
	label := "go mod"
	if modSub != "" {
		label = "go mod " + modSub
	}

	// graph/why/edit (-print) produce structured data on stdout that IS the
	// answer; only strip download noise but otherwise pass content through.
	switch modSub {
	case "graph", "why":
		return passthroughDataCmd(label, c)
	}

	src := c.Stderr
	if strings.TrimSpace(src) == "" {
		src = c.Stdout
	}

	var added, removed []ir.Item
	var kept []string
	engine.ScanLines(engine.StripANSI(src), func(line string) {
		t := strings.TrimSpace(line)
		if t == "" || isGoNoise(t) {
			return
		}
		if m := goAddedRE.FindStringSubmatch(t); m != nil {
			added = append(added, ir.Item{Key: m[1], Val: m[2]})
			return
		}
		if m := goRemovedRE.FindStringSubmatch(t); m != nil {
			removed = append(removed, ir.Item{Key: m[1], Val: "removed " + m[2]})
			return
		}
		kept = append(kept, strings.TrimRight(line, " \t"))
	})

	// Failure: surface kept (non-noise) lines, which carry the real error such
	// as "go: updates to go.mod needed" or version conflicts.
	if c.ExitCode != 0 {
		return ir.Report{
			Tool: "go", Subcommand: "mod",
			Summary:  label + " failed",
			Status:   ir.StatusFail,
			Notes:    capLines(kept, maxKeepLines),
			Filtered: true, Raw: rawOf(c),
		}, nil
	}

	var items []ir.Item
	items = append(items, added...)
	items = append(items, removed...)

	summary := "ok " + label
	if n := len(added) + len(removed); n > 0 {
		summary = fmt.Sprintf("%s: %d added, %d removed", label, len(added), len(removed))
	}

	rep := ir.Report{
		Tool: "go", Subcommand: "mod",
		Summary:  summary,
		Status:   ir.StatusOK,
		Items:    items,
		Filtered: true, Raw: rawOf(c),
	}
	// Keep any non-module informational lines (rare, e.g. notices) as notes.
	if len(items) == 0 && len(kept) > 0 {
		rep.Notes = capLines(kept, maxKeepLines)
	}
	return rep, nil
}

// passthroughDataCmd strips only download noise from stdout, keeping the data
// payload (go mod graph/why). Errors on stderr are surfaced as notes.
func passthroughDataCmd(label string, c engine.CaptureResult) (ir.Report, error) {
	lines := keepLines(c.Stdout)
	if c.ExitCode != 0 {
		errs := keepLines(c.Stderr)
		return ir.Report{
			Tool: "go", Subcommand: "mod",
			Summary:  label + " failed",
			Status:   ir.StatusFail,
			Notes:    capLines(errs, maxKeepLines),
			Filtered: true, Raw: rawOf(c),
		}, nil
	}
	if len(lines) == 0 {
		return ir.Report{
			Tool: "go", Subcommand: "mod",
			Summary: "ok " + label, Status: ir.StatusOK,
			Filtered: true, Raw: rawOf(c),
		}, nil
	}
	return ir.Report{
		Tool: "go", Subcommand: "mod",
		Summary:  fmt.Sprintf("%s: %d line(s)", label, len(lines)),
		Status:   ir.StatusOK,
		Text:     strings.Join(lines, "\n"),
		Filtered: true, Raw: rawOf(c),
	}, nil
}

// parseGoGet summarizes module changes from `go get`.
func parseGoGet(c engine.CaptureResult) (ir.Report, error) {
	src := c.Stderr
	if strings.TrimSpace(src) == "" {
		src = c.Stdout
	}

	var added, upgraded, downgraded, removed []ir.Item
	var kept []string
	engine.ScanLines(engine.StripANSI(src), func(line string) {
		t := strings.TrimSpace(line)
		if t == "" || isGoNoise(t) {
			return
		}
		switch {
		case goAddedRE.MatchString(t):
			m := goAddedRE.FindStringSubmatch(t)
			added = append(added, ir.Item{Key: m[1], Val: "added " + m[2]})
		case goUpgradedRE.MatchString(t):
			m := goUpgradedRE.FindStringSubmatch(t)
			upgraded = append(upgraded, ir.Item{Key: m[1], Val: "upgraded " + m[2]})
		case goDowngradedRE.MatchString(t):
			m := goDowngradedRE.FindStringSubmatch(t)
			downgraded = append(downgraded, ir.Item{Key: m[1], Val: "downgraded " + m[2]})
		case goRemovedRE.MatchString(t):
			m := goRemovedRE.FindStringSubmatch(t)
			removed = append(removed, ir.Item{Key: m[1], Val: "removed " + m[2]})
		default:
			kept = append(kept, strings.TrimRight(line, " \t"))
		}
	})

	if c.ExitCode != 0 {
		return ir.Report{
			Tool: "go", Subcommand: "get",
			Summary:  "go get failed",
			Status:   ir.StatusFail,
			Notes:    capLines(kept, maxKeepLines),
			Filtered: true, Raw: rawOf(c),
		}, nil
	}

	var items []ir.Item
	items = append(items, added...)
	items = append(items, upgraded...)
	items = append(items, downgraded...)
	items = append(items, removed...)

	n := len(items)
	summary := "ok go get (no changes)"
	if n > 0 {
		summary = fmt.Sprintf("go get: %d added, %d upgraded, %d downgraded, %d removed",
			len(added), len(upgraded), len(downgraded), len(removed))
	}
	return ir.Report{
		Tool: "go", Subcommand: "get",
		Summary:  summary,
		Status:   ir.StatusOK,
		Items:    items,
		Filtered: true, Raw: rawOf(c),
	}, nil
}

// parseGoInstall strips download noise; reports ok or surfaces compacted errors.
func parseGoInstall(c engine.CaptureResult) (ir.Report, error) {
	if c.ExitCode == 0 {
		return ir.Report{
			Tool: "go", Subcommand: "install",
			Summary: "ok go install", Status: ir.StatusOK,
			Filtered: true, Raw: rawOf(c),
		}, nil
	}
	// Failure: prefer file:line diagnostics if present, else keep error lines.
	if rep, ok := tryGoErrors("install", c); ok {
		return rep, nil
	}
	return ir.Report{
		Tool: "go", Subcommand: "install",
		Summary:  "go install failed",
		Status:   ir.StatusFail,
		Notes:    capLines(keepLines(rawOf(c)), maxKeepLines),
		Filtered: true, Raw: rawOf(c),
	}, nil
}

// parseGoRun compacts compile/build errors (file:line) like build; if the build
// succeeded the program's own stdout/stderr is passthrough.
func parseGoRun(c engine.CaptureResult) (ir.Report, error) {
	// Build/compile failures surface as "# pkg" headers + file:line errors on
	// stderr, and exit before the program runs. Detect those and compact them.
	if rep, ok := tryGoErrors("run", c); ok {
		return rep, nil
	}
	// No compile diagnostics: this is genuine program output (even on non-zero
	// exit, which is the program's own exit code). Pass it through verbatim.
	return ir.RawReport("go", rawOf(c), c.ExitCode), nil
}

// tryGoErrors attempts to extract file:line Go compiler diagnostics. Returns
// ok=false when none are present (so the caller can choose passthrough).
func tryGoErrors(sub string, c engine.CaptureResult) (ir.Report, bool) {
	src := c.Stderr
	if strings.TrimSpace(src) == "" {
		return ir.Report{}, false
	}
	var diags []ir.Diagnostic
	overflow := 0
	engine.ScanLines(engine.StripANSI(src), func(line string) {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "#") || isGoNoise(t) {
			return
		}
		m := goErrRE.FindStringSubmatch(t)
		if m == nil {
			return
		}
		if len(diags) >= maxGoErrors {
			overflow++
			return
		}
		ln := atoi(m[2])
		col := atoi(m[3])
		diags = append(diags, ir.Diagnostic{
			File: m[1], Line: ln, Col: col, Severity: ir.SevError, Message: m[4],
		})
	})
	if len(diags) == 0 {
		return ir.Report{}, false
	}
	var notes []string
	if overflow > 0 {
		notes = []string{fmt.Sprintf("… +%d more errors", overflow)}
	}
	return ir.Report{
		Tool: "go", Subcommand: sub,
		Summary:     fmt.Sprintf("%d error(s)", len(diags)+overflow),
		Status:      ir.StatusFail,
		Diagnostics: diags,
		Notes:       notes,
		Filtered:    true, Raw: rawOf(c),
	}, true
}

// parseGoFmt lists reformatted file paths (gofmt prints one path per changed
// file on stdout).
func parseGoFmt(c engine.CaptureResult) (ir.Report, error) {
	if c.ExitCode != 0 {
		return ir.Report{
			Tool: "go", Subcommand: "fmt",
			Summary:  "go fmt failed",
			Status:   ir.StatusFail,
			Notes:    capLines(keepLines(rawOf(c)), maxKeepLines),
			Filtered: true, Raw: rawOf(c),
		}, nil
	}
	var files []string
	engine.ScanLines(engine.StripANSI(c.Stdout), func(line string) {
		t := strings.TrimSpace(line)
		if t == "" || isGoNoise(t) {
			return
		}
		files = append(files, t)
	})

	if len(files) == 0 {
		return ir.Report{
			Tool: "go", Subcommand: "fmt",
			Summary: "ok, already formatted", Status: ir.StatusOK,
			Filtered: true, Raw: rawOf(c),
		}, nil
	}
	items := make([]ir.Item, 0, len(files))
	for _, f := range files {
		items = append(items, ir.Item{Key: f})
	}
	return ir.Report{
		Tool: "go", Subcommand: "fmt",
		Summary:  fmt.Sprintf("%d file(s) reformatted", len(files)),
		Status:   ir.StatusWarn,
		Items:    items,
		Filtered: true, Raw: rawOf(c),
	}, nil
}

// parseGoGenerate strips noise, keeps errors; emits a short summary.
func parseGoGenerate(c engine.CaptureResult) (ir.Report, error) {
	if c.ExitCode == 0 {
		return ir.Report{
			Tool: "go", Subcommand: "generate",
			Summary: "ok go generate", Status: ir.StatusOK,
			Filtered: true, Raw: rawOf(c),
		}, nil
	}
	return ir.Report{
		Tool: "go", Subcommand: "generate",
		Summary:  "go generate failed",
		Status:   ir.StatusFail,
		Notes:    capLines(keepLines(rawOf(c)), maxKeepLines),
		Filtered: true, Raw: rawOf(c),
	}, nil
}

const maxListItems = 50

// parseGoList caps potentially huge package lists with a "+N more" note.
func parseGoList(c engine.CaptureResult) (ir.Report, error) {
	if c.ExitCode != 0 {
		return ir.Report{
			Tool: "go", Subcommand: "list",
			Summary:  "go list failed",
			Status:   ir.StatusFail,
			Notes:    capLines(keepLines(rawOf(c)), maxKeepLines),
			Filtered: true, Raw: rawOf(c),
		}, nil
	}
	var lines []string
	engine.ScanLines(engine.StripANSI(c.Stdout), func(line string) {
		t := strings.TrimRight(line, " \t")
		if strings.TrimSpace(t) == "" || isGoNoise(strings.TrimSpace(t)) {
			return
		}
		lines = append(lines, t)
	})

	total := len(lines)
	if total == 0 {
		return ir.Report{
			Tool: "go", Subcommand: "list",
			Summary: "ok go list (empty)", Status: ir.StatusOK,
			Filtered: true, Raw: rawOf(c),
		}, nil
	}
	var notes []string
	if total > maxListItems {
		notes = []string{fmt.Sprintf("… +%d more", total-maxListItems)}
		lines = lines[:maxListItems]
	}
	items := make([]ir.Item, 0, len(lines))
	for _, l := range lines {
		items = append(items, ir.Item{Key: l})
	}
	return ir.Report{
		Tool: "go", Subcommand: "list",
		Summary:  fmt.Sprintf("%d package(s)", total),
		Status:   ir.StatusOK,
		Items:    items,
		Notes:    notes,
		Filtered: true, Raw: rawOf(c),
	}, nil
}

// goEnvLineRE matches a "KEY=value" or "KEY='value'" env line.
var goEnvLineRE = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)=(.*)$`)

// parseGoEnv turns key=value lines into Items. If specific keys are requested as
// args (e.g. `go env GOPATH GOROOT`), `go env` prints bare values one per line;
// we keep those as-is paired with the requested keys.
func parseGoEnv(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	if c.ExitCode != 0 {
		return ir.Report{
			Tool: "go", Subcommand: "env",
			Summary:  "go env failed",
			Status:   ir.StatusFail,
			Notes:    capLines(keepLines(rawOf(c)), maxKeepLines),
			Filtered: true, Raw: rawOf(c),
		}, nil
	}

	requested := requestedEnvKeys(o.Args)
	var lines []string
	engine.ScanLines(engine.StripANSI(c.Stdout), func(line string) {
		lines = append(lines, line)
	})
	// Drop a trailing empty line from the final newline.
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	var items []ir.Item
	if len(requested) > 0 && !anyHasEquals(lines) {
		// Bare-value mode: each line is the value for the corresponding key.
		for i, v := range lines {
			key := fmt.Sprintf("arg%d", i+1)
			if i < len(requested) {
				key = requested[i]
			}
			items = append(items, ir.Item{Key: key, Val: strings.TrimSpace(v)})
		}
	} else {
		for _, l := range lines {
			m := goEnvLineRE.FindStringSubmatch(strings.TrimSpace(l))
			if m == nil {
				continue
			}
			items = append(items, ir.Item{Key: m[1], Val: unquote(m[2])})
		}
	}

	if len(items) == 0 {
		return ir.RawReport("go", rawOf(c), c.ExitCode), nil
	}
	return ir.Report{
		Tool: "go", Subcommand: "env",
		Summary:  fmt.Sprintf("%d var(s)", len(items)),
		Status:   ir.StatusOK,
		Items:    items,
		Filtered: true, Raw: rawOf(c),
	}, nil
}

// parseGoShort handles clean/fix/version/work with short summaries.
func parseGoShort(sub string, c engine.CaptureResult) (ir.Report, error) {
	if sub == "version" {
		// `go version` is a single informative line — surface it verbatim.
		v := strings.TrimSpace(engine.StripANSI(c.Stdout))
		if v == "" {
			v = strings.TrimSpace(engine.StripANSI(c.Stderr))
		}
		return ir.Report{
			Tool: "go", Subcommand: "version",
			Summary: v, Status: ir.StatusOK,
			Filtered: true, Raw: rawOf(c),
		}, nil
	}

	label := "go " + sub
	if c.ExitCode != 0 {
		return ir.Report{
			Tool: "go", Subcommand: sub,
			Summary:  label + " failed",
			Status:   ir.StatusFail,
			Notes:    capLines(keepLines(rawOf(c)), maxKeepLines),
			Filtered: true, Raw: rawOf(c),
		}, nil
	}
	return ir.Report{
		Tool: "go", Subcommand: sub,
		Summary: "ok " + label, Status: ir.StatusOK,
		Filtered: true, Raw: rawOf(c),
	}, nil
}

// requestedEnvKeys returns the non-flag args after "env" (the keys queried).
func requestedEnvKeys(args []string) []string {
	var keys []string
	seenEnv := false
	for _, a := range args {
		if a == "" || a[0] == '-' {
			continue
		}
		if !seenEnv {
			if a == "env" {
				seenEnv = true
			}
			continue
		}
		keys = append(keys, a)
	}
	return keys
}

func anyHasEquals(lines []string) bool {
	for _, l := range lines {
		if goEnvLineRE.MatchString(strings.TrimSpace(l)) {
			return true
		}
	}
	return false
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '\'' || s[0] == '"') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// atoi is a small no-error wrapper used by the diagnostic extractor.
func atoi(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return n
		}
		n = n*10 + int(s[i]-'0')
	}
	return n
}
