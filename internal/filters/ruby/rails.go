package ruby

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

const (
	maxRailsItems = 60
	maxMigrations = 40
	maxTasks      = 40
)

// migrateStartRE matches "== 20230101120000 CreateUsers: migrating ======".
var migrateStartRE = regexp.MustCompile(`^==\s+(\d+)\s+(.+?):\s+(migrating|reverting)\b`)

// migrateDoneRE matches "== 20230101120000 CreateUsers: migrated (0.0150s) ===".
var migrateDoneRE = regexp.MustCompile(`^==\s+(\d+)\s+(.+?):\s+(migrated|reverted)\s*\(([^)]+)\)`)

// routeRowRE matches a route table row: "prefix VERB /uri(.:format) ctrl#action".
// Prefix is optional; verb may be empty (continuation) or a "|"-joined list.
var routeRowRE = regexp.MustCompile(`^\s*(\S*)\s+([A-Z|]+)\s+(\S+)\s+(\S+#\S+|\S+)\s*$`)

// genOpRE matches a generator/destroy file-op line: "      create  app/x.rb".
var genOpRE = regexp.MustCompile(`^\s*(create|identical|force|skip|conflict|remove|exist|invoke|insert|gsub|append|prepend|route|rename|run|readme|generate)\s+(.+?)\s*$`)

// aboutRowRE matches a `rails about` "Key: value" row.
var aboutRowRE = regexp.MustCompile(`^([A-Z][A-Za-z0-9 .#/_'-]+?):\s+(.+?)\s*$`)

type rails struct{}

func (rails) Tool() string       { return "rails" }
func (rails) Subcommand() string { return "" }

func (rails) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(rubyExec("rails", o.Args))
}

func (rails) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	out := engine.StripANSI(c.Stdout + "\n" + c.Stderr)
	raw := rawOf(c)
	sub := firstNonFlag(o.Args)

	switch {
	case sub == "test":
		// railsTest normally wins via Lookup, but be defensive.
		return parseMinitest(out, c.ExitCode, raw), nil
	case sub == "generate" || sub == "g" || sub == "destroy" || sub == "d":
		return parseRailsGenerate(out, sub, c.ExitCode, raw), nil
	case sub == "new":
		return parseRailsNew(out, c.ExitCode, raw), nil
	case sub == "routes":
		return parseRoutes(out, "rails", c.ExitCode, raw), nil
	case sub == "about":
		return parseRailsAbout(out, c.ExitCode, raw), nil
	case sub == "zeitwerk:check":
		return parseZeitwerk(out, c.ExitCode, raw), nil
	case strings.HasPrefix(sub, "db:"):
		return parseMigration(out, sub, "rails", c.ExitCode, raw), nil
	case sub == "assets:precompile":
		return parseAssetsPrecompile(out, "rails", c.ExitCode, raw), nil
	case sub == "version" || (sub == "" && hasAny(o.Args, "-v", "--version")):
		return parseRailsVersion(out, c.ExitCode, raw), nil
	// Interactive / streaming / sensitive / arbitrary → passthrough.
	case sub == "console" || sub == "c" || sub == "server" || sub == "s" ||
		sub == "dbconsole" || sub == "runner" ||
		strings.HasPrefix(sub, "credentials:") || strings.HasPrefix(sub, "secrets:"):
		return ir.RawReport("rails", raw, c.ExitCode), nil
	default:
		return ir.RawReport("rails", raw, c.ExitCode), nil
	}
}

