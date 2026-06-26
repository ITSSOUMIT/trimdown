package cloud

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

// kubeMutationRE matches lines from apply/create/delete/patch like
// "deployment.apps/web created" / "pod/x unchanged".
var kubeMutationRE = regexp.MustCompile(`^(\S+)\s+(created|configured|unchanged|deleted|patched|labeled|annotated|replaced)\b`)

// kube is the whole-tool dispatcher for kubectl and oc.
type kube struct{ tool string }

func (k kube) Tool() string     { return k.tool }
func (kube) Subcommand() string { return "" }

func (k kube) Exec(o registry.Opts) engine.CaptureResult {
	return engine.Capture(engine.ResolvedCommand(k.tool, o.Args...))
}

func (k kube) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	sub := firstNonFlagArg(o.Args)
	// Compact recognizable errors regardless of subcommand.
	if c.ExitCode != 0 {
		if r, ok := kubeCompactError(k.tool, c); ok {
			return r, nil
		}
	}
	switch sub {
	case "get":
		return kubeGet(k.tool, c, o.Args)
	case "describe":
		return kubeDescribe(k.tool, c)
	case "logs":
		return kubeLogs(k.tool, c)
	case "apply", "create", "delete", "patch", "label", "annotate":
		return kubeMutation(k.tool, c, sub)
	case "rollout":
		return kubeKeepLines(k.tool, c, "rollout")
	case "top":
		return kubeTop(k.tool, c)
	case "version":
		return kubeKeepLines(k.tool, c, "version")
	case "explain":
		return kubeCap(k.tool, c, "explain")
	case "config":
		return kubeCap(k.tool, c, "config")
	case "status": // oc status
		return kubeCap(k.tool, c, "status")
	case "project": // oc project
		return kubeKeepLines(k.tool, c, "project")
	case "adm": // oc adm — mostly passthrough unless tabular
		return kubeAdm(k.tool, c)
	default:
		if c.ExitCode != 0 {
			return ir.RawReport(k.tool, rawOf(c), c.ExitCode), nil
		}
		return ir.RawReport(k.tool, rawOf(c), c.ExitCode), nil
	}
}

func kubeGet(tool string, c engine.CaptureResult, args []string) (ir.Report, error) {
	if c.ExitCode != 0 {
		return ir.RawReport(tool, rawOf(c), c.ExitCode), nil
	}
	switch outputFormat(args) {
	case "json":
		return kubeGetJSON(tool, c)
	case "yaml":
		return kubeCap(tool, c, "get")
	default: // table (including -o wide)
		return kubeGetTable(tool, c)
	}
}

// kubeGetTable parses the default `get` table into NAME + the read/status/age
// columns.
func kubeGetTable(tool string, c engine.CaptureResult) (ir.Report, error) {
	lines := splitLines(engine.StripANSI(c.Stdout))
	if len(lines) == 0 {
		return ir.Report{Tool: tool, Status: ir.StatusOK, Summary: "no resources", Filtered: true, Raw: rawOf(c)}, nil
	}
	header := lines[0]
	cols := splitColumns(header)
	var items []ir.Item
	warn := false
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		vals := splitColumns(l)
		if len(vals) == 0 {
			continue
		}
		name := vals[0]
		// Collect the interesting columns by header name.
		var detail []string
		for i := 1; i < len(vals) && i < len(cols); i++ {
			switch cols[i] {
			case "READY", "STATUS", "RESTARTS", "AGE":
				detail = append(detail, vals[i])
				if cols[i] == "STATUS" && !kubeStatusOK(vals[i]) {
					warn = true
				}
			}
		}
		if len(detail) == 0 && len(vals) > 1 {
			detail = vals[1:]
		}
		items = append(items, ir.Item{Key: name, Val: strings.Join(detail, "  ")})
	}
	items, notes := capItems(items)
	st := ir.StatusOK
	if warn {
		st = ir.StatusWarn
	}
	return ir.Report{
		Tool: tool, Status: st,
		Summary: fmt.Sprintf("%d resource(s)", len(items)),
		Items:   items, Notes: notes, Filtered: true, Raw: rawOf(c),
	}, nil
}

func kubeStatusOK(s string) bool {
	switch s {
	case "Running", "Ready", "Active", "Completed", "Bound", "Succeeded":
		return true
	}
	return false
}

