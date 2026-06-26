package vcshost

import (
	"fmt"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

// gh is the GitHub CLI dispatcher.
type gh struct{}

func (gh) Tool() string       { return "gh" }
func (gh) Subcommand() string { return "" }

// Curated, version-stable --json field sets per subcommand.
const (
	ghPRListFields = "number,title,headRefName,state,isDraft,author"
	ghPRViewFields = "number,title,state,author,headRefName,baseRefName,additions,deletions,mergeable,reviewDecision,isDraft"
	ghIssueListFie = "number,title,state,labels,author"
	ghIssueViewFie = "number,title,state,author,labels"
	ghRunListField = "status,conclusion,name,workflowName,headBranch,createdAt,displayTitle"
	ghRepoViewFiel = "name,description,defaultBranchRef,stargazerCount,forkCount,isPrivate,visibility,url"
	ghRepoListFiel = "name,description,visibility,isPrivate"
	ghReleaseListF = "tagName,name,isLatest,publishedAt,isDraft,isPrerelease"
	ghReleaseViewF = "tagName,name,publishedAt,isDraft,isPrerelease,author"
)

func (gh) Exec(o registry.Opts) engine.CaptureResult {
	args := append([]string{}, o.Args...)
	sub := firstNonFlag(o.Args)
	sub2 := nthNonFlag(o.Args, 1)

	switch sub {
	case "pr":
		switch sub2 {
		case "list":
			args = injectJSON(args, ghPRListFields)
		case "view":
			args = injectJSON(args, ghPRViewFields)
		}
	case "issue":
		switch sub2 {
		case "list":
			args = injectJSON(args, ghIssueListFie)
		case "view":
			args = injectJSON(args, ghIssueViewFie)
		}
	case "run":
		if sub2 == "list" {
			args = injectJSON(args, ghRunListField)
		}
	case "repo":
		switch sub2 {
		case "view":
			args = injectJSON(args, ghRepoViewFiel)
		case "list":
			args = injectJSON(args, ghRepoListFiel)
		}
	case "release":
		switch sub2 {
		case "list":
			args = injectJSON(args, ghReleaseListF)
		case "view":
			args = injectJSON(args, ghReleaseViewF)
		}
	}
	return resolveExec("gh", args)
}

func (gh) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	raw := rawOf(c)
	sub := firstNonFlag(o.Args)
	sub2 := nthNonFlag(o.Args, 1)

	// Non-zero exit → passthrough so auth/permission errors stay visible.
	if c.ExitCode != 0 {
		return ir.RawReport("gh", raw, c.ExitCode), nil
	}

	switch sub {
	case "pr":
		switch sub2 {
		case "list":
			return ghIssueOrPRList("gh", "pr", c, raw), nil
		case "view":
			return ghPRView(c, raw), nil
		case "status", "checks":
			return compactTable("gh", "pr "+sub2, c), nil
		}
	case "issue":
		switch sub2 {
		case "list":
			return ghIssueOrPRList("gh", "issue", c, raw), nil
		case "view":
			return ghIssueView(c, raw), nil
		case "status":
			return compactTable("gh", "issue status", c), nil
		}
	case "run":
		switch sub2 {
		case "list":
			return ghRunList(c, raw), nil
		case "view", "watch":
			return compactTable("gh", "run "+sub2, c), nil
		}
	case "repo":
		switch sub2 {
		case "view":
			return ghRepoView(c, raw), nil
		case "list":
			return ghRepoList(c, raw), nil
		}
	case "release":
		switch sub2 {
		case "list":
			return ghReleaseList(c, raw), nil
		case "view":
			return ghReleaseView(c, raw), nil
		}
	case "workflow":
		if sub2 == "list" {
			return compactTable("gh", "workflow list", c), nil
		}
	case "gist":
		if sub2 == "list" {
			return compactTable("gh", "gist list", c), nil
		}
	case "api":
		return compactJSONText("gh", "api", c), nil
	case "auth":
		if sub2 == "status" {
			return ghAuthStatus(c, raw), nil
		}
	}
	return ir.RawReport("gh", raw, c.ExitCode), nil
}

