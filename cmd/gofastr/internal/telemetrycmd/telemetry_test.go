package telemetrycmd

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
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
		{Time: now.Add(-2 * time.Minute), TraceID: failedID, Route: "POST /orders\x1b[31m", Status: 500, Dur: 900 * ms, Error: true},
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
	if strings.Count(out, "\n") != 3 || !strings.Contains(out, "GET /orders/{id}") || !strings.Contains(out, "500 failed") ||
		!strings.Contains(out, "900.0ms") || strings.Contains(out, "GET /old") || strings.Contains(out, "\x1b") {
		t.Fatalf("requests:\n%q", out)
	}
	for args, c := range map[string]struct{ want, not string }{
		"--failed":                 {failedID, okID},
		"--slow 500ms":             {failedID, okID},
		"--route GET_/orders/{id}": {okID, failedID},
		"--since 0":                {"GET /old", "\x00"},
		"--since 0 --limit 1":      {failedID, "GET /old"},
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
