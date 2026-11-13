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
