package telemetrycmd

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
)

const (
	okID     = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa1"
	failedID = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa2"
)

var now = time.Date(2026, 11, 13, 12, 0, 0, 0, time.UTC)

func seed(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	ms := time.Millisecond
	write := func(kind string, at time.Time, line []byte) {
		f, err := os.OpenFile(filepath.Join(dir, itel.FileName(kind, at)), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if _, err := f.Write(line); err != nil {
			t.Fatal(err)
		}
	}
	for _, tr := range []itel.Trace{
		{Time: now.Add(-50 * time.Hour), TraceID: strings.Repeat("c", 32), Route: "GET /old", Status: 200, Dur: ms},
		{Time: now.Add(-3 * time.Minute), TraceID: okID, Route: "GET /orders/{id}", Path: "/orders/42", Status: 200, Dur: 20 * ms,
			Spans: []itel.Span{
				{SpanID: "01", Name: "HTTP GET GET /orders/{id}", Dur: 20 * ms},
				{SpanID: "02", ParentID: "01", Name: "SELECT", Offset: 5 * ms, Dur: 10 * ms, Error: true, Attrs: []itel.Attr{{Key: "db.query.text", Value: "SELECT * FROM orders"}}},
			}},
		{Time: now.Add(-2 * time.Minute), TraceID: failedID, Route: "POST /orders\x1b[31m", Status: 500, Dur: 900 * ms, Error: true, Err: "card 4411 declined\x1b[2J",
			Stack: "app.charge(...)\n\t/app/pay.go:41 +0x2\x1b[2J\napp.handler(...)\n\t/app/h.go:9 +0x1\n"},
		{Time: now.Add(-90 * time.Second), TraceID: strings.Repeat("d", 32), Route: "POST /orders\x1b[31m", Status: 500, Dur: 5 * ms, Error: true, Err: "card 9020 declined\x1b[2J"},
	} {
		write(itel.KindTraces, tr.Time, itel.AppendTrace(nil, tr))
	}
	for _, l := range []struct {
		at   time.Duration
		body string
	}{
		{-3 * time.Minute, `"level":"INFO","msg":"order.loaded","trace_id":"` + okID + `","order":42`},
		{-2 * time.Minute, `"level":"ERROR","msg":"payment declined\nforged line \u001b[2J","err":"card_declined"`},
		{-1 * time.Minute, `"level":"DEBUG","msg":"tick"`},
	} {
		at := now.Add(l.at)
		write(itel.KindLogs, at, []byte(`{"time":"`+at.Format(time.RFC3339Nano)+`",`+l.body+"}\n"))
	}
	// Every request counted: 200 orders read, 20 placed (5 failed, all
	// slower than anything measures), and 9 of a route two days ago.
	count := func(at time.Time, route string, n, failed int, dur time.Duration) {
		r := itel.NewRollup()
		for i := range n {
			r.Observe(at, route, dur, i < failed, dur >= 500*ms)
		}
		write(itel.KindStats, at.Add(time.Minute), r.Take(at.Add(time.Minute), true))
	}
	count(now.Add(-50*time.Hour), "GET /old", 9, 0, ms)
	count(now.Add(-3*time.Minute), "GET /orders/{id}", 200, 0, 7*ms)
	count(now.Add(-2*time.Minute), "POST /orders\x1b[31m", 20, 5, 30*time.Second)
	// The machine, read two minutes ago and, with a different disk, two
	// days ago.
	for at, disk := range map[time.Time]int64{now.Add(-2 * time.Minute): 23 << 30, now.Add(-50 * time.Hour): 5 << 30} {
		write(itel.KindHost, at, []byte(fmt.Sprintf(`{"time":%q,"cpu":0.3700,"mem_used":2254857830,"mem_total":4294967296,"disk_used":%d,"disk_total":26843545600,"goroutines":42,"heap":31457280,"rss":83886080}`+"\n",
			at.Format(time.RFC3339), disk)))
	}
	return dir
}

func run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var out bytes.Buffer
	err := Run(&out, args, now)
	return out.String(), err
}

