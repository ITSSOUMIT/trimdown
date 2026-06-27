package meta

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/itssoumit/trimdown/internal/store"
)

// Savings implements `trimdown savings [--json] [--all] [--since DUR] [-p]`.
func Savings(args []string) int {
	var o store.AggregateOpts
	asJSON, all := false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--json":
			asJSON = true
		case "--all", "-a":
			all = true
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
	sum.Scope = "global"
	if o.Project != "" {
		sum.Scope = "project"
	}
	daily := store.AggregateBuckets(events, o, store.Daily)
	weekly := store.AggregateBuckets(events, o, store.Weekly)

	if asJSON {
		out := struct {
			store.Summary
			Daily   []store.Bucket `json:"daily,omitempty"`
			Weekly  []store.Bucket `json:"weekly,omitempty"`
			Monthly []store.Bucket `json:"monthly,omitempty"`
		}{Summary: sum}
		if all {
			out.Daily = daily
			out.Weekly = weekly
			out.Monthly = store.AggregateBuckets(events, o, store.Monthly)
		}
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return 0
	}

	var b strings.Builder
	writeHeader(&b, sum, daily, weekly)
	if sum.Commands == 0 {
		fmt.Print(b.String())
		return 0
	}
	writeSavers(&b, sum)
	writeOpportunities(&b, sum)
	writeFailures(&b, sum)
	if all {
		writeBreakdown(&b, "Daily", "Date", daily)
		writeBreakdown(&b, "Weekly", "Week", weekly)
		writeBreakdown(&b, "Monthly", "Month", store.AggregateBuckets(events, o, store.Monthly))
	}
	fmt.Print(b.String())
	return 0
}

func writeHeader(b *strings.Builder, s store.Summary, daily, weekly []store.Bucket) {
	title := "trimdown savings — " + s.Scope
	if s.Since != "" {
		title += " (last " + s.Since + ")"
	}
	fmt.Fprintln(b, title)
	fmt.Fprintln(b, strings.Repeat("─", len([]rune(title))))
	if s.Commands == 0 {
		fmt.Fprintln(b, "  no commands recorded yet")
		return
	}

	// Coverage: of everything that ran through trimdown, how much we intercepted.
	raw := s.Passthrough + s.ParseFail
	fmt.Fprintf(b, "  Coverage    %d/%d commands filtered  %s  %.0f%%\n",
		s.Filtered, s.Commands, bar(s.Coverage/100, 16), s.Coverage)
	if raw > 0 {
		fmt.Fprintf(b, "              %d ran raw", s.Passthrough)
		if s.ParseFail > 0 {
			fmt.Fprintf(b, ", %d filter errors", s.ParseFail)
		}
		fmt.Fprintln(b)
	}

	// Effectiveness: how hard we compressed what we did filter.
	fmt.Fprintf(b, "  Compressed  %s → %s   saved %s  (%.0f%%)\n",
		store.Humanize(s.TotalIn), store.Humanize(s.TotalOut), store.Humanize(s.TotalSaved), s.Pct)

	// Value: tokens are abstract — dollars and context windows are not.
	dollars := float64(s.TotalSaved) / 1_000_000 * pricePerMTok()
	ctx := contextTokens()
	windows := float64(s.TotalSaved) / float64(ctx)
	fmt.Fprintf(b, "  Value       ≈ $%.2f saved (@$%g/M)  ·  %s a %s-token context\n",
		dollars, pricePerMTok(), fmtWindows(windows), store.Humanize(ctx))

	// Direction: a sparkline of daily saved + week-over-week effectiveness delta.
	if line := trendLine(daily, weekly); line != "" {
		fmt.Fprintf(b, "  Trend       %s\n", line)
	}
}

func writeSavers(b *strings.Builder, s store.Summary) {
	if len(s.Savers) == 0 {
		return
	}
	cmdW := commandWidth(s.Savers, "Command")
	maxSaved := 0
	for _, c := range s.Savers {
		if c.Saved > maxSaved {
			maxSaved = c.Saved
		}
	}
	fmt.Fprintf(b, "\nTop savers — where trimdown earns its keep\n")
	fmt.Fprintf(b, "  %3s  %-*s  %6s  %8s  %5s  %7s  %s\n",
		"#", cmdW, "Command", "Count", "Saved", "Rate", "Time", "Impact")
	for i, c := range s.Savers {
		frac := 0.0
		if maxSaved > 0 {
			frac = float64(c.Saved) / float64(maxSaved)
		}
		fmt.Fprintf(b, "  %3d  %-*s  %6d  %8s  %4.0f%%  %7s  %s\n",
			i+1, cmdW, truncate(c.Command, cmdW), c.Count,
			store.Humanize(c.Saved), c.Pct, store.HumanizeMS(c.AvgMS), bar(frac, 12))
	}
}

func writeOpportunities(b *strings.Builder, s store.Summary) {
	if len(s.Opportunities) == 0 {
		return
	}
	cmdW := commandWidth(s.Opportunities, "Command")
	fmt.Fprintf(b, "\nUntapped — ran raw, no filter yet (%s tokens total)\n", store.Humanize(s.OppTokens))
	for _, c := range s.Opportunities {
		fmt.Fprintf(b, "  %-*s  %6d×  %8s tokens\n",
			cmdW, truncate(c.Command, cmdW), c.Count, store.Humanize(c.In))
	}
	fmt.Fprintln(b, "  → these are the best candidates for a new filter.")
}

