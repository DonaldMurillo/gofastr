// Package telemetrycmd is `gofastr telemetry`, which reads the files battery/telemetry
// writes straight from the directory, so it works when the app is down.
package telemetrycmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
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
	"unicode/utf16"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/battery/telemetry"
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
      --grep <text>    only lines holding every word of text, in any case
  requests             kept requests (traces), newest first
      --failed         only requests that failed
      --slow <dur>     only requests that took at least this long (e.g. 500ms)
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

var errUsage = errors.New("usage")

// RunTelemetry runs the command and exits non-zero when it fails.
func RunTelemetry(args []string) {
	err := Run(os.Stdout, args, time.Now())
	if err == nil {
		return
	}
	if errors.Is(err, flag.ErrHelp) {
		fmt.Print(usage)
		cli.OsExit(0)
		return
	}
	cli.Fail("%s", textsafe.ScrubTerminal(err.Error()))
	if errors.Is(err, errUsage) {
		fmt.Fprint(os.Stderr, usage)
		cli.OsExit(2)
		return
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
	positional, err := cli.ParsePositionals(fs, rest)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return flag.ErrHelp
		}
		return fmt.Errorf("%w: %v", errUsage, err)
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
	return telemetry.DefaultDir(o.app)
}

func clean(s string) string {
	return strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(textsafe.ScrubTerminal(s))
}

func jsonLine(line []byte) []byte {
	if !json.Valid(line) {
		quoted, err := json.Marshal(string(line))
		if err != nil {
			return []byte(`""`)
		}
		line = quoted
	}
	risky := func(r rune) bool { return r >= 0x20 && textsafe.IsUnsafe(r) }
	if utf8.Valid(line) && !bytes.ContainsFunc(line, risky) {
		return line
	}
	out := make([]byte, 0, len(line)+16)
	for rest := line; len(rest) > 0; {
		r, size := utf8.DecodeRune(rest)
		raw := rest[:size]
		rest = rest[size:]
		if r == utf8.RuneError && size == 1 {
			out = append(out, `\ufffd`...)
			continue
		}
		if !risky(r) {
			out = append(out, raw...)
			continue
		}
		if r1, r2 := utf16.EncodeRune(r); r1 != utf8.RuneError {
			out = fmt.Appendf(out, "\\u%04x\\u%04x", r1, r2)
			continue
		}
		out = fmt.Appendf(out, "\\u%04x", r)
	}
	return out
}

type jsonWriter struct{ w io.Writer }

func (j jsonWriter) Write(p []byte) (int, error) {
	if _, err := j.w.Write(jsonLine(p)); err != nil {
		return 0, err
	}
	return len(p), nil
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
			fmt.Fprintf(w, "%s\n", jsonLine(e.Line))
			continue
		}
		fmt.Fprintf(w, "%s  %-5s  %s\n", stamp(e.Time), e.Level, clean(describe(e.Line)))
	}
	if len(lines) == 0 && !o.asJSON {
		fmt.Fprintln(w, "No log lines match.")
	}
	return nil
}

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
		enc := json.NewEncoder(jsonWriter{w})
		for _, s := range sums {
			if err := enc.Encode(itel.RequestRow(s)); err != nil {
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
		enc := json.NewEncoder(jsonWriter{w})
		for _, is := range issues {
			if err := enc.Encode(itel.IssueRow(is)); err != nil {
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
		fmt.Fprintf(w, "%d more failures fit none of the first %d issues and are not listed.\n", leftOut, itel.MaxIssues)
	}
	return nil
}

func (o options) stats(w io.Writer, store *itel.Store, now time.Time) error {
	const steps = 1000
	step := (o.since/steps + time.Minute - 1).Truncate(time.Minute)
	got, err := store.Stats(itel.StatsQuery{From: now.Add(-o.since), To: now, Step: max(step, time.Minute), Route: o.route})
	if err != nil {
		return err
	}
	var latest itel.HostReading
	if host, err := store.Host(now.Add(-o.since), now, max(step, time.Minute)); err == nil {
		latest = host.Latest
	}
	if o.asJSON {
		enc := json.NewEncoder(jsonWriter{w})
		job := false
		line := func(route string, total bool, c itel.StatsCount) error {
			return enc.Encode(map[string]any{
				"route": route, "total": total, "job": job, "requests": c.N, "failed": c.Failed, "slow": c.Slow,
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
		job = true
		for _, j := range got.Jobs[:min(len(got.Jobs), o.limit)] {
			if err := line(j.Route, false, j.StatsCount); err != nil {
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
	pct := func(n, of int64) string { return strconv.FormatFloat(float64(n)*100/float64(of), 'f', 1, 64) + "%" }
	if got.Total.N == 0 {
		fmt.Fprintln(w, "No requests counted.")
		if text := machineLine(latest); text != "" {
			fmt.Fprintf(w, "\n%s\n", text)
		}
		return o.jobs(w, got.Jobs, pct)
	}
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
	return o.jobs(w, got.Jobs, pct)
}

func (o options) jobs(w io.Writer, jobs []itel.RouteStats, pct func(n, of int64) string) error {
	if len(jobs) == 0 {
		return nil
	}
	fmt.Fprintln(w, "\nBackground work")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RUNS\tFAILED\tHALF WITHIN\t95% WITHIN\tWORK")
	shown := jobs[:min(len(jobs), o.limit)]
	for _, j := range shown {
		fmt.Fprintf(tw, "%d\t%d (%s)\t%s\t%s\t%s\n", j.N, j.Failed, pct(j.Failed, j.N), within(j.P50), within(j.P95), clean(j.Route))
	}
	if err := tw.Flush(); err != nil {
		return err
	}
	if left := len(jobs) - len(shown); left > 0 {
		fmt.Fprintf(w, "Quieter work not listed (raise --limit): %d\n", left)
	}
	return nil
}

func size(n int64) string {
	v, unit := itel.ByteSize(n)
	if unit == "GB" {
		return strconv.FormatFloat(v, 'f', 1, 64) + " GB"
	}
	return strconv.FormatFloat(v, 'f', 0, 64) + " MB"
}

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
	held := r.HeldBytes()
	parts = append(parts, fmt.Sprintf("this app holds %s in %d goroutines", size(held), r.Goroutines))
	return "Machine at " + stamp(r.Time) + ": " + strings.Join(parts, ", ") + "."
}

func span(d time.Duration) string {
	s := d.String()
	s = strings.TrimSuffix(s, "0s")
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

func within(d time.Duration) string {
	d = itel.RoundDuration(d)
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
	from, to := tr.LogWindow()
	logs, err := store.LogsForTrace(id, from, to)
	if err != nil {
		return err
	}
	if o.asJSON {
		fmt.Fprintf(w, "%s\n", jsonLine(line))
		for _, e := range logs {
			fmt.Fprintf(w, "%s\n", jsonLine(e.Line))
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

func steps(tr itel.Trace) []step {
	sts := itel.Steps(tr)
	out := make([]step, 0, len(sts))
	for _, s := range sts {
		name := s.Name
		if s.HasDetail {
			name += "  " + s.Detail
		}
		out = append(out, step{name: name, start: s.Offset, dur: s.Dur, depth: s.Depth, failed: s.Error})
	}
	return out
}
