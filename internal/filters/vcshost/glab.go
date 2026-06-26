package vcshost

import (
	"fmt"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

// glab is the GitLab CLI dispatcher. glab mirrors gh and also supports -R/-g
// flags, which firstNonFlag/nthNonFlag already skip over.
type glab struct{}

func (glab) Tool() string       { return "glab" }
func (glab) Subcommand() string { return "" }

const (
	glabMRListFields  = "iid,title,source_branch,state,author"
	glabMRViewFields  = "iid,title,state,author,source_branch,target_branch,merge_status"
	glabIssListFields = "iid,title,state,labels,author"
	glabIssViewFields = "iid,title,state,author,labels"
	glabRelListFields = "tag_name,name,released_at"
)

func (glab) Exec(o registry.Opts) engine.CaptureResult {
	args := append([]string{}, o.Args...)
	sub := firstNonFlag(o.Args)
	sub2 := nthNonFlag(o.Args, 1)

	switch sub {
	case "mr":
		switch sub2 {
		case "list", "ls":
			args = injectJSON(args, glabMRListFields)
		case "view", "show":
			args = injectJSON(args, glabMRViewFields)
		}
	case "issue":
		switch sub2 {
		case "list", "ls":
			args = injectJSON(args, glabIssListFields)
		case "view", "show":
			args = injectJSON(args, glabIssViewFields)
		}
	case "release":
		if sub2 == "list" {
			args = injectJSON(args, glabRelListFields)
		}
	}
	return resolveExec("glab", args)
}

func (glab) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	raw := rawOf(c)
	sub := firstNonFlag(o.Args)
	sub2 := nthNonFlag(o.Args, 1)

	if c.ExitCode != 0 {
		return ir.RawReport("glab", raw, c.ExitCode), nil
	}

	switch sub {
	case "mr":
		switch sub2 {
		case "list", "ls":
			return glabMRList(c, raw), nil
		case "view", "show":
			return glabMRView(c, raw), nil
		case "status", "checks":
			return compactTable("glab", "mr "+sub2, c), nil
		}
	case "issue":
		switch sub2 {
		case "list", "ls":
			return glabIssueList(c, raw), nil
		case "view", "show":
			return glabIssueView(c, raw), nil
		}
	case "ci", "pipeline":
		switch sub2 {
		case "list", "view", "status":
			return compactTable("glab", sub+" "+sub2, c), nil
		}
	case "release":
		switch sub2 {
		case "list":
			return glabReleaseList(c, raw), nil
		case "view", "show":
			return compactTable("glab", "release view", c), nil
		}
	case "repo":
		if sub2 == "view" {
			return compactTable("glab", "repo view", c), nil
		}
	case "api":
		return compactJSONText("glab", "api", c), nil
	case "auth":
		if sub2 == "status" {
			return ghAuthStatusFor("glab", c, raw), nil
		}
	}
	return ir.RawReport("glab", raw, c.ExitCode), nil
}

func glabMRList(c engine.CaptureResult, raw string) ir.Report {
	rows, ok := decodeJSONArray(c.Stdout)
	if !ok {
		return compactTable("glab", "mr list", c)
	}
	var items []ir.Item
	for _, r := range rows {
		key := "!" + str(r, "iid")
		title := truncateRunes(str(r, "title"), 90)
		var extras []string
		if b := str(r, "source_branch"); b != "" {
			extras = append(extras, "("+b+")")
		}
		if state := str(r, "state"); state != "" {
			extras = append(extras, strings.ToLower(state))
		}
		items = append(items, ir.Item{Key: key, Val: joinNonEmpty(" ", append([]string{title}, extras...)...)})
	}
	total := len(items)
	items, note := capItems(items, maxVCSItems)
	rep := keyValReport("glab", "mr list", fmt.Sprintf("%d mrs", total), ir.StatusOK, items, raw)
	if note != "" {
		rep.Notes = []string{note}
	}
	return rep
}

func glabMRView(c engine.CaptureResult, raw string) ir.Report {
	obj, ok := decodeJSONObject(c.Stdout)
	if !ok {
		return compactTable("glab", "mr view", c)
	}
	state := str(obj, "state")
	items := []ir.Item{
		{Key: "title", Val: truncateRunes(str(obj, "title"), vcsValMax)},
		{Key: "state", Val: state},
		{Key: "author", Val: nestedStr(obj, "author", "username")},
		{Key: "branch", Val: str(obj, "source_branch") + " → " + str(obj, "target_branch")},
	}
	if m := str(obj, "merge_status"); m != "" {
		items = append(items, ir.Item{Key: "merge", Val: m})
	}
	summary := "!" + str(obj, "iid") + " " + truncateRunes(str(obj, "title"), 80)
	return keyValReport("glab", "mr view", summary, vcsStatusFor(state), items, raw)
}

