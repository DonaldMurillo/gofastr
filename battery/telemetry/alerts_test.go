package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
)

func alertApp(t *testing.T, cfg Config) (*Plugin, *framework.App) {
	t.Helper()
	cfg.Dir, cfg.ExternalTracerProvider = t.TempDir(), true
	p := New(cfg)
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(p)
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	return p, app
}

// failRequests feeds n finished, failed requests to the plugin's own
// collector, as its span processor does.
func failRequests(p *Plugin, n int, at time.Time) {
	c := p.collector.Load()
	for i := range n {
		c.SpanEnded(itel.SpanEnd{TraceID: strings.Repeat("a", 31) + string(rune('a'+i%6)), Start: at, Root: true, Error: true, Route: "POST /checkout",
			Span: itel.Span{Name: "root", Dur: time.Millisecond}})
	}
}

// With no rules of its own an app is told when ten requests fail within
// five minutes: once when it starts, once when it is over. A notifier
// that fails or panics is logged and does not stop the next check.
func TestDefaultAlertNotifiesOnceEachWay(t *testing.T) {
	var mu sync.Mutex
	var got []Alert
	notify := func(_ context.Context, a Alert) error {
		mu.Lock()
		defer mu.Unlock()
		got = append(got, a)
		switch len(got) {
		case 1:
			return errors.New("the chat service is down")
		case 2:
			panic("a notifier bug")
		}
		return nil
	}
	p, app := alertApp(t, Config{Notify: notify})
	var logged bytes.Buffer
	app.SetLogger(slog.New(slog.NewJSONHandler(&logged, nil)))
	alerts := p.alerts.Load()
	now := time.Now()

	failRequests(p, 9, now)
	p.checkAlerts(app, alerts, now)
	if len(got) != 0 || len(p.Firing()) != 0 {
		t.Fatalf("nine failures: notified %d times", len(got))
	}
	failRequests(p, 1, now)
	p.checkAlerts(app, alerts, now)
	p.checkAlerts(app, alerts, now.Add(time.Minute))
	if len(got) != 1 || !got[0].Firing || got[0].Count != 10 || got[0].Rule.Name != "failed requests" || got[0].Rule.Kind != AlertFailed {
		t.Fatalf("the tenth failure: %+v", got)
	}
	if f := p.Firing(); len(f) != 1 || f[0].Count != 10 {
		t.Fatalf("Firing(): %+v", f)
	}
	if text := got[0].Text(); !strings.Contains(text, "Alert: failed requests") || !strings.Contains(text, "10 failed requests in the last 5m0s") {
		t.Errorf("Text() = %q", text)
	}
	// Ten minutes on they have aged out; the notifier panics this time.
	later := now.Add(10 * time.Minute)
	p.checkAlerts(app, alerts, later)
	if len(got) != 2 || got[1].Firing || got[1].Count != 0 || len(p.Firing()) != 0 || !strings.HasPrefix(got[1].Text(), "Resolved: failed requests") {
		t.Fatalf("after the failures aged out: %+v", got)
	}
	// Every change is a log line of its own, so the Logs page has it
	// even when nobody was notified; and both notifier failures are.
	for _, want := range []string{`"msg":"telemetry.alert","rule":"failed requests","state":"firing","count":10`,
		`"msg":"telemetry.alert","rule":"failed requests","state":"resolved"`,
		"the alert was not delivered", "the chat service is down", "the alert notifier panicked", "a notifier bug"} {
		if !strings.Contains(logged.String(), want) {
			t.Errorf("the log lacks %q:\n%s", want, logged.String())
		}
	}
	// A stray call, a query some loop ran outside any request, failing
	// is not a failed request.
	c := p.collector.Load()
	for range 30 {
		c.SpanEnded(itel.SpanEnd{TraceID: strings.Repeat("b", 32), Start: later, Root: true, Stray: true, Error: true, Span: itel.Span{Name: "SELECT"}})
	}
	p.checkAlerts(app, alerts, later)
	if len(got) != 2 {
		t.Fatalf("thirty failed stray calls fired the rule: %+v", got[2:])
	}
	// And the watcher is still standing: the next burst is noticed.
	failRequests(p, 10, later)
	p.checkAlerts(app, alerts, later)
	if len(got) != 3 || !got[2].Firing {
		t.Fatalf("after a notifier error and a panic, the next alert: %+v", got)
	}
}

