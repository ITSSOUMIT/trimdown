package store

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// CmdStat is per-command aggregated usage (grouped by tool + subcommand).
type CmdStat struct {
	Command string  `json:"command"`
	Count   int     `json:"count"`
	In      int     `json:"in"`
	Out     int     `json:"out"`
	Saved   int     `json:"saved"`
	Pct     float64 `json:"pct"`    // aggregate Saved/In
	AvgMS   int64   `json:"avg_ms"` // mean exec time per command
	totalMS int64
}

// Summary is the headline aggregate behind `trimdown savings`. It separates
// three things that matter for different reasons: filter *effectiveness*
// (Pct = saved/in over filtered commands), *coverage* (how many commands we
// actually intercepted), and *opportunity* (tokens that ran raw with no filter).
type Summary struct {
	Commands    int     `json:"commands"`
	Filtered    int     `json:"filtered"`
	Passthrough int     `json:"passthrough"`
	ParseFail   int     `json:"parse_fail"`
	Coverage    float64 `json:"coverage"` // Filtered/Commands

	TotalIn    int     `json:"total_in"`  // input tokens of filtered commands
	TotalOut   int     `json:"total_out"` // output tokens after filtering
	TotalSaved int     `json:"total_saved"`
	Pct        float64 `json:"pct"` // TotalSaved/TotalIn (filter effectiveness)

	OppTokens  int `json:"opportunity_tokens"` // tokens that ran raw (no filter)
	FailTokens int `json:"failure_tokens"`     // tokens wasted on parse fallbacks

	TotalMS int64 `json:"total_ms"`
	AvgMS   int64 `json:"avg_ms"`

	Savers        []CmdStat `json:"top_savers"`
	Opportunities []CmdStat `json:"opportunities,omitempty"`
	Failures      []CmdStat `json:"failures,omitempty"`

	Since string `json:"since,omitempty"`
	Scope string `json:"scope,omitempty"`
}

// AggregateOpts filters which events are summarized.
type AggregateOpts struct {
	Since   time.Duration // 0 = all time
	Project string        // "" = all projects
	TopN    int           // 0 → default 10
}

// included reports whether an event passes the project/since filter.
func included(ev Event, project string, cutoff time.Time) bool {
	if project != "" && ev.Project != project {
		return false
	}
	if !cutoff.IsZero() {
		if ts, err := time.Parse(time.RFC3339, ev.TS); err == nil && ts.Before(cutoff) {
			return false
		}
	}
	return true
}

func cutoffOf(o AggregateOpts) time.Time {
	if o.Since > 0 {
		return time.Now().Add(-o.Since)
	}
	return time.Time{}
}

// commandKey labels an event as "tool sub" (or just "tool" when there is no
// meaningful subcommand).
func commandKey(ev Event) string {
	if ev.Sub == "" {
		return ev.Tool
	}
	return ev.Tool + " " + ev.Sub
}

// Aggregate rolls events up into a Summary, classifying each command by mode:
// filtered (it saved tokens), passthrough (ran raw → an opportunity), or
// parse-failure (a filter fell back to raw → likely a bug).
func Aggregate(events []Event, o AggregateOpts) Summary {
	topN := o.TopN
	if topN == 0 {
		topN = 10
	}
	cutoff := cutoffOf(o)

	var s Summary
	savers := map[string]*CmdStat{}
	opps := map[string]*CmdStat{}
	fails := map[string]*CmdStat{}

	add := func(m map[string]*CmdStat, ev Event) {
		key := commandKey(ev)
		c := m[key]
		if c == nil {
			c = &CmdStat{Command: key}
			m[key] = c
		}
		c.Count++
		c.In += ev.In
		c.Out += ev.Out
		c.Saved += ev.Saved
		c.totalMS += ev.MS
	}

	for _, ev := range events {
		if !included(ev, o.Project, cutoff) {
			continue
		}
		s.Commands++
		s.TotalMS += ev.MS
		switch ev.Mode {
		case ModeFiltered:
			s.Filtered++
			s.TotalIn += ev.In
			s.TotalOut += ev.Out
			s.TotalSaved += ev.Saved
			add(savers, ev)
		case ModeParseFail:
			s.ParseFail++
			s.FailTokens += ev.In
			add(fails, ev)
		default: // ModePassthrough
			s.Passthrough++
			s.OppTokens += ev.In
			add(opps, ev)
		}
	}

	s.Pct = ratio(s.TotalSaved, s.TotalIn)
	s.Coverage = ratio(s.Filtered, s.Commands)
	if s.Commands > 0 {
		s.AvgMS = s.TotalMS / int64(s.Commands)
	}

	bySaved := func(a, b CmdStat) bool { return a.Saved > b.Saved }
	byIn := func(a, b CmdStat) bool { return a.In > b.In }
	byCount := func(a, b CmdStat) bool {
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.In > b.In
	}
	s.Savers = topStats(savers, topN, bySaved)
	s.Opportunities = topStats(opps, 5, byIn)
	s.Failures = topStats(fails, 5, byCount)

	if o.Since > 0 {
		s.Since = o.Since.String()
	}
	return s
}