// ghIssueOrPRList parses `gh pr list`/`gh issue list` JSON, falling back to the
// human table.
func ghIssueOrPRList(tool, kind string, c engine.CaptureResult, raw string) ir.Report {
	rows, ok := decodeJSONArray(c.Stdout)
	if !ok {
		return compactTable(tool, kind+" list", c)
	}
	var items []ir.Item
	for _, r := range rows {
		num := str(r, "number")
		title := truncateRunes(str(r, "title"), 90)
		state := str(r, "state")
		key := "#" + num
		var extras []string
		if kind == "pr" {
			if b := str(r, "headRefName"); b != "" {
				extras = append(extras, "("+b+")")
			}
			if d, _ := r["isDraft"].(bool); d {
				state = "DRAFT"
			}
		} else if labels := labelNames(r, "labels"); len(labels) > 0 {
			extras = append(extras, "["+strings.Join(labels, ",")+"]")
		}
		if state != "" {
			extras = append(extras, strings.ToLower(state))
		}
		val := joinNonEmpty(" ", append([]string{title}, extras...)...)
		items = append(items, ir.Item{Key: key, Val: val})
	}
	total := len(items)
	items, note := capItems(items, maxVCSItems)
	r := keyValReport(tool, kind+" list", fmt.Sprintf("%d %ss", total, kind), ir.StatusOK, items, raw)
	if note != "" {
		r.Notes = []string{note}
	}
	return r
}

func ghPRView(c engine.CaptureResult, raw string) ir.Report {
	obj, ok := decodeJSONObject(c.Stdout)
	if !ok {
		return compactTable("gh", "pr view", c)
	}
	state := str(obj, "state")
	if d, _ := obj["isDraft"].(bool); d {
		state = "DRAFT"
	}
	items := []ir.Item{
		{Key: "title", Val: truncateRunes(str(obj, "title"), vcsValMax)},
		{Key: "state", Val: state},
		{Key: "author", Val: nestedStr(obj, "author", "login")},
		{Key: "branch", Val: str(obj, "headRefName") + " → " + str(obj, "baseRefName")},
		{Key: "changes", Val: fmt.Sprintf("+%d -%d", num(obj, "additions"), num(obj, "deletions"))},
	}
	if m := str(obj, "mergeable"); m != "" {
		items = append(items, ir.Item{Key: "mergeable", Val: m})
	}
	if rd := str(obj, "reviewDecision"); rd != "" {
		items = append(items, ir.Item{Key: "review", Val: rd})
	}
	summary := "#" + str(obj, "number") + " " + truncateRunes(str(obj, "title"), 80)
	return keyValReport("gh", "pr view", summary, vcsStatusFor(state), items, raw)
}

func ghIssueView(c engine.CaptureResult, raw string) ir.Report {
	obj, ok := decodeJSONObject(c.Stdout)
	if !ok {
		return compactTable("gh", "issue view", c)
	}
	state := str(obj, "state")
	items := []ir.Item{
		{Key: "title", Val: truncateRunes(str(obj, "title"), vcsValMax)},
		{Key: "state", Val: state},
		{Key: "author", Val: nestedStr(obj, "author", "login")},
	}
	if labels := labelNames(obj, "labels"); len(labels) > 0 {
		items = append(items, ir.Item{Key: "labels", Val: strings.Join(labels, ", ")})
	}
	summary := "#" + str(obj, "number") + " " + truncateRunes(str(obj, "title"), 80)
	return keyValReport("gh", "issue view", summary, vcsStatusFor(state), items, raw)
}

func ghRunList(c engine.CaptureResult, raw string) ir.Report {
	rows, ok := decodeJSONArray(c.Stdout)
	if !ok {
		return compactTable("gh", "run list", c)
	}
	var items []ir.Item
	failed := 0
	for _, r := range rows {
		concl := str(r, "conclusion")
		status := str(r, "status")
		state := concl
		if state == "" {
			state = status
		}
		if strings.EqualFold(concl, "failure") || strings.EqualFold(concl, "cancelled") {
			failed++
		}
		name := str(r, "workflowName")
		if name == "" {
			name = str(r, "name")
		}
		title := str(r, "displayTitle")
		key := stateIcon(state) + " " + name
		val := joinNonEmpty(" · ", title, str(r, "headBranch"), strings.ToLower(state))
		items = append(items, ir.Item{Key: key, Val: val})
	}
	total := len(items)
	items, note := capItems(items, maxVCSItems)
	st := ir.StatusOK
	if failed > 0 {
		st = ir.StatusFail
	}
	r := keyValReport("gh", "run list", fmt.Sprintf("%d runs, %d failed", total, failed), st, items, raw)
	if note != "" {
		r.Notes = []string{note}
	}
	return r
}

