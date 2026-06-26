// Package vcshost holds native filters for code-host CLIs: gh (GitHub), glab
// (GitLab), and gt (Graphite). Each is a whole-tool dispatcher that switches on
// the subcommand (and sub-subcommand) and runs a dedicated structured parser.
// For gh/glab list/view commands we inject `--json <curated-fields>` in Exec
// (when the user didn't already request a format) and parse the JSON in Parse;
// if the captured output is not valid JSON we fall back to compacting the
// human table. Unknown subcommands and non-zero exits pass through unfiltered
// so auth/permission errors stay visible.
package vcshost

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

func init() {
	registry.Register(gh{})
	registry.Register(glab{})
	registry.Register(gt{})
}

const (
	maxVCSItems    = 30
	maxVCSLines    = 60
	vcsTruncateCol = 200
	vcsJSONMax     = 4000
	vcsValMax      = 140
)

// rawOf joins the captured streams for passthrough rendering.
func rawOf(c engine.CaptureResult) string {
	switch {
	case c.Stderr == "":
		return c.Stdout
	case c.Stdout == "":
		return c.Stderr
	default:
		return c.Stdout + c.Stderr
	}
}

func splitLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimSuffix(lines[i], "\r")
	}
	return lines
}

func truncateRunes(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	return string(r[:max-1]) + "…"
}

// firstNonFlag returns the first arg that is not a flag (and not a value of the
// few flags that take an argument, like -R repo). It is used to find the
// subcommand.
func firstNonFlag(args []string) string {
	skip := false
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if a == "" {
			continue
		}
		if a[0] == '-' {
			// -R/-g/-F/-H take a separate value arg (unless given as --x=y).
			if !strings.Contains(a, "=") && takesValue(a) {
				skip = true
			}
			continue
		}
		return a
	}
	return ""
}

func takesValue(flag string) bool {
	switch flag {
	case "-R", "--repo", "-g", "--group", "-F", "--field", "-H", "--header",
		"-X", "--method", "-f", "--raw-field":
		return true
	}
	return false
}

// nthNonFlag returns the nth (0-based) non-flag arg, for sub-subcommands.
func nthNonFlag(args []string, n int) string {
	skip := false
	idx := 0
	for _, a := range args {
		if skip {
			skip = false
			continue
		}
		if a == "" {
			continue
		}
		if a[0] == '-' {
			if !strings.Contains(a, "=") && takesValue(a) {
				skip = true
			}
			continue
		}
		if idx == n {
			return a
		}
		idx++
	}
	return ""
}

// hasFlag reports whether any of the named flags (or their --x= forms) appear.
func hasFlag(args []string, names ...string) bool {
	for _, a := range args {
		for _, n := range names {
			if a == n || strings.HasPrefix(a, n+"=") {
				return true
			}
		}
	}
	return false
}

// injectJSON appends `--json <fields>` to a copy of args when the user has not
// already asked for a structured/templated format.
func injectJSON(args []string, fields string) []string {
	if hasFlag(args, "--json", "--format", "--template", "-t", "--jq", "-q") {
		return args
	}
	out := append([]string{}, args...)
	return append(out, "--json", fields)
}

// decodeJSONArray parses stdout into a slice of generic maps. ok is false when
// the output is not a JSON array (caller should fall back to table parsing).
func decodeJSONArray(stdout string) (rows []map[string]any, ok bool) {
	s := strings.TrimSpace(stdout)
	if s == "" || s[0] != '[' {
		return nil, false
	}
	if json.Unmarshal([]byte(s), &rows) != nil {
		return nil, false
	}
	return rows, true
}

// decodeJSONObject parses stdout into a single generic map.
func decodeJSONObject(stdout string) (obj map[string]any, ok bool) {
	s := strings.TrimSpace(stdout)
	if s == "" || s[0] != '{' {
		return nil, false
	}
	if json.Unmarshal([]byte(s), &obj) != nil {
		return nil, false
	}
	return obj, true
}

