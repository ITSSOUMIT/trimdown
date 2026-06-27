package meta

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/itssoumit/trimdown/internal/store"
)

// resetSavings erases the usage log after an explicit y/yes confirmation,
// returning all savings totals to zero.
func resetSavings() int {
	fmt.Print("This erases all recorded savings and resets totals to 0. Continue? [y/N] ")
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	switch strings.TrimSpace(line) {
	case "y", "Y", "yes", "YES", "Yes":
	default:
		fmt.Println("aborted — nothing was changed")
		return 0
	}
	s, err := store.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "trimdown savings:", err)
		return 1
	}
	if err := s.Reset(); err != nil {
		fmt.Fprintln(os.Stderr, "trimdown savings:", err)
		return 1
	}
	fmt.Println("✓ savings reset to 0")
	return 0
}

// Savings implements `trimdown savings [--json] [--all] [--since DUR] [-p]`.
func Savings(args []string) int {
	var o store.AggregateOpts
	asJSON, all := false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--reset":
			return resetSavings()
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
		emit(&b)
		return 0
	}
	writeSavers(&b, sum)
	writeFailures(&b, sum)
	if all {
		writeBreakdown(&b, "Daily", "Date", daily)
		writeBreakdown(&b, "Weekly", "Week", weekly)
		writeBreakdown(&b, "Monthly", "Month", store.AggregateBuckets(events, o, store.Monthly))
	}
	emit(&b)
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
	fmt.Fprintf(b, "  Coverage    %d/%d commands filtered\n", s.Filtered, s.Commands)
	if raw > 0 {
		fmt.Fprintf(b, "              %d ran raw", s.Passthrough)
		if s.ParseFail > 0 {
			fmt.Fprintf(b, ", %d filter errors", s.ParseFail)
		}
		fmt.Fprintln(b)
	}

	// Effectiveness: how hard we compressed what we did filter, with the
	// savings rate broken out onto its own line.
	fmt.Fprintf(b, "  Compressed  %s → %s   saved %s\n",
		store.Humanize(s.TotalIn), store.Humanize(s.TotalOut), store.Humanize(s.TotalSaved))
	fmt.Fprintf(b, "  Savings     %s  %s\n", bar(s.Pct/100, 16), colorize(fmt.Sprintf("%.0f%%", s.Pct), s.Pct))

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
		rate := colorize(fmt.Sprintf("%4.0f%%", c.Pct), c.Pct)
		fmt.Fprintf(b, "  %3d  %-*s  %6d  %8s  %s  %7s  %s\n",
			i+1, cmdW, truncate(c.Command, cmdW), c.Count,
			store.Humanize(c.Saved), rate, store.HumanizeMS(c.AvgMS), bar(frac, 12))
	}
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

// baseWhite (#FFFFF5) is the foreground for all non-accent text; ansiReset
// clears it at the very end of the output.
const (
	baseWhite = "\x1b[38;2;255;255;245m"
	ansiReset = "\x1b[0m"
)

// colorize wraps text in a 24-bit truecolor chosen by the percentage it
// represents: green (#5FAD56) > 80, blue (#1CA9C9) 40–80, red (#FB1616) < 40,
// then returns to baseWhite. No-ops unless stdout is a terminal (and NO_COLOR
// is unset), so piped/agent output stays clean.
func colorize(text string, pct float64) string {
	if !colorEnabled() {
		return text
	}
	rgb := "251;22;22" // red #FB1616
	switch {
	case pct > 80:
		rgb = "95;173;86" // green #5FAD56
	case pct >= 40:
		rgb = "28;169;201" // blue #1CA9C9
	}
	return "\x1b[38;2;" + rgb + "m" + text + baseWhite
}

// emit prints the built report, wrapping it in the off-white base color when
// color is enabled so every non-accent character renders as #FFFFF5.
func emit(b *strings.Builder) {
	out := b.String()
	if colorEnabled() {
		out = baseWhite + out + ansiReset
	}
	fmt.Print(out)
}

func colorEnabled() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, err := os.Stdout.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
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