func TestAlertRulesOfTheAppsOwnAndNone(t *testing.T) {
	var got []Alert
	notify := func(_ context.Context, a Alert) error { got = append(got, a); return nil }
	p, app := alertApp(t, Config{Notify: notify, SlowThreshold: time.Second,
		Alerts: []AlertRule{{Name: "checkout", Kind: AlertFailed, AtLeast: 2, Within: time.Minute, Route: "POST /checkout"}}})
	now := time.Now()
	failRequests(p, 2, now)
	p.checkAlerts(app, p.alerts.Load(), now)
	if len(got) != 1 || got[0].Rule.Name != "checkout" || got[0].Rule.Route != "POST /checkout" || !strings.Contains(got[0].Text(), "on POST /checkout") {
		t.Fatalf("the app's own rule: %+v", got)
	}

	// With no notifier a firing rule is still logged, and nothing is
	// called.
	quiet, quietApp := alertApp(t, Config{})
	var logged bytes.Buffer
	quietApp.SetLogger(slog.New(slog.NewJSONHandler(&logged, nil)))
	failRequests(quiet, 10, now)
	quiet.checkAlerts(quietApp, quiet.alerts.Load(), now)
	if !strings.Contains(logged.String(), `"state":"firing"`) || strings.Contains(logged.String(), "notifier") {
		t.Fatalf("with no notifier: %s", logged.String())
	}

	off, _ := alertApp(t, Config{NoAlerts: true, Notify: notify})
	if off.alerts.Load() != nil || off.Firing() != nil {
		t.Fatal("NoAlerts left a rule running")
	}

	bad := New(Config{Dir: t.TempDir(), ExternalTracerProvider: true, Alerts: []AlertRule{{Name: "x", Kind: "loud", AtLeast: 1}}})
	app2 := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app2.RegisterPlugin(bad)
	if err := app2.InitPlugins(); err == nil || !strings.Contains(err.Error(), "Kind") {
		t.Fatalf("a rule of an unknown kind: Init returned %v", err)
	}
}

// The webhook notifier posts one JSON body a chat webhook reads, does
// not follow a redirect, and reports a refusal without the URL, which
// for a chat webhook is the secret.
func TestWebhookNotifier(t *testing.T) {
	var body map[string]any
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		switch r.URL.Path {
		case "/hook/SECRET":
			b, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(b, &body)
			if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
				w.WriteHeader(400)
			}
		case "/moved/SECRET":
			http.Redirect(w, r, "/hook/SECRET", http.StatusFound)
		default:
			w.WriteHeader(500)
		}
	}))
	defer srv.Close()
	a := Alert{Rule: AlertRule{Name: "failed requests", Kind: AlertFailed, AtLeast: 10, Within: 5 * time.Minute}, Firing: true, Count: 12, At: time.Now(), Since: time.Now()}

	if err := WebhookNotifier(srv.URL+"/hook/SECRET")(context.Background(), a); err != nil {
		t.Fatal(err)
	}
	alert, _ := body["alert"].(map[string]any)
	if body["text"] != a.Text() || body["content"] != a.Text() || alert["rule"] != "failed requests" || alert["count"] != 12.0 || alert["firing"] != true || alert["within_seconds"] != 300.0 {
		t.Fatalf("the posted body: %v", body)
	}
	hits = 0
	for name, target := range map[string]string{"a 500": "/fail/SECRET", "a redirect": "/moved/SECRET"} {
		err := WebhookNotifier(srv.URL+target)(context.Background(), a)
		if err == nil || strings.Contains(err.Error(), "SECRET") {
			t.Errorf("%s: err = %v; want an error that does not hold the URL's secret", name, err)
		}
	}
	// A host that cannot be reached is named, and the rest of the URL
	// is not: the transport's own error spells the whole of it.
	gone := httptest.NewServer(http.NotFoundHandler())
	goneURL := gone.URL
	gone.Close()
	if err := WebhookNotifier(goneURL+"/hook/SECRET")(context.Background(), a); err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("an unreachable host: err = %v", err)
	}
	if hits != 2 {
		t.Errorf("%d requests for two posts: a redirect was followed", hits)
	}
	for _, bad := range []string{"", "not a url", "ftp://x/y", "/relative", "https://"} {
		if err := WebhookNotifier(bad)(context.Background(), a); err == nil {
			t.Errorf("WebhookNotifier(%q) posted", bad)
		}
	}
}

