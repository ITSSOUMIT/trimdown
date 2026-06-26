package js

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

// npm is a whole-tool dispatcher: it switches on the first non-flag arg and
// runs a dedicated, structured parser per subcommand. There is no generic
// strip fallback — unknown subcommands pass through untouched.
type npm struct{}

func (npm) Tool() string       { return "npm" }
func (npm) Subcommand() string { return "" }

func (npm) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand("npm", o.Args...))
}

// Package-scope regexes for npm output.
var (
	// "added 12 packages, removed 3 packages, changed 1 package, and audited 240 packages in 3s"
	npmAddedRE   = regexp.MustCompile(`added (\d+) packages?`)
	npmRemovedRE = regexp.MustCompile(`removed (\d+) packages?`)
	npmChangedRE = regexp.MustCompile(`changed (\d+) packages?`)
	npmAuditedRE = regexp.MustCompile(`audited (\d+) packages?`)
	npmTimeRE    = regexp.MustCompile(`in ([\d.]+m?s)`)

	// "5 vulnerabilities (2 low, 1 moderate, 1 high, 1 critical)" or "found 0 vulnerabilities"
	npmVulnTotalRE = regexp.MustCompile(`(\d+) vulnerabilit(?:y|ies)`)
	npmLowRE       = regexp.MustCompile(`(\d+) low`)
	npmModerateRE  = regexp.MustCompile(`(\d+) moderate`)
	npmHighRE      = regexp.MustCompile(`(\d+) high`)
	npmCriticalRE  = regexp.MustCompile(`(\d+) critical`)

	// "npm warn deprecated foo@1.0.0: use bar instead"
	npmDeprecatedRE = regexp.MustCompile(`(?i)^npm (?:warn|WARN) deprecated `)
	// "npm error code E404" / "npm error <message>" (newer) and "npm ERR! ..." (older)
	npmErrRE = regexp.MustCompile(`(?i)^npm (?:err!|error\b)`)
	// The "A complete log of this run can be found in: ..." path noise.
	npmLogPathRE = regexp.MustCompile(`(?i)complete log of this run`)

	// npm run preamble: "> pkg@1.0.0 build" then "> tsc -p ."
	npmRunPreambleRE = regexp.MustCompile(`^> \S`)

	// "outdated" table columns and "npm view" key lines handled by ad-hoc parsing.
	npmVersionRE = regexp.MustCompile(`^v?\d+\.\d+\.\d+`)
)

const (
	maxNpmDeprecations = 10
	maxNpmErrLines     = 12
	maxNpmItems        = 40
	maxNpmAdvisories   = 15
)

func (n npm) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	sub := firstNonFlagArg(o.Args)
	raw := rawOf(c)
	out := engine.StripANSI(c.Stdout + "\n" + c.Stderr)

	// --version / -v with no subcommand.
	if sub == "" {
		if hasFlag(o.Args, "--version", "-v") {
			return npmVersionReport(out, raw, c.ExitCode), nil
		}
		return ir.RawReport("npm", raw, c.ExitCode), nil
	}

	switch sub {
	case "install", "i", "ci", "add", "update", "up":
		return npmInstallReport(sub, out, raw, c.ExitCode), nil
	case "audit":
		return npmAuditReport(out, raw, c.ExitCode), nil
	case "outdated":
		return npmOutdatedReport(out, raw, c.ExitCode), nil
	case "ls", "list":
		return npmLsReport(out, raw, c.ExitCode), nil
	case "view", "info", "show", "v":
		return npmViewReport(sub, out, raw, c.ExitCode), nil
	case "run", "run-script", "test", "t", "start", "stop", "restart":
		return npmRunReport(c, raw), nil
	case "publish":
		return npmPublishReport(out, raw, c.ExitCode), nil
	case "version":
		return npmSimpleVersionReport(out, raw, c.ExitCode), nil
	case "exec", "x":
		return npmExecReport(c, raw), nil
	case "init", "create", "dedupe", "ddp", "prune", "pack", "link", "ln", "uninstall", "remove", "rm", "un":
		return npmConciseReport(sub, out, raw, c.ExitCode), nil
	default:
		return ir.RawReport("npm", raw, c.ExitCode), nil
	}
}