// parseMigration compacts ActiveRecord migration logs (shared by rails db:* and
// rake db:*) into one line per migration. db:seed/create/drop/setup/etc. that
// emit no migration framing get a concise summary; errors are kept.
func parseMigration(out, sub, tool string, exitCode int, raw string) ir.Report {
	var items []ir.Item
	var errs []string
	migrated := map[string]bool{}

	for _, l := range splitLines(out) {
		t := strings.TrimSpace(l)
		if m := migrateDoneRE.FindStringSubmatch(t); m != nil {
			name := migrationShortName(m[2])
			mark := "OK"
			if m[3] == "reverted" {
				mark = "REVERT"
			}
			if !migrated[name] {
				migrated[name] = true
				if len(items) < maxMigrations {
					items = append(items, ir.Item{Key: name, Val: fmt.Sprintf("%s (%s)", mark, m[4])})
				}
			}
			continue
		}
		if migrateStartRE.MatchString(t) {
			continue
		}
		if strings.HasPrefix(t, "--") || strings.HasPrefix(t, "->") {
			continue // per-statement detail noise
		}
		if isRubyErrorLine(t) {
			if len(errs) < 8 {
				errs = append(errs, t)
			}
		}
	}

	status := ir.StatusOK
	if exitCode != 0 || len(errs) > 0 {
		status = ir.StatusFail
	}

	if total := len(migrated); total > len(items) {
		items = append(items, ir.Item{Key: fmt.Sprintf("+%d more", total-len(items))})
	}

	var summary string
	if len(migrated) > 0 {
		noun := "migration"
		if len(migrated) != 1 {
			noun = "migrations"
		}
		summary = fmt.Sprintf("%s %s: %d %s", status, sub, len(migrated), noun)
	} else {
		summary = fmt.Sprintf("%s %s", status, sub)
	}

	rep := ir.Report{
		Tool: tool, Subcommand: sub, Summary: summary, Status: status,
		Items: items, Filtered: true, Raw: raw, ExitCode: exitCode,
	}
	if len(errs) > 0 {
		rep.Notes = errs
	}
	return rep
}

func migrationShortName(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = strings.TrimSpace(s[i+1:])
	}
	return s
}

// parseRoutes compacts the route table into "VERB /uri -> ctrl#action" items.
func parseRoutes(out, tool string, exitCode int, raw string) ir.Report {
	var items []ir.Item
	total := 0
	for _, l := range splitLines(out) {
		t := strings.TrimRight(l, " ")
		if t == "" {
			continue
		}
		// Skip header.
		if strings.Contains(t, "URI Pattern") && strings.Contains(t, "Controller#Action") {
			continue
		}
		m := routeRowRE.FindStringSubmatch(t)
		if m == nil {
			continue
		}
		verb, uri, action := m[2], m[3], m[4]
		if verb == "Verb" {
			continue
		}
		total++
		if len(items) >= maxRailsItems {
			continue
		}
		items = append(items, ir.Item{Key: verb + " " + uri, Val: action})
	}

	if total == 0 {
		return ir.RawReport(tool, raw, exitCode)
	}
	if total > len(items) {
		items = append(items, ir.Item{Key: fmt.Sprintf("+%d more", total-len(items))})
	}
	noun := "route"
	if total != 1 {
		noun = "routes"
	}
	return ir.Report{
		Tool: tool, Subcommand: "routes",
		Summary: fmt.Sprintf("%d %s", total, noun), Status: ir.StatusOK,
		Items: items, Filtered: true, Raw: raw, ExitCode: exitCode,
	}
}

// parseRailsGenerate compacts generator/destroy file-operation lists.
func parseRailsGenerate(out, sub string, exitCode int, raw string) ir.Report {
	items, created, removed, conflicts := collectGenOps(out)

	status := ir.StatusOK
	if exitCode != 0 || conflicts > 0 {
		status = ir.StatusWarn
	}
	if exitCode != 0 {
		status = ir.StatusFail
	}

	verb := "generate"
	if sub == "destroy" || sub == "d" {
		verb = "destroy"
	}

	var summary string
	if verb == "destroy" {
		summary = fmt.Sprintf("%d removed", removed)
	} else {
		summary = fmt.Sprintf("%d files created", created)
	}
	if conflicts > 0 {
		summary += fmt.Sprintf(", %d conflicts", conflicts)
	}

	if len(items) == 0 {
		return ir.RawReport("rails", raw, exitCode)
	}
	return ir.Report{
		Tool: "rails", Subcommand: verb, Summary: summary, Status: status,
		Items: items, Filtered: true, Raw: raw, ExitCode: exitCode,
	}
}

// parseRailsNew compacts `rails new` (same file-op format) and strips the bundle
// install / git noise.
func parseRailsNew(out string, exitCode int, raw string) ir.Report {
	items, created, _, conflicts := collectGenOps(out)
	if len(items) == 0 {
		return ir.RawReport("rails", raw, exitCode)
	}
	status := ir.StatusOK
	if exitCode != 0 {
		status = ir.StatusFail
	}
	summary := fmt.Sprintf("ok new app: %d files", created)
	if conflicts > 0 {
		summary += fmt.Sprintf(", %d conflicts", conflicts)
	}
	return ir.Report{
		Tool: "rails", Subcommand: "new", Summary: summary, Status: status,
		Items: items, Filtered: true, Raw: raw, ExitCode: exitCode,
	}
}

