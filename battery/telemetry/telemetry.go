// Package telemetry keeps an app's logs and traces in capped plain files on its own machine.
package telemetry

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
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

// Config holds the plugin's settings.
type Config struct {
	// Dir is where the files go.
	Dir string
	// Retention is how long a day's files are kept.
	Retention time.Duration
	// MaxDiskBytes caps the directory's size.
	MaxDiskBytes int64
	// SampleRate is the share of ordinary traces kept, from 0 to 1.
	SampleRate float64
	// KeepNoOrdinary keeps only failed and slow traces.
	KeepNoOrdinary bool
	// SlowThreshold is how long a trace takes to count as slow.
	SlowThreshold time.Duration
	// MaxMemoryBytes caps the spans held for requests still running.
	MaxMemoryBytes int64
	// QuietPaths are path prefixes whose requests are kept only when they fail or are slow.
	QuietPaths []string
	// Alerts are the rules checked every fifteen seconds.
	Alerts []AlertRule
	// Notify is told when a rule starts firing and when it resolves.
	Notify func(ctx context.Context, a Alert) error
	// NoAlerts turns alerts off, the default rules included.
	NoAlerts bool
	// Heartbeat is a monitor's check-in URL the app asks for once a minute.
	Heartbeat string
	// EnableMCP adds the telemetry_* MCP tools.
	EnableMCP bool
	// MCPGate replaces the tools' default gate, an admin with no tenant.
	MCPGate func(ctx context.Context) error
	// ExternalTracerProvider leaves the global OpenTelemetry TracerProvider
	// alone.
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
	// TracesDropped is the traces given up unfinished.
	TracesDropped int64
	// SpansDropped is the spans past one trace's span limit.
	SpansDropped int64
	// LinesDropped is the lines refused.
	LinesDropped int64
	// FilesRemoved is the day files deleted for age or for the cap.
	FilesRemoved int64
	// DaysSealed is the finished days packed into compressed blocks.
	DaysSealed int64
	// TracesTrimmed is the traces written cut down to fit a megabyte.
	TracesTrimmed int64
}

// Plugin is the telemetry plugin.
type Plugin struct {
	cfg           Config
	writer        atomic.Pointer[itel.Writer]
	collector     atomic.Pointer[itel.Collector]
	processor     atomic.Pointer[otelspan.Processor]
	store         atomic.Pointer[itel.Store]
	alerts        atomic.Pointer[itel.Alerts]
	heartbeat     *url.URL
	heartbeatDown bool
	run           context.Context
	inited        atomic.Bool
	notifyTimeout time.Duration
	readHost      func(time.Time) itel.HostReading
	hostMinute    int64
	latestHost    atomic.Pointer[itel.HostReading]
	slow          atomic.Int64
	quiet         []string
	unwritten     atomic.Int64
	stopped       atomic.Bool
}

// New returns the plugin.
func New(cfg Config) *Plugin {
	return &Plugin{cfg: cfg, quiet: append([]string{"/__gofastr/", "/core-ui/"}, cfg.QuietPaths...)}
}

// QuietPaths returns the framework's quiet prefixes and Config.QuietPaths.
func (p *Plugin) QuietPaths() []string { return slices.Clone(p.quiet) }

// Name implements framework.Plugin.
func (*Plugin) Name() string { return "telemetry" }

// Init opens the files and installs a TracerProvider unless told not to.
func (p *Plugin) Init(app *framework.App) error {
	if !p.inited.CompareAndSwap(false, true) {
		return errors.New("telemetry: Init was already called on this plugin")
	}
	beat, err := parseHeartbeat(p.cfg.Heartbeat)
	if err != nil {
		return err
	}
	p.heartbeat = beat
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
	alerts, err := p.newAlerts()
	if err != nil {
		_ = w.Close()
		_ = store.Close()
		return err
	}
	if alerts != nil {
		c.WatchAlerts(alerts)
		p.alerts.Store(alerts)
	}
	counts := itel.NewRollup()
	c.WatchStats(counts)
	if resolved, err := cfg.Resolve(); err == nil {
		p.readHost = itel.NewHostSampler(resolved.Dir).Read
	}
	if resolved, err := cfg.Resolve(); err == nil {
		p.slow.Store(int64(resolved.SlowThreshold))
	}
	p.store.Store(store)
	p.writer.Store(w)
	p.collector.Store(c)
	p.processor.Store(otelspan.New(c, p.quiet...))

	if err := p.registerMCPTools(app); err != nil {
		p.store.Store(nil)
		p.writer.Store(nil)
		p.collector.Store(nil)
		p.processor.Store(nil)
		p.alerts.Store(nil)
		_ = w.Close()
		_ = store.Close()
		return err
	}

	var provider *sdktrace.TracerProvider
	if !p.cfg.ExternalTracerProvider {
		provider = sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(p.SpanProcessor()))
		otel.SetTracerProvider(provider)
	}
	stopAlerts, alertsDone := make(chan struct{}), make(chan struct{})
	run, stopRun := context.WithCancel(context.Background())
	p.run = run
	go p.watch(app, w, counts, alerts, stopAlerts, alertsDone)
	app.OnStop(func() error {
		p.stopped.Store(true)
		stopRun()
		close(stopAlerts)
		<-alertsDone
		if provider != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = provider.Shutdown(ctx)
		}
		flushed := w.FlushStats(counts, true)
		return errors.Join(flushed, w.Close(), store.Close())
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

// Stats returns the counters.
func (p *Plugin) Stats() Stats {
	st := Stats{LinesDropped: p.unwritten.Load()}
	if w := p.writer.Load(); w != nil {
		ws := w.Stats()
		st.LogLines, st.LogBytes, st.Traces, st.TraceBytes = ws.LogLines, ws.LogBytes, ws.Traces, ws.TraceBytes
		st.LinesDropped += ws.Dropped
		st.FilesRemoved, st.DaysSealed, st.TracesTrimmed = ws.FilesRemoved, ws.DaysSealed, ws.Trimmed
	}
	if c := p.collector.Load(); c != nil {
		cs := c.Stats()
		st.TracesSampledOut, st.TracesDropped, st.SpansDropped = cs.SampledOut, cs.Dropped, cs.SpansDropped
		st.LinesDropped += cs.WriteErrors
	}
	return st
}

// Reader returns the store the screens and tools read, or nil before Init.
func (p *Plugin) Reader() *itel.Store { return p.store.Load() }

// Host returns the latest machine reading, or nil before the first.
func (p *Plugin) Host() *itel.HostReading { return p.latestHost.Load() }

// SlowThreshold returns the slow-trace line, or zero before Init.
func (p *Plugin) SlowThreshold() time.Duration { return time.Duration(p.slow.Load()) }

// LogSink returns the sink that writes log lines to the day files.
func (p *Plugin) LogSink() log.Sink { return logSink{p} }

type logSink struct{ p *Plugin }

func (s logSink) Write(entry []byte) error {
	w := s.p.writer.Load()
	if w == nil || s.p.stopped.Load() {
		s.p.unwritten.Add(1)
		if w == nil {
			return nil
		}
		return log.ErrSinkClosed
	}
	if bytes.Contains(entry, []byte(`"level":"ERROR`)) {
		if c := s.p.collector.Load(); c != nil {
			if id, text, stack, ok := itel.LoggedError(entry); ok {
				c.NoteError(id, text, stack)
			}
		}
	}
	return w.WriteLog(entry)
}

// Close does nothing: the plugin closes the files when the app stops.
func (logSink) Close() error { return nil }

// SpanProcessor returns the processor that records finished spans.
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