// kubeGetJSON structurally compacts `get -o json`. Handles both a List
// (items[]) and a single object.
func kubeGetJSON(tool string, c engine.CaptureResult) (ir.Report, error) {
	var root map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(c.Stdout)), &root); err != nil {
		return genericJSONCompact(tool, c)
	}
	objs := []map[string]any{}
	if itemsRaw, ok := root["items"].([]any); ok {
		for _, it := range itemsRaw {
			if m, ok := it.(map[string]any); ok {
				objs = append(objs, m)
			}
		}
	} else {
		objs = append(objs, root)
	}
	var items []ir.Item
	for _, obj := range objs {
		name, ns := kubeMeta(obj)
		key := name
		if ns != "" {
			key = ns + "/" + name
		}
		val := kubeObjStatus(obj)
		items = append(items, ir.Item{Key: key, Val: val})
	}
	items, notes := capItems(items)
	return ir.Report{
		Tool: tool, Status: ir.StatusOK,
		Summary: fmt.Sprintf("%d resource(s)", len(items)),
		Items:   items, Notes: notes, Filtered: true, Raw: rawOf(c),
	}, nil
}

func kubeMeta(obj map[string]any) (name, ns string) {
	if md, ok := obj["metadata"].(map[string]any); ok {
		name, _ = md["name"].(string)
		ns, _ = md["namespace"].(string)
	}
	return name, ns
}

func kubeObjStatus(obj map[string]any) string {
	if st, ok := obj["status"].(map[string]any); ok {
		if phase, ok := st["phase"].(string); ok {
			return phase
		}
	}
	if kind, ok := obj["kind"].(string); ok {
		return kind
	}
	return ""
}

// kubeDescribe keeps the labeled top sections and compacts the Events list to
// the last few entries.
func kubeDescribe(tool string, c engine.CaptureResult) (ir.Report, error) {
	if c.ExitCode != 0 {
		return ir.RawReport(tool, rawOf(c), c.ExitCode), nil
	}
	lines := splitLines(engine.StripANSI(c.Stdout))
	var kept []string
	inEvents := false
	var events []string
	for _, l := range lines {
		t := strings.TrimRight(l, " ")
		if strings.HasPrefix(t, "Events:") {
			inEvents = true
			continue
		}
		if inEvents {
			tr := strings.TrimSpace(t)
			if tr == "" || strings.HasPrefix(tr, "Type") && strings.Contains(tr, "Reason") {
				continue
			}
			if tr == "<none>" {
				continue
			}
			events = append(events, truncateRunes(tr, 200))
			continue
		}
		// Keep the labeled key sections; drop deeply indented verbose detail.
		if t == "" {
			continue
		}
		kept = append(kept, truncateRunes(t, 200))
	}
	kept, notes := capLines(kept, maxCloudLines)
	// Append last few events.
	if n := len(events); n > 0 {
		const keepEvents = 5
		if n > keepEvents {
			events = events[n-keepEvents:]
			kept = append(kept, fmt.Sprintf("Events (last %d of %d):", keepEvents, n))
		} else {
			kept = append(kept, "Events:")
		}
		for _, e := range events {
			kept = append(kept, "  "+e)
		}
	}
	return ir.Report{Tool: tool, Status: ir.StatusOK, Text: strings.Join(kept, "\n"), Notes: notes, Filtered: true, Raw: rawOf(c)}, nil
}

func kubeLogs(tool string, c engine.CaptureResult) (ir.Report, error) {
	lines := splitLines(engine.StripANSI(rawOf(c)))
	lines = dedupLines(lines)
	for i := range lines {
		lines[i] = truncateRunes(lines[i], 200)
	}
	lines, notes := capLines(lines, maxCloudLines)
	st := ir.StatusOK
	if c.ExitCode != 0 {
		st = ir.StatusFail
	}
	return ir.Report{Tool: tool, Status: st, Text: strings.Join(lines, "\n"), Notes: notes, Filtered: true, Raw: rawOf(c)}, nil
}

// kubeMutation parses "x/y created|configured|..." lines into items + counts.
func kubeMutation(tool string, c engine.CaptureResult, sub string) (ir.Report, error) {
	if c.ExitCode != 0 {
		return ir.RawReport(tool, rawOf(c), c.ExitCode), nil
	}
	lines := splitLines(engine.StripANSI(c.Stdout))
	var items []ir.Item
	counts := map[string]int{}
	var order []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if m := kubeMutationRE.FindStringSubmatch(t); m != nil {
			items = append(items, ir.Item{Key: m[1], Val: m[2]})
			if _, seen := counts[m[2]]; !seen {
				order = append(order, m[2])
			}
			counts[m[2]]++
		} else {
			items = append(items, ir.Item{Key: t})
		}
	}
	items, notes := capItems(items)
	var sumParts []string
	for _, a := range order {
		sumParts = append(sumParts, fmt.Sprintf("%d %s", counts[a], a))
	}
	summary := sub
	if len(sumParts) > 0 {
		summary = strings.Join(sumParts, ", ")
	}
	return ir.Report{Tool: tool, Subcommand: sub, Status: ir.StatusOK, Summary: summary, Items: items, Notes: notes, Filtered: true, Raw: rawOf(c)}, nil
}