// Each round writes the request counts of the minutes that have ended,
// with or without alert rules, so the Overview is at most a minute and
// a round behind and an app that never stops still has its counts.
func TestEachRoundWritesTheFinishedMinutes(t *testing.T) {
	for name, cfg := range map[string]Config{"with the default rule": {}, "with alerts off": {NoAlerts: true}} {
		t.Run(name, func(t *testing.T) {
			p, app := alertApp(t, cfg)
			now := time.Now()
			counts := itel.NewRollup()
			counts.Observe(now.Add(-2*time.Minute), "GET /a", time.Millisecond, false, false)
			counts.Observe(now, "GET /a", time.Millisecond, false, false)
			p.tick(app, p.writer.Load(), counts, p.alerts.Load(), now)
			got, err := p.Reader().Stats(itel.StatsQuery{From: now.Add(-time.Hour), To: now.Add(time.Hour), Step: time.Hour})
			if err != nil || got.Total.N != 1 {
				t.Fatalf("after one round: %d requests, %v; want the finished minute's one", got.Total.N, err)
			}
		})
	}
}

// Machine is read once a minute, however many rounds the minute
// has, and each reading goes to the day's file and to the disk rule: a
// disk that passes its line notifies once, and once more when it has
// room again.
func TestMachineIsReadOnceAMinuteAndTheDiskRuleFollows(t *testing.T) {
	var got []Alert
	p, app := alertApp(t, Config{Notify: func(_ context.Context, a Alert) error { got = append(got, a); return nil }})
	gb := int64(1 << 30)
	disk, reads := int64(10), 0
	p.readHost = func(at time.Time) itel.HostReading {
		reads++
		return itel.HostReading{Time: at, CPU: 0.25, MemUsed: gb, MemTotal: 4 * gb, DiskUsed: disk * gb, DiskTotal: 25 * gb, Goroutines: 9}
	}
	start := time.Now().Truncate(time.Minute)
	round := func(at time.Time) { p.tick(app, p.writer.Load(), itel.NewRollup(), p.alerts.Load(), at) }

	round(start)
	round(start.Add(15 * time.Second))
	round(start.Add(45 * time.Second))
	if reads != 1 {
		t.Fatalf("%d readings in one minute, want 1", reads)
	}
	if h := p.Host(); h == nil || h.DiskPercent() != 40 || h.Goroutines != 9 {
		t.Fatalf("the latest reading: %+v", h)
	}
	disk = 23 // 92%
	round(start.Add(time.Minute))
	round(start.Add(75 * time.Second))
	if reads != 2 || len(got) != 1 || !got[0].Firing || got[0].Rule.Kind != AlertDisk || got[0].Count != 92 {
		t.Fatalf("%d readings, alerts %+v; want the default disk rule firing at 92", reads, got)
	}
	if want := "Alert: disk nearly full. The disk is 92% full (the rule fires at 90%)."; got[0].Text() != want {
		t.Fatalf("text %q, want %q", got[0].Text(), want)
	}
	disk = 20 // 80%
	round(start.Add(2 * time.Minute))
	if len(got) != 2 || got[1].Firing || got[1].Text() != "Resolved: disk nearly full. The disk is 80% full, down from at least 90%." {
		t.Fatalf("after room was made: %+v", got)
	}

	series, err := p.Reader().Host(start.Add(-time.Minute), start.Add(10*time.Minute), time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, pt := range series.Points {
		n += pt.Samples
	}
	if n != 3 || series.Latest.DiskUsed != 20*gb {
		t.Fatalf("%d readings in the file, latest %+v", n, series.Latest)
	}

	// With alerts off the machine is still read and written.
	quiet, quietApp := alertApp(t, Config{NoAlerts: true})
	quiet.tick(quietApp, quiet.writer.Load(), itel.NewRollup(), nil, start)
	if h := quiet.Host(); h == nil || h.Goroutines < 1 {
		t.Fatalf("with alerts off: %+v", h)
	}
}

// A reader that panics costs that minute's reading and nothing else:
// the round goes on to its alert check, and the next minute reads again.
func TestAPanickingMachineReaderDoesNotEndTheRound(t *testing.T) {
	p, app := alertApp(t, Config{})
	var logged bytes.Buffer
	app.SetLogger(slog.New(slog.NewJSONHandler(&logged, nil)))
	calls := 0
	p.readHost = func(at time.Time) itel.HostReading {
		calls++
		if calls == 1 {
			panic("a /proc file nobody expected")
		}
		return itel.HostReading{Time: at, CPU: -1, Goroutines: 3}
	}
	start := time.Now().Truncate(time.Minute)
	p.tick(app, p.writer.Load(), itel.NewRollup(), p.alerts.Load(), start)
	if p.Host() != nil || !strings.Contains(logged.String(), "reading the machine panicked") {
		t.Fatalf("after a panicking read: latest %+v, log %s", p.Host(), logged.String())
	}
	p.tick(app, p.writer.Load(), itel.NewRollup(), p.alerts.Load(), start.Add(time.Minute))
	if h := p.Host(); h == nil || h.Goroutines != 3 {
		t.Fatalf("the minute after: %+v", h)
	}
}

// The heartbeat is asked for once a minute, follows no redirect, and
// is logged about only when it starts failing and when it works again,
// with the host and never the URL. A URL that is not one stops the app
// at start.
func TestHeartbeatPingsOnceAMinute(t *testing.T) {
	var hits, elsewhere atomic.Int32
	var status atomic.Int32
	status.Store(200)
	other := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { elsewhere.Add(1) }))
	defer other.Close()
	monitor := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/ping/SECRET-TOKEN" {
			t.Errorf("asked %s %s", r.Method, r.URL.Path)
		}
		if code := int(status.Load()); code == 302 {
			http.Redirect(w, r, other.URL, code)
		} else {
			w.WriteHeader(code)
		}
	}))
	defer monitor.Close()

	p, app := alertApp(t, Config{Heartbeat: monitor.URL + "/ping/SECRET-TOKEN", NoAlerts: true})
	p.readHost = nil
	var logged bytes.Buffer
	app.SetLogger(slog.New(slog.NewJSONHandler(&logged, nil)))
	start := time.Now().Truncate(time.Minute)
	round := func(at time.Time) { p.tick(app, p.writer.Load(), itel.NewRollup(), nil, at) }

	round(start)
	round(start.Add(15 * time.Second))
	round(start.Add(45 * time.Second))
	if hits.Load() != 1 || logged.Len() != 0 {
		t.Fatalf("%d pings in one minute, log %q; want one ping and nothing said", hits.Load(), logged.String())
	}
	status.Store(500)
	round(start.Add(1 * time.Minute))
	round(start.Add(2 * time.Minute))
	if hits.Load() != 3 || strings.Count(logged.String(), "was not delivered") != 1 || !strings.Contains(logged.String(), "answered 500") {
		t.Fatalf("%d pings, log %s; want the failure said once", hits.Load(), logged.String())
	}
	status.Store(302)
	round(start.Add(3 * time.Minute))
	if elsewhere.Load() != 0 || strings.Count(logged.String(), "was not delivered") != 1 || strings.Contains(logged.String(), "being delivered again") {
		t.Fatalf("a redirect was followed (%d), said again as a new failure, or taken for a check-in: %s", elsewhere.Load(), logged.String())
	}
	status.Store(204)
	round(start.Add(4 * time.Minute))
	round(start.Add(5 * time.Minute))
	if strings.Count(logged.String(), "being delivered again") != 1 {
		t.Fatalf("recovery said %d times: %s", strings.Count(logged.String(), "being delivered again"), logged.String())
	}
	monitor.Close()
	round(start.Add(6 * time.Minute))
	if strings.Count(logged.String(), "was not delivered") != 2 || !strings.Contains(logged.String(), "no answer") {
		t.Fatalf("a monitor that is gone: %s", logged.String())
	}
	if strings.Contains(logged.String(), "SECRET-TOKEN") {
		t.Fatalf("the heartbeat URL reached the log: %s", logged.String())
	}

	for _, bad := range []string{"healthchecks.io/ping/x", "ftp://example.com/x", "http://", "://x", "https://exa mple.com/x"} {
		tel := New(Config{Dir: t.TempDir(), ExternalTracerProvider: true, Heartbeat: bad})
		a := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
		a.RegisterPlugin(tel)
		if err := a.InitPlugins(); err == nil {
			_ = a.Shutdown(context.Background())
			t.Errorf("Heartbeat %q: the app started", bad)
		}
	}
}

