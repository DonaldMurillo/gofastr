# Logs and traces on disk (experimental)

`battery/telemetry` keeps an app's logs and traces in plain files on the
machine the app runs on. There is no collector, agent or database beside
the app, and the files stay inside a disk and memory budget you set. The
defaults are sized for a 1 vCPU, 1 GB host.

The package is **Experimental** in the stability manifest. The file
layout and the API can change without a major version bump. Nothing
reads the files for you yet: the admin pages and MCP tools that browse
them come next. Until then they are JSON lines, so `grep` and `jq` work.

## Turn it on

<!-- gofastr:compile
stmt: _ = app
-->
```go
import (
    "github.com/DonaldMurillo/gofastr/battery/log"
    "github.com/DonaldMurillo/gofastr/battery/telemetry"
    "github.com/DonaldMurillo/gofastr/framework"
)

tel := telemetry.New(telemetry.Config{})
app := framework.NewApp(
    framework.WithConfig(framework.AppConfig{Name: "shop"}),
    framework.WithTracing(),
)
app.RegisterPlugin(tel) // before the log plugin
app.RegisterPlugin(log.New(log.Config{Sinks: []log.Sink{tel.LogSink()}}))
```

Three things make it work:

- `framework.WithTracing()` gives every request a span. Without it there
  are no request traces, only the spans your own code starts.
- `tel.LogSink()` sends log lines to the day files. Add other sinks
  beside it if you want them.
- Registering `tel` first opens the files before the log plugin writes
  its first line. A line that arrives earlier is dropped and counted.

`Init` installs an OpenTelemetry TracerProvider. If the app builds its
own, to export spans somewhere else as well, set
`Config.ExternalTracerProvider` and add `tel.SpanProcessor()` to that
provider before creating the app.

## What is written

Files go in `Config.Dir`, by default `telemetry/` under the app's state
directory (`$XDG_STATE_HOME/<app>` or `~/.local/state/<app>`). There is
one file per kind per UTC day, owner-readable only:

| File | One line is |
|------|-------------|
| `logs-YYYYMMDD.jsonl` | a log entry, as [log](log.md) describes it |
| `traces-YYYYMMDD.jsonl` | one complete trace, written when its request ends |

A log line written inside a traced request carries `trace_id` and
`span_id`, so `grep <trace_id> logs-*.jsonl` reads one request's lines.
A trace line starts with its summary and then lists its spans:

```json
{"time":"...","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","route":"GET /orders/{id}","status":200,"dur_us":18412,"error":false,"spans":[{"span_id":"00f067aa0ba902b7","parent_id":"","name":"HTTP GET GET /orders/{id}","start_us":0,"dur_us":18412,"attrs":{"http.status_code":"200"}},{"span_id":"53995c3f42cd8ad8","parent_id":"00f067aa0ba902b7","name":"SELECT","start_us":310,"dur_us":2104}]}
```

Each line is written straight to the file, with no buffer in between. A
line that was written survives the process being killed; a trace whose
request was still running does not.

## Which traces are kept

The decision waits until the request ends, so it can look at the whole
trace:

- a trace with a failed span is kept;
- a trace that took `SlowThreshold` or longer is kept;
- of the rest, `SampleRate` are kept, chosen from the trace ID so every
  process that sees the trace decides the same way.

Set `KeepNoOrdinary` to keep only the first two.

## Settings

| Field | Default | What it bounds |
|-------|---------|----------------|
| `Dir` | `telemetry/` in the state directory | where the files go |
| `Retention` | 14 days | how long a day's files are kept |
| `MaxDiskBytes` | 5 GiB | the directory's size |
| `SampleRate` | 0.05 | the share of ordinary traces kept |
| `KeepNoOrdinary` | false | keep failed and slow traces only |
| `SlowThreshold` | 500 ms | when a trace counts as slow |
| `MaxMemoryBytes` | 32 MiB | spans held for requests still running |
| `ExternalTracerProvider` | false | leave the global TracerProvider alone |

When a limit is reached, data is dropped and counted, never queued:

- **Disk.** Days past `Retention` are removed, then the oldest days while
  the directory is over `MaxDiskBytes`. If today's files alone fill the
  cap, new lines are refused until the day turns. The cap is checked
  every 8 MiB written, so the directory can pass it by that much.
- **Memory.** When the spans of unfinished requests pass
  `MaxMemoryBytes`, the oldest unfinished trace is dropped. A trace
  whose request has not ended after ten minutes is dropped too.

`tel.Stats()` reports what was written and every kind of drop.

## Cost

Measured through the default middleware and the log plugin, for a
request that logs 3 lines and makes 8 spans, on one CPU: writing the log
lines to the day file adds about 4 µs to a request, and tracing adds
about 26 µs more, most of it the OpenTelemetry SDK creating the spans.
A day at 20 requests a second is about 1.6 GB of files; days are not
compressed yet, so the default cap holds about three days at that rate.

## Common mistakes

- **Registering the plugin after the log plugin.** The files open in
  `Init`. A line logged before that is dropped and shows up in
  `Stats().LinesDropped`.
- **Leaving out `framework.WithTracing()`.** Logs are still written, but
  no request gets a span, so the trace file holds only the spans your
  own code starts and log lines carry no `trace_id`.
- **Logging without the request's context.** `logger.Info(...)` has no
  span to read. Use `logger.InfoContext(r.Context(), ...)` so the line
  names its trace.
- **Calling `otel.SetTracerProvider` after the plugin's `Init`.** That
  replaces the provider the plugin installed and traces stop. Set
  `ExternalTracerProvider` and add `tel.SpanProcessor()` to your own
  provider.
- **Sizing `MaxDiskBytes` from the default.** Days are not compressed
  yet. At 20 requests a second the default 5 GiB holds about three days,
  not the 14 that `Retention` allows; the cap wins.
