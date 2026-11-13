// Package telemetrycmd is `gofastr telemetry`: read the log and trace
// files battery/telemetry writes, from a shell. It opens the directory
// itself, so it works over SSH and when the app is down.
package telemetrycmd

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/cli"
	"github.com/DonaldMurillo/gofastr/internal/jsonexact"
	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
)

const usage = `Usage: gofastr telemetry <stats|logs|requests|issues|request> [flags]

Read the files battery/telemetry writes. Name the directory with --dir,
or the app with --app to read its default one
($XDG_STATE_HOME/<app>/telemetry or ~/.local/state/<app>/telemetry).

  stats                every request counted: how many, how many failed and
                       were slow, and how long they took, in all and by route
      --route <route>  only this route
  logs                 log lines, newest first
      --level <l>      minimum level: debug, info, warn, error
      --grep <text>    only lines containing text (case-sensitive)
  requests             kept requests (traces), newest first
      --failed         only requests that failed
      --slow <dur>     only requests that took at least this long (500ms)
      --route <route>  only this route, e.g. "GET /orders/{id}"
      --issue <id>     only the failures of one issue, from "issues"
  issues               failed requests grouped by what went wrong: how many
                       times, first and last seen, and the ID to list them by
  request <trace-id>   one request: its steps and its log lines

Flags for stats, logs, requests and issues:
      --since <dur>    how far back to read (default 24h; 0 for everything kept,
                       which stats does not take)
      --limit <n>      most lines to print (default 50)
      --json           print the lines as stored, one JSON object each
`

// errUsage marks a mistake in the command line: Run's caller prints the
// usage after it.
var errUsage = errors.New("usage")

// RunTelemetry runs the command and exits non-zero when it fails.
func RunTelemetry(args []string) {
	err := Run(os.Stdout, args, time.Now())
	if err == nil {
		return
	}
	if !errors.Is(err, flag.ErrHelp) {
		cli.Fail("%s", cli.ScrubTerminalOutput(err.Error()))
	}
	if errors.Is(err, errUsage) || errors.Is(err, flag.ErrHelp) {
		fmt.Print(usage)
	}
	if errors.Is(err, flag.ErrHelp) {
		cli.OsExit(0)
	}
	cli.OsExit(1)
}

type options struct {
	dir, app    string
	since       time.Duration
	limit       int
	asJSON      bool
	level, grep string
	failed      bool
	slow        time.Duration
	route       string
	issue       string
}

// Run executes one `gofastr telemetry` command line, writing to w.
func Run(w io.Writer, args []string, now time.Time) error {
	if len(args) == 0 {
		return fmt.Errorf("%w: name a subcommand", errUsage)
	}
	sub, rest := args[0], args[1:]
	if sub == "--help" || sub == "-h" || sub == "help" {
		return flag.ErrHelp
	}
	var o options
	fs := flag.NewFlagSet("telemetry "+sub, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&o.dir, "dir", "", "")
	fs.StringVar(&o.app, "app", "", "")
	fs.DurationVar(&o.since, "since", 24*time.Hour, "")
	fs.IntVar(&o.limit, "limit", 50, "")
	fs.BoolVar(&o.asJSON, "json", false, "")
	switch sub {
	case "logs":
		fs.StringVar(&o.level, "level", "", "")
		fs.StringVar(&o.grep, "grep", "", "")
	case "requests":
		fs.BoolVar(&o.failed, "failed", false, "")
		fs.DurationVar(&o.slow, "slow", 0, "")
		fs.StringVar(&o.route, "route", "", "")
		fs.StringVar(&o.issue, "issue", "", "")
	case "stats":
		fs.StringVar(&o.route, "route", "", "")
	case "issues", "request":
	default:
		return fmt.Errorf("%w: unknown subcommand %q", errUsage, sub)
	}
	// A trace ID may come before or after the flags.
	var positional []string
	for len(rest) > 0 {
		if err := fs.Parse(rest); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return flag.ErrHelp
			}
			return fmt.Errorf("%w: %v", errUsage, err)
		}
		rest = fs.Args()
		if len(rest) > 0 {
			positional = append(positional, rest[0])
			rest = rest[1:]
		}
	}
	if o.since < 0 || o.slow < 0 {
		return fmt.Errorf("%w: --since and --slow take a duration of zero or more", errUsage)
	}
	if o.limit < 1 || o.limit > 100000 {
		return fmt.Errorf("%w: --limit takes a number from 1 to 100000", errUsage)
	}
	dir, err := o.directory()
	if err != nil {
		return err
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		return fmt.Errorf("no telemetry directory at %s", dir)
	}
	store, err := itel.Open(itel.Config{Dir: dir})
	if err != nil {
		return err
	}
	defer store.Close()

	var from time.Time
	if o.since > 0 {
		from = now.Add(-o.since)
	}
	switch sub {
	case "stats":
		if len(positional) > 0 {
			return fmt.Errorf("%w: stats takes no argument", errUsage)
		}
		if o.since < time.Minute {
			return fmt.Errorf("%w: stats needs --since of a minute or more", errUsage)
		}
		return o.stats(w, store, now)
	case "logs":
		if len(positional) > 0 {
			return fmt.Errorf("%w: logs takes no argument (use --grep)", errUsage)
		}
		return o.logs(w, store, from)
	case "requests":
		if len(positional) > 0 {
			return fmt.Errorf("%w: requests takes no argument (use `request <trace-id>`)", errUsage)
		}
		return o.requests(w, store, from)
	case "issues":
		if len(positional) > 0 {
			return fmt.Errorf("%w: issues takes no argument", errUsage)
		}
		return o.issues(w, store, from)
	default:
		if len(positional) != 1 {
			return fmt.Errorf("%w: request takes one trace ID", errUsage)
		}
		return o.request(w, store, strings.ToLower(strings.TrimSpace(positional[0])))
	}
}