// ----- helpers shared by npm parsers -----

func firstNonFlagArg(args []string) string {
	for _, a := range args {
		if a == "" || a[0] == '-' {
			continue
		}
		return a
	}
	return ""
}

func firstSubmatchInt(re *regexp.Regexp, s string) int {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

// collectNpmNoise gathers deprecation warnings (capped) and ERR! block lines
// (code + message, dropping the log-path noise) from npm output.
func collectNpmNoise(lines []string) (deprecations, errLines []string, hasErr bool) {
	for _, l := range lines {
		t := strings.TrimSpace(l)
		switch {
		case npmDeprecatedRE.MatchString(t):
			if len(deprecations) < maxNpmDeprecations {
				deprecations = append(deprecations, truncateRunes(t, 160))
			}
		case npmErrRE.MatchString(t):
			hasErr = true
			if npmLogPathRE.MatchString(t) {
				continue // drop "A complete log of this run can be found in: ..."
			}
			if len(errLines) < maxNpmErrLines {
				errLines = append(errLines, truncateRunes(t, 200))
			}
		}
	}
	return
}

func vulnCounts(s string) (total, low, mod, high, crit int) {
	total = firstSubmatchInt(npmVulnTotalRE, s)
	low = firstSubmatchInt(npmLowRE, s)
	mod = firstSubmatchInt(npmModerateRE, s)
	high = firstSubmatchInt(npmHighRE, s)
	crit = firstSubmatchInt(npmCriticalRE, s)
	return
}

func vulnSummary(total, low, mod, high, crit int) string {
	if total == 0 {
		return "0 vulnerabilities"
	}
	var parts []string
	if low > 0 {
		parts = append(parts, strconv.Itoa(low)+" low")
	}
	if mod > 0 {
		parts = append(parts, strconv.Itoa(mod)+" moderate")
	}
	if high > 0 {
		parts = append(parts, strconv.Itoa(high)+" high")
	}
	if crit > 0 {
		parts = append(parts, strconv.Itoa(crit)+" critical")
	}
	s := strconv.Itoa(total) + " vulnerabilities"
	if len(parts) > 0 {
		s += " (" + strings.Join(parts, ", ") + ")"
	}
	return s
}

// ----- per-subcommand parsers -----

func npmVersionReport(out, raw string, exit int) ir.Report {
	for _, l := range splitLines(out) {
		t := strings.TrimSpace(l)
		if t != "" {
			return ir.Report{Tool: "npm", Summary: t, Status: ir.StatusOK, Filtered: true, Raw: raw}
		}
	}
	return ir.RawReport("npm", raw, exit)
}

func npmInstallReport(sub, out, raw string, exit int) ir.Report {
	lines := splitLines(out)
	deprecations, errLines, hasErr := collectNpmNoise(lines)

	added := firstSubmatchInt(npmAddedRE, out)
	removed := firstSubmatchInt(npmRemovedRE, out)
	changed := firstSubmatchInt(npmChangedRE, out)
	total, low, mod, high, crit := vulnCounts(out)

	status := ir.StatusOK
	if len(deprecations) > 0 || total > 0 {
		status = ir.StatusWarn
	}
	if hasErr || exit != 0 {
		status = ir.StatusFail
	}

	var b strings.Builder
	b.WriteString("ok ")
	b.WriteString(sub)
	b.WriteString(": +")
	b.WriteString(strconv.Itoa(added))
	b.WriteString(" -")
	b.WriteString(strconv.Itoa(removed))
	if changed > 0 {
		b.WriteString(" ~")
		b.WriteString(strconv.Itoa(changed))
	}
	if total > 0 {
		b.WriteString(", ")
		b.WriteString(strconv.Itoa(total))
		b.WriteString(" vulns")
	}
	if hasErr {
		b.Reset()
		b.WriteString("install failed")
	}
	summary := b.String()

	var textLines []string
	textLines = append(textLines, deprecations...)
	if total > 0 {
		textLines = append(textLines, vulnSummary(total, low, mod, high, crit))
	}
	textLines = append(textLines, errLines...)

	return ir.Report{
		Tool: "npm", Subcommand: sub, Summary: summary, Status: status,
		Text: strings.Join(textLines, "\n"), Filtered: true, Raw: raw,
	}
}

func npmAuditReport(out, raw string, exit int) ir.Report {
	lines := splitLines(out)
	total, low, mod, high, crit := vulnCounts(out)

	// Cap individual advisories (lines naming a package + severity).
	var advisories []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		// Advisory header lines like "# npm audit report" or "<pkg>  <sev>".
		if strings.Contains(t, "Severity:") || strings.HasPrefix(t, "fix available") {
			if len(advisories) < maxNpmAdvisories {
				advisories = append(advisories, truncateRunes(t, 160))
			}
		}
	}

	status := ir.StatusOK
	if total > 0 {
		status = ir.StatusWarn
	}
	if high > 0 || crit > 0 {
		status = ir.StatusFail
	}

	return ir.Report{
		Tool: "npm", Subcommand: "audit", Summary: vulnSummary(total, low, mod, high, crit),
		Status: status, Text: strings.Join(advisories, "\n"), Filtered: true, Raw: raw,
	}
}

