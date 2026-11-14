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

`battery/telemetry/telemetryadmin` adds **Overview**, **Requests**,
**Issues** and **Logs** to the admin's sidebar, behind the admin's own gate:

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

In the admin, the four pages refuse a caller scoped to a tenant
(`tenant.GetTenantID(ctx) != ""`), as the admin's queue and roles pages
do: the files hold every tenant's requests and log lines together. The
MCP tools' default gate refuses the same callers. On the standalone
mount the rule is yours to write in `Authorize`.

`Authorize` is required. The pages show request paths, client addresses
and whatever the app logs, so there is no open default: `Init` fails
without one, and a caller it refuses gets a 403.

### What the screens show

- **Overview** counts every request of the last hour, 24 hours or 7
  days: how many, how many failed, how many were slow, and how long 95
  in 100 took. Three bar charts show those over the range, and a table
  gives each route's own figures, busiest first; a column heading
  sorts by that column (which fails most, which is slowest), and a
  route links to its requests. Each chart says its highest bar in
  words. See [Request counts](#request-counts). A second table lists
  background work (queue jobs, scheduled runs), which is counted apart.
  Under them is [the machine](#the-machine): how busy its CPU is, how much memory and
  disk are used, and what this app holds.
- **Requests** lists the kept requests, newest first: route, the path
  asked for, status, how long it took and when. Narrow it to failed or
  slow requests, to one route (click a route), or to a time range. A
  row's route opens that request. Paste a trace ID to open one request.
  A request no route took is listed as `GET (no route)`, and the home
  page's `GET /{$}` as `GET /`.
- **One request** says what went wrong when it failed, and for a panic
  shows the call stack from the function that panicked. It shows its
  steps as bars on a shared time axis
  (`ui.Waterfall`): each query and outbound call, when it started, how
  long it took, and which failed. Under them are the log lines the app
  wrote while handling it.
- **Issues** groups the failed requests of a time range by what went
  wrong, latest first: the error, the route, the status, how many times,
  and when it was last and first seen. The error lists that issue's
  requests, and **Open** goes to the latest of them. See
  [Issues](#issues).
- **Logs** lists log lines, newest first, by minimum level, by text and
  by time range. A line written inside a request links to that request.
  The search finds lines that hold every word typed, in any order and
  any letter case, so text copied off the page (`GET /search → 200`,
  `dur_ms=52`) finds its line. Wrap words in double quotes for an exact
  phrase.
- Overview, Requests and Issues say so at the top while an
  [alert](#alerts) is firing.

Every filter is in the query string, so a view can be bookmarked and
shared. A time range is one click, and it holds as you move between
the four pages. Each view has its own page title ("Failed requests",
"Request 7e22a54e"), and a request opened from a filtered list leads
back to that list, on the page it was opened from. The lists show 25 rows a page, with **Newer**, **Older** and
**Back to newest**.

The newest page of each list refreshes every ten seconds and says
"Live", when that is a short read: a range of a day or less, with no
route, issue or search text narrowing it. A week, everything kept or a
search is loaded once, since refreshing it would read the whole range
again every ten seconds. So does the Overview for the last hour and the last 24 hours;
the 7-day view, whose bars are two hours wide, stays as loaded. A page you have paged back on stays as it is.
A refresh never moves keyboard focus or drops selected text: the
region waits while you are in it (see
[reactivity](reactivity.md)).

The Logs page leaves out the access lines of quiet paths (scripts,
styles, anything in `Config.QuietPaths`), which would otherwise fill
every page; a link on the page shows them.

A search for a rare word has to read every kept line. After three
seconds the page shows what it found, says how far back it read, and
its Older link carries on from there.

### From a shell

`gofastr telemetry` reads the directory itself, so it works over SSH and
when the app is down:

```sh
gofastr telemetry stats --app shop --since 6h
gofastr telemetry logs --app shop --level warn --since 1h
gofastr telemetry requests --dir /var/lib/shop/telemetry --failed
gofastr telemetry requests --app shop --slow 500ms --route "GET /orders/{id}"
gofastr telemetry issues --app shop --since 24h
gofastr telemetry requests --app shop --issue 9c1f0a2b7d3e4f55
gofastr telemetry request 4bf92f3577b34da6a3ce929d0e0e4736 --app shop
```

`--app <name>` reads the default directory for that app name; `--dir`
names one. `--json` prints lines as stored, for `jq`. Text from the
files is scrubbed of terminal control bytes before it is printed.

### From an agent

`Config.EnableMCP` adds five MCP tools beside the log plugin's:

| Tool | Returns |
|------|---------|
| `telemetry_stats` | every request counted: totals, each route, and a series of up to 60 steps, with `requests`, `failed`, `slow`, `p50_us`, `p95_us`; `since_ts` (default 24 hours ago), `until_ts`, `route`, `limit` |
| `telemetry_issues` | failed requests grouped by what went wrong, latest first, each with a count, first and last seen and an example `trace_id`; `since_ts`, `until_ts`, `limit` |
| `telemetry_requests` | kept requests, newest first; `failed`, `slow`, `route`, `since_ts`, `until_ts`, `limit` |
| `telemetry_request` | one request by `trace_id`: its spans and its log lines |
| `telemetry_logs` | log lines; `level`, `text`, `since_ts`, `until_ts`, `limit` |

They run behind `Config.MCPGate`, by default
`framework.MCPRequireRole("admin")`. An argument a tool cannot read is
an error, never a filter quietly dropped. `telemetry_logs` gives up
after ten seconds and returns what it found with `stopped_at`; pass
that as `until_ts` to carry on. `gofastr dev` turns the tools on,
ungated, for a loopback listener.

## Request counts

The traces on disk are a sample: every failed and slow request, and one
in twenty of the rest. A rate or a percentile worked out from them would
be wrong. So the plugin also counts every request as it ends, kept or
not, and writes the counts to a third day file, one line a route a
minute:

```json
{"time":"2026-11-13T15:36:00Z","route":"POST /checkout","n":41,"failed":2,"slow":3,"sum_us":5912044,"h":[0,0,0,4,9,11,8,6,3]}
```

`n` is the requests that ended in that minute, `failed` and `slow` the
ones that failed or took `SlowThreshold` or longer, and `sum_us` their
total time. `h` counts them by how long they took, in buckets ending at
1, 2, 5, 10, 20, 50, 100, 200 and 500 ms and 1, 2, 5 and 10 s, with one
more for anything slower; empty buckets at the end are left off.

What follows from that:

- **The Overview is about a minute behind.** A minute's counts are
  written once it is over, at the next fifteen-second check. When the
  app stops, the minute in progress is written as far as it got.
- **p50 and p95 are right to within a bucket.**
  They are read from the buckets, not from each request's time, so 180
  ms may be anything from 100 to 200 ms. The screens show two digits for
  that reason. A request slower than ten seconds reads as "10 s or
  more".
- **Quiet paths are not counted.** Scripts, styles and anything in
  `Config.QuietPaths` are not the traffic a failure rate is about. Nor
  is a query a background loop ran outside any request.
- **Background work is counted apart.** A queue job (`process <type>`)
  or a scheduled run (`cron <name>`) is a line with `"job":true`. It is
  in none of the request totals, charts or routes; the Overview, `stats`
  and `telemetry_stats` list it separately, with runs in place of
  requests and no "slow", since nobody waits on it. A trace counts as
  background work when its first span is not a server's.
- **A minute counts at most 200 routes apart.** Routes are patterns
  (`GET /orders/{id}`), so an app has few. Past 200 in one minute the
  rest count together as `(other)`.
- **Each replica counts what it served.** The files are per machine.

A day at five busy routes is under a megabyte, and about a tenth of
that once sealed. The file is removed with its day, as the others are.

## Issues

An issue is every failed request with the same route, the same status
and the same error. Numbers, UUIDs and hex IDs in the error are set
aside first, so `card 4407 declined by issuer` and
`card 4370 declined by issuer` are one issue, shown as
`card N declined by issuer`.

The error of a failed request is the first of these that exists:

1. the error of its first failed step (a query, an outbound call);
2. the first line the app logged at ERROR while handling it: its
   `panic`, `err` or `error` attribute, else its message;
3. the request's own status text, which for a panic is the panic.

So a handler that logs `slog.ErrorContext(ctx, "payment failed", "err", err)`
before answering 500 gets an issue named after `err`, not after
"Internal Server Error". Pass the request's context: a line logged
without it belongs to no request.

### Where it failed

A request that panics keeps the call stack, cut to start at the function
that panicked, and at most 8 KiB of it. The request's page shows it
under "Stack"; `gofastr telemetry request` prints it and
`telemetry_request` returns it as `stack`.

The stack comes from whichever caught the panic. The log plugin's
recovery logs it with the request's trace ID, and the plugin reads it
from that line. Without the log plugin, the tracing middleware puts it
on the request's span as `exception.stacktrace`, OpenTelemetry's name
for it, so an exporter of your own gets it too. A span that records an
error with OpenTelemetry's stack-trace option turned on is read the same
way. An error that is returned, not panicked, has no stack unless your
code records one.

Issues are worked out from the kept traces each time they are asked
for; nothing else is stored, and nothing is marked resolved. An issue
that stops happening leaves the list when its range no longer covers
it. At most 2,000 different issues are counted in one range; the page
says how many failures fit none of them.

## Alerts

The plugin counts failed and slow requests as they end and checks its
rules every fifteen seconds. With no rules of your own there are two:
ten failed requests within five minutes, and the disk 90% full.

<!-- gofastr:compile
stmt: _ = tel
-->
```go
import (
    "os"
    "time"

    "github.com/DonaldMurillo/gofastr/battery/telemetry"
)

tel := telemetry.New(telemetry.Config{
    Alerts: []telemetry.AlertRule{
        {Name: "checkout failing", Kind: telemetry.AlertFailed, AtLeast: 5, Within: 5 * time.Minute, Route: "POST /checkout"},
        {Name: "slow requests", Kind: telemetry.AlertSlow, AtLeast: 50, Within: 10 * time.Minute},
    },
    Notify: telemetry.WebhookNotifier(os.Getenv("ALERT_WEBHOOK_URL")),
})
```

A rule fires when `AtLeast` requests of its `Kind` ended within
`Within` (one minute to one hour; five minutes when zero), on `Route`
when one is set. It resolves when the count has fallen to half of
`AtLeast` or less, so a count hovering at the line does not fire again
and again. `Init` fails on a rule it cannot check.

A rule of kind `telemetry.AlertDisk` watches the disk the telemetry
directory is on. Its `AtLeast` is a percentage from 1 to 100, and it
takes no `Within` and no `Route`. It fires when the disk is that full
and resolves five points lower. The files this plugin writes are capped;
a database, an upload directory and the system's own logs are not, and
on a small machine they are what fills the disk. Rules of your own
replace the defaults, so add a disk rule to them if you want to keep it:

<!-- gofastr:compile
stmt: _ = rule
-->
```go
import (
    "github.com/DonaldMurillo/gofastr/battery/telemetry"
)

rule := telemetry.AlertRule{Name: "disk nearly full", Kind: telemetry.AlertDisk, AtLeast: 85}
```

Each change does three things:

- it is logged as `telemetry.alert`, at ERROR when firing and INFO when
  resolved, so it is in the Logs page and the day file;
- Overview, Requests and Issues show it at the top while it is firing;
- `Config.Notify` is called, when set. It gets fifteen seconds; an error
  or a panic in it is logged and the next check still runs.

`telemetry.WebhookNotifier(url)` posts the alert as JSON with the
sentence in both `text` and `content`, so a Slack, Mattermost, Teams or
Discord incoming-webhook URL works as it is. The details are under
`alert`: `rule`, `kind`, `route`, `firing`, `count`, `at_least`,
`within_seconds`, `at`, `since`. It does not follow redirects, and its
errors name the host only, since the URL of a chat webhook is its
secret. For mail or a pager, write the function yourself; `a.Text()` is
the sentence.

`tel.Firing()` returns the rules firing now. `NoAlerts` turns alerts
off, the default rules included.

### When the app is down

None of the above tells you the app has stopped: a process that is not
running sends nothing. `Config.Heartbeat` covers that from the other
side. Give it the check-in URL of a monitor that alerts when check-ins
stop (Healthchecks, Cronitor, Better Stack), and the app asks for that
URL once a minute while it runs:

<!-- gofastr:compile
stmt: _ = tel
-->
```go
import (
    "os"

    "github.com/DonaldMurillo/gofastr/battery/telemetry"
)

tel := telemetry.New(telemetry.Config{
    Heartbeat: os.Getenv("HEARTBEAT_URL"),
})
```

It is a plain GET with a ten-second limit that follows no redirect. An
answer outside 2xx is a missed check-in. The app logs when check-ins
start failing and when they work again, once each way, naming the host
and never the URL, which is the monitor's secret. `Init` fails on a
value that is not an http or https URL.

The counts are kept in memory, per process. A restart starts them from
zero, and each replica counts the requests it served itself, so with
three replicas a rule can fire on one of them while the other two stay
quiet.

## The machine

Once a minute the plugin reads the machine it runs on and writes one
line to a fourth day file:

```json
{"time":"2026-11-13T18:42:00Z","cpu":0.3712,"mem_used":2254857830,"mem_total":4294967296,"disk_used":24696061952,"disk_total":26843545600,"goroutines":42,"heap":31457280,"rss":83886080}
```

| Field | Is |
|-------|----|
| `cpu` | the share of all cores that were busy since the reading before, 0 to 1 |
| `mem_used`, `mem_total` | memory in bytes; used is the total less what the kernel says is available |
| `disk_used`, `disk_total` | the filesystem the telemetry directory is on; space only root may use counts as used |
| `goroutines`, `heap`, `rss` | this process: its goroutines, the Go heap in use, and the memory it holds |

The Overview shows the latest reading and charts CPU and memory over
its time range. `gofastr telemetry stats` says it in a sentence, and
`telemetry_stats` returns it as `machine` with the peaks of the range.

- **CPU, memory and `rss` are read from `/proc`, so Linux only.**
  Elsewhere the line carries the process's goroutines and heap, and the
  disk on macOS and FreeBSD. A field the platform does not give is left
  out, and the Overview shows a dash for it.
- **It is the whole machine, not the app's share.** In a container the
  kernel reports the host's CPU and memory. `rss` is the app's own.
- **A reading is a moment.** Memory and disk are as they were at that
  second; CPU is the mean of the minute before it.

A day is about 300 KB. The file is sealed and removed with its day.

## What is written

Files go in `Config.Dir`, by default `telemetry/` under the app's state
directory (`$XDG_STATE_HOME/<app>` or `~/.local/state/<app>`). Today's
lines go to one plain file per kind, owner-readable only:

| File | One line is |
|------|-------------|
| `logs-YYYYMMDD.jsonl` | a log entry, as [log](log.md) describes it |
| `traces-YYYYMMDD.jsonl` | one complete trace, written when its request ends |
| `stats-YYYYMMDD.jsonl` | one route's [request counts](#request-counts) for one minute |
| `host-YYYYMMDD.jsonl` | one reading of [the machine](#the-machine) |

When a UTC day is over, its files are **sealed** in the background: the
lines are packed into gzip blocks of about 1 MiB and the plain file is
removed. A sealed day is about a seventh of its plain size.

| File | Holds |
|------|-------|
| `logs-YYYYMMDD.jsonl.gz`, `traces-YYYYMMDD.jsonl.gz`, `stats-YYYYMMDD.jsonl.gz`, `host-YYYYMMDD.jsonl.gz` | the same lines. Each is one valid gzip stream: `zcat logs-20261112.jsonl.gz \| grep ...` works |
| `traces-YYYYMMDD.heads.gz` | each trace's summary, so a list of requests never unpacks a span |
| `logs-YYYYMMDD.idx`, `traces-YYYYMMDD.idx`, `stats-YYYYMMDD.idx`, `host-YYYYMMDD.idx` | where each block is, the times it covers, and which block holds each trace ID |

The index is written last. A day without one is read from its plain
file, so an app killed in the middle of sealing loses nothing and
finishes the job on its next start. An index that is lost or damaged is
rebuilt from the blocks. Sealing rests between blocks, so on one core it
leaves two thirds of the time to requests, and it stops at the next
block when the app shuts down.

A log line written inside a traced request carries `trace_id` and
`span_id`, so `grep <trace_id> logs-*.jsonl` reads one request's lines.
A trace line starts with its summary and then lists its spans:

```json
{"time":"...","trace_id":"4bf92f3577b34da6a3ce929d0e0e4736","route":"GET /orders/{id}","path":"/orders/42","status":200,"dur_us":18412,"error":false,"failed_steps":1,"err":"pq: deadlock detected","spans":[{"span_id":"00f067aa0ba902b7","parent_id":"","name":"HTTP GET GET /orders/{id}","start_us":0,"dur_us":18412,"attrs":{"http.status_code":"200"}},{"span_id":"53995c3f42cd8ad8","parent_id":"00f067aa0ba902b7","name":"SELECT","start_us":310,"dur_us":2104,"error":true}]}
```

`error` says the request as a whole failed. `failed_steps` counts the
spans under it that failed, whether or not the request did: a query
whose error the handler dealt with. `err` is what went wrong, as
[Issues](#issues) picks it, cut to 200 bytes. A trace that failed with a
known stack ends with `"stack":"..."`, after its spans. `path`,
`failed_steps`, `err` and `stack` are left out when empty, and a span's
`error` when false.

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
  only when it fails or is slow. `/__gofastr/` and `/core-ui/`, the
  framework's own scripts and styles, are always two; add your static
  files and health check.
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
| `Alerts` | ten failed in five minutes; disk 90% full | the rules checked every fifteen seconds |
| `Notify` | none | who is told when a rule fires or resolves |
| `NoAlerts` | false | turn alerts off |
| `Heartbeat` | none | a URL asked for once a minute, for a monitor that alerts when the app stops |
| `EnableMCP` | false | the five MCP tools |
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

A trace longer than a megabyte as written (hundreds of steps with long
attributes) is kept without its attributes, and without its later
steps if that is not enough: a line that long is one no reader returns.

`tel.Stats()` reports what was written, every kind of drop, how many
traces were trimmed (`TracesTrimmed`), and how many days have been
sealed.

## Cost

Measured through the default middleware and the log plugin, for a
request that logs 3 lines and makes 8 spans, on one CPU: writing the log
lines to the day file adds about 4 µs to a request, and tracing adds
about 26 µs more, most of it the OpenTelemetry SDK creating the spans.
Counting the request for the [request counts](#request-counts) adds
about 50 ns and no allocation.
A day at 20 requests a second is about 1.7 GB while it is today's and
about 240 MB once sealed, so the default 5 GiB cap holds about 13 sealed
days beside today. Sealing a day took about 15 seconds of CPU.

Measured at that rate with finished days sealed, on one CPU: the newest
log lines and one request's log lines come back in about 5 ms, a request
opened by trace ID in about 80 ms, a day's slow requests in about
100 ms, and a text search of the last day in a little over a second.
Searching sealed days means unpacking them, at about 5 seconds a day;
that is the search the three-second budget above is for.

The Overview's widest read, seven days of thirty routes with six of the
days sealed, took about 350 ms.

### On a small machine

The Meridian example, with everything on this page turned on, run in a
container held to one CPU and 1 GB of memory and asked for its
server-rendered pages:

| Load | Memory held | CPU | Slowest 1 in 100 |
|------|-------------|-----|------------------|
| idle | 23 to 29 MB | about 1% | |
| 20 requests a second | 26 MB | about 10% of the core | 5.7 ms |
| 200 requests a second | 30 MB | about 72% of the core | 4.9 ms |

Those are the whole app's figures, not telemetry's share. Telemetry's
own share, measured by running the same app with it off: about 32 µs of
CPU a request and half a megabyte of memory.

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
- **Sizing `MaxDiskBytes` without counting today.** Today's files are
  plain until the day is over, about seven times a sealed day. At 20
  requests a second the default 5 GiB holds today and about 13 sealed
  days; when the cap and `Retention` disagree, the cap wins.
- **Grepping only `*.jsonl`.** Finished days are `*.jsonl.gz`. Use
  `zcat`, or `gofastr telemetry logs --grep`, which reads both.
- **Looking for an ordinary request that is not there.** Nineteen in
  twenty are sampled out by default. Raise `SampleRate`, or find its log
  lines on the Logs page, which keeps every line.
- **Mounting the screens with `Authorize` that returns true.** That
  publishes every request path, client address and log line to anyone
  who finds the URL.
