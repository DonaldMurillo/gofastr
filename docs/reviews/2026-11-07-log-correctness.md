# Log metrics correctness review — November 7, 2026

Reviewed revision: `953ccc08256e825bdc17254c463a5695a27092b6`.

## C08 — P3: Metrics panics before plugin initialization

`battery/log/log.go:209` explicitly promises that `Plugin.Metrics` is safe
to call at any time. `New` returns a supported unregistered plugin, but
`Metrics` dereferences `p.handler.metrics` before `Init` creates the handler.
The public `MetricsHandler` delegates to that same method. Direct calls and
requests before initialization therefore panic instead of returning counters.

This is a narrow lifecycle-contract defect, not evidence that an ordinary
initialized application crashes. A handler panic does not inherently terminate
the process; a recovery layer may turn it into a 500. Both entry points share
one root cause. No production fix is included.

### Independent observation probe

The coordinator probe below uses only public APIs. It captures pre-Init panics,
then initializes the same plugin and checks direct metrics and the same HTTP
handler. An in-memory sink avoids default filesystem output. These assertions
describe the observed defect, not the desired regression expectations after a fix.

```go
package log_test

import (
    "context"
    "fmt"
    "net/http/httptest"
    "strings"
    "testing"

    logplugin "github.com/DonaldMurillo/gofastr/battery/log"
    "github.com/DonaldMurillo/gofastr/framework"
)

func TestCoordinatorMetricsInitializationObservation(t *testing.T) {
    plugin := logplugin.New(logplugin.Config{
        Sinks: []logplugin.Sink{logplugin.NewRingSink(8)},
        Console: logplugin.ConsoleOff,
        DisableLifecycleEvents: true,
    })
    handler := plugin.MetricsHandler()
    capturedPanic := func(action func()) (result any) {
        defer func() { result = recover() }()
        action()
        return nil
    }
    scrape := func() {
        recorder := httptest.NewRecorder()
        handler.ServeHTTP(recorder, httptest.NewRequest("GET", "/metrics", nil))
        if recorder.Code != 200 { t.Fatalf("status = %d", recorder.Code) }
        for _, name := range []string{
            "post_stop_drops", "sink_write_failures", "webhook_dropped", "webhook_gave_up",
        } {
            if !strings.Contains(recorder.Body.String(), "gofastr_log_"+name+"_total 0\n") {
                t.Fatalf("missing zero counter %s: %s", name, recorder.Body.String())
            }
        }
    }
    for _, entry := range []struct { name string; action func() }{
        {"direct", func() { _ = plugin.Metrics() }},
        {"handler", scrape},
    } {
        panicValue := capturedPanic(entry.action)
        t.Logf("before Init %s: panic=%v", entry.name, panicValue)
        if panicValue == nil || !strings.Contains(fmt.Sprint(panicValue), "nil pointer") {
            t.Fatalf("expected observed nil-pointer defect from %s", entry.name)
        }
    }
    app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "metrics-observation"}))
    app.RegisterPlugin(plugin)
    if err := app.InitPlugins(); err != nil { t.Fatal(err) }
    t.Cleanup(func() {
        if err := app.Shutdown(context.Background()); err != nil { t.Error(err) }
    })
    if snapshot := plugin.Metrics(); snapshot != (logplugin.Metrics{}) {
        t.Fatalf("unexpected initial counters: %+v", snapshot)
    }
    scrape()
    t.Log("after Init: direct metrics are zero; same handler returns 200 with four zero counters")
}
```

Extract the Go block into a temporary `metrics_test.go`. Create an overlay
mapping the absolute virtual filename
`<checkout>/battery/log/zz_coordinator_metrics_observation_test.go` to that
temporary file, then run from the checkout root:

```sh
go test -p 2 -count=1 -overlay /tmp/gofastr-coordinator-metrics/overlay.json \
  -run '^TestCoordinatorMetricsInitializationObservation$' -v ./battery/log/
go test -p 2 -count=1 ./battery/log/ ./core/tracing/ ./battery/print/
```

No browser, external service, real print job or actual email delivery is used.

### Executed results

- Fresh coordinator observation: direct Metrics and handler invocation both
  panic with `invalid memory address or nil pointer dereference` before Init.
- After Init: the same plugin returns zero direct counters and the same handler
  returns 200 with all four zero counter lines. Observation test passes.
- Full `battery/log`, `core/tracing` and `battery/print` package suites pass.
- These checks confirm a contract violation confined to the pre-Init window;
  they do not imply that initialized applications fail or that a fix was made.
