# Logs and traces on disk (experimental)

`battery/telemetry` keeps an app's logs and traces in plain files on the
machine the app runs on, and shows them: a list of requests, each
request's steps on a time axis with the log lines it wrote, and the log
itself. There is no collector, agent or database beside the app, and the
files stay inside a disk and memory budget you set. The defaults are
sized for a 1 vCPU, 1 GB host.

The package is **Experimental** in the stability manifest. The file
layout and the API can change without a major version bump.

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

Open the database with `tracing.OpenDB` (see
[observability](observability.md)) and each query becomes a step of the
request that ran it.

`Init` installs an OpenTelemetry TracerProvider. If the app builds its
own, to export spans somewhere else as well, set
`Config.ExternalTracerProvider` and add `tel.SpanProcessor()` to that
provider before creating the app.

## Look at it

There are three ways in. All of them read the same files.

### In the admin

`battery/telemetry/telemetryadmin` adds **Requests** and **Logs** to the
admin's sidebar, behind the admin's own gate:

<!-- gofastr:compile
stmt: _ = admin.New(admin.Config{Pages: telemetryadmin.Pages(tel, "/admin")})
var tel *telemetry.Plugin
-->
```go
import (
    "github.com/DonaldMurillo/gofastr/battery/admin"
    "github.com/DonaldMurillo/gofastr/battery/telemetry"
    "github.com/DonaldMurillo/gofastr/battery/telemetry/telemetryadmin"
)

admin.New(admin.Config{
    PathPrefix: "/admin",
    Pages:      telemetryadmin.Pages(tel, "/admin"),
})
```

The second argument is the admin's `PathPrefix`: the pages link to each
other under it.

### On their own

An app with no admin mounts the same screens with
`battery/telemetry/telemetryui`. It needs the app's UI host, and it
needs to be told who may look:

<!-- gofastr:compile
stmt: app.RegisterPlugin(telemetryui.New(tel, telemetryui.Config{Path: "/telemetry", Authorize: isOperator}))
var app *framework.App
var tel *telemetry.Plugin
var isOperator func(context.Context) bool
-->
```go
import (
    "context"

    "github.com/DonaldMurillo/gofastr/battery/telemetry"
    "github.com/DonaldMurillo/gofastr/battery/telemetry/telemetryui"
    "github.com/DonaldMurillo/gofastr/framework"
)

app.RegisterPlugin(telemetryui.New(tel, telemetryui.Config{
    Path:      "/telemetry",
    Authorize: isOperator, // func(context.Context) bool
}))
```

`Authorize` is required. The pages show request paths, client addresses
and whatever the app logs, so there is no open default: `Init` fails
without one, and a caller it refuses gets a 403.

### What the screens show

- **Requests** lists the kept requests, newest first: route, the path
  asked for, status, how long it took and when. Narrow it to failed or
  slow requests, to one route (click a route), or to a time range. Paste
  a trace ID to open one request.
- **One request** shows its steps as bars on a shared time axis
  (`ui.Waterfall`): each query and outbound call, when it started, how
  long it took, and which failed. Under them are the log lines the app
  wrote while handling it.
- **Logs** lists log lines, newest first, by minimum level, by text and
  by time range. A line written inside a request links to that request.

Every filter is in the query string, so a view can be bookmarked and
shared. The pages do not refresh on their own yet: reload for newer
lines.

### From a shell

`gofastr telemetry` reads the directory itself, so it works over SSH and
when the app is down:

```sh
gofastr telemetry logs --app shop --level warn --since 1h
gofastr telemetry requests --dir /var/lib/shop/telemetry --failed
gofastr telemetry requests --app shop --slow 500ms --route "GET /orders/{id}"
gofastr telemetry request 4bf92f3577b34da6a3ce929d0e0e4736 --app shop
```

`--app <name>` reads the default directory for that app name; `--dir`
names one. `--json` prints lines as stored, for `jq`. Text from the
files is scrubbed of terminal control bytes before it is printed.

### From an agent

