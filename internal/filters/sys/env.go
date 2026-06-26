package sys

import (
	"os"
	"sort"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

// secretRE flags env var names whose values should be masked.
var secretKeywords = []string{"SECRET", "TOKEN", "PASSWORD", "PASSWD", "KEY", "CREDENTIAL", "PRIVATE", "AUTH"}

// envFilter lists environment variables with secrets masked; optional name filter.
type envFilter struct{}

func (envFilter) Tool() string       { return "env" }
func (envFilter) Subcommand() string { return "" }

func (envFilter) Exec(registry.Opts) engine.CaptureResult {
	return engine.CaptureResult{Stdout: strings.Join(os.Environ(), "\n"), ExitCode: 0}
}

func (envFilter) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	filter := ""
	for i, a := range o.Args {
		if (a == "-f" || a == "--filter") && i+1 < len(o.Args) {
			filter = strings.ToUpper(o.Args[i+1])
		}
	}

	var items []ir.Item
	for _, line := range splitLines(c.Stdout) {
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if filter != "" && !strings.Contains(strings.ToUpper(k), filter) {
			continue
		}
		if isSecret(k) {
			v = "***"
		} else {
			v = truncateRunes(v, 120)
		}
		items = append(items, ir.Item{Key: k, Val: v})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Key < items[j].Key })

	return ir.Report{
		Tool:     "env",
		Status:   ir.StatusOK,
		Items:    items,
		Filtered: true,
		Raw:      c.Stdout,
	}, nil
}

func isSecret(key string) bool {
	up := strings.ToUpper(key)
	for _, kw := range secretKeywords {
		if strings.Contains(up, kw) {
			return true
		}
	}
	return false
}
