package sys

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/itssoumit/trimdown/internal/engine"
	"github.com/itssoumit/trimdown/internal/ir"
	"github.com/itssoumit/trimdown/internal/registry"
)

// jsonFilter prints a JSON file's structure (keys + types), stripping values —
// huge savings on large payloads. Pass --compact to instead re-emit minified JSON.
type jsonFilter struct{}

func (jsonFilter) Tool() string       { return "json" }
func (jsonFilter) Subcommand() string { return "" }

func (jsonFilter) Exec(o registry.Opts) engine.CaptureResult {
	fo := parseFileOpts(o.Args)
	return engine.CaptureResult{Stdout: readFilesOrStdin(fo.files), ExitCode: 0}
}

func (jsonFilter) Parse(c engine.CaptureResult, o registry.Opts) (ir.Report, error) {
	var v any
	if json.Unmarshal([]byte(c.Stdout), &v) != nil {
		return ir.Report{Filtered: false, Raw: c.Stdout, ExitCode: c.ExitCode}, nil
	}

	if hasFlag(o.Args, "--compact") {
		b, _ := json.Marshal(v)
		return ir.Report{Tool: "json", Status: ir.StatusOK, Text: string(b), Filtered: true, Raw: c.Stdout}, nil
	}

	var b strings.Builder
	writeSchema(&b, v, 0)
	return ir.Report{
		Tool:     "json",
		Summary:  "schema",
		Status:   ir.StatusOK,
		Text:     strings.TrimRight(b.String(), "\n"),
		Filtered: true,
		Raw:      c.Stdout,
	}, nil
}

const maxSchemaDepth = 6

func writeSchema(b *strings.Builder, v any, depth int) {
	indent := strings.Repeat("  ", depth)
	switch t := v.(type) {
	case map[string]any:
		if depth >= maxSchemaDepth {
			fmt.Fprintf(b, "%s{…}\n", indent)
			return
		}
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			switch t[k].(type) {
			case map[string]any, []any:
				fmt.Fprintf(b, "%s%s:\n", indent, k)
				writeSchema(b, t[k], depth+1)
			default:
				fmt.Fprintf(b, "%s%s: %s\n", indent, k, typeName(t[k]))
			}
		}
	case []any:
		if len(t) == 0 {
			fmt.Fprintf(b, "%s[] (empty)\n", indent)
			return
		}
		fmt.Fprintf(b, "%s[%d] of:\n", indent, len(t))
		writeSchema(b, t[0], depth+1)
	default:
		fmt.Fprintf(b, "%s%s\n", indent, typeName(v))
	}
}

func typeName(v any) string {
	switch v.(type) {
	case bool:
		return "bool"
	case float64:
		return "number"
	case string:
		return "string"
	case nil:
		return "null"
	case map[string]any:
		return "object"
	case []any:
		return "array"
	default:
		return "?"
	}
}

func hasFlag(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}