func npmOutdatedReport(out, raw string, exit int) ir.Report {
	lines := splitLines(out)
	var items []ir.Item
	overflow := 0
	for _, l := range lines {
		fields := strings.Fields(l)
		if len(fields) < 4 {
			continue
		}
		// Skip the header row.
		if fields[0] == "Package" {
			continue
		}
		// Columns: Package Current Wanted Latest [Location] [Depended by]
		pkg, current, latest := fields[0], fields[1], fields[3]
		if !npmVersionRE.MatchString(current) && current != "MISSING" {
			continue
		}
		if len(items) >= maxNpmItems {
			overflow++
			continue
		}
		items = append(items, ir.Item{Key: pkg, Val: current + "→" + latest})
	}

	if len(items) == 0 && overflow == 0 {
		return ir.Report{Tool: "npm", Subcommand: "outdated", Summary: "all up to date", Status: ir.StatusOK, Filtered: true, Raw: raw}
	}

	var notes []string
	if overflow > 0 {
		notes = append(notes, "+"+strconv.Itoa(overflow)+" more")
	}
	// npm outdated exits 1 when packages are outdated — treat as success.
	return ir.Report{
		Tool: "npm", Subcommand: "outdated",
		Summary: fmt.Sprintf("%d outdated", len(items)+overflow),
		Status:  ir.StatusWarn, Items: items, Notes: notes, Filtered: true, Raw: raw,
	}
}

func npmLsReport(out, raw string, exit int) ir.Report {
	lines := splitLines(out)
	var items []ir.Item
	var warnings []string
	overflow := 0
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if strings.Contains(t, "UNMET") || strings.Contains(t, "invalid") ||
			strings.Contains(t, "missing") || strings.Contains(t, "extraneous") ||
			npmErrRE.MatchString(t) {
			warnings = append(warnings, truncateRunes(t, 160))
			continue
		}
		// Tree entries begin with ├ │ └ ─ or whitespace + name@ver.
		cleaned := strings.TrimLeft(t, "├│└─ ")
		if cleaned == "" || !strings.Contains(cleaned, "@") {
			continue
		}
		if len(items) >= maxNpmItems {
			overflow++
			continue
		}
		items = append(items, ir.Item{Key: cleaned})
	}

	status := ir.StatusOK
	if len(warnings) > 0 {
		status = ir.StatusWarn
	}
	if exit != 0 {
		status = ir.StatusWarn
	}

	var notes []string
	for _, w := range warnings {
		notes = append(notes, w)
	}
	if overflow > 0 {
		notes = append(notes, "+"+strconv.Itoa(overflow)+" more")
	}
	return ir.Report{
		Tool: "npm", Subcommand: "ls",
		Summary: fmt.Sprintf("%d deps", len(items)+overflow),
		Status:  status, Items: items, Notes: notes, Filtered: true, Raw: raw,
	}
}