// Stopping the app does not wait out a ping to a monitor that never
// answers: the ping ends with the app.
func TestStopDoesNotWaitOutAHungPing(t *testing.T) {
	if testing.Short() {
		t.Skip("waits one watcher interval for the first ping")
	}
	asked := make(chan struct{}, 4)
	release := make(chan struct{})
	monitor := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		asked <- struct{}{}
		<-release
	}))
	defer monitor.Close()
	defer close(release)
	cfg := Config{Heartbeat: monitor.URL + "/ping", NoAlerts: true}
	cfg.Dir, cfg.ExternalTracerProvider = t.TempDir(), true
	p := New(cfg)
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(p)
	var logged bytes.Buffer
	app.SetLogger(slog.New(slog.NewJSONHandler(&logged, nil)))
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	// The watcher's first round is one interval in.
	select {
	case <-asked:
	case <-time.After(alertEvery + 10*time.Second):
		t.Fatal("the watcher never pinged")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	began := time.Now()
	if err := app.Shutdown(ctx); err != nil {
		t.Fatal(err)
	}
	if took := time.Since(began); took > 3*time.Second {
		t.Errorf("Shutdown took %v behind a ping in flight", took.Round(time.Millisecond))
	}
	// A ping the app cut short is not the monitor failing.
	if strings.Contains(logged.String(), "was not delivered") {
		t.Errorf("stopping logged a failed heartbeat: %s", logged.String())
	}
}