func kubeTop(tool string, c engine.CaptureResult) (ir.Report, error) {
	if c.ExitCode != 0 {
		return ir.RawReport(tool, rawOf(c), c.ExitCode), nil
	}
	lines := splitLines(engine.StripANSI(c.Stdout))
	if len(lines) == 0 {
		return ir.Report{Tool: tool, Status: ir.StatusOK, Filtered: true, Raw: rawOf(c)}, nil
	}
	var items []ir.Item
	for _, l := range lines[1:] {
		if strings.TrimSpace(l) == "" {
			continue
		}
		vals := splitColumns(l)
		if len(vals) == 0 {
			continue
		}
		items = append(items, ir.Item{Key: vals[0], Val: strings.Join(vals[1:], "  ")})
	}
	items, notes := capItems(items)
	return ir.Report{Tool: tool, Subcommand: "top", Status: ir.StatusOK, Items: items, Notes: notes, Filtered: true, Raw: rawOf(c)}, nil
}

func kubeKeepLines(tool string, c engine.CaptureResult, sub string) (ir.Report, error) {
	if c.ExitCode != 0 {
		return ir.RawReport(tool, rawOf(c), c.ExitCode), nil
	}
	lines := splitLines(engine.StripANSI(c.Stdout))
	var kept []string
	for _, l := range lines {
		if strings.TrimSpace(l) == "" {
			continue
		}
		kept = append(kept, truncateRunes(strings.TrimRight(l, " "), 200))
	}
	kept, notes := capLines(kept, maxCloudLines)
	return ir.Report{Tool: tool, Subcommand: sub, Status: ir.StatusOK, Text: strings.Join(kept, "\n"), Notes: notes, Filtered: true, Raw: rawOf(c)}, nil
}

func kubeCap(tool string, c engine.CaptureResult, sub string) (ir.Report, error) {
	if c.ExitCode != 0 {
		return ir.RawReport(tool, rawOf(c), c.ExitCode), nil
	}
	lines := splitLines(engine.StripANSI(c.Stdout))
	for i := range lines {
		lines[i] = truncateRunes(lines[i], 200)
	}
	lines, notes := capLines(lines, maxCloudLines)
	return ir.Report{Tool: tool, Subcommand: sub, Status: ir.StatusOK, Text: strings.Join(lines, "\n"), Notes: notes, Filtered: true, Raw: rawOf(c)}, nil
}

func kubeAdm(tool string, c engine.CaptureResult) (ir.Report, error) {
	if c.ExitCode != 0 {
		return ir.RawReport(tool, rawOf(c), c.ExitCode), nil
	}
	lines := splitLines(engine.StripANSI(c.Stdout))
	// If it looks tabular (NAME header), compact like a table.
	if len(lines) > 0 && strings.HasPrefix(lines[0], "NAME") {
		return kubeGetTable(tool, c)
	}
	return ir.RawReport(tool, rawOf(c), c.ExitCode), nil
}

// kubeCompactError compacts NotFound / forbidden errors into a one-liner;
// unknown errors pass through raw.
func kubeCompactError(tool string, c engine.CaptureResult) (ir.Report, bool) {
	s := strings.TrimSpace(rawOf(c))
	low := strings.ToLower(s)
	if strings.Contains(low, "notfound") || strings.Contains(low, "not found") ||
		strings.Contains(low, "forbidden") || strings.Contains(low, "(unauthorized)") {
		first := splitLines(s)
		msg := ""
		if len(first) > 0 {
			msg = truncateRunes(strings.TrimPrefix(first[0], "Error from server "), 300)
		}
		return ir.Report{Tool: tool, Status: ir.StatusFail, Summary: msg, Filtered: true, Raw: rawOf(c), ExitCode: c.ExitCode}, true
	}
	return ir.Report{}, false
}

// --- shared helpers ---

// outputFormat returns the -o/--output value (json/yaml/wide/...) or "".
func outputFormat(args []string) string {
	for i, a := range args {
		if (a == "-o" || a == "--output") && i+1 < len(args) {
			return args[i+1]
		}
		if strings.HasPrefix(a, "--output=") {
			return strings.TrimPrefix(a, "--output=")
		}
		if strings.HasPrefix(a, "-o=") {
			return strings.TrimPrefix(a, "-o=")
		}
	}
	return ""
}

var kubeColSplitRE = regexp.MustCompile(`\s{2,}|\t`)

// splitColumns splits a whitespace-aligned table row into fields.
func splitColumns(line string) []string {
	line = strings.TrimRight(line, " ")
	if line == "" {
		return nil
	}
	parts := kubeColSplitRE.Split(strings.TrimLeft(line, " "), -1)
	var out []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