func npmViewReport(sub, out, raw string, exit int) ir.Report {
	if exit != 0 {
		return ir.RawReport("npm", raw, exit)
	}
	lines := splitLines(out)
	var items []ir.Item
	wantKeys := map[string]bool{
		"name": true, "version": true, "description": true, "license": true,
		"homepage": true, "dist.tarball": true, "dependencies": true,
	}
	for _, l := range lines {
		t := strings.TrimSpace(l)
		// "key = value" form (npm view single field prints just the value).
		if i := strings.Index(t, " = "); i > 0 {
			key := strings.TrimSpace(t[:i])
			if wantKeys[key] {
				items = append(items, ir.Item{Key: key, Val: truncateRunes(strings.TrimSpace(t[i+3:]), 120)})
			}
			continue
		}
		// "key: value" form.
		if i := strings.Index(t, ": "); i > 0 {
			key := strings.TrimSpace(t[:i])
			if wantKeys[key] {
				items = append(items, ir.Item{Key: key, Val: truncateRunes(strings.TrimSpace(t[i+2:]), 120)})
			}
		}
	}
	if len(items) == 0 {
		// Single-field output (e.g. `npm view pkg version`) prints a bare value.
		for _, l := range lines {
			t := strings.TrimSpace(l)
			if t != "" {
				return ir.Report{Tool: "npm", Subcommand: sub, Summary: truncateRunes(t, 120), Status: ir.StatusOK, Filtered: true, Raw: raw}
			}
		}
		return ir.RawReport("npm", raw, exit)
	}
	return ir.Report{
		Tool: "npm", Subcommand: sub, Summary: fmt.Sprintf("%d fields", len(items)),
		Status: ir.StatusOK, Items: items, Filtered: true, Raw: raw,
	}
}

// npmRunReport strips the "> pkg@ver script" + "> command" preamble and passes
// the inner script output through. If npm ERR! is present, it is compacted.
func npmRunReport(c engine.CaptureResult, raw string) ir.Report {
	// The preamble appears on stdout; inner output may be on either stream.
	stdout := engine.StripANSI(c.Stdout)
	stderr := engine.StripANSI(c.Stderr)

	innerStdout := stripRunPreamble(stdout)

	_, errLines, hasErr := collectNpmNoise(splitLines(stderr))
	// npm ERR! may also land on stdout in some setups.
	if !hasErr {
		_, e2, h2 := collectNpmNoise(splitLines(innerStdout))
		if h2 {
			hasErr = true
			errLines = e2
			innerStdout = dropNpmErrLines(innerStdout)
		}
	}
	innerStderr := dropNpmErrLines(stderr)

	inner := strings.TrimRight(innerStdout, "\n")
	if s := strings.TrimRight(innerStderr, "\n"); s != "" {
		if inner != "" {
			inner += "\n"
		}
		inner += s
	}

	if hasErr || c.ExitCode != 0 {
		text := inner
		if len(errLines) > 0 {
			if text != "" {
				text += "\n"
			}
			text += strings.Join(errLines, "\n")
		}
		return ir.Report{
			Tool: "npm", Subcommand: "run", Summary: "script failed",
			Status: ir.StatusFail, Text: text, Filtered: true, Raw: raw,
		}
	}

	// Clean run: pass the inner output through unchanged (framing stripped).
	if inner == "" {
		return ir.Report{Tool: "npm", Subcommand: "run", Summary: "ok", Status: ir.StatusOK, Filtered: true, Raw: raw}
	}
	return ir.Report{
		Tool: "npm", Subcommand: "run", Status: ir.StatusOK,
		Text: inner, Filtered: true, Raw: raw,
	}
}