// A notifier that never returns is left behind at its deadline: the
// watcher goes on, and the log names the rule. One that returns when
// its context ends is waited for, and its error is the one logged.
func TestHungNotifierIsLeftBehind(t *testing.T) {
	never := make(chan struct{})
	defer close(never)
	var obeys atomic.Bool
	notify := func(ctx context.Context, _ Alert) error {
		if obeys.Load() {
			<-ctx.Done()
			return ctx.Err()
		}
		<-never
		return nil
	}
	p, app := alertApp(t, Config{Notify: notify})
	p.notifyTimeout = 50 * time.Millisecond
	var logged bytes.Buffer
	app.SetLogger(slog.New(slog.NewJSONHandler(&logged, nil)))
	a := Alert{Rule: AlertRule{Name: "failed requests"}, Firing: true}

	began := time.Now()
	p.notify(app, a)
	if took := time.Since(began); took > 5*time.Second {
		t.Fatalf("a hung notifier held the watcher for %v", took)
	}
	if !strings.Contains(logged.String(), "did not return") || !strings.Contains(logged.String(), "failed requests") {
		t.Errorf("nothing says the notifier was left running: %s", logged.String())
	}

	logged.Reset()
	obeys.Store(true)
	p.notify(app, a)
	if !strings.Contains(logged.String(), "was not delivered") || strings.Contains(logged.String(), "did not return") {
		t.Errorf("a notifier that returned at its deadline: %s", logged.String())
	}
}

// Init runs once. A second one would open the files again and leave the
// plugin pointing at closed ones.
func TestSecondInitIsRefused(t *testing.T) {
	p, app := alertApp(t, Config{EnableMCP: true})
	if err := p.Init(app); err == nil {
		t.Fatal("a second Init was accepted")
	}
	if err := p.LogSink().Write([]byte(`{"time":"2026-11-14T00:00:00Z","level":"INFO","msg":"x"}`)); err != nil {
		t.Errorf("after a refused second Init the plugin cannot write: %v", err)
	}
	if p.Reader() == nil {
		t.Error("after a refused second Init the plugin has no reader")
	}
}

// The same, without the wait for the watcher's first round: a ping in
// flight ends when the app's run ends, and is not logged as a failure.
func TestPingEndsWithTheApp(t *testing.T) {
	release := make(chan struct{})
	monitor := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer monitor.Close()
	defer close(release)
	p, app := alertApp(t, Config{Heartbeat: monitor.URL + "/ping", NoAlerts: true})
	var logged bytes.Buffer
	app.SetLogger(slog.New(slog.NewJSONHandler(&logged, nil)))
	run, stop := context.WithCancel(context.Background())
	p.run = run
	time.AfterFunc(100*time.Millisecond, stop)
	began := time.Now()
	p.beat(app)
	if took := time.Since(began); took > 3*time.Second {
		t.Errorf("a ping outlived the app's run by %v", took.Round(time.Millisecond))
	}
	if logged.Len() != 0 {
		t.Errorf("a ping the app cut short was logged: %s", logged.String())
	}
}

// An Init that fails after it opened the files leaves nothing reachable
// through the plugin: a page or a log line must not find closed files.
func TestFailedInitLeavesNothingBehind(t *testing.T) {
	cfg := Config{EnableMCP: true}
	cfg.Dir, cfg.ExternalTracerProvider = t.TempDir(), true
	p := New(cfg)
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	// The app already has a tool by one of the plugin's names.
	if err := app.MCP.RegisterTool("telemetry_logs", "taken", map[string]any{"type": "object"}, func(context.Context, map[string]any) (any, error) { return nil, nil }); err != nil {
		t.Fatal(err)
	}
	if err := p.Init(app); err == nil {
		t.Fatal("setup: Init did not fail on the taken tool name")
	}
	if p.Reader() != nil || p.writer.Load() != nil || p.collector.Load() != nil || p.alerts.Load() != nil || p.processor.Load() != nil {
		t.Error("a failed Init left the plugin holding closed files")
	}
}