// collectGenOps scans generator-style file-op lines, returning capped items and
// counts of created/removed/conflict actions.
func collectGenOps(out string) (items []ir.Item, created, removed, conflicts int) {
	for _, l := range splitLines(out) {
		m := genOpRE.FindStringSubmatch(l)
		if m == nil {
			continue
		}
		action, path := m[1], strings.TrimSpace(m[2])
		switch action {
		case "create":
			created++
		case "remove":
			removed++
		case "conflict":
			conflicts++
		case "run", "readme", "generate":
			// bundle/git/runner noise — skip.
			continue
		}
		if action == "conflict" || len(items) < maxRailsItems {
			items = append(items, ir.Item{Key: action, Val: path})
		}
	}
	if len(items) > maxRailsItems {
		extra := len(items) - maxRailsItems
		items = items[:maxRailsItems]
		items = append(items, ir.Item{Key: fmt.Sprintf("+%d more", extra)})
	}
	return items, created, removed, conflicts
}

// parseRailsAbout keeps the `rails about` key:value environment table.
func parseRailsAbout(out string, exitCode int, raw string) ir.Report {
	var items []ir.Item
	for _, l := range splitLines(out) {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "About ") {
			continue
		}
		if m := aboutRowRE.FindStringSubmatch(t); m != nil {
			items = append(items, ir.Item{Key: strings.TrimSpace(m[1]), Val: strings.TrimSpace(m[2])})
		}
	}
	if len(items) == 0 {
		return ir.RawReport("rails", raw, exitCode)
	}
	return ir.Report{
		Tool: "rails", Subcommand: "about", Summary: "environment", Status: ir.StatusOK,
		Items: items, Filtered: true, Raw: raw, ExitCode: exitCode,
	}
}

// parseZeitwerk summarizes `rails zeitwerk:check`.
func parseZeitwerk(out string, exitCode int, raw string) ir.Report {
	if exitCode == 0 && strings.Contains(out, "All is good!") {
		return ir.Report{
			Tool: "rails", Subcommand: "zeitwerk:check",
			Summary: "zeitwerk: all good", Status: ir.StatusOK,
			Filtered: true, Raw: raw, ExitCode: exitCode,
		}
	}
	var notes []string
	for _, l := range splitLines(out) {
		t := strings.TrimSpace(l)
		if t == "" || strings.HasPrefix(t, "Hold on") || strings.Contains(t, "is going to be checked") {
			continue
		}
		if len(notes) < 12 {
			notes = append(notes, t)
		}
	}
	return ir.Report{
		Tool: "rails", Subcommand: "zeitwerk:check",
		Summary: "zeitwerk: check failed", Status: ir.StatusFail,
		Notes: notes, Filtered: true, Raw: raw, ExitCode: exitCode,
	}
}

// parseAssetsPrecompile strips Sprockets per-asset noise, keeping errors.
func parseAssetsPrecompile(out, tool string, exitCode int, raw string) ir.Report {
	var errs []string
	for _, l := range splitLines(out) {
		t := strings.TrimSpace(l)
		if isRubyErrorLine(t) {
			if len(errs) < 12 {
				errs = append(errs, t)
			}
		}
	}
	status := ir.StatusOK
	summary := "ok assets:precompile"
	if exitCode != 0 || len(errs) > 0 {
		status = ir.StatusFail
		summary = "fail assets:precompile"
	}
	rep := ir.Report{
		Tool: tool, Subcommand: "assets:precompile", Summary: summary, Status: status,
		Filtered: true, Raw: raw, ExitCode: exitCode,
	}
	if len(errs) > 0 {
		rep.Notes = errs
	}
	return rep
}

func parseRailsVersion(out string, exitCode int, raw string) ir.Report {
	v := firstNonEmptyLine(out)
	if v == "" {
		return ir.RawReport("rails", raw, exitCode)
	}
	return ir.Report{
		Tool: "rails", Subcommand: "version", Summary: v, Status: ir.StatusOK,
		Filtered: true, Raw: raw, ExitCode: exitCode,
	}
}

func firstNonEmptyLine(s string) string {
	for _, l := range splitLines(s) {
		if t := strings.TrimSpace(l); t != "" {
			return t
		}
	}
	return ""
}

// isRubyErrorLine flags lines that look like errors/exceptions worth keeping.
func isRubyErrorLine(t string) bool {
	if t == "" {
		return false
	}
	low := strings.ToLower(t)
	return strings.Contains(low, "error") || strings.Contains(low, "exception") ||
		strings.Contains(low, "failed") || strings.Contains(low, "aborted!") ||
		rubyExcRE.MatchString(t)
}
