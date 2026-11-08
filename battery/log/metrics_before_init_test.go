package log_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/log"
	"github.com/DonaldMurillo/gofastr/framework"
)

// scrapeMetrics GETs url over a real connection and returns the status
// and body.
func scrapeMetrics(t *testing.T, url string) (int, string) {
	t.Helper()
	response, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	return response.StatusCode, string(body)
}

// TestMetricsBeforeInitIsZero is C08: Metrics is documented as safe at
// any time, yet before Init it dereferenced the nil fan-out handler, so
// a direct call and a scrape of MetricsHandler both panicked. The same
// plugin and handler then count for real once Init has run.
func TestMetricsBeforeInitIsZero(t *testing.T) {
	plugin := log.New(log.Config{
		Sinks:                  []log.Sink{log.NewRingSink(8)},
		Console:                log.ConsoleOff,
		DisableLifecycleEvents: true,
	})
	if got := plugin.Metrics(); got != (log.Metrics{}) {
		t.Fatalf("Metrics before Init = %+v, want zero", got)
	}
	server := httptest.NewServer(plugin.MetricsHandler())
	t.Cleanup(server.Close)
	status, body := scrapeMetrics(t, server.URL)
	if status != http.StatusOK {
		t.Fatalf("scrape before Init = %d %q, want 200", status, body)
	}
	for _, name := range []string{"post_stop_drops", "sink_write_failures", "webhook_dropped", "webhook_gave_up"} {
		if !strings.Contains(body, "gofastr_log_"+name+"_total 0\n") {
			t.Errorf("scrape before Init lacks a zero %s counter:\n%s", name, body)
		}
	}

	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "metrics-before-init"}))
	app.RegisterPlugin(plugin)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	plugin.Logger().Info("after the sinks closed")
	if got := plugin.Metrics().PostStopDrops; got != 1 {
		t.Errorf("PostStopDrops after Init and Shutdown = %d, want 1", got)
	}
	if _, body := scrapeMetrics(t, server.URL); !strings.Contains(body, "gofastr_log_post_stop_drops_total 1\n") {
		t.Errorf("the handler made before Init does not see the live counter:\n%s", body)
	}
}
