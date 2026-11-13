package telemetry_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/DonaldMurillo/gofastr/battery/log"
	"github.com/DonaldMurillo/gofastr/battery/telemetry"
	"github.com/DonaldMurillo/gofastr/framework"
	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
)

type appUnderTest struct {
	app   *framework.App
	tel   *telemetry.Plugin
	store *itel.Store
}

// start builds an app with the plugin on a TracerProvider of the test's
// own, installed before the app exists. OpenTelemetry's global can be
// pointed at a real provider once per process: after that, a tracer the
// middleware took earlier keeps the provider it was given, so a test
// that let Init install one would work only when it ran first.
// TestInitInstallsATracerProvider covers that path on its own.
func start(t *testing.T, cfg telemetry.Config) appUnderTest {
	t.Helper()
	if cfg.Dir == "" {
		cfg.Dir = t.TempDir()
	}
	cfg.ExternalTracerProvider = true
	tel := telemetry.New(cfg)
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(tel.SpanProcessor())))
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}), framework.WithTracing())
	app.RegisterPlugin(tel)
	app.RegisterPlugin(log.New(log.Config{Sinks: []log.Sink{tel.LogSink()}, Console: log.ConsoleOff}))
	if err := app.InitPlugins(); err != nil {
		t.Fatalf("InitPlugins: %v", err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	logger := app.Logger()
	app.Router().Get("/orders/{id}", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, span := otel.Tracer("test").Start(r.Context(), "SELECT")
		span.End()
		logger.InfoContext(r.Context(), "order.loaded", "id", r.PathValue("id"))
		switch r.PathValue("id") {
		case "boom":
			w.WriteHeader(500)
		case "declined":
			// The commonest failure: log what went wrong, answer 500,
			// with no step of the request marked failed.
			logger.ErrorContext(r.Context(), "payment failed", "err", "card 4411 declined")
			w.WriteHeader(500)
		}
	}))
	store, err := itel.Open(itel.Config{Dir: cfg.Dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return appUnderTest{app, tel, store}
}

func (a appUnderTest) get(path string) {
	a.app.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
}

// The whole path, through the framework: a request's log lines and its
// trace land in the files, the lines name the trace, and the trace is
// found by the ID a line carries.
func TestRequestLeavesLinkedLogsAndTrace(t *testing.T) {
	a := start(t, telemetry.Config{SampleRate: 1})
	a.get("/orders/42")

	tail, err := a.store.TailLogs(100, slog.LevelDebug)
	if err != nil {
		t.Fatal(err)
	}
	var access struct {
		Msg     string `json:"msg"`
		TraceID string `json:"trace_id"`
		Status  int    `json:"status"`
	}
	for _, e := range tail {
		if strings.Contains(string(e.Line), `"msg":"http.access"`) {
			if err := json.Unmarshal(e.Line, &access); err != nil {
				t.Fatal(err)
			}
		}
	}
	if len(access.TraceID) != 32 || access.Status != 200 {
		t.Fatalf("no access line with a trace ID among %d lines: %+v", len(tail), access)
	}

	line, ok, err := a.store.TraceByID(access.TraceID)
	if err != nil || !ok {
		t.Fatalf("the trace an access line names is not in the trace file: ok=%v err=%v", ok, err)
	}
	var tr struct {
		Route  string `json:"route"`
		Status int    `json:"status"`
		Spans  []struct {
			Name string `json:"name"`
		} `json:"spans"`
	}
	if err := json.Unmarshal(line, &tr); err != nil {
		t.Fatal(err)
	}
	if tr.Route != "GET /orders/{id}" || tr.Status != 200 || len(tr.Spans) != 2 || tr.Spans[1].Name != "SELECT" {
		t.Fatalf("trace line: %s", line)
	}

	now := time.Now()
	lines, err := a.store.LogsForTrace(access.TraceID, now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil || len(lines) != 2 {
		t.Fatalf("LogsForTrace = %d lines, %v; want the handler's line and the access line", len(lines), err)
	}
	if st := a.tel.Stats(); st.Traces != 1 || st.LogLines < 2 || st.LinesDropped != 0 || st.TracesDropped != 0 {
		t.Fatalf("stats %+v", st)
	}
}

// With ordinary traces off, a failed request still leaves its trace and
// a fine one leaves only its log lines.
func TestOnlyFailedRequestsKeptWhenOrdinaryOff(t *testing.T) {
	a := start(t, telemetry.Config{KeepNoOrdinary: true})
	// Enough ordinary requests that the default 5% would keep some.
	const ordinary = 200
	for range ordinary {
		a.get("/orders/42")
	}
	a.get("/orders/boom")
	st := a.tel.Stats()
	if st.Traces != 1 || st.TracesSampledOut != ordinary {
		t.Fatalf("stats %+v: want the failed request's trace kept and the other sampled out", st)
	}
	slow, err := a.store.SlowestTraces("", time.Time{}, time.Time{}, 10)
	if err != nil || len(slow) != 1 || slow[0].Status != 500 || !slow[0].Error {
		t.Fatalf("traces on disk: %+v, %v", slow, err)
	}
}

// An app with its own TracerProvider keeps it: the plugin installs
// nothing, and records spans once its processor is added there.
func TestExternalTracerProviderIsLeftAlone(t *testing.T) {
	prev := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	tel := telemetry.New(telemetry.Config{Dir: t.TempDir(), SampleRate: 1, ExternalTracerProvider: true})
	own := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(tel.SpanProcessor()))
	otel.SetTracerProvider(own)

	// Before Init the processor has nowhere to write: nothing panics.
	_, early := own.Tracer("test").Start(context.Background(), "early")
	early.End()

	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(tel)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	if otel.GetTracerProvider() != own {
		t.Fatal("Init replaced the app's TracerProvider")
	}
	_, span := own.Tracer("test").Start(context.Background(), "job")
	span.End()
	if st := tel.Stats(); st.Traces != 1 {
		t.Fatalf("stats %+v: want the one span after Init written", st)
	}

	// After shutdown, lines and spans are dropped quietly.
	if err := app.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	_, late := own.Tracer("test").Start(context.Background(), "late")
	late.End()
	if err := tel.LogSink().Write([]byte(`{"time":"2026-11-13T00:00:00Z","msg":"late"}`)); err != log.ErrSinkClosed {
		t.Fatalf("a line after shutdown: err = %v, want log.ErrSinkClosed", err)
	}
	if st := tel.Stats(); st.Traces != 1 || st.LinesDropped != 1 {
		t.Fatalf("after shutdown: %+v", st)
	}
}

// Left to itself, Init installs a TracerProvider that writes to the
// files: a tracer taken from the global afterwards records there.
func TestInitInstallsATracerProvider(t *testing.T) {
	prev := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	tel := telemetry.New(telemetry.Config{Dir: t.TempDir(), SampleRate: 1})
	app := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "test"}))
	app.RegisterPlugin(tel)
	if err := app.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Shutdown(context.Background()) })
	if _, ok := otel.GetTracerProvider().(*sdktrace.TracerProvider); !ok {
		t.Fatalf("global TracerProvider is %T after Init", otel.GetTracerProvider())
	}
	_, span := otel.GetTracerProvider().Tracer("test").Start(context.Background(), "job")
	span.End()
	if st := tel.Stats(); st.Traces != 1 {
		t.Fatalf("stats %+v: want the span written", st)
	}
}