func glabIssueList(c engine.CaptureResult, raw string) ir.Report {
	rows, ok := decodeJSONArray(c.Stdout)
	if !ok {
		return compactTable("glab", "issue list", c)
	}
	var items []ir.Item
	for _, r := range rows {
		key := "#" + str(r, "iid")
		title := truncateRunes(str(r, "title"), 90)
		var extras []string
		if labels := glabLabels(r); len(labels) > 0 {
			extras = append(extras, "["+strings.Join(labels, ",")+"]")
		}
		if state := str(r, "state"); state != "" {
			extras = append(extras, strings.ToLower(state))
		}
		items = append(items, ir.Item{Key: key, Val: joinNonEmpty(" ", append([]string{title}, extras...)...)})
	}
	total := len(items)
	items, note := capItems(items, maxVCSItems)
	rep := keyValReport("glab", "issue list", fmt.Sprintf("%d issues", total), ir.StatusOK, items, raw)
	if note != "" {
		rep.Notes = []string{note}
	}
	return rep
}

func glabIssueView(c engine.CaptureResult, raw string) ir.Report {
	obj, ok := decodeJSONObject(c.Stdout)
	if !ok {
		return compactTable("glab", "issue view", c)
	}
	state := str(obj, "state")
	items := []ir.Item{
		{Key: "title", Val: truncateRunes(str(obj, "title"), vcsValMax)},
		{Key: "state", Val: state},
		{Key: "author", Val: nestedStr(obj, "author", "username")},
	}
	if labels := glabLabels(obj); len(labels) > 0 {
		items = append(items, ir.Item{Key: "labels", Val: strings.Join(labels, ", ")})
	}
	summary := "#" + str(obj, "iid") + " " + truncateRunes(str(obj, "title"), 80)
	return keyValReport("glab", "issue view", summary, vcsStatusFor(state), items, raw)
}

func glabReleaseList(c engine.CaptureResult, raw string) ir.Report {
	rows, ok := decodeJSONArray(c.Stdout)
	if !ok {
		return compactTable("glab", "release list", c)
	}
	var items []ir.Item
	for _, r := range rows {
		val := joinNonEmpty(" · ", truncateRunes(str(r, "name"), 80), dateOnly(str(r, "released_at")))
		items = append(items, ir.Item{Key: str(r, "tag_name"), Val: val})
	}
	total := len(items)
	items, note := capItems(items, maxVCSItems)
	rep := keyValReport("glab", "release list", fmt.Sprintf("%d releases", total), ir.StatusOK, items, raw)
	if note != "" {
		rep.Notes = []string{note}
	}
	return rep
}

// glabLabels handles GitLab's labels field, which may be []string or
// [{name:...}].
func glabLabels(m map[string]any) []string {
	arr, ok := m["labels"].([]any)
	if !ok {
		return nil
	}
	var out []string
	for _, e := range arr {
		switch v := e.(type) {
		case string:
			out = append(out, v)
		case map[string]any:
			if n := str(v, "name"); n != "" {
				out = append(out, n)
			}
		}
	}
	return out
}

// ghAuthStatusFor is the glab-flavored auth status compactor (shared shape).
func ghAuthStatusFor(tool string, c engine.CaptureResult, raw string) ir.Report {
	src := engine.StripANSI(c.Stdout + "\n" + c.Stderr)
	var kept []string
	for _, l := range splitLines(src) {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if strings.Contains(t, "Logged in") || strings.Contains(t, "Token") ||
			strings.Contains(t, "scopes") || strings.Contains(t, "API") ||
			strings.HasSuffix(t, ":") {
			kept = append(kept, truncateRunes(t, vcsTruncateCol))
		}
	}
	if len(kept) == 0 {
		return ir.RawReport(tool, raw, c.ExitCode)
	}
	return ir.Report{
		Tool: tool, Subcommand: "auth status", Status: ir.StatusOK,
		Text: strings.Join(kept, "\n"), Filtered: true, Raw: raw,
	}
}
