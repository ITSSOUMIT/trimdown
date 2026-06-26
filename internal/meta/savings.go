package meta

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/itssoumit/trimdown/internal/store"
)

// Savings implements `trimdown savings [--json] [--since DUR] [--project]`.
func Savings(args []string) int {
	var o store.AggregateOpts
	asJSON := false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			asJSON = true
		case "--project", "-p":
			if wd, err := os.Getwd(); err == nil {
				o.Project = wd
			}
		case "--since", "-s":
			if i+1 < len(args) {
				i++
				if d, err := time.ParseDuration(args[i]); err == nil {
					o.Since = d
				}
			}
		}
	}

	s, err := store.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "trimdown savings:", err)
		return 1
	}
	events, err := s.Read()
	if err != nil {
		fmt.Fprintln(os.Stderr, "trimdown savings:", err)
		return 1
	}
	sum := store.Aggregate(events, o)

	if asJSON {
		b, _ := json.MarshalIndent(sum, "", "  ")
		fmt.Println(string(b))
		return 0
	}
	fmt.Print(formatSummary(sum))
	return 0
}

func formatSummary(s store.Summary) string {
	var b strings.Builder
	b.WriteString("trimdown savings")
	if s.Since != "" {
		b.WriteString(" (last " + s.Since + ")")
	}
	b.WriteByte('\n')
	if s.Commands == 0 {
		b.WriteString("  no commands recorded yet\n")
		return b.String()
	}
	fmt.Fprintf(&b, "  commands:     %d (%d filtered)\n", s.Commands, s.Filtered)
	fmt.Fprintf(&b, "  tokens saved: %s of %s (%.1f%% avg)\n",
		store.Humanize(s.TotalSaved), store.Humanize(s.TotalIn), s.AvgPct)
	if len(s.TopTools) > 0 {
		b.WriteString("  top tools:\n")
		for _, t := range s.TopTools {
			fmt.Fprintf(&b, "    %-14s %s saved  (%d×, %.0f%%)\n",
				t.Tool, store.Humanize(t.Saved), t.Count, t.AvgPct)
		}
	}
	return b.String()
}
