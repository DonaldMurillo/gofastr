package log_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/log"
	"github.com/DonaldMurillo/gofastr/framework"
)

// TestMetricsHandlerExpositionFormat pins the Prometheus text format
// (version 0.0.4): each counter preceded by HELP + TYPE comments,
// numeric value on its own line. Operators rely on this contract for
// scrape-time parsing.
func TestMetricsHandlerExpositionFormat(t *testing.T) {
	sink := &memSink{}
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(log.New(log.Config{Sinks: []log.Sink{sink}}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}

	p, _ := app.Plugins.Get("log")
	lp := p.(*log.Plugin)

	srv := httptest.NewServer(lp.MetricsHandler())
	defer srv.Close()
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("Content-Type = %q, want text/plain prefix", got)
	}
	body, _ := io.ReadAll(resp.Body)
	out := string(body)

	// Each of the four counters must appear with HELP + TYPE + value.
	wantNames := []string{
		"gofastr_log_post_stop_drops_total",
		"gofastr_log_sink_write_failures_total",
		"gofastr_log_webhook_dropped_total",
		"gofastr_log_webhook_gave_up_total",
	}
	for _, name := range wantNames {
		helpRe := regexp.MustCompile(`(?m)^# HELP ` + regexp.QuoteMeta(name) + ` .+$`)
		typeRe := regexp.MustCompile(`(?m)^# TYPE ` + regexp.QuoteMeta(name) + ` counter$`)
		valueRe := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(name) + ` \d+$`)
		if !helpRe.MatchString(out) {
			t.Errorf("missing HELP line for %s", name)
		}
		if !typeRe.MatchString(out) {
			t.Errorf("missing TYPE line for %s", name)
		}
		if !valueRe.MatchString(out) {
			t.Errorf("missing value line for %s", name)
		}
	}
}

// TestMetricsHandlerReflectsCounters pins that the handler reads
// fresh values on every scrape — not a snapshot taken at Init time.
func TestMetricsHandlerReflectsCounters(t *testing.T) {
	// Server returns 500 → webhook retries fail → gaveUp increments.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer upstream.Close()

	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(log.New(log.Config{
		Sinks: []log.Sink{
			log.WebhookSink(upstream.URL, log.WebhookOpts{
				BatchSize: 1, BatchInterval: time.Hour, MaxRetries: 0, Timeout: 50 * time.Millisecond,
			}),
		},
	}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	p, _ := app.Plugins.Get("log")
	lp := p.(*log.Plugin)

	srv := httptest.NewServer(lp.MetricsHandler())
	defer srv.Close()

	app.Logger().Info("kick the webhook")

	// Poll the metrics endpoint for the counter to advance.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get(srv.URL)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		if strings.Contains(string(body), "gofastr_log_webhook_gave_up_total 1") ||
			strings.Contains(string(body), "gofastr_log_webhook_gave_up_total 2") {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("counter never advanced in scraped output")
}