// topStats finalizes per-command stats, sorts them, and keeps the top n.
func topStats(m map[string]*CmdStat, n int, less func(a, b CmdStat) bool) []CmdStat {
	out := make([]CmdStat, 0, len(m))
	for _, c := range m {
		c.Pct = ratio(c.Saved, c.In)
		if c.Count > 0 {
			c.AvgMS = c.totalMS / int64(c.Count)
		}
		out = append(out, *c)
	}
	sort.Slice(out, func(i, j int) bool {
		if less(out[i], out[j]) {
			return true
		}
		if less(out[j], out[i]) {
			return false
		}
		return out[i].Command < out[j].Command
	})
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Period is a time-bucket granularity for the breakdown tables.
type Period int

const (
	Daily Period = iota
	Weekly
	Monthly
)

// Bucket is one time bucket in a breakdown table.
type Bucket struct {
	Label string  `json:"label"`
	Cmds  int     `json:"cmds"`
	In    int     `json:"in"`
	Out   int     `json:"out"`
	Saved int     `json:"saved"`
	Pct   float64 `json:"pct"`
	MS    int64   `json:"ms"` // total exec time in the bucket
}

// AggregateBuckets rolls events into chronologically-sorted time buckets.
func AggregateBuckets(events []Event, o AggregateOpts, p Period) []Bucket {
	cutoff := cutoffOf(o)
	type acc struct {
		b    Bucket
		sort string
	}
	m := map[string]*acc{}

	for _, ev := range events {
		if !included(ev, o.Project, cutoff) {
			continue
		}
		t, err := time.Parse(time.RFC3339, ev.TS)
		if err != nil {
			continue
		}
		key, label, sortKey := bucketKey(t, p)
		a := m[key]
		if a == nil {
			a = &acc{b: Bucket{Label: label}, sort: sortKey}
			m[key] = a
		}
		a.b.Cmds++
		a.b.MS += ev.MS
		// In/Out/Saved measure filtered work, so Save% stays "effectiveness".
		if ev.Mode == ModeFiltered {
			a.b.In += ev.In
			a.b.Out += ev.Out
			a.b.Saved += ev.Saved
		}
	}

	out := make([]*acc, 0, len(m))
	for _, a := range m {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].sort < out[j].sort })

	buckets := make([]Bucket, 0, len(out))
	for _, a := range out {
		a.b.Pct = ratio(a.b.Saved, a.b.In)
		buckets = append(buckets, a.b)
	}
	return buckets
}

// bucketKey returns the map key, display label, and chronological sort key for
// a timestamp at the given period.
func bucketKey(t time.Time, p Period) (key, label, sortKey string) {
	switch p {
	case Weekly:
		wd := int(t.Weekday())
		if wd == 0 {
			wd = 7 // treat Sunday as the last day of the week
		}
		monday := t.AddDate(0, 0, -(wd - 1))
		sortKey = monday.Format("2006-01-02")
		label = monday.Format("01-02") + " → " + monday.AddDate(0, 0, 6).Format("01-02")
		return sortKey, label, sortKey
	case Monthly:
		key = t.Format("2006-01")
		return key, key, key
	default: // Daily
		key = t.Format("2006-01-02")
		return key, key, key
	}
}

func ratio(saved, in int) float64 {
	if in <= 0 {
		return 0
	}
	return float64(saved) / float64(in) * 100
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

// HumanizeMS renders a per-command duration compactly (5ms, 1.9s, 27.8s).
func HumanizeMS(ms int64) string {
	if ms < 1000 {
		return strconv.FormatInt(ms, 10) + "ms"
	}
	return trimZero(float64(ms)/1000) + "s"
}

// HumanizeDuration renders a total duration (e.g. 13h 8m, 5m 3s, 42s).
func HumanizeDuration(ms int64) string {
	sec := ms / 1000
	switch {
	case sec >= 3600:
		return strconv.FormatInt(sec/3600, 10) + "h " + strconv.FormatInt((sec%3600)/60, 10) + "m"
	case sec >= 60:
		return strconv.FormatInt(sec/60, 10) + "m " + strconv.FormatInt(sec%60, 10) + "s"
	default:
		return strconv.FormatInt(sec, 10) + "s"
	}
}

func trimZero(f float64) string {
	s := strconv.FormatFloat(f, 'f', 1, 64)
	return strings.TrimSuffix(s, ".0")
}
