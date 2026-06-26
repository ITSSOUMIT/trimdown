package store

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// ToolStat is per-tool aggregated usage.
type ToolStat struct {
	Tool     string  `json:"tool"`
	Count    int     `json:"count"`
	Saved    int     `json:"saved"`
	AvgPct   float64 `json:"avg_pct"`
	totalPct float64
}

// Summary is the aggregate report behind `trimdown savings`.
type Summary struct {
	Commands   int        `json:"commands"`
	Filtered   int        `json:"filtered"`
	TotalIn    int        `json:"total_in"`
	TotalOut   int        `json:"total_out"`
	TotalSaved int        `json:"total_saved"`
	AvgPct     float64    `json:"avg_pct"`
	TopTools   []ToolStat `json:"top_tools"`
	Since      string     `json:"since,omitempty"`
}

// AggregateOpts filters which events are summarized.
type AggregateOpts struct {
	Since   time.Duration // 0 = all time
	Project string        // "" = all projects
	TopN    int           // 0 → default 10
}

// Aggregate rolls events up into a Summary. Only filtered commands contribute to
// the savings percentage (passthroughs are counted but don't dilute the rate).
func Aggregate(events []Event, o AggregateOpts) Summary {
	topN := o.TopN
	if topN == 0 {
		topN = 10
	}
	var cutoff time.Time
	if o.Since > 0 {
		cutoff = time.Now().Add(-o.Since)
	}

	var s Summary
	byTool := map[string]*ToolStat{}
	var pctSum float64

	for _, ev := range events {
		if o.Project != "" && ev.Project != o.Project {
			continue
		}
		if !cutoff.IsZero() {
			if ts, err := time.Parse(time.RFC3339, ev.TS); err == nil && ts.Before(cutoff) {
				continue
			}
		}
		s.Commands++
		s.TotalIn += ev.In
		s.TotalOut += ev.Out
		s.TotalSaved += ev.Saved

		ts := byTool[ev.Tool]
		if ts == nil {
			ts = &ToolStat{Tool: ev.Tool}
			byTool[ev.Tool] = ts
		}
		ts.Count++
		ts.Saved += ev.Saved

		if ev.Mode == ModeFiltered {
			s.Filtered++
			pctSum += ev.Pct
			ts.totalPct += ev.Pct
		}
	}

	if s.Filtered > 0 {
		s.AvgPct = pctSum / float64(s.Filtered)
	}

	tools := make([]ToolStat, 0, len(byTool))
	for _, ts := range byTool {
		if ts.Count > 0 {
			ts.AvgPct = ts.totalPct / float64(ts.Count)
		}
		tools = append(tools, *ts)
	}
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].Saved != tools[j].Saved {
			return tools[i].Saved > tools[j].Saved
		}
		return tools[i].Tool < tools[j].Tool
	})
	if len(tools) > topN {
		tools = tools[:topN]
	}
	s.TopTools = tools

	if o.Since > 0 {
		s.Since = o.Since.String()
	}
	return s
}

// Humanize renders a token count compactly (1.2K, 3.4M).
func Humanize(n int) string {
	switch {
	case n >= 1_000_000:
		return trimZero(float64(n)/1_000_000) + "M"
	case n >= 1_000:
		return trimZero(float64(n)/1_000) + "K"
	default:
		return strconv.Itoa(n)
	}
}

func trimZero(f float64) string {
	s := strconv.FormatFloat(f, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0")
}