// stripRunPreamble removes leading "> ..." npm framing lines.
func stripRunPreamble(s string) string {
	lines := splitLines(s)
	var kept []string
	skipping := true
	for _, l := range lines {
		if skipping && (npmRunPreambleRE.MatchString(l) || strings.TrimSpace(l) == "") {
			continue
		}
		skipping = false
		kept = append(kept, l)
	}
	return strings.Join(kept, "\n")
}

func dropNpmErrLines(s string) string {
	var kept []string
	for _, l := range splitLines(s) {
		if npmErrRE.MatchString(strings.TrimSpace(l)) {
			continue
		}
		kept = append(kept, l)
	}
	return strings.Join(kept, "\n")
}

func npmExecReport(c engine.CaptureResult, raw string) ir.Report {
	// npm exec runs an arbitrary binary; pass its output through, compacting
	// only npm's own ERR! framing if the invocation itself failed.
	stdout := engine.StripANSI(c.Stdout)
	stderr := engine.StripANSI(c.Stderr)
	_, errLines, hasErr := collectNpmNoise(splitLines(stderr))
	if hasErr {
		inner := strings.TrimRight(dropNpmErrLines(stdout), "\n")
		text := inner
		if len(errLines) > 0 {
			if text != "" {
				text += "\n"
			}
			text += strings.Join(errLines, "\n")
		}
		return ir.Report{Tool: "npm", Subcommand: "exec", Summary: "exec failed", Status: ir.StatusFail, Text: text, Filtered: true, Raw: raw}
	}
	return ir.RawReport("npm", raw, c.ExitCode)
}

func npmPublishReport(out, raw string, exit int) ir.Report {
	lines := splitLines(out)
	_, errLines, hasErr := collectNpmNoise(lines)
	if hasErr || exit != 0 {
		return ir.Report{
			Tool: "npm", Subcommand: "publish", Summary: "publish failed",
			Status: ir.StatusFail, Text: strings.Join(errLines, "\n"), Filtered: true, Raw: raw,
		}
	}
	// "+ pkg@1.2.3" marks a successful publish.
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "+ ") && strings.Contains(t, "@") {
			return ir.Report{
				Tool: "npm", Subcommand: "publish", Summary: "ok published " + strings.TrimSpace(t[2:]),
				Status: ir.StatusOK, Filtered: true, Raw: raw,
			}
		}
	}
	return ir.Report{Tool: "npm", Subcommand: "publish", Summary: "ok published", Status: ir.StatusOK, Filtered: true, Raw: raw}
}

func npmSimpleVersionReport(out, raw string, exit int) ir.Report {
	// `npm version <bump>` prints the new version, e.g. "v1.2.4".
	for _, l := range splitLines(out) {
		t := strings.TrimSpace(l)
		if npmVersionRE.MatchString(t) {
			return ir.Report{Tool: "npm", Subcommand: "version", Summary: t, Status: ir.StatusOK, Filtered: true, Raw: raw}
		}
	}
	return ir.RawReport("npm", raw, exit)
}

func npmConciseReport(sub, out, raw string, exit int) ir.Report {
	lines := splitLines(out)
	deprecations, errLines, hasErr := collectNpmNoise(lines)

	status := ir.StatusOK
	if len(deprecations) > 0 {
		status = ir.StatusWarn
	}
	if hasErr || exit != 0 {
		status = ir.StatusFail
	}

	added := firstSubmatchInt(npmAddedRE, out)
	removed := firstSubmatchInt(npmRemovedRE, out)
	changed := firstSubmatchInt(npmChangedRE, out)

	var summary string
	if hasErr {
		summary = sub + " failed"
	} else if added+removed+changed > 0 {
		summary = fmt.Sprintf("ok %s: +%d -%d ~%d", sub, added, removed, changed)
	} else {
		summary = "ok " + sub
	}

	var textLines []string
	textLines = append(textLines, deprecations...)
	textLines = append(textLines, errLines...)
	return ir.Report{
		Tool: "npm", Subcommand: sub, Summary: summary, Status: status,
		Text: strings.Join(textLines, "\n"), Filtered: true, Raw: raw,
	}
}
