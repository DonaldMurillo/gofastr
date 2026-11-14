package otelspan_test

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"

	"github.com/DonaldMurillo/gofastr/battery/log"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/internal/telemetry"
	"github.com/DonaldMurillo/gofastr/internal/telemetry/otelspan"
)

// What logs and traces cost one request, through the real stack: the
// framework's default middleware, battery/log's handler and access
// line, and a handler that logs twice and makes seven child spans (the
// design load's request).
//
//	off     no tracer, log lines discarded
//	logs    no tracer, log lines to the day file
//	sdk     SDK tracer with a processor that does nothing, log lines to the day file
//	on      SDK tracer + collector + trace file, log lines to the day file
//
// sdk separates what the OpenTelemetry SDK costs to make the spans from
// what this package costs to keep them.
//
// Targets: on costs at most 5% of a request's throughput and adds at
// most 100 µs at p99 over off. Run with GOMAXPROCS=1 for the droplet.

type discard struct{}

func (discard) Write([]byte) error { return nil }
func (discard) Close() error       { return nil }

type fileSink struct{ w *telemetry.Writer }

func (s fileSink) Write(entry []byte) error { return s.w.WriteLog(entry) }
func (fileSink) Close() error               { return nil }

type nopProcessor struct{}

func (nopProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}
func (nopProcessor) OnEnd(sdktrace.ReadOnlySpan)                     {}
func (nopProcessor) Shutdown(context.Context) error                  { return nil }
func (nopProcessor) ForceFlush(context.Context) error                { return nil }

func benchRequest(b *testing.B, files, sdk, traces bool) {
	cfg := telemetry.Config{Dir: b.TempDir()}
	var sink log.Sink = discard{}
	opts := []framework.AppOption{framework.WithConfig(framework.AppConfig{Name: "bench"})}
	var w *telemetry.Writer
	if files {
		var err error
		if w, err = telemetry.NewWriter(cfg); err != nil {
			b.Fatal(err)
		}
		defer w.Close()
		sink = fileSink{w}
	}
	var c *telemetry.Collector
	if sdk {
		var proc sdktrace.SpanProcessor = nopProcessor{}
		if traces {
			var err error
			if c, err = telemetry.NewCollector(cfg, w.WriteTrace); err != nil {
				b.Fatal(err)
			}
			proc = otelspan.New(c)
		}
		prev := otel.GetTracerProvider()
		otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(proc)))
		defer otel.SetTracerProvider(prev)
		opts = append(opts, framework.WithTracing())
	}
	app := framework.NewApp(opts...)
	app.RegisterPlugin(log.New(log.Config{Sinks: []log.Sink{sink}, Level: slog.LevelDebug}))
	if err := app.InitPlugins(); err != nil {
		b.Fatal(err)
	}
	defer app.Shutdown(context.Background())
	logger, tracer := app.Logger(), otel.Tracer("bench")
	app.Router().Get("/orders/{id}", http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		logger.DebugContext(ctx, "handler.start", "route", "/orders/{id}", "user_id", 4821, "tenant", "acme")
		for range 7 {
			_, span := tracer.Start(ctx, "SELECT", trace.WithAttributes(
				attribute.String("db.query.text", "SELECT id, name, status, total, created_at FROM orders WHERE tenant_id = ? AND status = ? ORDER BY created_at DESC LIMIT ?"),
				attribute.Int("db.rows", 20)))
			span.End()
		}
		logger.InfoContext(ctx, "app.step", "detail", "cache miss rows loaded", "rows", 20, "user_id", 4821)
		_, _ = io.WriteString(rw, "ok")
	}))
	h := app.Router()

	durs := make([]time.Duration, 0, b.N)
	b.ReportAllocs()
	for b.Loop() {
		req := httptest.NewRequest(http.MethodGet, "/orders/42", nil)
		rec := httptest.NewRecorder()
		start := time.Now()
		h.ServeHTTP(rec, req)
		durs = append(durs, time.Since(start))
		if rec.Code != 200 {
			b.Fatalf("status %d", rec.Code)
		}
	}
	b.StopTimer()
	slices.Sort(durs)
	b.ReportMetric(float64(durs[len(durs)*99/100].Nanoseconds()), "p99-ns")
	if w != nil {
		st := w.Stats()
		if st.Dropped != 0 || st.LogLines != 3*int64(len(durs)) {
			b.Fatalf("writer stats %+v after %d requests", st, len(durs))
		}
		b.ReportMetric(float64(st.LogBytes+st.TraceBytes)/float64(len(durs)), "disk-B/op")
	}
	if c != nil {
		st := c.Stats()
		if st.Kept+st.SampledOut != int64(len(durs)) || st.Dropped != 0 || st.OpenTraces != 0 {
			b.Fatalf("collector stats %+v after %d requests", st, len(durs))
		}
	}
}

func BenchmarkRequestOff(b *testing.B)  { benchRequest(b, false, false, false) }
func BenchmarkRequestLogs(b *testing.B) { benchRequest(b, true, false, false) }
func BenchmarkRequestSDK(b *testing.B)  { benchRequest(b, true, true, false) }
func BenchmarkRequestOn(b *testing.B)   { benchRequest(b, true, true, true) }

// Every trace kept: the cost of a request whose trace is written.
func BenchmarkWriteTrace(b *testing.B) {
	w, err := telemetry.NewWriter(telemetry.Config{Dir: b.TempDir()})
	if err != nil {
		b.Fatal(err)
	}
	defer w.Close()
	tr := telemetry.Trace{Time: time.Now(), TraceID: "4bf92f3577b34da6a3ce929d0e0e4736", Route: "GET /orders/{id}", Status: 200, Dur: 20 * time.Millisecond}
	for range 8 {
		tr.Spans = append(tr.Spans, telemetry.Span{SpanID: "00f067aa0ba902b7", ParentID: "00f067aa0ba902b6", Name: "SELECT", Dur: time.Millisecond,
			Attrs: []telemetry.Attr{{Key: "db.query.text", Value: "SELECT id, name, status, total, created_at FROM orders WHERE tenant_id = ? AND status = ? ORDER BY created_at DESC LIMIT ?"}, {Key: "db.rows", Value: "20"}}})
	}
	b.ReportAllocs()
	for b.Loop() {
		if err := w.WriteTrace(tr); err != nil {
			b.Fatal(err)
		}
	}
}