// A line the disk cap refuses is an error to the log plugin and a
// counted drop, never a silent one.
func TestLinesRefusedAtTheDiskCapAreCounted(t *testing.T) {
	dir := t.TempDir()
	today := itel.FileName(itel.KindLogs, time.Now())
	if err := os.WriteFile(filepath.Join(dir, today), make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	a := start(t, telemetry.Config{Dir: dir, MaxDiskBytes: 1024})
	if err := a.tel.LogSink().Write([]byte(`{"time":"2026-11-13T00:00:00Z","msg":"x"}`)); err == nil {
		t.Fatal("a line past the disk cap was accepted")
	}
	if st := a.tel.Stats(); st.LinesDropped == 0 || st.LogLines != 0 {
		t.Fatalf("stats %+v: want the refused line counted as dropped", st)
	}
}

func TestDefaultDirNeedsAnAppName(t *testing.T) {
	prev := otel.GetTracerProvider()
	t.Cleanup(func() { otel.SetTracerProvider(prev) })
	state := t.TempDir()
	t.Setenv("XDG_STATE_HOME", state)

	app := framework.NewApp()
	app.RegisterPlugin(telemetry.New(telemetry.Config{}))
	if err := app.InitPlugins(); err == nil || !strings.Contains(err.Error(), "Config.Dir") {
		t.Fatalf("no app name and no Dir: err = %v", err)
	}

	named := framework.NewApp(framework.WithConfig(framework.AppConfig{Name: "shop"}))
	tel := telemetry.New(telemetry.Config{})
	named.RegisterPlugin(tel)
	if err := named.InitPlugins(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = named.Shutdown(context.Background()) })
	if err := tel.LogSink().Write([]byte(`{"time":"2026-11-13T00:00:00Z","msg":"x"}`)); err != nil {
		t.Fatal(err)
	}
	store, err := itel.Open(itel.Config{Dir: state + "/shop/telemetry"})
	if err != nil {
		t.Fatalf("the default directory was not created: %v", err)
	}
	store.Close()
}

// A handler that logs an error and answers 500 has its failure named by
// what it logged, not by "Internal Server Error": that is what tells
// one issue from another.
func TestFailureIsNamedByTheErrorTheAppLogged(t *testing.T) {
	a := start(t, telemetry.Config{SampleRate: 1})
	a.get("/orders/declined")
	a.get("/orders/declined")
	a.get("/orders/boom")
	issues, _, err := a.store.Issues(time.Time{}, time.Time{}, 10)
	if err != nil || len(issues) != 2 {
		t.Fatalf("%d issues, %v; want the declined card and the unexplained 500 apart", len(issues), err)
	}
	byErr := map[string]int{}
	for _, is := range issues {
		byErr[is.Err] = is.Count
	}
	if byErr["card N declined"] != 2 || byErr["Internal Server Error"] != 1 {
		t.Fatalf("issues: %v", byErr)
	}
}