`Config.EnableMCP` adds three MCP tools beside the log plugin's:

| Tool | Returns |
|------|---------|
| `telemetry_requests` | kept requests, newest first; `failed`, `slow`, `route`, `since_ts`, `until_ts`, `limit` |
| `telemetry_request` | one request by `trace_id`: its spans and its log lines |
| `telemetry_logs` | log lines; `level`, `text`, `since_ts`, `until_ts`, `limit` |

They run behind `Config.MCPGate`, by default
`framework.MCPRequireRole("admin")`. An argument a tool cannot read is
an error, never a filter quietly dropped. `gofastr dev` turns the tools
on, ungated, for a loopback listener.

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
{"time":"...","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","route":"GET /orders/{id}","path":"/orders/42","status":200,"dur_us":18412,"error":false,"failed_steps":1,"spans":[{"span_id":"00f067aa0ba902b7","parent_id":"","name":"HTTP GET GET /orders/{id}","start_us":0,"dur_us":18412,"attrs":{"http.status_code":"200"}},{"span_id":"53995c3f42cd8ad8","parent_id":"00f067aa0ba902b7","name":"SELECT","start_us":310,"dur_us":2104,"error":true}]}
```

`error` says the request as a whole failed. `failed_steps` counts the
spans under it that failed, whether or not the request did: a query
whose error the handler dealt with. `path` and `failed_steps` are left
out when empty, and a span's `error` when false.

Each line is written straight to the file, with no buffer in between. A
line that was written survives the process being killed; a trace whose
request was still running does not.

## Which traces are kept

The decision waits until the request ends, so it can look at the whole
trace:

- a request that failed is kept: its own span failed, which the tracing
  middleware does for a 5xx or a panic, and the queue for a job that
  returned an error;
- a request that took `SlowThreshold` or longer is kept;
- of the rest, `SampleRate` are kept, chosen from the trace ID so every
  process that sees the trace decides the same way.

Set `KeepNoOrdinary` to keep only the first two. Two kinds of work are
never part of the sample:

- **Quiet paths.** A request under one of `Config.QuietPaths` is kept
  only when it fails or is slow. `/__gofastr/`, the framework's own
  scripts and styles, is always one; add your static files and health
  check.
- **Stray calls.** A query or outbound call made outside any request or
  job (a poll loop) is kept only when it is slow.

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
| `QuietPaths` | none | path prefixes kept only when failed or slow |
| `EnableMCP` | false | the three MCP tools |
| `MCPGate` | the admin role | who may call them |
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

Queries read the files directly, with no index. Over three days at that
rate, on one CPU: the newest log lines and one request's log lines come
back in a few milliseconds, a day's slow requests in about 100 ms, and a
text search of one day in about a second. Opening a request by trace ID
reads every trace file until it finds it, about 75 ms a day of files.

## Common mistakes

- **Registering the plugin after the log plugin.** The files open in
  `Init`. A line logged before that is dropped and shows up in
  `Stats().LinesDropped`.
- **Leaving out `framework.WithTracing()`.** Logs are still written, but
  no request gets a span, so the Requests list stays empty and log lines
  carry no `trace_id`.
- **Logging without the request's context.** `logger.Info(...)` has no
  span to read. Use `logger.InfoContext(r.Context(), ...)` so the line
  names its trace and shows on its request's page.
- **Calling `otel.SetTracerProvider` after the plugin's `Init`.** That
  replaces the provider the plugin installed and traces stop. Set
  `ExternalTracerProvider` and add `tel.SpanProcessor()` to your own
  provider.
- **Sizing `MaxDiskBytes` from the default.** Days are not compressed
  yet. At 20 requests a second the default 5 GiB holds about three days,
  not the 14 that `Retention` allows; the cap wins.
- **Looking for an ordinary request that is not there.** Nineteen in
  twenty are sampled out by default. Raise `SampleRate`, or find its log
  lines on the Logs page, which keeps every line.
- **Mounting the screens with `Authorize` that returns true.** That
  publishes every request path, client address and log line to anyone
  who finds the URL.