func TestLogs(t *testing.T) {
	dir := seed(t)
	out, err := run(t, "logs", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 3 || !strings.Contains(lines[0], "tick") || !strings.Contains(lines[2], "order.loaded  order=42  trace_id="+okID) {
		t.Fatalf("logs, newest first:\n%s", out)
	}
	// A message is what a caller sent: its escape sequence and its line
	// break do not reach the terminal.
	if strings.Contains(out, "\x1b") || !strings.Contains(lines[1], "payment declined forged line") {
		t.Fatalf("a control byte or a line break from a message was printed:\n%q", out)
	}
	for args, want := range map[string]string{
		"--level error":       "payment declined",
		"--grep card_":        "payment declined",
		"--level warn":        "payment declined",
		"--grep order.loaded": "order.loaded",
	} {
		out, err := run(t, append([]string{"logs", "--dir", dir}, strings.Fields(args)...)...)
		if err != nil || strings.Count(out, "\n") != 1 || !strings.Contains(out, want) {
			t.Errorf("logs %s: %v\n%s", args, err, out)
		}
	}
	if out, _ := run(t, "logs", "--dir", dir, "--limit", "1"); strings.Count(out, "\n") != 1 {
		t.Errorf("--limit 1 printed:\n%s", out)
	}
	if out, _ := run(t, "logs", "--dir", dir, "--since", "90s"); !strings.Contains(out, "tick") || strings.Contains(out, "payment") {
		t.Errorf("--since 90s printed:\n%s", out)
	}
	if out, _ := run(t, "logs", "--dir", dir, "--grep", "nothing-like-this"); !strings.Contains(out, "No log lines match") {
		t.Errorf("no match printed:\n%s", out)
	}
	// --json is the line as stored: its escapes are still escapes.
	if out, _ := run(t, "logs", "--dir", dir, "--json", "--level", "error"); strings.Contains(out, "\x1b") || !strings.Contains(out, `\u001b[2J`) {
		t.Errorf("--json printed:\n%q", out)
	}
}

func TestRequests(t *testing.T) {
	dir := seed(t)
	out, err := run(t, "requests", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(out, "\n") != 4 || !strings.Contains(out, "GET /orders/{id}") || !strings.Contains(out, "500 failed") ||
		!strings.Contains(out, "900.0ms") || strings.Contains(out, "GET /old") || strings.Contains(out, "\x1b") {
		t.Fatalf("requests:\n%q", out)
	}
	for args, c := range map[string]struct{ want, not string }{
		"--failed":                 {failedID, okID},
		"--slow 500ms":             {failedID, okID},
		"--route GET_/orders/{id}": {okID, failedID},
		"--since 0":                {"GET /old", "\x00"},
		"--since 0 --limit 1":      {strings.Repeat("d", 32), "GET /old"},
		"--json --failed":          {`"trace_id":"` + failedID + `"`, okID},
	} {
		argv := append([]string{"requests", "--dir", dir}, strings.Fields(args)...)
		for i := range argv {
			argv[i] = strings.ReplaceAll(argv[i], "_", " ")
		}
		out, err := run(t, argv...)
		if err != nil || !strings.Contains(out, c.want) || strings.Contains(out, c.not) {
			t.Errorf("requests %s: %v\n%s", args, err, out)
		}
	}
}

// Failures that differ in a number are one issue with a count, printed
// without the control bytes an error can carry, and its ID lists them.
func TestIssues(t *testing.T) {
	dir := seed(t)
	out, err := run(t, "issues", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[1], "2 ") || !strings.Contains(lines[1], "card N declined") || strings.Contains(out, "\x1b") {
		t.Fatalf("issues:\n%q", out)
	}
	id := lines[1][strings.LastIndex(lines[1], " ")+1:]
	reqs, err := run(t, "requests", "--dir", dir, "--issue", id)
	if err != nil || strings.Count(reqs, "\n") != 3 || !strings.Contains(reqs, failedID) || strings.Contains(reqs, okID) {
		t.Fatalf("requests --issue %s: %v\n%s", id, err, reqs)
	}
	if js, _ := run(t, "issues", "--dir", dir, "--json"); !strings.Contains(js, `"count":2`) || !strings.Contains(js, `"id":"`+id+`"`) {
		t.Errorf("issues --json: %s", js)
	}
	if none, _ := run(t, "issues", "--dir", dir, "--since", "30s"); !strings.Contains(none, "Nothing failed") {
		t.Errorf("issues in a quiet window: %s", none)
	}
	if _, err := run(t, "issues", "--dir", dir, "extra"); !errors.Is(err, errUsage) {
		t.Errorf("issues with an argument: %v", err)
	}
}

func TestStats(t *testing.T) {
	dir := seed(t)
	out, err := run(t, "stats", "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// Blank lines aside: the total, the machine, the header, two routes.
	lines = slices.DeleteFunc(lines, func(l string) bool { return l == "" })
	if len(lines) != 5 || strings.Contains(out, "\x1b") || strings.Contains(out, "GET /old") {
		t.Fatalf("stats:\n%q", out)
	}
	if want := "220 requests in the last 24h: 5 failed (2.3%), 20 slow (9.1%). Half finished within 7.8ms, 95% within 10s+."; lines[0] != want {
		t.Errorf("the total:\n got %q\nwant %q", lines[0], want)
	}
	// Busiest first, each with its own shares.
	if f := strings.Fields(lines[3]); len(f) < 8 || f[0] != "200" || f[1] != "0" || f[5] != "7.5ms" || !strings.HasSuffix(lines[3], "GET /orders/{id}") {
		t.Errorf("the busiest route: %q", lines[3])
	}
	if f := strings.Fields(lines[4]); len(f) < 8 || f[0] != "20" || f[1] != "5" || f[2] != "(25.0%)" || f[6] != "10s+" {
		t.Errorf("the failing route: %q", lines[4])
	}

	if want := "Machine at 2026-11-13 11:58:00.000Z: CPU 37% busy, memory 2.1 GB of 4.0 GB (52%), disk 23.0 GB of 25.0 GB (92%), this app holds 80 MB in 42 goroutines."; lines[1] != want {
		t.Errorf("the machine:\n got %q\nwant %q", lines[1], want)
	}

	week, err := run(t, "stats", "--dir", dir, "--since", "168h", "--limit", "2")
	if err != nil || !strings.Contains(week, "229 requests in the last 168h") || !strings.Contains(week, "Quieter routes not listed (raise --limit): 1") || strings.Contains(week, "GET /old") {
		t.Errorf("stats --since 168h --limit 2: %v\n%s", err, week)
	}
	one, err := run(t, "stats", "--dir", dir, "--route", "GET /orders/{id}")
	if err != nil || !strings.HasPrefix(one, "200 requests") || strings.Contains(one, "POST") {
		t.Errorf("stats --route: %v\n%s", err, one)
	}
	js, err := run(t, "stats", "--dir", dir, "--json")
	if err != nil || strings.Count(js, "\n") != 4 || !strings.Contains(js, `"machine":true`) || !strings.Contains(js, `"disk_used":24696061952`) || !strings.Contains(js, `"requests":220`) || !strings.Contains(js, `"total":true`) ||
		!strings.Contains(js, `"route":"GET /orders/{id}","slow":0,"total":false`) {
		t.Errorf("stats --json: %v\n%s", err, js)
	}
	if none, _ := run(t, "stats", "--dir", dir, "--since", "1m"); !strings.Contains(none, "No requests counted") || strings.Contains(none, "Machine") {
		t.Errorf("stats in a window with no requests and no reading: %s", none)
	}
	if quiet, err := run(t, "stats", "--dir", dir, "--since", "1m", "--json"); err != nil || strings.Contains(quiet, "machine") || strings.Count(quiet, "\n") != 1 {
		t.Errorf("stats --json in a window with no reading: %v\n%s", err, quiet)
	}
	// A reading with no requests beside it is still said.
	only := t.TempDir()
	at := now.Add(-time.Minute)
	if err := os.WriteFile(filepath.Join(only, itel.FileName(itel.KindHost, at)), []byte(`{"time":"`+at.Format(time.RFC3339)+`","goroutines":3,"heap":10485760}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out, err := run(t, "stats", "--dir", only); err != nil || !strings.Contains(out, "No requests counted.") || !strings.HasSuffix(out, ": this app holds 10 MB in 3 goroutines.\n") {
		t.Errorf("a machine that says little, with no requests: %v\n%q", err, out)
	}
	for _, argv := range [][]string{{"stats", "--dir", dir, "extra"}, {"stats", "--dir", dir, "--since", "0"}, {"stats", "--dir", dir, "--since", "30s"}} {
		if _, err := run(t, argv...); !errors.Is(err, errUsage) {
			t.Errorf("%v: %v", argv, err)
		}
	}
	// A year by the minute would be too many steps; the command picks
	// its own, so any window answers.
	if year, err := run(t, "stats", "--dir", dir, "--since", "8760h"); err != nil || !strings.HasPrefix(year, "229 requests") {
		t.Errorf("stats --since 8760h: %v\n%s", err, year)
	}
}

func TestRequest(t *testing.T) {
	dir := seed(t)
	// The ID may sit before or after the flags, in any case.
	for _, argv := range [][]string{{"request", okID, "--dir", dir}, {"request", "--dir", dir, strings.ToUpper(okID)}} {
		out, err := run(t, argv...)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"GET /orders/{id}  (200, 20.0ms)", "path   /orders/42", "trace  " + okID,
			"0µs    20.0ms  HTTP GET GET /orders/{id}", "5.0ms  10.0ms    SELECT  SELECT * FROM orders  FAILED", "order.loaded  order=42"} {
			if !strings.Contains(out, want) {
				t.Errorf("request %v: missing %q in\n%s", argv, want, out)
			}
		}
		if strings.Contains(out, "payment declined") {
			t.Errorf("another request's log line was printed:\n%s", out)
		}
	}
	if _, err := run(t, "request", strings.Repeat("0", 32), "--dir", dir); err == nil || !strings.Contains(err.Error(), "no request with that trace ID") {
		t.Errorf("unknown ID: %v", err)
	}
}

// One failed request says what went wrong and where, each stack line
// on a line of its own with nothing in it that moves the cursor.
func TestRequestShowsTheErrorAndTheStack(t *testing.T) {
	dir := seed(t)
	out, err := run(t, "request", failedID, "--dir", dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"error  card 4411 declined", "\nWhere it failed\n  app.charge(...)\n      /app/pay.go:41 +0x2", "\n  app.handler(...)\n      /app/h.go:9 +0x1\n\nLog lines"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b") || strings.Index(out, "Steps") > strings.Index(out, "Where it failed") {
		t.Errorf("an escape got through, or the stack is before the steps:\n%q", out)
	}
	ok, err := run(t, "request", okID, "--dir", dir)
	if err != nil || strings.Contains(ok, "Where it failed") || strings.Contains(ok, "error  ") {
		t.Errorf("a request that did not fail: %v\n%s", err, ok)
	}
}

func TestUsageErrors(t *testing.T) {
	dir := seed(t)
	for name, argv := range map[string][]string{
		"no subcommand":    {},
		"unknown":          {"traces", "--dir", dir},
		"no dir":           {"logs"},
		"dir and app":      {"logs", "--dir", dir, "--app", "shop"},
		"app is a path":    {"logs", "--app", "../etc"},
		"bad level":        {"logs", "--dir", dir, "--level", "loud"},
		"bad flag":         {"logs", "--dir", dir, "--failed"},
		"limit zero":       {"logs", "--dir", dir, "--limit", "0"},
		"negative since":   {"logs", "--dir", dir, "--since", "-1h"},
		"stray argument":   {"logs", "--dir", dir, "extra"},
		"no trace ID":      {"request", "--dir", dir},
		"two trace IDs":    {"request", okID, failedID, "--dir", dir},
		"trace ID not hex": {"request", "nope", "--dir", dir},
	} {
		if out, err := run(t, argv...); !errors.Is(err, errUsage) {
			t.Errorf("%s: err = %v, want a usage error (printed %q)", name, err, out)
		}
	}
	if _, err := run(t, "logs", "--dir", filepath.Join(dir, "missing")); err == nil || errors.Is(err, errUsage) || !strings.Contains(err.Error(), "no telemetry directory") {
		t.Errorf("a missing directory: %v", err)
	}
	for _, argv := range [][]string{{"--help"}, {"logs", "--help"}, {"help"}} {
		if _, err := run(t, argv...); !errors.Is(err, flag.ErrHelp) {
			t.Errorf("%v: err = %v, want help", argv, err)
		}
	}
}

// --app reads the directory battery/telemetry writes to by default.
func TestAppFlagFindsTheDefaultDirectory(t *testing.T) {
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)
	dir := filepath.Join(state, "shop", "telemetry")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	src := seed(t)
	entries, _ := os.ReadDir(src)
	for _, e := range entries {
		b, _ := os.ReadFile(filepath.Join(src, e.Name()))
		if err := os.WriteFile(filepath.Join(dir, e.Name()), b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if out, err := run(t, "requests", "--app", "shop"); err != nil || !strings.Contains(out, okID) {
		t.Fatalf("--app shop: %v\n%s", err, out)
	}
}