func writeFailures(b *strings.Builder, s store.Summary) {
	if len(s.Failures) == 0 {
		return
	}
	cmdW := commandWidth(s.Failures, "Command")
	fmt.Fprintf(b, "\n⚠ Filters that fell back to raw (parse errors — likely a bug)\n")
	for _, c := range s.Failures {
		fmt.Fprintf(b, "  %-*s  %6d×  %8s tokens unfiltered\n",
			cmdW, truncate(c.Command, cmdW), c.Count, store.Humanize(c.In))
	}
}

func writeBreakdown(b *strings.Builder, title, labelHead string, buckets []store.Bucket) {
	if len(buckets) == 0 {
		return
	}
	labelW := len(labelHead)
	for _, bk := range buckets {
		if n := len([]rune(bk.Label)); n > labelW {
			labelW = n
		}
	}

	fmt.Fprintf(b, "\n%s breakdown (%d)\n", title, len(buckets))
	fmt.Fprintf(b, "  %-*s  %6s  %8s  %8s  %5s  %7s\n",
		labelW, labelHead, "Cmds", "Saved", "Of", "Rate", "Time")

	var tCmds, tIn, tSaved int
	var tMS int64
	for _, bk := range buckets {
		avg := int64(0)
		if bk.Cmds > 0 {
			avg = bk.MS / int64(bk.Cmds)
		}
		fmt.Fprintf(b, "  %-*s  %6d  %8s  %8s  %4.0f%%  %7s\n",
			labelW, bk.Label, bk.Cmds, store.Humanize(bk.Saved), store.Humanize(bk.In),
			bk.Pct, store.HumanizeMS(avg))
		tCmds += bk.Cmds
		tIn += bk.In
		tSaved += bk.Saved
		tMS += bk.MS
	}
	avg := int64(0)
	if tCmds > 0 {
		avg = tMS / int64(tCmds)
	}
	fmt.Fprintf(b, "  %-*s  %6d  %8s  %8s  %4.0f%%  %7s\n",
		labelW, "TOTAL", tCmds, store.Humanize(tSaved), store.Humanize(tIn),
		pctOf(tSaved, tIn), store.HumanizeMS(avg))
}

// --- helpers ---

func commandWidth(cmds []store.CmdStat, head string) int {
	w := len(head)
	for _, c := range cmds {
		if n := len(c.Command); n > w {
			w = n
		}
	}
	if w > 26 {
		w = 26
	}
	return w
}

// trendLine renders a sparkline of recent daily saved tokens plus a
// week-over-week effectiveness delta.
func trendLine(daily, weekly []store.Bucket) string {
	if len(daily) == 0 {
		return ""
	}
	if len(daily) > 14 {
		daily = daily[len(daily)-14:]
	}
	vals := make([]int, len(daily))
	for i, d := range daily {
		vals[i] = d.Saved
	}
	line := sparkline(vals)

	if n := len(weekly); n >= 2 {
		cur, prev := weekly[n-1].Pct, weekly[n-2].Pct
		delta := cur - prev
		arrow, sign := "≈", ""
		switch {
		case delta >= 0.5:
			arrow, sign = "▲", "+"
		case delta <= -0.5:
			arrow = "▼"
		}
		line += fmt.Sprintf("   %.0f%% this week  (%s %s%.0f vs prior)", cur, arrow, sign, delta)
	}
	return line
}

func sparkline(vals []int) string {
	if len(vals) == 0 {
		return ""
	}
	max := 0
	for _, v := range vals {
		if v > max {
			max = v
		}
	}
	chars := []rune("▁▂▃▄▅▆▇█")
	var b strings.Builder
	for _, v := range vals {
		idx := 0
		if max > 0 {
			idx = v * (len(chars) - 1) / max
		}
		b.WriteRune(chars[idx])
	}
	return b.String()
}

// bar renders a proportional block bar of the given width.
func bar(frac float64, width int) string {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	n := int(frac*float64(width) + 0.5)
	return strings.Repeat("█", n) + strings.Repeat("░", width-n)
}

func fmtWindows(w float64) string {
	if w >= 10 {
		return fmt.Sprintf("%.0f×", w)
	}
	return fmt.Sprintf("%.1f×", w)
}

func pctOf(saved, in int) float64 {
	if in <= 0 {
		return 0
	}
	return float64(saved) / float64(in) * 100
}

func truncate(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}

// pricePerMTok is the assumed $/1M tokens for the value estimate (configurable).
func pricePerMTok() float64 {
	if v := os.Getenv("TRIMDOWN_PRICE_PER_MTOK"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f >= 0 {
			return f
		}
	}
	return 3.0
}

// contextTokens is the assumed model context size for the "windows freed" view.
func contextTokens() int {
	if v := os.Getenv("TRIMDOWN_CONTEXT_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return 200_000
}