// str pulls a string-ish field from a decoded JSON map.
func str(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		// Integers come back as float64; format without trailing ".0".
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// num pulls an integer field (JSON numbers decode as float64).
func num(m map[string]any, key string) int {
	if v, ok := m[key].(float64); ok {
		return int(v)
	}
	return 0
}

// nestedStr follows nested objects, e.g. author.login or defaultBranchRef.name.
func nestedStr(m map[string]any, path ...string) string {
	cur := m
	for i, p := range path {
		if i == len(path)-1 {
			return str(cur, p)
		}
		next, ok := cur[p].(map[string]any)
		if !ok {
			return ""
		}
		cur = next
	}
	return ""
}

// labelNames extracts label/name strings from a JSON array of {name:...}.
func labelNames(m map[string]any, key string) []string {
	arr, ok := m[key].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, e := range arr {
		if em, ok := e.(map[string]any); ok {
			if n := str(em, "name"); n != "" {
				out = append(out, n)
			}
		}
	}
	return out
}

// capItems caps an item slice and returns an overflow note ("" if none).
func capItems(items []ir.Item, n int) ([]ir.Item, string) {
	if len(items) <= n {
		return items, ""
	}
	overflow := len(items) - n
	return items[:n], fmt.Sprintf("… +%d more", overflow)
}

// compactTable strips ANSI/blanks, truncates wide lines, and caps length — the
// fallback when JSON parsing is unavailable for a known subcommand.
func compactTable(tool, sub string, cr engine.CaptureResult) ir.Report {
	src := engine.StripANSI(cr.Stdout)
	var kept []string
	for _, l := range splitLines(src) {
		if strings.TrimSpace(l) == "" {
			continue
		}
		kept = append(kept, truncateRunes(l, vcsTruncateCol))
	}
	var notes []string
	if len(kept) > maxVCSLines {
		notes = append(notes, fmt.Sprintf("… +%d more lines", len(kept)-maxVCSLines))
		kept = kept[:maxVCSLines]
	}
	return ir.Report{
		Tool: tool, Subcommand: sub, Status: ir.StatusOK,
		Text: strings.Join(kept, "\n"), Notes: notes, Filtered: true, Raw: rawOf(cr),
	}
}

// compactJSONText minifies a JSON document (or compacts text) for `api`-style
// commands where the output is JSON the user explicitly requested.
func compactJSONText(tool, sub string, cr engine.CaptureResult) ir.Report {
	body := strings.TrimSpace(cr.Stdout)
	var v any
	if body != "" && json.Unmarshal([]byte(body), &v) == nil {
		b, _ := json.Marshal(v) // minified
		return ir.Report{
			Tool: tool, Subcommand: sub, Status: ir.StatusOK,
			Text: truncateRunes(string(b), vcsJSONMax), Filtered: true, Raw: rawOf(cr),
		}
	}
	// Not JSON — compact as text.
	return compactTable(tool, sub, cr)
}

// prStateIcon maps a PR/MR/issue state string to a status.
func vcsStatusFor(state string) ir.Status {
	switch strings.ToUpper(state) {
	case "OPEN", "OPENED":
		return ir.StatusOK
	case "MERGED", "CLOSED":
		return ir.StatusWarn
	default:
		return ir.StatusOK
	}
}

// joinNonEmpty joins parts with sep, skipping empties.
func joinNonEmpty(sep string, parts ...string) string {
	var kept []string
	for _, p := range parts {
		if p != "" {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, sep)
}

// resolveExec is the shared Exec helper: build the command from the tool name
// and args (optionally with injected --json) and capture it.
func resolveExec(tool string, args []string) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand(tool, args...))
}

// keyValReport builds an OK report from key/value items with a summary.
func keyValReport(tool, sub, summary string, status ir.Status, items []ir.Item, raw string) ir.Report {
	return ir.Report{
		Tool: tool, Subcommand: sub, Summary: summary, Status: status,
		Items: items, Filtered: true, Raw: raw,
	}
}

// ensure the unused-import guards stay satisfied across files.
var _ = registry.Opts{}