// directory resolves --dir, or --app's default directory, the one
// battery/telemetry uses when its Config.Dir is empty.
func (o options) directory() (string, error) {
	switch {
	case o.dir != "" && o.app != "":
		return "", fmt.Errorf("%w: give --dir or --app, not both", errUsage)
	case o.dir != "":
		return o.dir, nil
	case o.app == "":
		return "", fmt.Errorf("%w: give --dir <directory> or --app <name>", errUsage)
	case o.app != filepath.Base(o.app) || o.app == "." || o.app == "..":
		return "", fmt.Errorf("%w: --app takes an app name, not a path", errUsage)
	}
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, o.app, "telemetry"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", o.app, "telemetry"), nil
}

// clean makes text from a file safe to print: a log message is whatever
// a caller sent, and a terminal obeys the control bytes in it.
// A line break goes too: one entry is one line, and a message must not
// be able to start a second that looks like another entry.
func clean(s string) string {
	return strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(cli.ScrubTerminalOutput(s))
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05.000Z") }

func (o options) logs(w io.Writer, store *itel.Store, from time.Time) error {
	q := itel.LogQuery{Text: o.grep, From: from, MinLevel: slog.LevelDebug - 100}
	if o.level != "" {
		if err := q.MinLevel.UnmarshalText([]byte(strings.ToUpper(o.level))); err != nil {
			return fmt.Errorf("%w: --level takes debug, info, warn or error", errUsage)
		}
	}
	lines, err := store.Logs(q, o.limit)
	if err != nil {
		return err
	}
	for _, e := range lines {
		if o.asJSON {
			fmt.Fprintf(w, "%s\n", e.Line)
			continue
		}
		fmt.Fprintf(w, "%s  %-5s  %s\n", stamp(e.Time), e.Level, clean(describe(e.Line)))
	}
	if len(lines) == 0 && !o.asJSON {
		fmt.Fprintln(w, "No log lines match.")
	}
	return nil
}

// describe writes a log line as its message followed by its other
// attributes, "key=value" in key order, the trace ID last.
func describe(line []byte) string {
	var m map[string]any
	if err := jsonexact.Unmarshal(line, &m); err != nil {
		return string(line)
	}
	var sb strings.Builder
	msg, _ := m["msg"].(string)
	sb.WriteString(msg)
	keys := slices.Sorted(maps.Keys(m))
	for _, last := range []bool{false, true} {
		for _, k := range keys {
			switch k {
			case "time", "level", "msg", "span_id":
				continue
			}
			if (k == "trace_id") != last {
				continue
			}
			var val string
			switch v := m[k].(type) {
			case nil:
				continue
			case string:
				val = v
			case map[string]any, []any:
				b, err := json.Marshal(v)
				if err != nil {
					continue
				}
				val = string(b)
			default:
				val = fmt.Sprint(v)
			}
			if val == "" {
				continue
			}
			sb.WriteString("  " + k + "=" + val)
		}
	}
	return sb.String()
}

func (o options) requests(w io.Writer, store *itel.Store, from time.Time) error {
	sums, err := store.Traces(itel.TraceQuery{Route: o.route, Failed: o.failed, MinDur: o.slow, Issue: o.issue, From: from}, o.limit)
	if err != nil {
		return err
	}
	if o.asJSON {
		enc := json.NewEncoder(w)
		for _, s := range sums {
			if err := enc.Encode(map[string]any{
				"time": s.Time.UTC().Format(time.RFC3339Nano), "trace_id": s.TraceID, "route": s.Route, "path": s.Path,
				"status": s.Status, "dur_us": s.Dur.Microseconds(), "error": s.Error, "failed_steps": s.FailedSteps,
			}); err != nil {
				return err
			}
		}
		return nil
	}
	if len(sums) == 0 {
		fmt.Fprintln(w, "No requests match.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TIME\tSTATUS\tTOOK\tROUTE\tTRACE")
	for _, s := range sums {
		status := "-"
		if s.Status > 0 {
			status = fmt.Sprint(s.Status)
		}
		if s.Error {
			status += " failed"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", stamp(s.Time), status, took(s.Dur), clean(s.Route), s.TraceID)
	}
	return tw.Flush()
}

func (o options) issues(w io.Writer, store *itel.Store, from time.Time) error {
	issues, leftOut, err := store.Issues(from, time.Time{}, o.limit)
	if err != nil {
		return err
	}
	if o.asJSON {
		enc := json.NewEncoder(w)
		for _, is := range issues {
			if err := enc.Encode(map[string]any{
				"id": is.ID, "route": is.Route, "status": is.Status, "error": is.Err, "example": is.Example, "count": is.Count,
				"first": is.First.UTC().Format(time.RFC3339Nano), "last": is.Last.UTC().Format(time.RFC3339Nano), "last_trace_id": is.LastTraceID,
			}); err != nil {
				return err
			}
		}
		return nil
	}
	if len(issues) == 0 {
		fmt.Fprintln(w, "Nothing failed.")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "TIMES\tLAST SEEN\tSTATUS\tROUTE\tWHAT WENT WRONG\tISSUE")
	for _, is := range issues {
		status, what := "-", is.Err
		if is.Status > 0 {
			status = fmt.Sprint(is.Status)
		}
		if what == "" {
			what = "(no error text)"
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\t%s\n", is.Count, stamp(is.Last), status, clean(is.Route), clean(what), is.ID)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if leftOut > 0 {
		fmt.Fprintf(w, "%d more failures fit none of the first 2000 issues and are not listed.\n", leftOut)
	}
	return nil
}

// stats prints the counts of every request since o.since ago: the
// total, then each route, busiest first.
func (o options) stats(w io.Writer, store *itel.Store, now time.Time) error {
	// One answer for the whole window: the fewest steps that cover it.
	// The window starts on a step, so it may reach back a little
	// further than asked, by less than a thousandth of its length.
	const steps = 1000
	step := (o.since/steps + time.Minute - 1).Truncate(time.Minute)
	got, err := store.Stats(itel.StatsQuery{From: now.Add(-o.since), To: now, Step: max(step, time.Minute), Route: o.route})
	if err != nil {
		return err
	}
	// The machine's latest reading in the window, when there is one. A
	// directory that cannot give it still gives the counts.
	var latest itel.HostReading
	if host, err := store.Host(now.Add(-o.since), now, max(step, time.Minute)); err == nil {
		latest = host.Latest
	}
	if o.asJSON {
		enc := json.NewEncoder(w)
		line := func(route string, total bool, c itel.StatsCount) error {
			return enc.Encode(map[string]any{
				"route": route, "total": total, "requests": c.N, "failed": c.Failed, "slow": c.Slow,
				"p50_us": c.P50.Microseconds(), "p95_us": c.P95.Microseconds(), "mean_us": c.Mean.Microseconds(),
			})
		}
		if err := line("", true, got.Total); err != nil {
			return err
		}
		for _, r := range got.Routes[:min(len(got.Routes), o.limit)] {
			if err := line(r.Route, false, r.StatsCount); err != nil {
				return err
			}
		}
		if latest.Time.IsZero() {
			return nil
		}
		m := map[string]any{"machine": true, "time": latest.Time.UTC().Format(time.RFC3339),
			"mem_used": latest.MemUsed, "mem_total": latest.MemTotal, "disk_used": latest.DiskUsed, "disk_total": latest.DiskTotal,
			"goroutines": latest.Goroutines, "heap": latest.HeapBytes, "rss": latest.RSSBytes}
		if latest.CPU >= 0 {
			m["cpu"] = latest.CPU
		}
		return enc.Encode(m)
	}
	if got.Total.N == 0 {
		fmt.Fprintln(w, "No requests counted.")
		if text := machineLine(latest); text != "" {
			fmt.Fprintf(w, "\n%s\n", text)
		}
		return nil
	}
	pct := func(n, of int64) string { return strconv.FormatFloat(float64(n)*100/float64(of), 'f', 1, 64) + "%" }
	t := got.Total
	fmt.Fprintf(w, "%d requests in the last %s: %d failed (%s), %d slow (%s). Half finished within %s, 95%% within %s.\n\n",
		t.N, span(o.since), t.Failed, pct(t.Failed, t.N), t.Slow, pct(t.Slow, t.N), within(t.P50), within(t.P95))
	if text := machineLine(latest); text != "" {
		fmt.Fprintf(w, "%s\n\n", text)
	}
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "REQUESTS\tFAILED\tSLOW\tHALF WITHIN\t95% WITHIN\tROUTE")
	shown := got.Routes[:min(len(got.Routes), o.limit)]
	for _, r := range shown {
		fmt.Fprintf(tw, "%d\t%d (%s)\t%d (%s)\t%s\t%s\t%s\n", r.N, r.Failed, pct(r.Failed, r.N), r.Slow, pct(r.Slow, r.N), within(r.P50), within(r.P95), clean(r.Route))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if left := len(got.Routes) - len(shown); left > 0 {
		fmt.Fprintf(w, "Quieter routes not listed (raise --limit): %d\n", left)
	}
	return nil
}

// size writes a byte count: whole megabytes under a gigabyte, gigabytes
// to one decimal over it.
func size(n int64) string {
	const mb, gb = 1 << 20, 1 << 30
	if n < gb {
		return strconv.FormatInt((n+mb/2)/mb, 10) + " MB"
	}
	return strconv.FormatFloat(float64(n)/gb, 'f', 1, 64) + " GB"
}

// machineLine says the machine's latest reading in a sentence, leaving
// out what the platform did not give. A zero reading says nothing.
func machineLine(r itel.HostReading) string {
	if r.Time.IsZero() {
		return ""
	}
	var parts []string
	if r.CPU >= 0 {
		parts = append(parts, fmt.Sprintf("CPU %d%% busy", int(r.CPU*100+0.5)))
	}
	if p := r.MemPercent(); p >= 0 {
		parts = append(parts, fmt.Sprintf("memory %s of %s (%d%%)", size(r.MemUsed), size(r.MemTotal), p))
	}
	if p := r.DiskPercent(); p >= 0 {
		parts = append(parts, fmt.Sprintf("disk %s of %s (%d%%)", size(r.DiskUsed), size(r.DiskTotal), p))
	}
	held := r.RSSBytes
	if held == 0 {
		held = r.HeapBytes
	}
	parts = append(parts, fmt.Sprintf("this app holds %s in %d goroutines", size(held), r.Goroutines))
	return "Machine at " + stamp(r.Time) + ": " + strings.Join(parts, ", ") + "."
}

// span writes a window the way it was asked for: "24h", not "24h0m0s".
func span(d time.Duration) string {
	s := d.String()
	s = strings.TrimSuffix(s, "0s")
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

// within writes a percentile to two digits, which is as much as the
// duration buckets it is read from can say, and marks one at the
// ceiling: the request took that long or longer.
func within(d time.Duration) string {
	unit := time.Duration(1)
	for d/unit >= 100 {
		unit *= 10
	}
	d = (d + unit/2) / unit * unit
	var s string
	switch {
	case d < time.Millisecond:
		s = fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		s = strconv.FormatFloat(float64(d.Microseconds())/1e3, 'f', -1, 64) + "ms"
	default:
		s = strconv.FormatFloat(d.Seconds(), 'f', -1, 64) + "s"
	}
	if d >= itel.StatsCeiling {
		s += "+"
	}
	return s
}

// took writes a duration to three significant places with its unit.
func took(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%dµs", d.Microseconds())
	case d < time.Second:
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1e3)
	default:
		return fmt.Sprintf("%.2fs", d.Seconds())
	}
}

func (o options) request(w io.Writer, store *itel.Store, id string) error {
	line, ok, err := store.TraceByID(id)
	switch {
	case errors.Is(err, itel.ErrBadTraceID):
		return fmt.Errorf("%w: a trace ID is 32 hex digits", errUsage)
	case err != nil:
		return err
	case !ok:
		return errors.New("no request with that trace ID is in the files: it was not kept, its day was removed, or the ID is wrong")
	}
	tr, err := itel.ParseTrace(line)
	if err != nil {
		return err
	}
	logs, err := store.LogsForTrace(id, tr.Time.Add(-tr.Dur-time.Second), tr.Time.Add(time.Second))
	if err != nil {
		return err
	}
	if o.asJSON {
		fmt.Fprintf(w, "%s\n", line)
		for _, e := range logs {
			fmt.Fprintf(w, "%s\n", e.Line)
		}
		return nil
	}
	status := "no status"
	if tr.Status > 0 {
		status = fmt.Sprint(tr.Status)
	}
	if tr.Error {
		status += ", failed"
	}
	fmt.Fprintf(w, "%s  (%s, %s)\n", clean(tr.Route), status, took(tr.Dur))
	if tr.Path != "" {
		fmt.Fprintf(w, "path   %s\n", clean(tr.Path))
	}
	fmt.Fprintf(w, "ended  %s\ntrace  %s\n", stamp(tr.Time), tr.TraceID)
	if tr.Err != "" && (tr.Error || tr.FailedSteps > 0) {
		fmt.Fprintf(w, "error  %s\n", clean(tr.Err))
	}
	fmt.Fprint(w, "\nSteps\n")

	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "  START\tTOOK\tSTEP")
	for _, s := range steps(tr) {
		mark := ""
		if s.failed {
			mark = "  FAILED"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s%s%s\n", took(s.start), took(s.dur), strings.Repeat("  ", s.depth), clean(s.name), mark)
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if tr.Stack != "" {
		// Each line cleaned on its own: the stack's line breaks are its
		// shape, and nothing else in it may move the cursor.
		fmt.Fprintln(w, "\nWhere it failed")
		for line := range strings.SplitSeq(strings.TrimRight(tr.Stack, "\n"), "\n") {
			fmt.Fprintf(w, "  %s\n", clean(strings.ReplaceAll(line, "\t", "    ")))
		}
	}
	fmt.Fprintln(w, "\nLog lines")
	if len(logs) == 0 {
		fmt.Fprintln(w, "  none carry this trace ID")
	}
	for _, e := range logs {
		fmt.Fprintf(w, "  %s  %-5s  %s\n", stamp(e.Time), e.Level, clean(describe(e.Line)))
	}
	return nil
}

type step struct {
	name       string
	start, dur time.Duration
	depth      int
	failed     bool
}

// steps orders a trace's spans as a tree, each under its parent and
// siblings by start. A span whose parent is missing hangs from the top.
func steps(tr itel.Trace) []step {
	known := map[string]bool{}
	for _, sp := range tr.Spans {
		known[sp.SpanID] = true
	}
	children := map[string][]int{}
	var roots []int
	for i, sp := range tr.Spans {
		if sp.ParentID != "" && sp.ParentID != sp.SpanID && known[sp.ParentID] {
			children[sp.ParentID] = append(children[sp.ParentID], i)
		} else {
			roots = append(roots, i)
		}
	}
	out := make([]step, 0, len(tr.Spans))
	seen := make([]bool, len(tr.Spans))
	var walk func(idx []int, depth int)
	walk = func(idx []int, depth int) {
		slices.SortStableFunc(idx, func(a, b int) int { return int(tr.Spans[a].Offset - tr.Spans[b].Offset) })
		for _, i := range idx {
			if seen[i] {
				continue
			}
			seen[i] = true
			sp := tr.Spans[i]
			name := sp.Name
			for _, key := range []string{"db.query.text", "http.url", "url.full"} {
				if j := slices.IndexFunc(sp.Attrs, func(a itel.Attr) bool { return a.Key == key }); j >= 0 {
					name += "  " + sp.Attrs[j].Value
					break
				}
			}
			out = append(out, step{name: name, start: sp.Offset, dur: sp.Dur, depth: depth, failed: sp.Error})
			walk(children[sp.SpanID], depth+1)
		}
	}
	walk(roots, 0)
	return out
}
