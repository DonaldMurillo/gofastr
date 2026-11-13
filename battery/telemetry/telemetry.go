// Package telemetry keeps an app's logs and traces in plain files on
// the machine it runs on, inside a fixed disk and memory budget, with
// no collector, agent or database beside the app.
//
// Log lines go to one JSON-lines file per UTC day. Traces go to another:
// one complete trace per line, written when its request ends. Every
// error and slow trace is kept and a share of the rest; every log line
// inside a traced request names its trace. Old days are removed by age
// and by a cap on the directory's size.
//
//	tel := telemetry.New(telemetry.Config{})
//	app := framework.NewApp(framework.WithTracing())
//	app.RegisterPlugin(tel) // before the log plugin
//	app.RegisterPlugin(log.New(log.Config{Sinks: []log.Sink{tel.LogSink()}}))
//
// Experimental: the file layout and this API may change.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/DonaldMurillo/gofastr/battery/log"
	"github.com/DonaldMurillo/gofastr/framework"
	itel "github.com/DonaldMurillo/gofastr/internal/telemetry"
	"github.com/DonaldMurillo/gofastr/internal/telemetry/otelspan"
)

// Config holds the plugin's settings. The zero value of a field means
// its default, sized for a 1 vCPU, 1 GB host.
type Config struct {
	// Dir is where the files go. Default: telemetry/ under the app's
	// state directory ($XDG_STATE_HOME/<app> or ~/.local/state/<app>),
	// beside the log plugin's server.log.
	Dir string
	// Retention is how long a day's files are kept. Default 14 days.
	Retention time.Duration
	// MaxDiskBytes caps the directory's size. The oldest days are
	// removed first; if today alone fills it, new lines are dropped and
	// counted until the day turns. Default 5 GiB.
	MaxDiskBytes int64
	// SampleRate is the share of ordinary traces kept, from 0 to 1.
	// Traces with a failed span, and traces at or over SlowThreshold,
	// are always kept. Default 0.05.
	SampleRate float64
	// KeepNoOrdinary keeps only failed and slow traces.
	KeepNoOrdinary bool
	// SlowThreshold is how long a trace takes to count as slow.
	// Default 500 ms.
	SlowThreshold time.Duration
	// MaxMemoryBytes caps the spans held for requests still running.
	// Past it the oldest unfinished trace is dropped and counted.
	// Default 32 MiB.
	MaxMemoryBytes int64
	// QuietPaths are path prefixes of requests kept only when they fail
	// or are slow, never as part of the sample: static files, health
	// checks. "/__gofastr/" and "/core-ui/", the framework's own scripts
	// and styles, are always two. The Logs screen leaves these paths'
	// access lines out unless asked for them.
	QuietPaths []string
	// EnableMCP adds the MCP tools telemetry_requests, telemetry_request
	// and telemetry_logs, so a connected agent can read what the files
	// hold. Off by default; `gofastr dev` turns them on for a loopback
	// listener.
	EnableMCP bool
	// MCPGate replaces the precondition every tool runs behind,
	// framework.MCPRequireRole("admin") by default: the tools hand out
	// request paths, client addresses and log lines.
	MCPGate func(ctx context.Context) error
	// ExternalTracerProvider leaves the global OpenTelemetry
	// TracerProvider alone. Set it when the app builds its own (to
	// export spans elsewhere too) and add SpanProcessor to that one.
	ExternalTracerProvider bool
}

// Stats counts what the plugin has written and what it has dropped.
type Stats struct {
	// LogLines and LogBytes are the log lines written to day files.
	LogLines, LogBytes int64
	// Traces and TraceBytes are the traces written.
	Traces, TraceBytes int64
	// TracesSampledOut is the finished ordinary traces not kept.
	TracesSampledOut int64
	// TracesDropped is the traces given up unfinished: the span buffer
	// was full, or the request's span never ended.
	TracesDropped int64
	// SpansDropped is the spans past one trace's span limit.
	SpansDropped int64
	// LinesDropped is the lines refused: the disk cap was reached, a
	// write failed, or the line came before Init or after shutdown.
	LinesDropped int64
	// FilesRemoved is the day files deleted for age or for the cap.
	FilesRemoved int64
	// DaysSealed is the finished days packed into compressed blocks,
	// counted once for logs and once for traces.
	DaysSealed int64
}

// Plugin is the telemetry plugin. It implements framework.Plugin.
type Plugin struct {
	cfg       Config
	writer    atomic.Pointer[itel.Writer]
	collector atomic.Pointer[itel.Collector]
	processor atomic.Pointer[otelspan.Processor]
	store     atomic.Pointer[itel.Store]
	slow      atomic.Int64 // the resolved SlowThreshold
	quiet     []string     // set once in New
	unwritten atomic.Int64 // lines that arrived with no writer open
	stopped   atomic.Bool
}

// New returns the plugin. Register it before the log plugin, so the
// files are open when the first line is logged.
func New(cfg Config) *Plugin {
	return &Plugin{cfg: cfg, quiet: append([]string{"/__gofastr/", "/core-ui/"}, cfg.QuietPaths...)}
}

// QuietPaths returns the path prefixes whose requests are kept only
// when they fail or are slow: the framework's own "/__gofastr/" and
// "/core-ui/", and Config.QuietPaths.
func (p *Plugin) QuietPaths() []string { return slices.Clone(p.quiet) }

