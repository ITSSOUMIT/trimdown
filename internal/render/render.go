// Package render turns a typed ir.Report into output: a generic compact text
// renderer (shared by every native filter), or structured JSON. Decoupling
// render from parse means one formatter for all tools and a free --json mode.
package render

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/itssoumit/trimdown/internal/ir"
)

// Opts controls rendering.
type Opts struct {
	JSON  bool
	Raw   bool
	Quiet bool
}

// Render produces the final string for a report.
func Render(r ir.Report, o Opts) string {
	if o.JSON {
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return r.Raw
		}
		return string(b)
	}
	if o.Raw || !r.Filtered {
		return r.Raw
	}
	// Text-mode filters (declarative engine, simple strip filters) carry
	// pre-formatted output; emit it (with an optional summary line) verbatim.
	if r.Text != "" {
		if r.Summary == "" {
			return r.Text
		}
		return icon(r.Status) + " " + r.Summary + "\n" + r.Text
	}
	return text(r)
}

func text(r ir.Report) string {
	var b strings.Builder
	if r.Summary != "" {
		b.WriteString(icon(r.Status))
		b.WriteByte(' ')
		b.WriteString(r.Summary)
		b.WriteByte('\n')
	}

	renderDiagnostics(&b, r.Diagnostics)
	renderTests(&b, r.Tests)
	renderItems(&b, r.Items)

	for _, n := range r.Notes {
		b.WriteString(n)
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

func renderDiagnostics(b *strings.Builder, diags []ir.Diagnostic) {
	if len(diags) == 0 {
		return
	}
	// Group by file, preserving first-seen order.
	order := []string{}
	byFile := map[string][]ir.Diagnostic{}
	for _, d := range diags {
		f := d.File
		if f == "" {
			f = "(general)"
		}
		if _, ok := byFile[f]; !ok {
			order = append(order, f)
		}
		byFile[f] = append(byFile[f], d)
	}
	for _, f := range order {
		ds := byFile[f]
		b.WriteString(f)
		b.WriteString(" (")
		b.WriteString(itoa(len(ds)))
		b.WriteString(")\n")
		for _, d := range ds {
			b.WriteString("  ")
			b.WriteString(loc(d))
			if d.Rule != "" {
				b.WriteString(d.Rule)
				b.WriteByte(' ')
			}
			b.WriteString(d.Message)
			b.WriteByte('\n')
			for _, c := range d.Context {
				b.WriteString("    ")
				b.WriteString(c)
				b.WriteByte('\n')
			}
		}
	}
}

func renderTests(b *strings.Builder, tests []ir.TestResult) {
	if len(tests) == 0 {
		return
	}
	for _, t := range tests {
		b.WriteString("  ")
		b.WriteString(icon(t.Status))
		b.WriteByte(' ')
		if t.Package != "" {
			b.WriteString(t.Package)
			b.WriteString(" · ")
		}
		b.WriteString(t.Name)
		b.WriteByte('\n')
		for _, d := range t.Detail {
			b.WriteString("     ")
			b.WriteString(d)
			b.WriteByte('\n')
		}
	}
}

func renderItems(b *strings.Builder, items []ir.Item) {
	if len(items) == 0 {
		return
	}
	for _, it := range items {
		b.WriteString("  ")
		b.WriteString(it.Key)
		if it.Val != "" {
			b.WriteString("  ")
			b.WriteString(it.Val)
		}
		b.WriteByte('\n')
	}
}

func loc(d ir.Diagnostic) string {
	if d.Line == 0 {
		return ""
	}
	s := ":" + itoa(d.Line)
	if d.Col > 0 {
		s += ":" + itoa(d.Col)
	}
	return s + " "
}

func icon(s ir.Status) string {
	switch s {
	case ir.StatusOK:
		return "✓"
	case ir.StatusWarn:
		return "⚠"
	case ir.StatusFail:
		return "✗"
	default:
		return "•"
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