func ghRepoView(c engine.CaptureResult, raw string) ir.Report {
	obj, ok := decodeJSONObject(c.Stdout)
	if !ok {
		return compactTable("gh", "repo view", c)
	}
	items := []ir.Item{
		{Key: "name", Val: str(obj, "name")},
		{Key: "description", Val: truncateRunes(str(obj, "description"), vcsValMax)},
		{Key: "default", Val: nestedStr(obj, "defaultBranchRef", "name")},
		{Key: "stars", Val: str(obj, "stargazerCount")},
		{Key: "forks", Val: str(obj, "forkCount")},
	}
	if v := str(obj, "visibility"); v != "" {
		items = append(items, ir.Item{Key: "visibility", Val: v})
	}
	return keyValReport("gh", "repo view", str(obj, "name"), ir.StatusOK, items, raw)
}

func ghRepoList(c engine.CaptureResult, raw string) ir.Report {
	rows, ok := decodeJSONArray(c.Stdout)
	if !ok {
		return compactTable("gh", "repo list", c)
	}
	var items []ir.Item
	for _, r := range rows {
		items = append(items, ir.Item{Key: str(r, "name"), Val: truncateRunes(str(r, "description"), vcsValMax)})
	}
	total := len(items)
	items, note := capItems(items, maxVCSItems)
	rep := keyValReport("gh", "repo list", fmt.Sprintf("%d repos", total), ir.StatusOK, items, raw)
	if note != "" {
		rep.Notes = []string{note}
	}
	return rep
}

func ghReleaseList(c engine.CaptureResult, raw string) ir.Report {
	rows, ok := decodeJSONArray(c.Stdout)
	if !ok {
		return compactTable("gh", "release list", c)
	}
	var items []ir.Item
	for _, r := range rows {
		key := str(r, "tagName")
		val := joinNonEmpty(" · ", truncateRunes(str(r, "name"), 80), dateOnly(str(r, "publishedAt")))
		if l, _ := r["isLatest"].(bool); l {
			val = joinNonEmpty(" · ", val, "latest")
		}
		items = append(items, ir.Item{Key: key, Val: val})
	}
	total := len(items)
	items, note := capItems(items, maxVCSItems)
	rep := keyValReport("gh", "release list", fmt.Sprintf("%d releases", total), ir.StatusOK, items, raw)
	if note != "" {
		rep.Notes = []string{note}
	}
	return rep
}

func ghReleaseView(c engine.CaptureResult, raw string) ir.Report {
	obj, ok := decodeJSONObject(c.Stdout)
	if !ok {
		return compactTable("gh", "release view", c)
	}
	items := []ir.Item{
		{Key: "tag", Val: str(obj, "tagName")},
		{Key: "name", Val: truncateRunes(str(obj, "name"), vcsValMax)},
		{Key: "published", Val: dateOnly(str(obj, "publishedAt"))},
	}
	return keyValReport("gh", "release view", str(obj, "tagName"), ir.StatusOK, items, raw)
}

func ghAuthStatus(c engine.CaptureResult, raw string) ir.Report {
	// gh auth status prints to stderr; keep the few meaningful status lines.
	src := engine.StripANSI(c.Stdout + "\n" + c.Stderr)
	var kept []string
	for _, l := range splitLines(src) {
		t := strings.TrimSpace(l)
		if t == "" {
			continue
		}
		if strings.Contains(t, "Logged in") || strings.Contains(t, "Token") ||
			strings.Contains(t, "scopes") || strings.Contains(t, "account") ||
			strings.HasSuffix(t, ".com") || strings.HasSuffix(t, ":") {
			kept = append(kept, truncateRunes(t, vcsTruncateCol))
		}
	}
	if len(kept) == 0 {
		return ir.RawReport("gh", raw, c.ExitCode)
	}
	return ir.Report{
		Tool: "gh", Subcommand: "auth status", Status: ir.StatusOK,
		Text: strings.Join(kept, "\n"), Filtered: true, Raw: raw,
	}
}

// stateIcon maps a CI/PR state to a leading marker.
func stateIcon(state string) string {
	switch strings.ToLower(state) {
	case "success", "completed", "passed":
		return "✓"
	case "failure", "failed", "cancelled", "timed_out", "error":
		return "✗"
	case "in_progress", "queued", "pending", "waiting":
		return "·"
	default:
		return "·"
	}
}

// dateOnly trims an ISO timestamp to its date part.
func dateOnly(s string) string {
	if i := strings.IndexByte(s, 'T'); i > 0 {
		return s[:i]
	}
	return s
}