// Name implements framework.Plugin.
func (*Plugin) Name() string { return "telemetry" }

// Init opens the files and, unless ExternalTracerProvider is set,
// installs a TracerProvider that feeds them.
func (p *Plugin) Init(app *framework.App) error {
	dir := p.cfg.Dir
	if dir == "" {
		state, err := stateDir(app.Config.Name)
		if err != nil {
			return fmt.Errorf("telemetry: %w", err)
		}
		dir = filepath.Join(state, "telemetry")
	}
	cfg := itel.Config{
		Dir: dir, Retention: p.cfg.Retention, MaxDiskBytes: p.cfg.MaxDiskBytes,
		SampleRate: p.cfg.SampleRate, KeepNoOrdinary: p.cfg.KeepNoOrdinary,
		SlowThreshold: p.cfg.SlowThreshold, MaxMemoryBytes: p.cfg.MaxMemoryBytes,
	}
	w, err := itel.NewWriter(cfg)
	if err != nil {
		return err
	}
	c, err := itel.NewCollector(cfg, w.WriteTrace)
	if err != nil {
		_ = w.Close()
		return err
	}
	store, err := itel.Open(cfg)
	if err != nil {
		_ = w.Close()
		return err
	}
	if resolved, err := cfg.Resolve(); err == nil {
		p.slow.Store(int64(resolved.SlowThreshold))
	}
	p.store.Store(store)
	p.writer.Store(w)
	p.collector.Store(c)
	p.processor.Store(otelspan.New(c, p.quiet...))

	if err := p.registerMCPTools(app); err != nil {
		_ = w.Close()
		_ = store.Close()
		return err
	}

	var provider *sdktrace.TracerProvider
	if !p.cfg.ExternalTracerProvider {
		provider = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(p.SpanProcessor()))
		otel.SetTracerProvider(provider)
	}
	app.OnStop(func() error {
		p.stopped.Store(true)
		if provider != nil {
			// Nothing is queued: a span is recorded as it ends. The
			// bound is for the provider's own bookkeeping.
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = provider.Shutdown(ctx)
		}
		return errors.Join(w.Close(), store.Close())
	})
	return nil
}

func stateDir(appName string) (string, error) {
	if appName == "" {
		return "", fmt.Errorf("set Config.Dir or framework.AppConfig.Name: the default directory is named after the app")
	}
	if x := os.Getenv("XDG_STATE_HOME"); x != "" {
		return filepath.Join(x, appName), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", appName), nil
}

// Stats returns the counters. They are zero before Init.
func (p *Plugin) Stats() Stats {
	st := Stats{LinesDropped: p.unwritten.Load()}
	if w := p.writer.Load(); w != nil {
		ws := w.Stats()
		st.LogLines, st.LogBytes, st.Traces, st.TraceBytes = ws.LogLines, ws.LogBytes, ws.Traces, ws.TraceBytes
		st.LinesDropped += ws.Dropped
		st.FilesRemoved, st.DaysSealed = ws.FilesRemoved, ws.DaysSealed
	}
	if c := p.collector.Load(); c != nil {
		cs := c.Stats()
		st.TracesSampledOut, st.TracesDropped, st.SpansDropped = cs.SampledOut, cs.Dropped, cs.SpansDropped
		st.LinesDropped += cs.WriteErrors
	}
	return st
}

// Reader returns the store the framework's own screens and tools read
// the files through, or nil before Init. Its type is internal to this
// module: the query API is not part of this package's surface yet.
func (p *Plugin) Reader() *itel.Store { return p.store.Load() }

// SlowThreshold returns the duration at which a trace counts as slow:
// the configured one, or the default. It is zero before Init.
func (p *Plugin) SlowThreshold() time.Duration { return time.Duration(p.slow.Load()) }

// LogSink returns the sink that writes log lines to the day files. Pass
// it in the log plugin's Config.Sinks, alone or beside other sinks.
func (p *Plugin) LogSink() log.Sink { return logSink{p} }

type logSink struct{ p *Plugin }

func (s logSink) Write(entry []byte) error {
	w := s.p.writer.Load()
	if w == nil || s.p.stopped.Load() {
		s.p.unwritten.Add(1)
		if w == nil {
			return nil // before Init: counted, and no sink error to report
		}
		return log.ErrSinkClosed
	}
	return w.WriteLog(entry)
}

// Close does nothing: the plugin closes the files when the app stops.
func (logSink) Close() error { return nil }

// SpanProcessor returns the processor that records finished spans. Init
// installs it on a provider of its own; an app that builds its own
// TracerProvider sets ExternalTracerProvider and adds this to it.
func (p *Plugin) SpanProcessor() sdktrace.SpanProcessor { return spanProcessor{p} }

type spanProcessor struct{ p *Plugin }

func (spanProcessor) OnStart(context.Context, sdktrace.ReadWriteSpan) {}

func (s spanProcessor) OnEnd(span sdktrace.ReadOnlySpan) {
	if proc := s.p.processor.Load(); proc != nil && !s.p.stopped.Load() {
		proc.OnEnd(span)
	}
}

func (spanProcessor) Shutdown(context.Context) error   { return nil }
func (spanProcessor) ForceFlush(context.Context) error { return nil }
