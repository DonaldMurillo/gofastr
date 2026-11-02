# Changelog

All notable changes to GoFastr. Follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) with semver-ish
calendar versions (`YYYY-MM-DD` per substantive release until the API
stabilises). Breaking changes are clearly marked with **BREAKING**.

Older releases live in [`changelog/`](changelog/), ten minor versions per
file: [v0.80–v0.89](changelog/v0.80-v0.89.md),
[v0.70–v0.79](changelog/v0.70-v0.79.md),
[v0.60–v0.69](changelog/v0.60-v0.69.md),
[v0.50–v0.59](changelog/v0.50-v0.59.md),
[v0.40–v0.49](changelog/v0.40-v0.49.md),
[v0.30–v0.39](changelog/v0.30-v0.39.md),
[v0.20–v0.29](changelog/v0.20-v0.29.md),
[v0.10–v0.19](changelog/v0.10-v0.19.md) and
[v0.3–v0.9](changelog/v0.3-v0.9.md). This file holds Unreleased and the
current ten. When a v0.N0.0 release ships, the ten before it move to
`changelog/` unchanged except for repo-relative links, which gain `../`;
`TestChangelogArchiveLayout` in `internal/upgrade` fails until they do.

Keep each entry to what changed and what a reader must do about it, in a
few lines, and link the PR or the docs page for the rest.

## [Unreleased]

### Added
- **`ui.DatePicker`** ([issue 610](https://github.com/DonaldMurillo/gofastr/issues/610)).
  A calendar that stays on the page beside the native date input it
  fills, for a range in one control, refused days, or notes on days. The
  native input is the value and posts without script; the
  `headless-datepicker` module owns the date-grid keyboard, and a month
  change is an island the page serves with `ui.DatePickerMonth`. An
  ordinary date stays `ui.DateField`. Built on `headless.DatePicker`;
  `core/i18n` gains `DateYearMonthLong`, `WeekdayName`, `WeekdayAbbr` and
  `FirstWeekday` for it. See `form-module.md`, "When the native picker is
  not enough".
- **Untrusted process modules can be hosted by an app** ([#608](https://github.com/DonaldMurillo/gofastr/issues/608)).
  `RegisterProcessModule` now builds the sandbox runner itself for a
  `TrustUntrusted` descriptor, probing the backend once per App; before,
  no app could pass one and every untrusted registration failed.
  `WithSandboxBackend` overrides the host's built-in backend,
  `App.ProcessModuleSandbox()` lets a host check before it registers, and
  `SandboxConformanceError` carries the report. The probe child is the
  app's own binary, answered by the framework's package `init`: there is
  no hook to add to `main`, and `GOFASTR_SANDBOX_PROBE` is now reserved.
  `ProbeResult.Reported` says whether a probe child ran the probe body.
  `examples/processmodule-demo/host` hosts the demo module untrusted. See
  [Process modules](framework/docs/content/process-modules.md#hosting-an-untrusted-module).
- **The Linux sandbox passes every conformance probe**, so untrusted
  process modules now run on Linux with bubblewrap and util-linux
  `prlimit`. The child runs as uid 65534 in its own user namespace, sees no
  `/etc` beyond the loader cache and timezone, has a read-only root and no
  `/tmp` (it writes to its working directory), and runs under fd, process
  and memory rlimits (default 1024 / 256 / 1 GiB). On a root host bwrap
  itself starts as uid 65534. `prlimit` is now required on Linux. The P6
  memory probe maps memory directly and asks for 256 MiB (was 64 MiB, below
  what a cgo-linked Go binary needs to start). See
  [What the Linux sandbox gives a module](framework/docs/content/process-modules.md#what-the-linux-sandbox-gives-a-module).
- **Bracket filter keys.** `?field[op]=value` names the operator by word
  (`eq`, `ne`, `gt`, `gte`, `lt`, `lte`, `like`, `in`), and the text
  before the brackets is always the whole field or wire name, so fields
  `check` and `check_in` are both reachable (`?check_in[eq]=`,
  `?check[in]=`). It works at the top level, in `?rel.field[op]=` and in
  `include=rel(field[op]=…)`. New in `framework/filter`: `ReadKey`,
  `BracketKey`, `SuffixKey`, `IsBracketKey`, `IsAmbiguousKey`,
  `KeySpeller` / `SpellerFor`, `BracketOps`, `ParsedFilter.Key()` and
  `RepeatedFilterError`.
- **`store.Slice.Value(ctx)`** returns a slice's value for this request
  (the seed, else the default), and `ui.SignalToggleConfig.Ctx`.
- **`core/handler.JSONErrorDetail`, `BodyReadDetail` and
  `JSONTrailingData`** phrase a JSON decode error for your own decode
  sites.
- **`schema.DecimalColumn`, `schema.DecimalColumnType`,
  `schema.MaxDecimalExponent` and `schema.DecimalExponentFits`.**
- **`headless.CounterProps.Seeded`**, and the `headless-copy` behaviour
  module (the copy control, split out of `headless-feedback`).
- **`embedskip` analyzer.** `gofastr verify` and `make analyze` report a
  struct field walk (`NumField()` or the `Fields()` iterator) that maps
  fields to keys by their tags and never handles an embedded struct, so
  fields `encoding/json` would promote are silently skipped. A walk that
  checks `f.Anonymous`, or descends into every struct-kind field, is
  quiet; a deliberate top-level walk carries
  `//gofastr:allow(embedskip) <why>`.
- **`quoteident` analyzer.** Reports `query.QuoteIdent` outside
  `core/query`. AutoMigrate writes its DDL unquoted, so Postgres folds a
  declared `Widgets` to `widgets`, and a raw-SQL site that keeps the case
  names a table that does not exist. Use `query.QuoteFolded`; a name read
  back from the database catalog carries
  `//gofastr:allow(quoteident) <why>`.
- **`floatnum` analyzer.** Reports `json.Unmarshal`, `Decoder.Decode`
  or `handler.UnmarshalStrict` into a value that holds an empty
  interface (`any`, `map[string]any`, `[]any`, or a struct field of one)
  with no `UseNumber`. `encoding/json` stores every number under an
  interface as a float64, so 9007199254740993 reads back as
  9007199254740992. A decode whose numbers are never read carries
  `//gofastr:allow(floatnum) <why>`.
- **`query.QuoteIndexName`, `query.IndexOn`, `query.SQLitePragma` and
  `query.RefuseCaseKeptTables`.**
  `QuoteIndexName` quotes the folded last part of a name, for an index
  or trigger named after a possibly qualified table. `IndexOn` renders
  the `<index> ON <table>` of a `CREATE INDEX` with the table's schema
  where each engine takes it, and `SQLitePragma` puts a table's schema
  on the pragma. `RefuseCaseKeptTables`
  probes Postgres for a table an older build created with its case kept
  and returns an error holding the `ALTER TABLE ... RENAME` that moves it.
- **Seven analyzers from the 2026-10-27 bug bash.** `make analyze` and
  CI's vet step now report:
  - `keytrim`: `strings.TrimSpace` on an id, tenant or actor value that
    is then kept, so `" x "` acts on row `x`;
  - `tightenshared`: `os.MkdirAll` followed by an owner-only chmod of a
    directory the caller named, which rewrites a directory someone else
    owns;
  - `settlectx`: an ack, settle or record write on the cancellable
    context the work ran under, so a shutdown drops the outcome;
  - `resolvedial`: an SSRF guard that resolves and checks a host, then
    lets the transport resolve it again to dial;
  - `wrapwriter`: a ResponseWriter wrapper with no `Unwrap`, and a
    header change after the next handler ran;
  - `unrechecked`: a long-lived stream that never re-checks the
    principal, authz or tenant it resolved at connect;
  - `nowritedeadline`: a client write in a stream's park loop with no
    write deadline, and a `ResponseController` deadline a parked stream
    arms and never clears.

  `worldreadable`, `mapwriter`, `rawtext`'s `ctxless` arm, `intwrap`,
  `nowaitdelay`, `credfetch`, `rootwrite`, `rootread`, `emitident`,
  `unboundedbody` and `unboundedresp` cover more spellings of their
  shapes. The rules are listed in [CLAUDE.md](CLAUDE.md).
- **`GOFASTR1415 erasecover`.** `gofastr verify` reports DDL in `core`,
  `framework` or `battery` for a user-keyed table that no
  `datexport.DataEraser` and `DataExporter` cover. The catalog holds 81
  rules.
- **`core-ui/check.LintNumberFill` and the runtime's
  `NS.formatNumber`.** A `{{n}}` phrase filled with `String(n)` fails
  the runtime lint; `NS.formatNumber` formats the number in the page's
  `<html lang>`, so counters and slider outputs read `1.234` on a German
  page.
- **More erasure surface.** `datexport.DataEraser.Where` (a list of
  `datexport.Equals`) narrows an eraser's match, and
  `datexport.EraseRemoveMember` takes the erased id out of a JSON-array
  column and keeps the row. `datexport.ScopedName(base, table)` names a
  registration after its table, so two stores on two tables both stay
  registered. Stores tag per-user rows for erasure through
  `search.Document.Owner`, `semantic.Document.Owner` (copied to
  `Chunk.Owner`, and to the hybrid keyword copy through
  `semantic.OwnedKeywordBackend`), `webhook.Manager.PublishFor`,
  `webhook.IngestConfig.Owner`
  and `outbox.Outbox.AppendFor`. `webhook.Bridge` queues each CRUD event
  for the owner it names under the new `event.DataKeyOwnerID`. The
  battery-plane table in
  [data export](framework/docs/content/data-export.md) lists every store
  and how it is erased.
- **Linux desktop: several shell lifetimes, window effects and drag
  regions (#605).** A process can run the GTK shell again after `Run`
  returns. Every lifetime runs on one toolkit thread, because WebKit
  aborts on a second one. `Transparent` windows work under a
  compositing manager, materials get a theme tint and blur under KWin
  on X11, and `data-cui-window-drag` moves the window. CI now drives
  the real GTK file choosers, including the overwrite confirmation.
  See [desktop](framework/docs/content/desktop.md#host-support).
- **Gaps from building on v0.95.0 (#635).**
  `service.Config.OpenPrefixes` and `desktop.Config.OpenPrefixes` skip
  the browser sign-in on the paths they name, for a client with no
  browser; the app's own auth (an API key, a bearer token) protects
  them;
  `service.Config.BindAddress` serves beyond loopback
  ([service](framework/docs/content/service.md#callers-with-no-browser-openprefixes)).
  `webhook.Subscriber.Auth` sends a bearer, API key or basic credential
  (over `https://` only, unless `Options.AllowPlaintextAuth`), and each
  delivery records `ResponseStatus` and `ResponseBody`
  ([webhooks](framework/docs/content/webhooks.md#authenticating-to-the-receiver)).
  `openapi.DocsHandlerWith` chooses the spec and page paths and lists
  the operations; `Spec.SetDescription` sets `info.description`.
  `CORSConfig.AllowAnyHeader`/`AllowAnyMethod`, `handler.RefuseCrossSite`,
  `desktop.LaunchArgs` with `gofastr desktop build --launch-args` and
  `--ldflags`, and the `gofastr_nodesktop` build tag for a static server
  image. The CLI warns when `go.mod` requires another GoFastr release
  (`GOFASTR_NO_VERSION_CHECK=1` silences it).

### Changed
- **`core/static` redirects `/dir` to `/dir/`.** A request without the
  trailing slash for a directory that has an `IndexFile` used to get the
  index at `/dir`, so its relative links resolved against the parent.
  It now answers `301` to `./dir/` (query kept), like `net/http`'s
  `FileServer`; the relative `Location` keeps the prefix and cannot
  name another host.
- **`core/static`'s SPA fallback answers only client routes.** With
  `SPA: true`, a missing path whose last segment has a file extension
  (`/app.3f2a.js`, `/x.css`, `/logo.png`) now gets `404` instead of the
  index: the browser ran the HTML as a script or a stylesheet, and a
  CDN could store it under the asset's URL. Extensionless paths
  (`/dashboard/settings`) still get the index. See
  [core packages](framework/docs/content/core-packages.md#static).
- **`/readyz` drains before shutdown.** `App.Shutdown` now turns
  `/readyz` to `503` (`{"status":"draining"}`) at once and keeps serving
  for a grace period, so a load balancer stops routing here before the
  listener closes; `/healthz` stays `200` until exit. The grace is 5s by
  default, set with `framework.WithReadinessDrain` (`0` skips it), and
  `0` in a Go test binary and under `gofastr dev`. A stop now takes up
  to the grace longer: the default SIGTERM handler's deadline is
  `ShutdownTimeout` plus the grace. See
  [Health and readiness](framework/docs/content/health-checks.md#draining-on-shutdown).
- **BREAKING for custom A2A stores: retention counts per tier.**
  `a2a.Config.TerminalTaskRetention` and `PausedTaskRetention` now bound
  one owner's tasks per tier (`a2a.RetentionTier`: the tenant and scope
  set a task was made under), not per owner. A narrow token's tasks, or
  the same user's tasks in another tenant, pushed the user's other tasks
  past the bound, deleting results and canceling paused work that tier
  could not even read. `RetentionTrimmer.TrimTerminalTasks` gains a tier
  argument: `TrimTerminalTasks(ctx, owner, tier, keep)`. A custom store
  implementing the old signature no longer satisfies `RetentionTrimmer`
  and is silently not trimmed; update the method to filter by
  `tier.Tenant` and `tier.Scope`. An owner now retains up to 64 terminal
  and 64 paused tasks per tenant and scope set.
- **`gofastr verify` and `make analyze` read more spellings of the shapes
  they already refuse.** A 2026-11-02 pass wrote small variations of each
  analyzer's own bug shape and widened every rule that missed one:
  - `fixedtmp`: `"/tmp/app-" + pid`, `filepath.Join("/tmp/app", name)`,
    `path.Join`, `fmt.Sprintf("/tmp/app-%d", pid)` and an
    `exec.Cmd{Dir: …, Path: …}` literal;
  - `timestampid`: an id bound in `Session{ID: …}`, `var token = …` or
    a multi-value assignment, built with `fmt.Sprint`, or made from
    `time.Now().Format(…)` / `.String()`. A `make([]byte, 16)` rand
    buffer now silences it as a `[16]byte` array always did. A trailing
    `key` counts only behind a credential word (`apiKey`, `signingKey`):
    a date-bucketed storage `Key` or `cacheKey` names a place, not a
    capability, and stays quiet;
  - `errleak`: `fmt.Sprintf("query failed: %v", err)` and
    `fmt.Sprint(err)` on a 5xx;
  - `discardeddecode`: a discarded `handler.UnmarshalStrict`,
    `handler.DecodeStrict`, `jsonexact.Unmarshal` or yaml `Decode`;
  - `recoverlog` and `controlbytes`: `log.Fatal*` / `log.Panic*` (they
    print the message before exiting), and for `recoverlog`
    `os.Stderr.WriteString`;
  - `worldreadable`: a mode that is constant but not a bare literal —
    `os.FileMode(0o644)`, a named const, `fs.ModePerm`, `0o600|0o044`;
  - `unboundedbody` and `unboundedresp`: `ioutil.ReadAll`,
    `io.CopyBuffer`, `buf.ReadFrom(body)`, a body read through
    `bufio.NewReader` or a `body := r.Body` alias, and for responses
    `io.Copy` / `ReadFrom` into a writer that keeps the bytes
    (`bytes.Buffer`, `strings.Builder`, a `[]byte` writer). A streaming
    copy into a `ResponseWriter`, a file or an `io.Writer` stays quiet;
  - `GOFASTR1413`: `Header().Del("Vary")` and `h["Vary"] = …`, which
    clobber the earlier entries as `Set` does. `h["Vary"] =
    append(h["Vary"], …)` keeps them and stays quiet;
  - `secretcompare`: `bytes.Equal` on a credential pair, a
    `bytes.Compare` / `strings.Compare` tested `== 0` / `!= 0` (one that
    sorts stays quiet), a `switch token { case stored: }`, a
    `string(sig)` conversion, and a `~string` type parameter;
  - `credfetch`: `req.SetBasicAuth`, which writes `Authorization`;
  - `keytrim`: `strings.Trim` / `TrimLeft` / `TrimRight` / `TrimFunc`
    and `bytes.TrimSpace` on an id, and a request read that reaches the
    trim through a local.

  Code that passed verify can fail it on upgrade. Fix the finding, or
  mark a site that is the shape on purpose with
  `//gofastr:allow(<analyzer>) <why>`.
- **`make analyze` and CI vet the windows and darwin files too.** The
  repo analyzers ran on linux only, so no `_windows.go` or `_darwin.go`
  file was ever vetted. Both now run the vettool again with
  `GOOS=windows` and `GOOS=darwin` over the affected packages, listed
  for that OS. The first run found three things: `discardederr` now
  ignores `(*syscall.LazyProc).Call` and `(*syscall.Proc).Call` (and
  the `golang.org/x/sys/windows` twins), whose error is always non-nil;
  the Windows `EvalAsync` result slot is numbered by a counter, not the
  clock; and `gofastr desktop build` writes its cached WebView2 SDK
  package with `fileperm.WriteOwnerOnly`.
- **`/openapi.json` documents what `?include=` returns.** An entity's
  schema component is its read view: a relation field's property is
  `anyOf` its key, the related record or null, since an include replaces
  the key with the record, and each other relation is an optional
  property (a record or null, or an array of records). Request bodies
  still take the key. A generated client reads the field as a union
  (see `includes.md`).
- **Local run registry registers local runs only (#635).** A server
  bound to every interface or a LAN address, with no `gofastr dev` and
  no `battery/service`, writes no record. Set `GOFASTR_RUNS=on` to keep
  registering it; `GOFASTR_RUNS_DIR` only says where the registry is.
- **API docs page (#635).** `/api/docs/` lists the operations, ships no
  inline CSS, and answers 404 under its mount except at the page and
  spec paths. The webhook SQL tables gain `auth`, `response_status`
  and `response_body`, added when the store opens.


### Fixed
- **MCP `initialize` negotiates the protocol version.** It answered
  `2025-06-18` whatever the client asked for. Per the 2025-06-18
  Lifecycle spec it now echoes a requested version the server supports
  (`2025-06-18`, `2025-03-26`) and otherwise answers the newest.
  `mcp.LatestProtocolVersion` and `mcp.SupportedProtocolVersions()` are
  new exports.
- **The MCP notification stream's `endpoint` event names its mount
  path.** `ServeSSE(path)` opened every GET stream with
  `event: endpoint` / `data: /sse` whatever `path` was, so a client that
  follows the event posted to a path nothing serves (the framework
  mounts `/mcp`). It now carries `path`.
- **MCP `tools/call` always answers a `content` array.** A handler
  returning a nil `[]mcp.Content`, or an `mcp.ToolResult` flagging
  `IsError` with no blocks, put `"content": null` on the wire; the MCP
  schema requires an array and validating clients rejected the reply.
  It is now `[]`. `prompts/get` likewise answers `"messages": []` for a
  prompt handler that returns no messages.
- **A2A: a panicking `Store` or push client no longer crashes the
  process.** A run's finalize (the store read and write after the
  handler returns, the publish, the retention trim) and each push
  delivery through `PushOptions.Client` ran on the server's own
  goroutines with no recover, so one panic there took down every
  owner's runs and streams. The run now ends with an
  `a2a: run finalize panicked` log line and is released; the delivery
  fails with `a2a: push delivery panicked` and the drain carries on.
- **An MCP response that cannot encode is still answered.** The MCP
  transports dropped the JSON encode error, so a result holding a NaN
  where the dispatch does not marshal it first (a content block's or a
  prompt message's `_meta`) answered HTTP with an empty 200 body, an SSE
  event with empty data, and stdio with no line at all, leaving the
  client waiting forever. Every transport now answers an internal error
  carrying the request's id and logs the encode error.
- **An MCP gate's own `*RPCError` keeps its code on every gate.**
  `WithToolGate` and the server-wide gate (`SetGate`, and
  `CallToolFor`'s run of it) wrapped a gate's `*RPCError` in a second
  invalid-params error reading `RPC error -32001: ...`, while `Gated`,
  `WithResourceGate` and `WithPromptGate` passed it through. All five now
  pass it through.
- **MCP `resources/unsubscribe` releases only the caller's own
  subscription.** The per-`uri` count was shared by every caller, so
  any caller past the server gate could unsubscribe a `uri` it never
  subscribed to and switch `notifications/resources/updated` off for
  everyone who had. Each subscribe is now recorded under its caller
  (the resolved user, else the TCP peer's host); delivery is unchanged.
  That record is capped at 4096 caller/`uri` subscriptions across all
  callers, beside the existing 1024 distinct uris: an anonymous caller
  is its peer host, so without the cap a client with many addresses
  grew it without end. A subscribe that would add one past the cap
  fails.
- **An idle MCP notification stream re-checks its caller.** The GET
  stream `ServeSSE` holds re-checked the caller only before a
  notification, and notifications are rare, so a revoked session's
  stream kept its seat, goroutine and buffer for as long as the server
  stayed quiet, and a half-open connection was never written to and
  never freed. The stream now re-checks the caller every 15 seconds and
  writes an SSE comment line (`: keep-alive`) that clients ignore.
- **A queued admin bulk run over a CRUD-off entity finishes.** The admin
  bound `Config.BulkJobs` to `Config.UI`, which sees no write routes for
  a named entity with CRUD off, so every run the admin's bulk route
  queued over one stopped before its first record. The bound UI now
  counts the admin's routes for the entities it exposes and the app's
  for the rest. New `entityui.UI.WritePath(e)` reports a UI's write
  base.
- **`gofastr upgrade` scans files that import `"C"`.** The go command
  compiles cgo's rewrite of such a file from the build cache, so the
  imports, fields and strings matchers dropped every hit in it and the
  report called it "not compiled"; under `CGO_ENABLED=0` no load
  reached it at all. Hits now land on the file as written, and a
  cgo-off scan loads it with cgo on. Only cgo's copy is renamed back:
  a goyacc-style file whose `//line` directive names its `.y` grammar
  keeps its own name and its hits.
- **`gofastr upgrade` lists a `gofastr.yml` it could not parse.** The
  config matchers skipped the file silently, so every `config:` note
  read "Nothing found" over a spec nobody had read. It now sits in the
  report's "could not be scanned" NOTE with the parse error.
- **`gofastr upgrade` handles vendored apps and release candidates.**
  `--apply` runs `go mod vendor` after `go mod tidy` when the app has
  `vendor/modules.txt`; its `go build` used to fail on inconsistent
  vendoring. Resolving the newest release without `--to` no longer
  fails inside a vendored module ("offline?") and picks the newest
  release over a newer prerelease, the way `go get …@latest` does. A
  prerelease `--to v0.96.0-rc.1` shows v0.96.0's notes instead of none,
  and `--to v0.95.0+meta` on a v0.95.0 app is the same release, not a
  downgrade.
- **`gofastr upgrade` stops telling a ported app to edit correct lines.**
  Thirteen `hits: edit` notes named a symbol that survives its release
  with a new shape (`app.ScreenLLMMD`, `pagination.NewCursorPage`,
  `admin.AuthPrincipal`, `ui.WorkbenchConfig.RailWidth` and ten more),
  so every ported call stayed under "Edit these". They now name the old
  shape under `shapes`, a value condition, only the removed member
  (`crud.EventOutbox.Append`, not the interface), or `hits: review`
  (`semantic.NewPgVector`, whose signature did not change). The v0.66.0
  queue note never found a `MemoryQueue.Nack` call: its regex spelled
  the first parameter `_` where v0.65.0 named it `ctx`. Two registry
  gates in `evals/upgrade-fixtures` now refuse both shapes of mistake
  (`TestRegistryShapesMatchTheirRelease`, and the edit arm of
  `TestRegistrySymbolsExistedAtPriorTag`).
- **Cron jobs run once across a daylight-saving change.** The in-process
  `framework.Scheduler` and `battery/queue`'s in-memory `Scheduler`
  matched each tick's wall clock with no DST handling: on a fall-back
  night `"30 1 * * *"` ran at both 01:30s, and leader election did not
  stop it (the tick claim is per UTC minute); on a spring-forward day
  `"30 2 * * *"` did not run at all. Both now follow vixie cron, as the
  durable scheduler already did: a fixed-time job runs once in a repeated
  hour and once right after a skipped one, and a job with a wildcard
  minute or hour keeps real elapsed time. The rule is the new
  `cron.Schedule.FiresAt`, which all three schedulers share. Hosts in UTC
  see no change.
- **A DBQueue built `WithTable` is exported and erased.** The queue
  registered only the default `queue_jobs` table, so a queue on
  `billing_jobs` sat outside `App.ExportData` and `App.EraseUserData`: an
  erasure left that user's jobs, payloads included. `NewDBQueue` now
  registers its own table under `datexport.ScopedName("queue_jobs",
  table)`, as the outbox and the durable scheduler do. A host that
  registered the renamed table by hand can drop that registration, or
  the table is exported twice.
- **A DBQueue job claimed as shutdown lands is handed back unstarted.**
  The worker ran its handler on the already-cancelled context, the
  handler failed at once, and the job spent an attempt on a run that
  never happened; a `MaxAttempts: 1` job was dead-lettered. The worker
  now returns it to pending with the attempt refunded, as the outbox
  relay does with the rest of a batch.
- **A DBQueue that cannot claim jobs says so.** The worker loop read every
  claim error as an empty queue, so a lost database, a renamed table or a
  row the claim cannot decode stopped every job with no log line. The
  onset is now logged once at ERROR and the recovery once at INFO, as the
  outbox relay already did.
- **The outbox no longer drops an event for a consumer whose delivery
  was not created yet.** Each consumer's deliveries are expanded in their
  own batch, so with two relays, or a backlog older than the handler
  grace (any outage past 15 minutes), a parent could hold consumer `a`'s
  delivery and not yet `b`'s. Dispatching `a` completed the parent, and
  expansion only visits pending parents, so `b` never received the event
  and no dead letter recorded it: two relays over a 300-event backlog
  lost up to five of 600 deliveries. A parent now completes only once
  every consumer the relay declares for its type has a delivery.
- **`queue.WithHandlerTimeout(0)` keeps the MemoryQueue's 30s budget.** A
  zero budget failed every job the moment it started ("exceeded its 0s
  budget"), retried it and dead-lettered it, though its handler may have
  run each time. `WithDBHandlerTimeout(0)` means no timeout, so the same
  value meant opposite things on the two queues.
- **A skipped durable schedule no longer spins `DurableScheduler.Start`.**
  A schedule the pass skips (a corrupted row, a stored interval under the
  floor, a cron spec that no longer parses) keeps its past `next_run`,
  and the loop woke for it every 10ms: a hundred passes a second, each
  writing the lease row and logging the skip at ERROR. It now waits for
  the next schedule that is not yet due, or the heartbeat.
- **A DBQueue job dead-lettered by lease expiry says so.** The sweep that
  fails a final-attempt claim whose worker died wrote only the status: the
  job listed with its claim's `updated_at` and an earlier attempt's
  `last_error`, or none. It now records the sweep time and a `last_error`
  naming the lease expiry.
- **An erasure drops the job a `MemoryQueue` worker is running.** A job a
  `Start` worker had taken off the heap sat in no store the erasure read,
  so it was not counted, and when its handler failed the worker put the
  user's payload back on the heap or the dead list after an erasure that
  had reported success. The erasure now counts it, and the worker drops
  it when the handler returns, as a `DBQueue` or `RedisQueue` settle
  already did.
- **A gated DBQueue job survives a shutdown during its hand-back.** A
  worker that claimed a job and then found its type gated released it on
  the worker's context; a shutdown at that moment left the row claimed
  with an attempt spent, and on a last attempt the dead-letter sweep
  failed a job that never ran. The release now settles on a detached
  context, as Ack and Nack do.
- **Outbox options keep their defaults for zero or less.**
  `outbox.WithPollInterval(0)` crashed the process when the relay
  started (`time.NewTicker` panics on its goroutine); `WithBatchSize(0)`
  delivered nothing, and a negative size was unlimited on SQLite but an
  error on every Postgres pump; `WithMaxAttempts(0)` dead-lettered a
  delivery on its first failure.
- **`queue.WithLeaseTimeout(0)` keeps the five-minute lease.** A zero or
  negative lease made every claim count as abandoned the moment it was
  written: the next poll ran a live job a second time, and on a job's
  last attempt the dead-letter sweep failed it while it was still
  running. `WithLeaseTimeout` and `SetLeaseTimeout` now ignore
  `d <= 0`, as the durable scheduler's `LeaseDuration` already did, and
  so does `RedisQueue.SetVisibilityTimeout`, whose zero window made
  `Reclaim` re-deliver a job its worker had just claimed.
- **The in-memory `queue.Scheduler` refuses an interval below one
  millisecond.** `Every(5)` (five nanoseconds) made the scheduler's
  timer loop enqueue a job per iteration, over 12,000 in 20ms. `Register`
  now returns an error for a negative interval or a positive one under
  1ms, the floor `DurableScheduler` already had; `Every(0)` still
  registers.
- **An outbox handler budget longer than the claim lease no longer runs
  twice.** The relay's lease is a fixed five minutes, but
  `outbox.WithHandlerTimeout` took any budget: a handler allowed ten
  minutes was re-claimed by another relay at minute five and ran
  concurrently, and its own success was then fenced out. The lease is
  now twice the handler budget plus the settle window when that is
  longer than five minutes.
- **Docs search excerpts hold the match in whole characters.**
  `framework_docs_search` and `gofastr docs --grep` cut a long line at
  byte offsets, splitting an em dash or accented letter, and located the
  match in the lowercased line, which is longer or shorter than the line
  itself when it holds a character such as `İ`, so the excerpt could miss
  the match.
- **`gofastr pack` prints no terminal escapes.** A string from the app's
  source with an ESC, a C1 control, a bidi override or a zero-width
  space went into the YAML bare, so `gofastr pack` in a cloned repo
  could drive the terminal (an OSC 52 sequence writes the clipboard).
  Such a value is now double-quoted with the character escaped and reads
  back unchanged; a map key holding one is refused, and the check's
  notes percent-encode them in file names.
- **`gofastr agents` writes only inside the project.** `agents skill`,
  `agents init`, `agents sync` and `init --reinit` followed a symlink
  anywhere: a cloned repo carrying
  `.claude/skills/gofastr-host/SKILL.md -> ~/.bashrc` had `agents skill`
  overwrite `~/.bashrc` with the skill, and a `.agents` link sent the
  detail files to another directory; `init --reinit --force` did the same
  to a linked `AGENTS.md` or `CLAUDE.md`, and created a dangling
  `DESIGN.md` link's target. Writes now go through an `os.Root` on the
  project; a link inside it is still followed, one out of it is refused.
- **A slow module answer no longer kills the module.** A `moduleproto`
  peer remembered the last 1,024 calls it gave up on, answered or not,
  so 1,024 timed-out calls answered in time pushed out a slow call still
  owed, and its answer faulted the peer as unsolicited; the supervisor
  then killed the module and every request in flight. The window now
  counts only ids still owed an answer.
- **`/readyz` no longer races a check that outlives its deadline.** On
  timeout the handler read and copied the result rows while a check that
  ignored `ctx` could still write its own row, a data race `go test
  -race` reports. Checks now hand their rows over a channel, and a late
  one is dropped. A check registered with an empty name that had passed
  is no longer reported as `"timeout"` when another check hangs.
- **`log_filter historical=true` skips a log line past 1 MiB.** The
  file tail used `bufio.Scanner`, which stops the scan on such a line,
  and the error was swallowed, so the historical read returned nothing
  while one large entry sat in its window. The line is now skipped.
- **The cache keeps large integers exact under an interface.**
  `MemoryCache`, `RedisCache` and `GetOrSet` decoded with
  `json.Unmarshal`, so a value read into `map[string]any` or `*any` had
  every number as float64 and a cached id `9007199254740993` came back
  as `9007199254740992`. A number whose magnitude is 2^53 or more now
  reads back as a `json.Number`, even one a float64 holds exactly
  (`1e20`, a `UnixNano` timestamp); every smaller number is still a
  float64.
- **`cache.GetOrSet` returns a loaded value whose entry was already
  evicted.** Every waiter read its value back from the cache after the
  fill, so an entry an LRU bound or Redis `maxmemory` evicted (or a short
  TTL expired) in between made a successful load return `ErrCacheMiss`.
  The waiters now get the loader's value; a negative `ttl` still
  surfaces the miss.
- **`RedisCache.Set` with a negative TTL deletes the key.** It passed
  the negative duration to `RedisClient.Set`, and a go-redis adapter
  stores that with no expiry (`-1` is KEEPTTL), so a lifetime already in
  the past made an entry that never expired. It now matches
  `MemoryCache`: nothing is stored and the existing entry is dropped.
- **`check-embed` reads a template literal's `${…}` as code.** It skipped
  the whole template as a string, so a ClientJS like
  ``G.toast(`saved ${G.serverAction("save")}`)``, which the action
  compiler rewrites into a live server action, passed `gofastr build`'s
  gate and was refused only at boot.
- **`static.Mount` works under a prefix.** It registered both
  `/static/` and `/static/{path...}`, which the router reads as one
  pattern, so `Mount` with any `Prefix` but the root panicked at boot
  with a route conflict (the spelling `porting.md` shows). A trailing
  slash (`/static/`) is now accepted too. The handler also strips
  `Prefix` on a segment boundary only: `/staticsecret.txt` served
  `secret.txt`. A path holding a NUL byte (`/a%00b`) answers 404 (or
  the SPA index) instead of 500.
- **`static.Builder.Watch` no longer rebuilds forever.** It polled
  every file under the watched dirs, `OutDir` included when it sat under
  one (watch `.`, write `./dist`), and every rebuild rewrites the
  export, so one source edit started a loop that never settled. `OutDir`
  is left out of the poll.
- **The SPA fallback is `no-cache`.** `core/static` answered a URL that
  names no file with the index under `Config.MaxAge`, so a long
  `MaxAge` meant for fingerprinted assets stored the app shell, as
  `text/html`, under a client route or under a new asset's name that a
  newer replica linked mid-deploy, for that long. The fallback now
  revalidates every time (its ETag makes that a 304); files that exist
  keep `MaxAge`.
- **The analyzer wiring test checks every analyzer, not every
  package.** `TestEveryAnalyzerIsWiredOrExplained` read one variable
  named `Analyzer` in `<pkg>/<pkg>.go` and counted any mention of the
  package name in a `suite.go` comment as the reason it was held back.
  So `hygiene` (whose two analyzers are not named `Analyzer`) was
  skipped, `unseated`'s two arms in `arms.go` were never looked at, and
  `rawtext` was "explained" by the comment above its own registration.
  Deleting those four registrations, with their marker names, passed
  every test while `make analyze` and CI stopped running them. The test
  now reads every exported `&analysis.Analyzer{}` in every file of each
  package, and only a `<name> — <reason>` line holds one back.
- **An analyzer marker needs a reason in words.**
  `//gofastr:allow(<analyzer>) --` silenced the vettool and the
  `core-ui/check` lints while `gofastr verify` reported the same marker as
  having no reason. Separator punctuation alone (`--`, `—`, `:`) no
  longer counts as a reason anywhere.
- **An analyzer change selects the generated-code vet gates.** The gates
  that vet emitted code with the repo vettool (`TestUnionApp`, the
  generated-CLI and styles gates in `appgen` and `generatecmd`) build
  `./cmd/vettool` in a subprocess, which no import edge shows, so
  `cmd/affected` never selected them for a change under
  `internal/analyzers/`: a widened rule that fired on emitted code passed
  the pull request and failed main. Each now declares
  `//gofastr:test-reads internal/analyzers/ cmd/vettool/`, and
  `TestRepoVettoolCallersDeclareTheirReads` holds every future caller of
  `clitest.RepoVettool` to the same directive.
- **`core-ui/check.LintNumberFill` reads every spelling of the `{{n}}`
  pattern.** It matched only a quoted `'{{n}}'` and a regex with escaped
  braces, so `.replace(` + "`{{n}}`" + `, String(n))` (a template
  literal) and `.replace(/{{n}}/g, String(n))` (unescaped braces, the
  same regex to JavaScript) filled the count with ASCII digits unseen.
- **`core-ui/check.LintRuntimeProse` reads every reader-facing sink.**
  `alert` / `confirm` / `prompt`, a literal passed to `append`,
  `prepend`, `replaceChildren`, `before`, `after` or `replaceWith`
  (each makes a text node), `nodeValue`, and the ARIA text attributes
  and properties beyond `aria-label` (`aria-valuetext`,
  `aria-roledescription`, `aria-placeholder`, `ariaDescription`, …)
  all put English in front of a reader with the lint silent. An
  `append(name, value)` whose first literal is a bare field name
  (`Headers`, `FormData`, `URLSearchParams`) adds no text node and stays
  quiet. The shipped JavaScript stays clean under the wider rule.
- **`AppConfig.JSONCase: crud.CaseSnake` reaches the entity routes
  again.** Since v0.94.0 every `App.Entity`, `App.GroupEntity` and
  `App.View` route answered camelCase keys in a snake_case app: the
  handler built its wire keys before the app set the case. They are
  snake_case again, as `entity-declarations.md` says; clients written
  against the camelCase keys need the column names back.
- **`/openapi.json` names a snake_case app's fields by their column
  names.** It camelCased every property, required entry and record-key
  path parameter whatever `AppConfig.JSONCase` said. The new
  `openapi.EntityOpenAPIFor(registry, openapi.Options{…, JSONCase})`
  takes the case; `EntityOpenAPI` and `EntityOpenAPIWithBulk` keep
  camelCase.
- **`/openapi.json` types a relation field by the key it names.** Every
  relation was a `uuid` string. A foreign key to an `int` key is an
  `integer` property and filter now, as crud stores, answers and parses
  it; a key to a text slug or other non-uuid key drops the `uuid`
  format no value of it met.
- **`/openapi.json` lists the refusals the routes answer.** The CRUD
  writes gain 409 (a unique value taken, a missing or still-referencing
  record), 413 and 415; a declared endpoint gains its gate's 401 and its
  cross-site 403, typed by a new `GateError` schema, since that gate
  nests the refusal under `error` where the CRUD routes' `Error` keeps it
  flat. The SDK docs' errors page names 409, 415 and the second shape.
- **`gofastr generate cli --from-openapi` keeps spec bytes off the
  terminal.** A binary body's content type reached the generated
  `--file` help raw, so a spec could write ESC or C1 sequences to
  whoever ran `<cli> <op> --help`; it must now be a media type with no
  control byte. A refused spec's error encodes control bytes instead of
  printing them, and `--dry-run --json` errors are valid JSON (they were
  spelled with Go's `%q`, whose `\x1b` no JSON parser reads).
- **The generated Go and JS clients bound their SSE feed.** `Watch` /
  `watch` joined an event's `data:` lines until a blank line, and the JS
  client buffered a line until a newline, with no limit, so a feed that
  never ended one grew the subscriber's heap. Both now stop at the
  1 MiB body cap with an error. Regenerate the SDK or client
  (`gofastr build`) to pick it up.
- **battery/auth bug bash (2026-11-01).** Details in
  [auth.md](framework/docs/content/auth.md):
  - **BREAKING: token management needs the second factor.** On a 2FA
    account `POST/GET/DELETE /auth/tokens` need the cookie session that
    passed it; a session from before enrollment answers 403 and a bare
    bearer JWT 401. Either minted a permanent `*:*` token before.
  - **BREAKING: the 2FA challenge replaces the session.** A passing code
    deletes the pending session and sets a new cookie, so a planted
    pending token never becomes a verified one.
  - **BREAKING: CSRF skips only `Authorization: Bearer` and `X-API-Key`.**
    Basic, Negotiate and NTLM, which the browser attaches by itself, no
    longer skip the check (`middleware.SkipBearerAuth`).
  - **BREAKING: OAuth.** Provider keys must be 1-64 letters, digits, `-`
    or `_` (a dotted key never completed a flow). An identity with no
    email and no existing link answers 400 `auth.oauth_no_email` (two
    such identities merged into one account). OIDC `email_verified`
    counts only when the same claim set's `email` is the kept address,
    so a remapped `EmailClaim` no longer auto-links on another
    address's flag.
  - **BREAKING: a password form login of a 2FA account reaches the
    challenge.** It redirected to its `next` page, which refuses the
    pending session. Next to a 2FA plugin, `Init` now requires
    `NewCorePlugin().WithTwoFactorURL(path)`.
  - **BREAKING: OAuth sign-in of a 2FA account reaches the challenge.**
    It redirected to `/`, which refuses the pending session. Next to a
    2FA plugin, `Init` now requires `OAuth2Config.TwoFactorURL`, as
    passkey and magic link do.
  - **OAuth sends PKCE.** The OIDC, Google and GitHub providers send an
    S256 `code_challenge` and the `code_verifier`, so a stolen
    authorization code no longer redeems in an attacker's own flow
    (authorization-code injection, RFC 9700). `DisablePKCE` on each
    provider turns it off; custom providers opt in through
    `auth.OAuth2PKCEProvider`.
  - **OAuth refresh is serialized across replicas.** `SQLOAuthTokenStore`
    leases a grant (`auth.OAuthTokenLeaser`, new `refresh_lease_ms`
    column) before its refresh token is spent; a replica that finds
    another's lease waits for that refresh and never spends the token,
    failing with `auth.ErrOAuthRefreshInProgress` if it does not land.
  - OAuth: an account claim deletes the squatter's stored provider
    tokens; callbacks without a code no longer fill the replay table
    and lock every login out; concurrent refreshes spend a refresh token
    once; tokens with no `expires_in` stay usable; the `/link` flow
    stores tokens and refuses a binding it lost to another account; a
    JWKS or discovery outage no longer refetches under a lock on every
    login. New `(*OAuth2Plugin).ValidOAuthToken` and
    `RefreshOAuthToken` read the row under the registry key.
  - `ttl_seconds` past the `time.Duration` range answers 400 (a wrapped
    negative minted a permanent token); an unreadable SQLite
    `revoked_at`/`expires_at` reads as revoked/expired; token display
    prefixes keep 7 random chars after a long brand.
  - Passkey RSA keys are capped at 8192 bits (a 4-Mbit key cost a minute
    of CPU per anonymous sign-in attempt).
  - At most 8 distinct session cookies are looked up per request (2999
    copies drove 2999 queries).
  - `RequireAuth(mgr.JWT())` works when wired before `Init`;
    `RequireAuth(nil)` panics at wiring; a negative `JWTExpiry` fails
    `Init`.
  - Audit rows survive a client that hangs up after the change, and an
    actor-less SQL audit row stores `email_hash`, not the address.
  - `EntitySessionStore` indexes `token` (unique) and `user_id` whoever
    created the table; `EraseUserData` reaches the in-memory auth stores.
  - `AuthManager` reports its `BasePath` to the embed host.
  - **Per-IP limits key IPv6 by its /64** (`ratelimit.IPKey`,
    `(*Limiter).ClientKey`): one client could rotate source addresses
    inside its own /64 past every per-IP limit. `Middleware()`, the auth
    battery's per-IP limiters and `framework.AgentRateLimitKey` use it.
  - Every `AuditSink`, a host's own included, gets the request's context
    minus its cancellation, with a 5-second deadline: a sink doing slow
    remote work is cut off there.
  - `middleware.CSRF` puts the verified token on the context for a
    mutating request too (`TokenFromContext`), and `auth.CSRFToken`
    reads `__Host-auth_csrf` before `auth_csrf`.
  - `auth.CheckPassword` knows only the two built-in formats again; the
    battery's sign-in asks a custom `DefaultHasher` first.
  - Passwords: the 8-character floor counts characters, so a short CJK
    password that passed on bytes is refused; placeholders for
    passwordless accounts are made by `DefaultHasher`.
- **BREAKING: a magic-link sign-in of a 2FA account reaches the
  challenge.** The confirm POST always redirected to `OnSuccessURL`,
  which refuses the pending session it had just minted. It now goes to
  the new `MagicLinkConfig.TwoFactorURL`, and `Init` refuses to start
  when a 2FA plugin is registered and that field is empty, as the
  passkey plugin does. Set it to the page that posts the code to
  `/2fa/challenge`.
- **Magic-link send and forgot-password limit each address.** One mail
  per address every two minutes (`AddressRateLimit`), however many IPs
  ask; within the cooldown the request gets the same 200 and nothing is
  minted or mailed. A cooldown rather than an hourly budget, so a
  stranger cannot stop the owner's links.
- **Password hashing follows `auth.DefaultHasher`.** Passwordless and
  cleared accounts store a placeholder made by it, so login timing no
  longer tells them from unknown addresses under argon2id; a custom
  hasher's `Verify` is called; `Argon2Hasher.Hash` refuses parameters
  its verifier refuses; the 8-character floor counts characters, not
  bytes. See [auth.md](framework/docs/content/auth.md#password-hashing).
- **Multipart login and register forms work.** Only `ParseForm` ran,
  which never reads a multipart body, so every one answered 400. A
  file part or a repeated `email`/`password` field answers 400.
- **A list on a bare filter key is no longer sent as an IN.** The
  server reads `?status=a,b` as equality with the literal `a,b`, so a
  list there matched nothing it was meant to. The generated JS SDK sent
  an array on any filter key that way; it now throws on an array whose
  key does not end in `_in`, or whose key is a column of its own (a
  `check_in` column with no `check` column), naming the `<key>_in` to
  use (`sort`, `fields` and `include` arrays still go comma-joined). The generated
  CLI's bare `--<field>` flag claimed "comma list = IN" and sent the
  same query; it is equality only now, and a new `--<field>-in` flag
  sends `<field>_in`.
- **`gofastr generate cli` refuses a `custom.go` from the other mode.**
  The generator never rewrites `custom.go`, so moving between the entity
  and `--from-openapi` modes kept a file importing the other mode's client,
  and the CLI stopped building. The run now names the import and stops;
  `--force` moves the file to `custom.go.bak` and writes a fresh one. See
  [App CLI](framework/docs/content/app-cli.md).
- **BREAKING: `client.d.ts` types nullable fields `T | null`.** The
  server returns SQL `NULL` as `null` for any optional column, but the
  generated types said `T`, so strict TypeScript compiled code that read
  `null` as a string or number. A field that is not required, generated or
  part of the key is now `T | null` on the entity interface and its Input
  (where `null` clears it). See [SDK](framework/docs/content/sdk.md).
- **BREAKING: the JS SDK keeps integers past 2^53.** `client.js` parsed
  with `JSON.parse`, so a big id or counter lost digits. It now hands such
  an integer over as its exact decimal string, and `client.d.ts` types
  every integer field, and an integer `id`, `number | string`. TypeScript
  code that does arithmetic on one narrows it first. See
  [SDK](framework/docs/content/sdk.md).
- **The harness MCP server cancels a request from another POST.** Over
  Streamable HTTP each POST had its own in-flight table, so the
  `notifications/cancelled` an MCP client sends as a separate POST found
  nothing and the turn ran on. POSTs now share one table, owned by the
  token principal plus `Mcp-Session-Id`, so another caller's cancel finds
  nothing. The duplicate-id check and the 32-request cap count per owner
  across POSTs.
- **BREAKING: a presence topic named in the URL needs `AuthorizeTopic`.**
  With no hook, any `?presence=` topic joined, so anyone who could guess a
  topic read its roster, emails included. A screen now declares its topics
  with `PresenceTopics(ctx)` (`uihost.PresenceScreen`); the host signs the
  list into the page's stream URL for that viewer, and those topics join
  with no hook. A `?presence=` topic is refused unless a hook admits it, and
  an installed hook now answers for declared topics too.
  `island.Manager.ServeSSEWithPresence` takes an `island.PresenceTopics`
  (`Declared`, `Requested`) in place of a `[]string`. See
  [Live presence](framework/docs/content/presence.md).
- **A desktop window on Windows can no longer open a second, unguarded
  window.** WebView2 answered `window.open` and `target="_blank"` with a
  new web view that had no navigation policy, so a page could open a
  foreign site in a window that looked like the app's. The Windows shell
  now marks every new-window request handled, so nothing opens, as on
  macOS and Linux.
- **A static export's `llm.md` carries the page's SEO front matter.** The
  live `/llm.md` opens with the screen's description, canonical and
  other head metadata; the exported file was the bare doc. Both now go
  through the new `UIHost.LLMMDWithSEO`, so an exported doc matches the
  served one byte for byte.
- **One A2A caller can no longer take every push-delivery slot.** The 16
  in-flight deliveries and the 16-event backlog were shared by every
  owner, so a caller whose receiver hung held them all and every other
  owner's notifications were dropped. One owner now holds at most 4 of
  each.
- **A page whose cache entry was dropped keeps its own signals on Back
  and Forward.** When an RPC write invalidated the page's entry, leaving
  it captured only its `route.*` signals, so a replay showed the page's
  markup with the previous page's values (a cart quantity, a selected
  tab). The envelope navigator now tracks the page's whole page-scoped
  seed, and a deferred part's seed adds to it instead of replacing it.
- **BREAKING: auth's own screens draw with the design system.** The
  magic-link confirmation and the same-site hop (the page a cross-site
  OAuth link callback or webmail verification click lands on) were
  hand-written HTML. The new `AuthConfig.Pages` draws both, and
  `authui.Pages(host)` (`battery/auth/authui`) renders them as a
  `ui.AuthCard` through the app's UIHost. `MagicLinkConfig.ConfirmPage`
  is removed: implement `auth.Pages` instead, whose `ConfirmPageData`
  now carries the screen's words in the caller's locale. The hop now
  reloads through a `Refresh` header, so a page drawn through a UIHost
  (which drops a meta refresh from a screen's head) still continues. With
  `Pages` nil the plain documents stay.
- **BREAKING: a blank write in a declared `Access` block is refused.**
  Declaring `Access{Read: "posts:read"}` to gate reads left Create, Update
  and Delete open to every caller, anonymous ones included. Once an
  `Access` block names any permission, a blank `Create`, `Update` or
  `Delete` now answers **403** for every caller, a wildcard role included,
  and its MCP tool is refused too; server code under `crud.WithElevation`
  still writes. The new `framework.Anyone` (`anyone` in an app spec or
  entity declaration) opens an operation on purpose. A blank `Read` still
  leaves reads open.
- **A reboot no longer undoes a process-module revoke.** The next boot
  compared the descriptor with the stored row, read the revoked grant as a
  grant change, wrote it back and disabled the module. The store now
  records revokes (`DesiredState.RevokedGrants`, a `revoked_grants` column
  `EnsureSchema` adds to an existing table), the boot subtracts them from
  the descriptor, and only a grant the row does not hold disables the
  module. A descriptor that only drops grants reports the new
  `InstallGrantsNarrowed` outcome and keeps the module enabled.
- **A revoked process-module grant stops working when the drain starts.**
  A revoke or disable drains the child for up to `DrainPerModule` (30 s by
  default), and the broker kept answering its `host.*` reverse calls under
  the old grant until the child exited. Those drains now refuse reverse
  calls at once; an upgrade that takes no grant away and the shutdown drain
  keep them open. `moduleproto.WithServeGate` is the new peer option behind
  it.
- **BREAKING: an rtc join with no user is refused unless the app opts
  in.** Every caller whose `Join.User` was empty shared one seat bucket,
  so one anonymous caller opening sockets displaced every other anonymous
  caller across all rooms. `Authorize` returning an empty `User` now gets
  a 403 (`rtc.user_required`) before the upgrade. An app whose callers
  are anonymous sets `Config.AnonymousSeats: rtc.AnonymousPerSocket`:
  each such socket holds its own seat and is never displaced by another.
  `examples/rtc-call` opts in.
- **BREAKING: webhook signatures cover the event name and delivery id.**
  `v1` signed `<ts>.<body>`, so a delivery captured inside the tolerance
  window could be re-sent with another `X-GoFastr-Event` or
  `X-GoFastr-Delivery-ID` and still verify. It now signs
  `<ts>\n<event>\n<delivery id>\n<body>`. `SignWithTimestamp` and
  `VerifyTimestamped` take the event and id; `TimestampedVerifier` reads
  them from the request. Every receiver must update its verifier.
  ([webhooks](framework/docs/content/webhooks.md))
- **A password reset removes every OAuth link.** On a verified account
  the links survived, so an IdP identity linked from a stolen session
  signed the attacker back in after the owner's reset. The reset now
  unlinks every provider and deletes its stored tokens (an account claim
  deletes them too), then mails a notice naming the unlinked providers
  (`PasswordResetConfig.NoticeTemplate`). ([auth](framework/docs/content/auth.md))
- **BREAKING: `access.Can`, `CanResource` and `CanResourceExact` check
  token scopes.** A scoped API token passed them on its owner's roles, so
  an `orders:update` token an admin minted could run an `orders:approve`
  transition or the state override. They now also require a scope
  granting the permission, as `RequirePermission` does, and a Decider's
  Allow does not lift it.
  ([access control](framework/docs/content/access-control.md))
- **BREAKING: `auth.RequireRole` and `auth.MCPRole` need a role scope on
  an API token.** A token carries its owner's roles, so a `customers:read`
  token an admin minted passed `RequireRole("admin")`. A scoped token now
  counts a role only when it holds `role:<name>` (`role:*`, `*:*`), as
  `framework.MCPRequireRole` already required. Sessions and JWTs are
  unaffected. ([auth](framework/docs/content/auth.md))
- **BREAKING: WebSocket text writes must be valid UTF-8.** `Write`,
  `WriteString` and `Hub.Broadcast` sent any bytes as a TEXT frame, and
  a browser then dropped the connection with 1007. They now return
  `stream.ErrInvalidUTF8` and send nothing; `Hub.Broadcast` and
  `BroadcastWait` return `error` for it. New `WriteBinary`,
  `BroadcastBinary` and `BroadcastBinaryWait` send BINARY frames, in
  order with text, through `Close`'s drain too. A `StateChannel`
  snapshot that is not valid UTF-8 closes the connection; such an event
  is dropped for that role.
- **BREAKING: an empty query or header value is treated as absent.**
  `?page=` answered 400 on a number field, and `?q=` bound `""` to a
  string. Every kind now leaves the field unset, as if the parameter
  were missing. A `,empty` tag option (`query:"q,empty"`) keeps an
  explicit empty: `""`, a zero, or a non-nil pointer to one, so a
  handler can tell `?q=` from no `q`. A generated endpoint's optional
  `*string` query field now gets nil for `?q=` unless the input tags
  it `query:"q,empty"`, which the endpoint generator now accepts. An
  unknown tag option is refused.
- **Tag binding honours `encoding.TextUnmarshaler`.** A query, path,
  header or tool parameter whose type implements it is filled through
  `UnmarshalText`, and its error is a 400. `?status=bogus` used to get
  past a type that validates in `UnmarshalText`; `time.Time` and UUID
  types now bind from a query.
- **A tool call refuses case-folded twin keys across the whole params
  object.** `{"q":"safe","Q":"danger"}` bound `danger` to a `query:"q"`
  field while `params["q"]` read `safe`.
- **`middleware.Tracing` joins a trace from the configured propagator.**
  It injected with the global otel propagator but always extracted W3C,
  so an app on B3 or Jaeger started a new trace per request. It now
  extracts with the global propagator and falls back to W3C
  `traceparent` when that finds no upstream span; with no global
  configured, nothing changes.
- **The SQL idempotency store refuses a NUL principal on Postgres.** It
  can't be stored as text, and escaping it could match another user's
  id. That keyed write answers 503 (or runs unkeyed under `FailOpen`);
  every other principal is stored as is.
- **A Group's `NotFound` and `MethodNotAllowed` answer misses under its
  prefix.** They ran only when the group itself was served, and every
  app serves the root, so a group's custom 404 was silently ignored. A
  miss now runs the handler of the longest-prefix group that set one
  (`{name}` segments and host groups match), else the root's.
  `WrapNotFound` on a group wraps the handler it would inherit.
- **BREAKING: a top-level `Group` prefix starts with `/` or names a
  host.** `Group("v1")` registered routes for Host `v1`, so `/v1/x`
  answered 404 with no error. It now panics with the fix (`"/v1"`). A
  host group needs a dot or `localhost`, and no port, since the mux
  matches the request host without its port and `Group("localhost:8080")`
  could never match. Nested groups are not checked.
- **The SQL idempotency store runs on Postgres.** Its table declared
  `body BLOB`, which Postgres has no type for, and its key joins its
  parts with NUL bytes, which Postgres text refuses, so
  `NewSQLIdempotencyStore` failed on Postgres and could never store a
  key. The body is `BYTEA` there and the key is escaped on Postgres
  only; SQLite keys are unchanged. See `idempotency.md`.
- **A panic no longer corrupts a response that already started.**
  `Recovery` and `handler.HandlerAdapter` cut the connection instead of
  writing a 500 after a status, a flush or body bytes went out; before,
  the client got a 200 with error text appended. Recovery's 500 drops
  the handler's representation headers (`Content-Encoding`, `ETag`, …)
  and is sent `Cache-Control: no-store`. The panic stack in the log
  names the handler, not `Timeout`. Metrics and tracing count a
  panicked request as a 500 (or the status already sent) and mark its
  span Error. See `security.md` and `observability.md`.
- **Middleware status and claim bookkeeping.** An idempotency claim is
  released when the handler panics, so a retry no longer gets 409 until
  the in-flight TTL. A failed `Hijack` under `Timeout` keeps the writer
  buffered and the deadline armed. The logging, metrics and tracing
  wrappers record the status the client got (1xx passes through, a
  first write or flush latches 200). `Compress` maps its `-gzip` ETag
  back for `If-Match` too. A body cap of `math.MaxInt64` no longer reads
  an empty body. `WarnIfCSRFUnconfigured` accepts a nil `*slog.Logger`.
  An `Idempotency-Key` outside printable ASCII answers 400; on Postgres
  it reached the store and answered 503.
- **`core/handler`.** A pointer input (`Handler[*T, O]`,
  `ToolAdapter[*T]`) gets the struct rules: duplicate, case-folded and
  unknown keys are refused and query, path and header tags bind. A body
  over the cap answers 413, not 400. A `Handler[I, *Out]` returning
  `(nil, nil)` answers 204; a nil slice or map is still a 200.
  `WriteError` sends a nil `*Error` or a code outside 400–599 as 500.
  Numbers under an unexported embedded struct settle to `float64`.
- **`core/stream`.** WebSocket `Close` writes the messages `Write`
  already accepted before its Close frame, bounded by `WriteTimeout`,
  which also cuts a frame stalled on a peer that stopped reading; a
  close the peer started replies at once. A malformed frame closes with
  1002 and a message past `ReadLimit` with 1009, instead of an empty
  status. The upgrade refuses
  HTTP/1.0 and reads `Connection` and `Sec-WebSocket-Protocol` across
  every header line. A negative `SendBuffer` means the default instead
  of panicking after the hijack. `Hub` and `StateChannel` stop a
  registration's close watcher on `Unregister`. An SSE id made only of
  control bytes writes no empty `id:` line, and a subscriber's `?event=`
  filter applies before its buffer, so ignored events no longer evict
  wanted ones.
- **`router.Handle` names an unclean pattern** (`Group("/api/")` builds
  `/api//x`) as an invalid route, not a conflict with a registration
  that does not exist.
- **Headless accessibility decisions.** `headless.RangeSliderProps` /
  `ui.RangeSliderConfig` `ValueLow`/`ValueHigh` are `*int` (BREAKING): nil
  is the unset end and a set `0..0` pair round-trips. `headless.Rating`
  renders its stars in reading order, so arrow keys move the way the stars
  read (flipped in RTL); `ui.RatingInput` fills them with `:has()`. The
  password reveal is a toggle button with one name and word ("Show
  password", "Show") and `aria-pressed` as its state (BREAKING:
  `Strings.HidePassword`/`RevealHide`, `i18nui.PasswordInputHide`/
  `PasswordRevealHide` and the `data-hui-show/hide-*` hooks are removed);
  `ui.PasswordInput` shades the pressed button, and its typed text uses
  `--color-text` (a revealed password was black on the dark surface). A
  `ToggleAction`'s accessible name is its `IdleLabel` in both states. An
  unlabelled sidebar drawer trigger is named by the new
  `Strings.SidebarOpenDrawer` ("Open navigation").
- **Decimal writes are checked against their column.** A `decimal`
  field refuses, with a 400 naming the field, an exponent past ±1000
  (`decimal_exponent`), more integer digits than the column holds
  (`decimal_integer_digits`, 15 for the default `DECIMAL(19,4)`) or more
  fraction digits than its scale (`decimal_fraction_digits`, 4). Postgres
  refused the first two with a 500 and rounded the third while SQLite
  stored all three. A `RawType` of `DECIMAL(p,s)`/`NUMERIC(p,s)` sets the
  limit.
- **Malformed JSON bodies answer in the client's terms.** The message names
  the JSON path and type (`invalid JSON: ids[0]: expected a string, got a
  number`), the byte of a syntax error, or an empty or cut-off body,
  instead of `encoding/json`'s text naming the server's Go types. Every
  part is a phrase; `reason` codes are unchanged. `?where=`, `?cursor=`
  and `_batch` envelope refusals follow, and MCP `invalid params`
  messages use the same wording.
- **A screen answers GET and HEAD only.** POST, PUT, PATCH or DELETE to a
  screen path no route takes answers 405 with `Allow: GET, HEAD` (problem
  JSON for a machine Accept) instead of rendering the page, running its
  policy and `Load` and minting a session. OPTIONS answers 204. A route
  registered at the path still wins its method.
- **Slice-backed `ui.Counter`, `ui.Tabs` and `ui.SignalToggle` paint the
  request's seed**, not the slice's default: a counter seeded to 7
  rendered 0 while its first + made 8.
- **Headless.** A pane drawer releases its scroll lock when an island swap
  removes it; a bare `headless.Counter` steps from its own `Value`; when
  overlapping SortableList saves fail the list returns to the last order
  the server confirmed; a tab strip whose signal holds no tab of the strip
  writes the tab it paints back into the signal. Rating builds its radio
  ids from `ID` when set.
- **`ui.Repeater` and `ui.FormRepeater` controls are kit buttons** (ghost
  or danger Remove, secondary Add) instead of browser buttons or the
  form repeater's own button CSS; their lists drop bullets and indent.
- **A repeated scalar nested filter key** (`?author.age_lt=100&author.age_lt=5`)
  is a 400 `filter.repeated_filter`, as at the top level.
- **A disabled menu link row renders no `href`.** `aria-disabled` and
  `pointer-events` stopped a mouse, but a keyboard Enter or an
  assistive-tech activation still followed the link, as `headless.Button`
  already prevented.
- **`?q=` refuses text Postgres cannot bind.** A search term holding
  invalid UTF-8 or a NUL byte answers 400 naming `q` on both dialects
  (Postgres answered 500, SQLite an empty 200). BREAKING for direct
  callers: `filter.SearchConditions` returns `([]Condition, error)`. The
  search case contract now says that Unicode lowering also folds
  compatibility look-alikes (the Kelvin sign finds `k`), on purpose.
- **Query input (`framework/filter`, `framework/dsl`, `framework/pagination`).**
  Two `_in` keys on one column (its name and its `WireName`) are two
  ANDed IN lists, at the top level and inside `include=rel(...)`, not one
  union; new `filter.ParsedFilter.InList` / `SameINList` carry the key.
  A scalar filter key repeated in one query string is a 400
  (`filter.repeated_filter`), `LenientFilters` included: before, the
  first value ran and the rest were dropped. `?where=` decodes strictly
  (duplicate, case-folded and unknown members, a `value` the op does not
  read, an empty group: 400). Values Postgres cannot bind are 400s on
  both dialects: invalid UTF-8 or a NUL (nested `_like` included), a
  Decimal exponent past ±1000, a year-0 Timestamp or Date. DSL: every
  `where()` operator follows `filter.OpSuitsType`, `in` is capped at
  1000 values and `order()` at 16, unbalanced unquoted brackets are
  refused, `include("x")` keeps the bare name, `ASC`/`DESC` match ASCII
  only, and `after()` emits one `ORDER BY <cursor> ASC` and refuses an
  `order()` on another column or `DESC`. `query.Cursor` adds no second
  ORDER BY key for a column already ordered. `pagination.NewCursorPage`
  returns `ErrCursorKeyNull` / `ErrCursorKeyMissing` instead of minting a
  `"null"` cursor or `HasMore` with an empty one.
- **CRUD.** A `?page=` or `?offset=` whose skip overflows int, or too long
  to parse, is 400 `offset_too_large` instead of page 1. POST, batch
  create, `CreateOne` and `UpsertOne` refuse `""` in a one-column
  caller-chosen key. A batch update item with no key answers
  `reason: "missing_key"` with a phrase. Batch key-fold and multipart
  duplicate-key refusals are deterministic and phrased (`keys_fold`,
  `duplicate_form_key`, `duplicate_file_part`, `malformed_multipart`).
  llm.md no longer documents a cursor `total`.
- **UI host.** A `SetStaticFS` directory no longer shadows a screen of the
  same name (it answered a 301 into a 404). `RenderScreen`'s navigation
  answer carries the `#gofastr-signals-partial` seed. Navigating to an
  untitled screen, or a `RenderAlt` alt with no title, titles the tab
  with the app name instead of keeping the previous page's.
- **Headless markup.** Enter in a `StepWizard` field submits Next, not
  Back (a hidden twin of Next, part `default-action`, is the form's first
  submit); Enter in a `Repeater` field no longer submits row 0's Remove,
  and `Repeater.Action` is where add/remove post. The Carousel speaks its
  slide through a live region, off while it rotates. A named `Counter`
  binds its input's value. Gallery deeplinks survive `&`, `=` and `+`.
  A `Textarea` value that starts with a line break keeps it. A huge
  Pagination `Window` keeps the current page. Menu refuses duplicate ids
  under an id-less submenu and disables a Disabled form row's submit.
  Combobox Meta is its own part (`combobox-meta`). MultiSelect,
  NumberInput, Slider, RangeSlider, Rating and TagInput take `Ctx` and
  get unique ids per page. TagInput's label names its field and Help
  describes it. A disabled link Button drops its Action wiring. Only the
  last Href-less breadcrumb is current. JSONTree truncates on character
  boundaries. Alert names its dismiss from Text without a Title. Tree row
  links leave the tab order. Carousel and PaneHost words go through
  Strings (`CarouselRole`, `CarouselSlideRole`, `PaneSecondary`,
  `PaneTertiary`).
- **Headless behaviours.** Navigating away from an open phone-width pane
  drawer no longer leaves the next page scroll-locked. A link click
  closes only popups (Dismiss, Trap, menus) and only when followed, so a
  tab or pager inside an accordion no longer collapses it. One Escape
  closes one level, and Escape outside every disclosure closes popups,
  never accordion sections. Combobox ignores the Safari IME Enter
  (keyCode 229). Carousel ignores caret keys in a field and modifier
  chords, and keeps rotating in kept chrome after a navigation. The
  animated counter lets a live update stand; the number stepper lands on
  the step base. The copy button says "Copied" only when the clipboard
  write succeeds and announces `ui.copy.failed` otherwise. Conditional
  fields follow a form reset; the leave guard counts `form=` controls.
  SortableList flips cross-column arrows in RTL, cancels a grab when
  focus leaves, and a slow failed save no longer reverts a later one.
  Theme radios flip in RTL. Wizard and repeater focus return survives an
  unrelated insertion. The selection count uses the page's digits. Menu
  and tree type-ahead cycle on a repeated letter.
- **Layout primitives (`framework/ui`).** A bottom-edge `ui.Sticky` with
  an `Offset` takes it from the bottom instead of also pinning to the top,
  and a top Sticky draws its hairline only while stuck (browsers that
  report `scroll-state`), not at rest mid-page. `AspectRatio`, `Sticky`
  `Edge`/`Offset`, `Box` `Pad` and `Center` `MinHeight` panic on an
  unknown value, as `ZIndexTier` already did: an empty Ratio collapsed
  its media to 0px, and a spaced value added classes. A side-only
  `FormFrame` takes the full width. `Container{Start: true}` on the page
  measure keeps the band's measure. A nested layer's `.layout-content`
  inside a has-nav `ContentRow` is not padded a second time. The Sidebar
  marks `MatchPath: "/"` current on the root page only, compares a
  trailing-slash `MatchPath` in its bare form the way the runtime does,
  keeps `RaisedCurrent` on `SidebarBody`, the drawer and the NativeMobile
  body, and mirrors the compact guide line and the closed section chevron
  in right-to-left pages. New `HeroSplitConfig.HeadingID` names the hero
  through its h1 (`aria-labelledby`).
- **Slugs keep every script's letters.** `ui.Section` anchors and the
  headless Section, Table and Fieldset ids keep Unicode letters and
  digits, so a Japanese or Cyrillic heading keeps its anchor and two
  different ones no longer share `section--title`.
- **`axetest.Scan` fails when an axe rule crashes.** A rule that throws
  checks no node, and axe files it as incomplete, which the scan never
  read. The docs site's header had a `color-mix()` that serialised to
  `oklch(L C none)`; axe could not parse it, so `color-contrast` checked
  nothing on any page. Allowlisting the rule still skips it.
- **`ui.StepRail` links and meta line meet 4.5:1.** They used
  `--color-text-subtle` on the rail's `--color-surface-soft` (4.39:1 in
  the default light theme) and now use `--color-text-muted`.
- **`ui.CodeBlock` word marks keep token colors readable.** The mark
  tint drops from 28% to 12% of `--color-warning` and gains a
  warning-colored underline. In dark, 28% put function names at 3.91:1
  and comments at 2.96:1.
- **BREAKING: `desktopui.Theme()` is removed (#607).** It returned the
  macOS theme on every OS. Call `desktopui.ThemeFor(runtime.GOOS)`, or
  `ThemeFor("darwin")` for the old result. The primary color stays
  `#0071E3`: macOS's `#007AFF` fails WCAG AA for 13px button labels on
  white, and `TestPrimaryStaysAAUnderWhite` holds that line.
- **A queued desktop permission call gives up with its request.** While
  one permission alert was on screen, a second first-time call waited
  behind it even after its page aborted the fetch, then read the grant
  store on a dead context. It now returns `cancelled` as soon as its
  request ends.
- **Linux notifications name the current shell.** libnotify keeps the
  app name from its first `notify_init`, so a second shell lifetime
  with another title showed the first one's name. `Show` now sets the
  name every time.
- **A deep link can no longer carry a path separator or control byte
  past the navigate grammar.** The grammar decoded each segment but
  refused only `.` and `..`, so `gofastr-notes://files/..%2f..%2fsecret`
  reached a `GET /files/{name}` route with `PathValue("name")` set to
  `../../secret`, and `%00` arrived as a NUL byte. A segment that
  decodes to `/`, `\` or a control character is now refused, and the
  link is dropped with a Warn.
- **A deep link that arrives while the window comes up is no longer
  lost, and links keep their arrival order.** The battery checked for
  the window and wrote its pre-window queue under different locks, so a
  link could land in the queue after its one flush had run. On Windows
  and macOS each URL also got its own goroutine, so a later URL could
  reach the battery first. The check, the queue write and the flush now
  share one lock, and both shells hand URLs to the battery from one
  goroutine, at most 16 waiting.
- **On Windows, a second launch's deep link reaches the running app
  when a Settings window or widget is in front.** Every window the app
  opens shares one window class, and the second instance forwards to
  the first match in Z-order, but only the main window accepted the
  URL. The second instance reported "running app refused the URL" and
  the link was lost. Any of the app's windows now accepts it, and the
  main window comes forward and navigates.
- **`gofastr desktop feed` no longer signs archives the updater
  refuses.** The feed writer extracts the archive with the updater's
  own extractor before signing. A stray top-level file (`README.txt`,
  `.DS_Store`), a missing directory entry for the bundle root or a
  symlink now fails at release time, not on every client after it
  ships.
- **A retried update install no longer deletes the only copy of the
  app.** When moving the new bundle into place failed and moving the
  old one back failed too, `<Name>.app.old` was all that remained, and
  the next install removed it before swapping. An install now refuses
  when the running bundle is missing, and the log says to rename
  `<Name>.app.old` back.
- **Updater log lines no longer carry filesystem paths.** Failed
  renames, removals, moves and staging-directory creation logged OS
  errors with absolute paths under the user's home directory, and
  codesign output named the staging path. They now log the operation
  and the OS error (`rename: permission denied`), and command output
  names files relative to the bundle, as the desktop docs already said.
- **A typed Before hook takes a form post.** `OnBeforeCreate[T]` and
  `OnBeforeUpdate[T]` decoded the body before the blank and number
  cleanup, so a blank optional int, a `"2"` or a checkbox's `"true"`
  failed with a 400 naming the Go struct. The cleanup now runs before
  the hooks too, an Int column takes decimal text as the integer, and a
  body that still does not fit `T` gets a plain phrase instead of the
  `encoding/json` detail.
- **Shutdown closes connections accepted while it ends streams.** A
  connection that arrived during the WebSocket close wait (up to 2s)
  sat idle and held `Server.Shutdown` for five seconds. The close now
  runs as a shutdown hook, after the listener stops accepting.
- **Core UI runtime bug bash (lazy loading, CSS/HTML/JS chunking).**
  - Asset serving: `/__gofastr/runtime/<name>.js` and `/__gofastr/compute/*`
    cache `immutable` only when `?v=` names the served bytes (otherwise
    `no-cache` with an `ETag`), so a rolling deploy cannot pin one
    build's code under another build's URL. A refused behaviour graph
    answers 500 on every request instead of serving an empty
    `manifest.js` after the first, and a `framework.App` refuses to boot
    on one. Static exports write registered compute workers and WASM.
    `RegisterBehavior` after the registry froze panics, as
    `RegisterStyle` does.
  - Component CSS: an unregistered `data-cui-comp` marker no longer
    turns the page's bundle into a 404; lazily loaded sheets land before
    `app.css` (now `id="cui-app-css"`) so host overrides keep winning; a
    failed sheet or bundle is retried; markup from any insertion path
    loads its sheets; an embed frame paints its content styled.
  - Navigation: a repeat click on a loading link, a link to the current
    page and a click a listener already prevented no longer reload or
    navigate; an untitled screen's partial carries the app name as its
    title; Back across a `#hash` jump and to an earlier visit of the
    same path restores the right scroll; prefetches under a
    percent-encoded layer key are used; a forced refresh is never
    answered by a stale cache replay.
  - RPC and widgets: field errors from typed handlers
    (`handler.ValidationError`) reach the form; a per-row widget opened
    from another row remounts with that row's context; a confirm dialog
    inside a modal widget gets Tab and Escape; a submit cancels a
    pending debounced input request; RPC clicks inside server-inlined
    widget chrome work before the catalog mounts it; signals named like
    `toString` seed; a computed signal keeps an untrusted input
    untrusted; a re-inserted poll region polls again.
  - The JS minifier no longer hangs on `x = {} / 2` followed by a newline,
    and keeps ASI newlines, regex flags, empty statements, CR/U+2028
    line ends and class fields named `const` intact.
  - `data-action-mount` fires for markup inserted outside a navigation
    (an intercepted route's overlay, an island swap, widget chrome), once
    per element, and the component's compiled action script loads for it.
    Static exports stop preloading `rpc.js`, which their runtime never
    loads.
  - `reveal` leaves content already on screen at load visible instead
    of blinking it; the theme toggles work with site storage blocked.
- **Images.** `StripMetadata` keeps the Adobe colour marker, so a
  stripped CMYK JPEG still decodes. Image-field renditions and
  placeholders follow the upload's EXIF orientation. A long upload name
  no longer fails when renditions are stored: rendition names are cut
  from the front and keep the random token (`image.MaxVariantNameBytes`,
  and `CheckVariantNames` refuses a suffix that leaves the base fewer
  than `image.MinVariantBaseBytes`). EXIF orientation is read past 0xFF
  fill bytes.
- **An upsert that updates an existing row refreshes `updated_at`.** One
  that sends only the key leaves it alone, on owner- and tenant-scoped
  entities too.
- **WebMCP tools registered with `Handle` on a grouped router advertise
  the prefixed path**, so in-browser agent calls reach the route.
- **Process modules.** An over-cap request answer no longer blocks the
  read loop; a peer that stops reading faults with `ErrStalled`. A
  proxied request whose base64 frame would pass `Limits.FrameBytes` gets
  413 instead of a retryable 503. A cancelled MCP tool call sends
  `module.cancel` to the module.
- **YAML quotes.** `name: O'Reilly # note` drops the comment, and
  `[O'Reilly, Jane]` parses as a list.
- **Kiln keeps integers past 2^53 exactly** in tool calls through the
  agent loop, MCP and ACP.
- **Sub-millisecond theme durations emit valid CSS** (`0.5ms`, `2.5ms`).
- **The notify email channel accepts what the SMTP sender accepts**
  (`o'brien@example.com`, `App <noreply@example.com>`) and refuses a
  comma list. New `email.ValidateAddress`.
- **Contracts rules.** GOFASTR1410 catches `os.WriteFile` and `os.Root`
  writes and storage types declared in another file, and an unrelated
  Fold-named call no longer silences it. GOFASTR1409 no longer accepts
  `IS NOT NULL` or a token clear as a fence, checks the whole package,
  and accepts reversed lease bounds and cancelled or expired statuses.
  GOFASTR1408 catches counters written through `CAST`, `COALESCE`,
  `LEAST`, `::int` or a quoted column. GOFASTR1411 and GOFASTR1413 match
  header names in any case. GOFASTR1412 catches single-quoted URL
  attributes and an escaped value held in a variable. GOFASTR1414 counts
  a literal as SQL only when a statement verb starts it, and reads an
  `UPDATE` with a table alias or `ONLY`. GOFASTR1415
  reads field-assigned eraser tables and `ALTER TABLE … ADD COLUMN`.
- **A panel opened just before a navigation no longer opens, or writes
  its deep link, on the next page.**
- **Signal attributes.** A signal bound to `disabled`, `hidden`,
  `checked`, `open` or `selected` removes the attribute on false
  (`disabled="false"` disabled the control), and `checked`, `selected`
  and `value` keep following the signal after the user edits the field.
  `store.Slice.BindAttr` leaves a false boolean out of the server render
  too, so the control no longer arrives disabled or hidden. A named bool
  type and the string `"false"` count as false there.
- **Escape closes a popover opened inside a modal before the modal.**
- **Removing the last live island closes the SSE stream.**
- **A percent-encoded fragment finds its target** (`#caf%C3%A9` →
  `id="café"`).
- **Client navigation honours `:int`, `:uuid`, `:alpha` and `:alnum`
  route constraints**, so it judges the document boundary on the route
  the server serves. It decodes each segment first, as the server does,
  so `/orders/%31` is the `:int` route on both sides.
- **A corner or unanchored popover, and a `FloatingPanel`, render their
  panel surface.** New `widget.Builder.Surface()`.
- **Toasts.** The timer runs only while the row is neither hovered nor
  holding focus, and a row keeps one timer at most.
- **Combobox, SearchInput and `data-cui-clear-on-esc` leave IME
  composition keys alone.**
- **Lightbox arrow keys step the gallery only while the lightbox is the
  top modal.** Alt, Ctrl, Meta or Shift with an arrow stays the
  browser's (history, word jumps, selection), including while the
  lightbox module is still loading.
- **MultiSelect keeps focus after a chip is removed** (next chip,
  previous chip, or the trigger).
- **SearchInput, FileDropzone, autogrow textareas, dropdowns, the form
  helpers (`data-cui-clear-on-esc`, `-persist-storage`, `-tick-elapsed`
  and the rest) and local forms wire up when a swap hands over the marked
  element itself.**
- **Rail re-resolves a swapped region, targets or links** on the next
  arrival.
- **TagInput applies its maxlength in code points, before the duplicate
  check.**
- **The mobile sidebar drawer is announced as "Navigation"**
  (`ui.sidebar.drawer`).
- **Desktop.** Linux apps start after a crash left the cookie database's
  rollback journal behind, and a persistent profile relaunches after
  WebKit caches a body of 16 KiB or more: the cache hard-links it inside
  the profile, and startup refused every hard link. A double-clicked Install no longer runs two
  updates at once. Closing a secondary macOS window frees its web view,
  and a call on a closed window reports it closed. On Windows, a
  forwarded deep link keeps a maximized window maximized, and window
  titles longer than 1023 UTF-16 units read back whole.
- **BREAKING: a desktop menu `Key` needs cmd, ctrl or alt.** A bare or
  shift-only key (`n`, `shift+n`) took that keystroke from every text
  field; `New` now refuses it.
- **CLI.** `gofastr verify --sarif --json` (any valued flag followed by
  a flag) errors instead of writing a file named `--json`. The removed
  `gofastr migrate diff` exits non-zero. `gofastr semantic clear`
  refuses while `index` or `watch` holds the index, and keeps the lock
  file.
- **Kiln.** A `delete_entity` that SQLite refuses leaves the CRUD rows
  in place. Generated seed ids are journaled, so a restart, reset or
  undo rebuilds them with the same ids; journals written before this
  still get fresh ids.
- **Repo tooling.** `make bench` and the tier targets fail when a
  benchmark fails, and the pre-commit hook builds vettool in a private
  temp dir.
- **An offset with no limit runs on SQLite.** `query.Build` wrote
  `OFFSET $n` alone, which SQLite refuses, so `TypedQuery.Offset`
  without `Limit` failed there and ran on Postgres. An offset alone now
  carries the largest `LIMIT` both dialects accept.
- **WHERE conditions with comments or `ESCAPE'\'` bind the right
  arguments.** The `core/query` builders renumber each condition's `$N`
  placeholders, and three spellings broke that. A quote right after a
  word ending in E (`ESCAPE'\'`, `LIKE'x\'`) was read as a Postgres
  `E'…'` string, so the placeholders after it kept their numbers and
  bound the wrong arguments; only an E that starts a token opens one
  now. A condition ending in a `--` comment swallowed the closing paren
  and every clause after it, so the statement did not parse; the
  builder now ends the line first. A block comment holding `/*` (a glob
  like `/api/*`) left the placeholders after it unrenumbered, though
  SQLite ends the comment at the first `*/` and runs them; when the
  comment would otherwise never close, it now closes there.
- **`kiln agent` starts when ports 8765–8774 are all busy.** The
  fallback handed serve port 0 while the panel URL said `localhost:0`,
  so the readiness check timed out and killed a healthy server. The
  fallback now asks the kernel for a free loopback port and uses it.
- **BREAKING: `freeze.FreezeAndGenerate` takes a context.** A generator
  that exited while a child it started still held the output pipe hung
  the call until that child exited. It now runs under the caller's
  context and collects output for at most two seconds after the
  generator exits. Pass the context first:
  `freeze.FreezeAndGenerate(ctx, w, dir, out)`.
- **An ICE URL with no port is accepted, and one with a path is
  refused.** `stun:stun.example.net` and `turn:[2001:db8::1]` were
  refused, though RFC 7064/7065 make the port optional, while a host
  carrying `/path` passed. A host is now a DNS name, an IPv4 literal or
  a bracketed IPv6 literal (brackets around anything else are refused),
  and a path, userinfo or fragment is refused in the `stun://host` form
  too.
- **`gofastr desktop build` on macOS refuses a GOARCH other than arm64.**
  The build set arm64 whatever `GOARCH` asked for, and an Intel Mac got
  a bundle reported written that could not launch. The desktop shell is
  arm64-only, so another `GOARCH` is an error, and an Intel host gets a
  warning that the bundle will not run there.
- **Kiln's `min` and `max` compare integers exactly.** Integers were
  compared as float64, so past 2^53 two neighbours looked equal and
  `min(9007199254740992, 9007199254740993)` returned the larger.
- **A span's `error.type` sees through an error with several `%w`
  verbs.** `fmt.Errorf("begin: %w (attempt %w)", err, ctxErr)` reported
  `*fmt.wrapErrors`; it now reports the first wrapped error's type.
- **The cache battery stops `MemoryCache`'s cleanup goroutine.**
  `OnStop` looked for `io.Closer`, but `MemoryCache.Close` returns
  nothing, so the goroutine outlived the app.
- **`gofastr test` runs the packages it is given and refuses unknown
  flags.** It dropped every argument it did not know and ran `./...`,
  so `gofastr test ./pkg` or `--run TestFoo` (the space form) ran the
  whole suite and could exit 0. Package patterns are passed through,
  `--run`, `--bench` and `--timeout` take their value either way, and
  another flag is an error.
- **A language refused with `q=0` stays refused when a longer tag
  falls back to it.** `Accept-Language: en;q=0, en-GB;q=0.9, fr;q=0.8`
  served `en`, reached by trimming `en-GB`, ahead of the accepted `fr`.
- **`store.Computed` validates its dependency names.** A dep holding a
  comma split into two signals on the client, and an empty or reserved
  name shipped a binding that never fired. Deps follow the slice-name
  rules, and `route.*` signals stay readable.
- **BREAKING: `ui.SparklineConfig.Color` takes a theme preset only.**
  The doc said a raw CSS color passed through, but every value other
  than `success`, `warning`, `danger` and `info` drew the default, and
  a value with a space added classes to the chart. The field is now a
  `ui.SparklineColor` (`SparklinePrimary`, `SparklineSuccess`,
  `SparklineWarning`, `SparklineDanger`, `SparklineInfo`): a quoted
  preset still compiles, a `string` variable needs the conversion, and
  any other value panics.
- **desktop-focus completes a session paused at 00:00.** The pause
  stored no time left and a paused tick never completes, so the timer
  froze and no break started.
- **Entities on tables that differ only by case get the column-conflict
  check.** `Widgets` and `widgets` are one table once AutoMigrate's
  unquoted DDL folds them, but the registry compared names exactly, so a
  second entity's wider `String.Max` reused the first one's
  `VARCHAR(10)`. The names are folded and the conflict panics at
  registration.
- **An include that names a relation twice returns each row once.**
  `?include=comments(status=draft),comments(body=x)` loads both filters
  into one list, and a comment matching both came back twice, at the top
  level and one hop down.
- **The log webhook sink refuses a write that races Close.** A write
  that passed the closed check while Close ran could append behind the
  final drain, an entry never sent and never counted in `Dropped()`. It
  returns `ErrSinkClosed`.
- **`cmd/mutate` no longer scores a slow build as a caught mutant.** A
  mutant run past its deadline counted as caught, though with a cold
  cache the deadline usually went on the build and no test ran. It is
  caught only when the test binary hit its own `-timeout`, otherwise the
  run stops with a harness error, and mutant runs get the baseline's
  five-minute build allowance.
- **webmcp-remote-assist answers 410 for a command on a session swept
  mid-command.** The command looked the session up and wrote to it under
  two separate locks, so a sweep in between dropped it and the caller
  still got 200.
- **The character counter and elapsed ticker follow the reader's
  locale.** `data-cui-charcount-source` wrote `1234 chars` and
  `data-cui-tick-elapsed` wrote `2.4s` on every locale. Both say their
  words through the `runtime.charcount`, `runtime.elapsed_ms` and
  `runtime.elapsed_s` phrases, with the number in the page's language.
  `core-ui/check.LintRuntimeProse` now also refuses a word glued to a
  value with `+`, the shape both sites used.
- **The elapsed ticker stops when no ticker is left.** After a client
  navigation removed every `[data-cui-tick-elapsed]` node, the interval
  kept querying the document five times a second. It clears itself, and
  the next ticker the scan finds starts it again.
- **A widget poll never stops on a stale or partial snapshot.** The
  `/state` handler read the signals and asked `Builder.PollTerminal`
  afterwards, so a job that finished in between stopped the poll on a
  value still saying "running". It asks first now, and sends no
  `X-Gofastr-Poll-Stop` on a response missing a signal whose read or
  JSON encoding failed.
- **`cache.GetOrSet` callers leave at their own deadline.** A stalled
  loader held every caller of the key past its deadline, after the
  timeout middleware had already answered. Each caller now returns
  `ctx.Err()` at its deadline while the fill runs on for the others,
  and the loader's context is cancelled once no caller waits. A fill no
  caller waits for stores nothing, so it cannot overwrite a value a
  later caller filled, and it never writes the destination of a caller
  that left. A cache with value receivers fills alone for each caller,
  who still leaves at the deadline. A loader panic comes back as an
  error instead of a re-panic.
- **A rolled-back update that only set a column to NULL publishes no
  event.** The ambient-transaction confirm query skipped NULL values, so
  such an update was confirmed on its key alone. NULLs are matched with
  `IS NULL`.
- **`migrate` group mode works with a tracking table in an ATTACHed
  SQLite database.** A name like `aux._migrations` made the column and
  key probes miss the table and the key upgrade build an invalid temp
  name, so every `Up` failed once a group was registered.
- **The desktop window closes when the app server stops under it.** A
  listener failure or a `Shutdown` from elsewhere after the window
  opened left it on a dead app until the user closed it. `Run` now quits
  the window and returns.
- **The OpenAPI write bodies match what the server reads.** The create
  body listed the owner and `MultiTenant` tenant columns, required when
  declared required, though the server stamps both. And the PUT body
  kept the entity's required list, though PUT runs the same sparse
  update as PATCH. No write body lists the scope columns now, and
  neither update body requires a field.
- **`PgVectorStore` ranks a zero-vector document at score 0.** The query
  ordered by raw distance, where pgvector's NaN sorts last, so the
  document ranked below negative matches and a small `topK` dropped it.
- **A closed `semantic.Index` refuses writes.** `Add` and `Remove` after
  `Close` returned nil for a change that was gone on reopen; they and
  `Snapshot` return `semantic.ErrClosed` now. A second `Close` returns
  nil instead of snapshotting stale state over the next writer's
  directory.
- **kiln's Claude Code adapter limits the agent to Bash.** It passed
  `--allowedTools Bash`, which only skips Bash's permission prompt, next
  to `--dangerously-skip-permissions`, so the agent kept Read, Write,
  Edit and the rest. It passes `--tools Bash` now. Bash still reaches
  the disk: this is no sandbox.
- **An rtc status change from another replica reaches local peers once,
  and only from the peer's current replica.** A remote status was applied
  to whichever replica's copy of the member it named, then dropped if it
  repeated, so a roster beat that repaired a missed status sent nothing,
  and a status from a replica the peer had left was announced over the
  newer one. Status now goes through the merged remote view, as joins
  and leaves do.
- **The SQLite legacy-timestamp normalizer keeps a lease taken while it
  runs.** The queue and outbox rewrote each row's times from a read made
  before the write, so a `claimed_at` a worker set in between went back
  to the old value. Each column's write now checks it still holds the
  text that was read. The queue also stopped rewriting rows that were
  already in the canonical layout, which it did on every boot.
- **`TryEntity` refuses routes that overlap an existing route.** Its
  preflight compared patterns for equality, so `GET /{kind}/_events`
  next to an entity's `GET /posts/{id}` passed it and the mux panicked
  after half the routes were registered. The preflight asks the same
  question the mux does, for the CRUD routes and the declaration's
  endpoints, before anything is registered.
- **A local-entity form keeps an untouched timestamp's seconds.** The
  `datetime-local` control shows minutes, and saving wrote what it
  showed, so editing any other field cut `14:30:45.123Z` to `14:30`. A
  control the user did not change saves the stored value.
- **Semantic coverage keeps every package's records when test binaries
  finish together.** Each `Flush` read the manifest, merged and renamed
  over it, so the later of two writers dropped the earlier one's routes.
  The merge now runs under an OS file lock, as the axe-coverage manifest
  already did.
- **A desktop `Quit` on Windows before the main window exists is
  kept.** A `Quit` after `Run` started but before the window was created
  had no window to post to and was dropped, so the window opened on an
  app that had been told to quit. `Run` checks again once the window is
  up.
- **A handler that sends 103 Early Hints keeps its real status.**
  Idempotency, metrics, tracing, timeout, the access log, the cache, the
  dev live-reload injector and the test-binary render writer each took
  the 103 as the response, dropped the handler's own status, and the
  client got 200. The idempotency store also never saved the response,
  so a retry ran the handler again. Each wrapper now forwards an
  informational status and waits for the final one; the buffering
  wrappers (timeout, cache) drop the hint. The WebMCP observer reported
  such a tool call as status 103, and the dev retired-markup scan read
  the body kind from the header as it stood at the hint.
- **Kiln's `set_theme` no longer undoes an app config edit that landed
  while it ran.** It wrote back the whole config it had read, so a
  rename in between was lost. A config edit built from a config the
  world no longer holds now answers `conflict` (the journal returns
  `journal.ErrAppConfigChanged`); replaying an older log still applies it.
- **A double click on an RPC span or link sends one request.** Only
  buttons and inputs were disabled while in flight, and two clicks
  before the rpc module loaded both dispatched. A trigger that carries
  no value and no response signal now ignores a click while its request
  is in flight; forms, controls and signal-targeted triggers still let
  the newest dispatch win.
- **A webhook claim with no lease recorded is delivered.** A host store
  that claims rows itself leaves the lease unset, and the worker read
  that as expired, so it handed every claim back and sent nothing.
- **A WebSocket server that accepts and closes at once is not redialed
  every second.** The backoff reset on every open, so a 4503 "not now"
  close came back after one second forever. `attempts` now resets only
  after a socket stays open 10 seconds.
- **A `%` in the page title no longer breaks client navigation.** A
  screen with no title sends no `X-Gofastr-Title`, and the runtime
  decoded `document.title` in its place; an app named "50% Off Co."
  threw on every such navigation and showed the connection error. Only
  the header is decoded now. The `decodeuriraw` lint fires on a header
  or `document.title` decoded outside `try`.
- **Escape leaves a popover open under a modal that refuses Escape.**
  The key fell through to the popover stack and closed a popover beneath
  the scrim. A popover opened after the modal still closes.
- **Back keeps the scroll anchor of a page visited recently.** The
  50-page anchor store evicted paths in alphabetical order, so a busy
  `/orders` lost its anchor to a page seen once. It evicts the least
  recently recorded now.
- **A hover or visible prefetch keeps the link's query.** It fetched the
  bare path, so the click on `/detail?page=2` missed the entry and
  fetched again.
- **`gofastr dev`, `desktop run|build|types` and `new` refuse an unknown
  flag.** A typo kept the defaults, and a flag with no value took the
  next flag as one: `--addr --no-a11y` set the address to `--no-a11y`
  and kept the a11y gate on. `new handler --method POST` (the space form
  its usage shows) scaffolded GET. `dev` refuses `--addr` with `-p`.
- **`gofastr migrate` refuses a flag its subcommand does not read.**
  `migrate down --db-ur=<url>` or `--db-url <url>` rolled back the
  database `DATABASE_URL` names.
- **CLI rewrites of existing files are atomic.** `init . --force`,
  `init`'s `.gitignore`, `AGENTS.md` and `CLAUDE.md` merges, `agents
  sync`, `theme init --force`, `pack -o` and the unmount/remount import
  rewire truncated each file and wrote it in place, so a crash or a full
  disk left it empty or half written. Each now writes a temporary file
  beside it and renames it over, keeping the file's mode, writing
  through a symlink, and refusing a read-only file as before. An emptied
  `.agents/` detail file used to block the next sync as hand-written.
  The `worldreadable` vet check reads the new `atomicfile.Rewrite` as
  it reads `os.WriteFile`, so the switch hid no mode from it.
- **An rtc room spanning replicas stays within `MaxPeers`.** Two joins
  on different replicas could each pass the cap before their roster
  beats met, leaving a room capped at 1 with two members on both. Each
  replica now ranks the merged room by join order and closes its own
  peers past the cap with `4409` room full.
- **A WebSocket client cannot flood Ping or Pong frames.** The read pump
  answers each Ping itself, so rtc's `MaxFramesPerSecond` never counted
  them and a raw client could send them without limit. The new
  `WSConfig.MaxControlFramesPerSecond` (default 32 a second, burst twice
  that, negative disables) closes the connection with 1008 past it.
- **`core/yaml` skips a leading byte order mark and reads `#` after a
  tab as a comment.** A `gofastr.yml` saved with a BOM reported an
  unknown key naming an invisible character, and `port: 8080<TAB># note`
  kept the comment in the value.
- **`di.Provide` refuses a constructor with parameters, and a second
  Provide for a type that already has a value.** The first passed and
  then panicked in reflect on every request that injected it; the second
  returned nil while the first value went on being served.
- **The queue's admin job list reads a NULL payload as none**, as the
  dequeue path does. One such row (restore tooling, an external write)
  failed the whole list.
- **`AuthManager.Init` refuses a negative `SessionTTL`.** It used to
  become the 7-day default, against auth.md; zero still means seven
  days.
- **BREAKING: `TwoFAConfig.Digits` is removed, and the enrolment URL
  carries `TwoFAConfig.Period`.** The URL always said `period=30` while
  verification used the configured period, so with `Period: 60` every
  code was refused. `Digits` was never read: codes are six digits.
  Delete the field from your config.
- **`ui.NetworkRetryBanner` refuses an unsafe `HealthEndpoint`.** It
  went into the retry link's `href` with no URL policy; a value
  `urlsafe.CleanAnchor` refuses now panics, as an empty one does.
- **Web Bot Auth refuses a key whose `use`, `alg` or `kid` is not a
  string.** `"use":[]` or `"alg":false` read as absent, so the key
  passed both restriction checks.
- **BREAKING: passkey `MaxPerUser` holds when registrations race.**
  The plugin counted and then stored, so two registrations with one slot
  left both stored. `PasskeyStore.AddPasskey` now takes the cap and
  refuses past it with `auth.ErrPasskeyLimit`, counting and inserting as
  one step; a custom store adds the parameter. On Postgres the step is
  READ COMMITTED whatever the server default, behind an advisory lock on
  the table's resolved oid, so two spellings of one table share it.
- **`kiln acp --agent X` no longer runs the agent on a refused ACP
  prompt.** The ACP session journals the prompt and answers `refusal`,
  and the chat watcher picked the journaled message up and ran a turn on
  it anyway. The watcher now skips messages an ACP session wrote.
- **A Bool field refuses a number that only rounds to 0 or 1, and stores
  a text default as the boolean.** `1.0000000000000001` was stored as
  true and `1e-400` as false. A `Default: "true"` was bound as text, which
  `?field=true` never matched on SQLite.
- **A batch write keeps a JSON field's numbers the way a single write
  does.** The `_batch` envelope decoded item numbers as float64, so a
  `json` field's `1e400` failed the whole batch as invalid JSON.
- **`access.CachedResolver` at a zero TTL shares one lookup with its
  waiters.** The result expired as it landed, so each waiting call ran
  its own lookup in turn, in place of the single flight `WithTTL`
  documents.
- **The relay aborts an upstream response that runs past
  `MaxBodyBytes`.** The cap ended the body with a clean EOF, so a chunked
  response arrived as a whole 200 that was cut short, and `CacheOK` could
  keep it. It is now a read error, which aborts the stream.
- **`mcp.Server.ServeStdio` returns when its context ends on silent
  input.** It checked the context only between lines, so `kiln mcp` with
  an idle client ignored SIGTERM until the supervisor's SIGKILL. Kiln's
  stdio modes now exit 0 on the signal.
- **`App.Shutdown` cancels A2A runs and waits for them.** A run is
  detached from its request, so nothing stopped it: Shutdown returned
  while skills ran, and they died with the process. The new
  `a2a.Server.Shutdown(ctx)` refuses new runs, cancels the live ones and
  waits for each to store its final state.
- **Kiln refuses a symlinked adapter work directory.** A co-user could
  plant `/tmp/kiln-<adapter>` as a link to a directory this user owns;
  the chmod followed it, tightened that directory to 0700 and ran the
  turn inside it. The directory is opened without following a link and
  chmodded through the handle, so a name swapped for a link between the
  check and the chmod is refused too.
- **OIDC follows the provider's token endpoint auth method.** The secret
  always went in the form body, so a provider accepting only
  `client_secret_basic` could not log anyone in. A provider whose
  discovery lists Basic and not post now gets an `Authorization` header;
  `OIDCConfig.TokenAuthMethod` overrides discovery.
- **BREAKING: the admin reserves `/account` and `/shortcuts`, and audits
  a profile-name change.** An app `Page` at either path silently replaced
  the built-in screen; boot now fails. The name change wrote no audit row.
- **Strict JSON decoding folds nested keys the way the decoder does.**
  The nested-key check folded with `strings.ToLower`, which keeps `ſ`
  (U+017F) apart from `s`, while `encoding/json` binds `ſource` to the
  `source` field, so `{"source":"a","ſource":"b"}` passed and decoded
  as `"b"`. The new `handler.FoldJSONKey` matches `bytes.EqualFold`;
  `UnmarshalStrict` and every `CheckObjectKeys`/`CheckTopLevelKeys`
  caller in the repo (MCP, A2A, ACP, module protocol, cursors, crud
  numbers, the harness) use it.
- **A 2FA re-enrolment no longer takes a pending verify's write.** A
  disable plus re-enrol between `/2fa/verify`'s read and its update
  re-created the row at version 0 with the same empty codes and flags,
  so the swap marked the new pending enrolment verified with the old
  secret. `EntityTwoFAStore` starts a new row at a random version.
- **`ui.StatStrip` no longer marks its cells `data-cui-internal`.** A
  cell holds the caller's content, so an owned style sheet could not
  reach what the app placed there.
- **Kiln's semantic context fences each chunk with a run longer than
  any backtick run in it.** A chunk holding its own ```` ``` ```` line
  closed the fixed fence and left the rest as bare system-prompt text.
- **BREAKING: `handler.SSEStream` is removed.** It ranged over its
  channel with no request context and ignored write errors, so a quiet
  channel kept the handler parked after the client left, and it took no
  stream seat. Nothing in the repo called it. Server push goes through
  the one `/__gofastr/sse` bus, or `core/stream`'s `SSEBroker`, which
  admits through the seat policy and bounds each write. `handler.SSE`, the one-event
  response, stays and now reports a failed write.
- **CRUD log lines percent-encode control bytes.** A driver error
  quotes the request's value back (Postgres names the invalid `uuid` it
  was handed), so `GET /legacy/%1B%5B2J%0AFORGED` forged a log line
  through `CrudHandler.Get`. Every log line in `framework/crud` is
  scrubbed now.
- **The search results count follows the reader's locale.** The status
  line wrote 1234 as `1234` on every locale; it now reads `1 234` in
  French, as the chart numbers do.
- Custom not-found screens retain their context-aware title provider.
  `uihost.NotFoundTitler` can choose a title from the request path; empty
  titles use the standard screen title or generic not-found title.
  Navigation partials carry the effective 404 title instead of retaining
  the previous page title or borrowing the failed route's title, and an
  app with no default layout puts it on its bare 404 page.
  Unmatched routes retain their path when a root layout outlet also has
  a not-found fallback.
- **The admin's Needs attention panel reads clear under every locale.**
  It skipped an empty watched view only when the formatted count was
  `"0"`, so under `ar-EG` (`٠`) or `fa` (`۰`) each empty view drew an
  empty preview instead of the all-clear line. It compares the number.
- **Email verification links opened from webmail work.** A click in
  Gmail or Outlook on the web is a cross-site navigation, so the browser
  withheld the `SameSite=Strict` session cookie and `GET
  /auth/verify-email` always answered 401. That click now gets a page
  from this origin that opens the link again with the cookie, the hop
  OAuth linking already uses. See [auth](framework/docs/content/auth.md).
- **An `EmitAsync` subscriber no longer runs on a cancelled request
  context.** CRUD emits on the request's context, and the handler
  returned before the subscriber ran, so the webhook bridge's subscriber
  query failed with `context canceled` and a committed write lost its
  webhook. Subscribers and the fanout tap keep the context's values
  without its cancel, each bounded by the new `event.AsyncHandlerTimeout`
  (one minute). See [events](framework/docs/content/events.md).
- **Bulk "Every match" and CSV export on a keyed list stay in its view.**
  The every-match read rebuilt the list without its key, so it looked
  for `view` instead of `<key>_view`. On `List(e).Key("inv").View("open")`
  a delete of every match removed rows outside the open view, and the
  export held them too.
- **BREAKING: `RecordBuilder.Steps` takes the list the drawer opens
  from.** `Steps()` rebuilt a bare list, so a keyed list's params were
  read under the wrong names and its `View` and `Where` pins were
  dropped. Previous and next could land on a record the list did not
  show. Call `Steps(list)` with the same `ListBuilder` the list screen
  renders; the admin battery does.
- **BREAKING: CRUD events carry their record's owner into the outbox.**
  `crud.EventOutbox` declares `AppendFor(ctx, ex, owner, eventType,
  data)` in place of `Append`, and the outbox table gains an `owner`
  column, so `App.EraseUserData` sets an erased user's staged event
  payloads to JSON `null`. A custom `EventOutbox` renames its method and
  takes the owner. The outbox adds the column on boot; a host that passes
  `WithoutEnsureTable` adds `owner TEXT NOT NULL DEFAULT ''` itself.
- **User data erasure reaches every store table.** `App.EraseUserData`
  and `App.ExportData` missed tables that held one user's data. These
  stores now register from their constructors: admin saved views, auth
  OAuth and API tokens (an API token owned by a service account with the
  same id survives), A2A tasks and push configs, entityui bulk jobs (the
  creator is anonymized), feature-flag user lists (the id is removed,
  the flag kept), the SQL idempotency store (a new `principal` column,
  added on boot), the outbox, outbound webhook deliveries, inbound
  webhook envelopes, search documents and pgvector chunks. The audit
  table is now exported too. Fanout spill rows expire on a purge ticker
  instead of waiting for a later large publish. `FlatStore`'s snapshot
  and WAL, and Redis queue persistence, are not reached by erasure.
- **BREAKING: configured store tables fold their case on Postgres.** A
  store that creates its own table quoted a configured name whole, so
  `Jobs` made a table `"Jobs"` that a host's raw SQL on `jobs` never
  found, and `app.Jobs` made one table named `app.Jobs` in the default
  schema. The queue (and its scheduler tables), outbox, fanout, A2A
  store, access grants, the auth OAuth-token, API-token,
  service-account and rate-limit stores, the audit table, the embed
  burn table, search, pgvector and the migrate tracking table now fold
  the name and quote each dotted part, the way AutoMigrate's tables
  are named. Index and trigger names follow their table: on Postgres
  the index name is bare and the table carries the schema, on SQLite
  the schema goes on the index name and on `PRAGMA table_info`, so a
  store configured as `main.jobs` or on an attached schema builds its
  indexes there too. The same holds for the auth user, OAuth-link,
  session and passkey stores and kiln's column read. On boot each
  store checks for a table an older build made with the case kept and
  refuses to start with the rename to run, instead of creating an empty
  table beside it (or, for the tracking table, re-running every
  migration). Lowercase names are unaffected.
- **Integers past 2^53 keep their digits.** These paths decoded JSON
  numbers under an `any` as float64, so 9007199254740993 came back as
  9007199254740992:
  - `handler.Bind` request bodies (a `map[string]any` field), and
    payloads that aren't maps reaching a typed hook;
  - events bridged across replicas (`event.AttachFanout`), outbox
    deliveries, and process-module events on the host bus;
  - a process module's tool input schema in MCP `tools/list`;
  - the record Activity tab, the admin audit log and the dashboard
    activity feed;
  - `headless.JSONTree`, and `EntityDeclaration` field defaults and
    properties decoded from JSON;
  - A2A message metadata, part data and task metadata, through
    `SendMessage` params, task copies and SQL-stored tasks;
  - the log MCP tools `log_recent` and `log_filter`, including history
    read from the log file;
  - Postgres full-text search result fields, pgvector chunk metadata,
    and `gofastr semantic query` against a remote app;
  - desktop `appstate.Get` and the capability manifest's schemas;
  - codegen `json_file` and `json_dir` sources;
  - Kiln tool calls (field defaults, entity properties, page props, hook
    and route params, seed rows), in the live world, after a restart,
    in the chat timeline and the panel's edit preview, and in the rows
    seeded into the database.

  Numbers that fit in a float64 decode as before. Only a value that
  would round now arrives as a `json.Number`, which marshals back with
  the same digits.
- **`sdk.SchemaHash` sees a field default that changes past 2^53.** Two
  such defaults hashed alike, so the SDK drift check missed the change.
  An app with one sees its hash change once.
- **Process-module entity filters send plain decimals.** A filter number
  of 1e6 or more went to the CRUD query as `1.234567e+06`, which an
  integer filter refused, and one past 2^53 went rounded.
- **A number default past 2^53 renders as a number in DDL.** An `int64`
  default, or one decoded from a JSON declaration, was written quoted
  (`DEFAULT '9007199254740993'`).
- **Kiln handles integers past 2^53 after a decode.** Freeze wrote one
  in a seed row or default to `gofastr.yml` as a quoted string; it now
  writes a number. A `respond_json` status that large answered 200; it
  is now refused like any out-of-range status. A hook expression reading
  one failed with "requires numbers"; it now does exact integer
  arithmetic, equality and ordering (`<`, `>`, `<=`, `>=`).
- **A process module's crash is recorded before it can respawn twice.**
  The exit watcher cleared the child, unlocked, logged, and only then
  marked the slot crashed. In between, the slot read as a childless
  Ready, which the supervise loop takes for a lease drain and respawns;
  the watcher then marked that replacement crashed, and crash handling
  spawned another child without draining it. The crash is now recorded
  with the child cleared.
- **A Redis cache whose prefix holds `[`, `]` or `\` clears its keys.**
  `RedisCache.Clear` passed the prefix to `KeyScanner.Keys` unescaped,
  and Redis read it as a glob, so `WithPrefix("a[b]c")` matched none of
  its own keys and `Clear` returned nil with every entry still stored.
  The prefix is now escaped in the scan pattern.
- **`AssertJSON` tells big integers apart.** The test harness compared
  both sides after decoding numbers as float64, so a response holding
  9007199254740992 passed an assertion that expected 9007199254740993.
  Integers past 2^53 now compare by their digits.
- **An MCP tool keeps an integer past 2^53.** `handler.ToolAdapter`
  decoded tool params as float64, so 9007199254740993 under a
  `map[string]any` field reached the handler as 9007199254740992, where
  the same body through the route's `Bind` kept its digits. The tool path
  now decodes the way `Bind` does.
- **BREAKING: `WithResourceGate` hides the resource from callers it
  refuses.** A refused caller still saw the resource's URI, name and
  description in `resources/list`, and was told by
  `notifications/resources/list_changed` that it appeared. Both now pass
  the gate, the rule tools, prompts and resource templates already
  follow, and pagination walks the filtered set. A resource that clients
  must discover before they pass the gate drops `WithResourceGate` and
  checks the caller inside its contents func. Its
  `notifications/resources/updated` then reaches every subscriber too,
  so keep the gate when even update timing is private. See
  [MCP](framework/docs/content/mcp.md).
- **Tags on an embedded struct bind.** `handler.Bind` and the MCP
  `ToolAdapter` ignored `header`, `query` and `path` tags on fields of an
  embedded struct, so an input that embedded a shared `Pagination` got
  zero values. They now bind like fields of the outer struct, allocating
  a nil embedded pointer only when one of its values arrives.
- **DI fills fields of an embedded struct.** `di.Container.Inject` skipped
  `inject:""` fields promoted from an embedded struct, so a screen that
  embedded shared services rendered with nil ones, and a value component
  asking for DI only through an embed was not refused. `Inject` now
  descends (allocating a nil embedded pointer that holds inject tags,
  and refusing one it cannot set), and the new `di.InjectTagCount`
  counts promoted tags for the value-component check.
- **A JWT ends when the password does.** A JWT kept working after a
  password reset, a password change, or a mailbox claim of the account,
  so a squatter's token or a stolen one outlived the event meant to end
  it. A JWT from `JWTAuth` with a user store now carries a tag derived
  from the password hash it was issued against and answers 401 once the
  hash changes. A claim writes a fresh unusable hash, so it ends the
  JWTs of an account that never had a password too. Each JWT request
  does one more indexed lookup, JWTs
  issued before the upgrade answer 401 once, and `GenerateToken` errors
  for a user the store cannot resolve by email to the same id.
- **A login overtaken by a password reset leaves no session.** A login
  whose password check passed just before a reset purged the user's
  sessions minted one afterwards, and so did a magic link redeemed just
  before one. Both re-read the credential after minting and, if it
  changed, delete the session and answer 401.
- **BREAKING: backup-code regeneration no longer writes over a
  concurrent 2FA change.** Regenerating codes wrote back the state it
  read first, so a TOTP code used in between could be used again, and a
  disable in between was undone. It now swaps against the fresh row
  through `TwoFAStateSwapper` and answers 409 when the factor was
  disabled, removed or re-keyed. `TwoFACompareAndSetter` and
  `MemoryTwoFAStore.CompareAndSetTwoFA`, which only this path used, are
  removed.
- **OAuth account linking works from a real browser.** The provider's
  redirect back is cross-site, so the browser withheld the
  `SameSite=Strict` session cookie and every link answered 401. A
  cross-site callback with a valid link state and no session now answers
  a page that reloads the callback from this site, where the cookie
  rides. New phrases: `auth.oauth_hop_title`, `auth.oauth_hop_continue`.
- **An app with a process module boots more than once.** The second
  boot against a persistent database, and every second replica sharing
  one, panicked in `RegisterProcessModule` with "module already
  installed". Registering now compares the descriptor with the stored
  row through the new `ProcessModuleStore.InstallOrUpdate`. The same
  descriptor keeps its state, and a new artifact bumps the generation and
  stays enabled. A grant change disables the module until an operator
  re-enables it, and logs a Warn. `RegisterResult` reports the
  `Outcome` and `PreviousGrants`.
- **A storage failure during a CRUD upload answers a fixed 500.** A
  multipart create or update whose storage `Save` failed answered 400
  with the backend's error text, so a backend that does not scrub its
  errors (S3, a custom one) put its paths or bucket in the client's
  response. It answers 500 with the standard phrase and logs the detail.
- **An A2A stream always ends on a terminal event.** A `CancelTask`
  landing between a resumed stream's claim and its subscription left the
  stream at WORKING with no end. The stream falls back to polling the
  store, as `SubscribeToTask` does, so it ends on CANCELED.
- **Local storage reads refuse a case-folded key.** `Save` and `Delete`
  refused `tenanta/report.txt` when `TenantA/report.txt` was stored, but
  `Get`, `GetRange` and `Exists` read the other tenant's file through it
  on a case-insensitive filesystem. They refuse it too, in
  `battery/storage` and in `upload.NewLocalStorage`.
- **The relay keeps the base path's trailing slash.** A request for the
  route itself, with no tail, went upstream as `/cdn/api/events` when
  the base was `/cdn/api/events/`.
- **BREAKING: `FileField.URL` is escaped.** A file name holding `#` or `%` gave a
  URL that cut at the fragment or failed to parse. `URL` escapes each
  segment, and CRUD uploads store `StorageRef`, the literal key, in the
  column, as host code should.
- **`upload.UniqueFilename` stays within 255 bytes.** A long original
  name produced a stored name the filesystem refused. The stem is cut on
  a rune boundary and the extension kept.
- **The idempotency fingerprint uses the escaped path.** `/items/a%2Fb`
  and `/items/a/b` shared one fingerprint, so a key reused across them
  replayed the first response. The second now answers 422.
- **A 404 or 405 `Allow` header names every method a path answers.**
  The router probed only the most specific pattern, so `GET /foo/{x}`
  beside `POST /foo/bar` answered `Allow: POST` for `/foo/bar`, and a
  gated route hid the methods of the wildcard behind it.
- **An IPv4-mapped IPv6 embed origin is refused at boot.** It normalized
  to `https://[127.0.0.1]`, which never matched a request and put an
  invalid host-source in `frame-ancestors`.
- **BREAKING: `WithLocaleResolver` takes an `i18n.LocaleResolver`.**
  A locale chosen by a cookie reached the response with no `Vary` on
  the cookie, so a CDN or the cache battery replayed one reader's
  language to the next. `i18n.LocaleResolver{Resolve, Vary}` names the
  headers a resolver reads, and the middleware adds them to `Vary`.
  `i18n.CookieLocale` returns one with `Vary: Cookie`, so
  `WithLocaleResolver(i18n.CookieLocale("locale"))` compiles unchanged;
  a custom func becomes `i18n.LocaleResolver{Resolve: f, Vary: …}`.
- **A region tag finds a script catalog.** `zh-Hant-HK` fell back
  straight to `zh` and then the fallback locale, never trying
  `zh-Hant`. Fallback now drops one subtag at a time, for negotiation,
  `Translate` and `i18n.Match`.
- **A plural with no form for a count no longer renders empty.** A
  plural message with neither the selected form nor `other` rendered
  `""`. It falls back like a missing key, to the next locale candidate,
  the fallback locale, then the phrase's English, and
  `i18n.CheckCatalog` reports the missing `other`.
- **Notification `HTMLBody` values are escaped for where they sit.**
  `battery/notify` escaped every value as element text, so a value in
  an unquoted attribute added attributes (`x onerror=…`) and one in
  `href` kept a `javascript:` URL. `HTMLBody` renders through
  `html/template`, as the email battery does. Once a value is filled,
  template comments are dropped, and a placeholder where no value can
  go is a Render error.
- **A rebuilt static export removes pages it no longer writes.** A page
  that became gated, or a route that was dropped, kept its old file in
  the output directory, and the static host went on serving it with a
  200. The export records its pages in `OutDir/.gofastr-pages` and
  removes listed files the next build does not write; files it never
  listed stay.
- **The PWA offline page carries the export's CSP.** Every exported page
  had the CSP meta tag except the offline page.
- **Static export and the sitemap keep a page ID's `?` and `#`.** The
  synthetic request for a static page, its `llm.md` and the sitemap
  check was built from the raw path, so an ID holding `?` or `#` was cut
  there. It is built from the escaped path. An exported `llm.md` also
  renders under that request, where a screen that read it panicked, and
  an ID mixing non-ASCII and `%` stays in the sitemap with the trailing
  slash its links carry.
- **The exported llm.md index links only docs the export wrote.** It
  linked `/products/:slug/llm.md` and docs of pages the export skipped.
  A dynamic route now lists a link per exported page, and a page with no
  doc is listed unlinked. `app.WithExportedDocs` carries the written
  docs to the index.
- **The sitemap expands `StaticPaths` per locale.** Every locale listed
  the default locale's slugs (`/es/p/about` where the Spanish page is
  `/es/p/acerca`). Each locale runs its own expansion, and alternates
  appear only where that locale lists the path.
- **An encoded locale prefix is not prefixed again.** A link to
  `/%65s/about` became `/es/%65s/about`; the prefix check now decodes
  the first segment, as the router does.
- **Typed hooks merge fields promoted from an embedded struct.** A
  typed hook on `type Row struct { Fields; … }` that cleared
  `v.Secret` left the original secret in the body: the merge treated
  `Fields` as one field (or skipped it, when its type was unexported),
  while JSON promotes its fields. The merge now walks an embedded
  struct whose tag names no key (`json:",omitempty"` included), so a
  redacting After hook redacts and a Before hook overrides the promoted
  column. A hook that sets an embedded pointer to nil clears the fields
  it promoted. The merge follows JSON's field set: an embedded field an
  outer one of the same name shadows, or two embedded fields tied at one
  depth, never reach the body. An unexported embedded struct with a tag
  name merges under that key, and a tag name JSON rejects (`bad\name`)
  falls back to the Go field name, as in JSON.
- **`EmitAsync` runs each subscriber on its own goroutine.** It ran
  them one after another on one goroutine, so a subscriber blocked on a
  slow service held back every later one, the SSE listener included,
  though the events doc promised otherwise. A payload whose
  `MarshalJSON` panicked also crashed the process from the fanout tap;
  the tap now skips the fanout for that event and local subscribers
  still get it.
- **Cron ticks no longer drift past a minute.** The scheduler rearmed
  one minute after a tick finished, so a slow leader-election `Acquire`
  pushed each tick later until a whole minute, and the jobs scheduled
  in it, was skipped. Every tick now fires on the next minute boundary.
- **The OpenAPI batch update item requires only the key.** Each item of
  `PATCH /{table}/_batch` carried the entity's `required` list, so a
  sparse item such as `{"id":"p1","views":7}`, which the server accepts,
  failed validation in any client generated from the document. Items
  now require only their key columns, as a single `PATCH` does.
- **Back to the first page restores its signals.** Back or Forward to
  the page the browser loaded first kept the page-scoped signal values
  the next page left, and a navigation that swapped in a whole document
  never applied that document's seed. The host now lists a page's
  page-scoped names in a `#gofastr-signals-page` head block, and the
  runtime resets those names on both paths while keeping app-global
  values the user changed.
- **An invalidated prefetch no longer comes back.** A prefetch still in
  flight when a mutation's `X-Gofastr-Invalidate` named its path was
  cached when it landed, so the next click painted the pre-mutation page.
  A matching in-flight response is now dropped.
- **Back and Forward compare every query value.** A move between
  `?tag=a&tag=b` and `?tag=a&tag=c`, or between `?tag=` and no `tag` at
  all, compared only the first value and kept the wrong page. All values
  of each key are compared.
- **RPC forms send the clicked submit button.** A `data-cui-rpc` form
  with several named submit buttons sent none of them, so a handler
  reading `intent=publish` saw nothing. The clicked button's name and
  value are in the payload, as on a native submit.
- **`?include=` and nested filters apply the child's read scope.** A
  child entity's `BeforeList` scope (and `BeforeGet`, for to-one
  relations) narrowed its own routes but not an `?include=` of it or a
  `?rel.field=` filter through it, so `GET /projects/p1?include=notes`
  returned notes the caller's team could not list. Both now run the
  scope; a hook that needs a record ID sees an empty one there and
  should refuse. A refusal answers 400 with the body the child's own
  route sends.
- **A many-to-many join entity's read gate covers includes.** A
  registered pivot entity whose permission or token scope the caller
  lacked still disclosed its associations through `?include=` and
  `?rel.id=` filters. Its gate now applies on both, and a pivot with CRUD
  turned off refuses them, as a CRUD-off target does.
- **A many-to-many join entity's `BeforeList` hooks scope includes and
  nested filters.** `?include=` and `?rel.field=` through a registered
  pivot ran the target's `BeforeList` but not the pivot's, so a link
  `GET /<pivot>` hid from the caller still served the related row
  behind it and matched a nested filter. The pivot's `BeforeList`
  clauses now narrow the link rows on both paths, and a hook that
  refuses or panics refuses the read the way a target hook does.
- **`access.CachedResolver` caches roles per tenant.** It keyed cached
  roles by user alone, so a user who was admin in tenant A got A's roles
  on a request in tenant B within the TTL. The tenant is part of the key,
  and `Invalidate(userID)` clears every tenant. `handler.SetTenantID` and
  `handler.GetTenantID` are the new core slot `tenant.SetTenantID` and
  `tenant.GetTenantID` write and read.
- **CRUD events go to the record's owner, not the writer.** A write
  under `owner.AllowCrossOwner` (or `tenant.AllowCrossTenant`) stamped
  its `_events` update with the actor's owner and tenant, so Alice's
  stream received Bob's booking after server code promoted it from her
  request. The stamps come from the record's columns.
- **A2A entity skills run the MCP gate.** `WithMCPGate` refused a
  caller on `/mcp` while the same caller's A2A skill ran the tool, and
  the public agent card listed every entity skill, naming entities and
  fields the gate withholds. Skill calls go through the new
  `mcp.Server.CallToolFor`, which applies the server-wide gate, and the
  card lists only what an anonymous `tools/list` shows.
- **An exposed dev bind withdraws the introspection tools.** The `app_*`
  introspection tools and `contracts_verify` that `gofastr dev` turns on
  stayed registered on a non-loopback listener, unlike the control
  tools. They are withdrawn unless `GOFASTR_DEV_MCP_EXPOSE=1`; an
  explicit `WithMCPIntrospection()` keeps them.
- **Process modules have one enable switch.** `app.Modules().Enable`
  and the `app_module_*` tools toggled a flag the supervisor never read,
  while `app.ProcessModules().Enable` toggled the supervisor's row, so a
  module could be listed as enabled and never start, and a never-enabled
  module answered 503 instead of 404. Every path now drives the
  supervisor's state and takes effect before it returns. Enabling after
  `App.Start` spawns the child, a lease-expiry drain no longer counts
  toward the crash circuit (and the drained child's late exit no longer
  marks its replacement crashed), a request body over the limit answers 413
  instead of reaching the child truncated, and the runner removes the
  scratch directory it made for each child.
- **A full disk no longer leaves a semantic index that cannot reopen.**
  A WAL append that failed partway stayed in the file, later appends
  landed after it, and every restart failed to decode the WAL. A failed
  append is cut back off and the next write snapshots first. `POST
  /index` answers 400 for a document with no id or one the chunker
  refuses, where it answered 500.
- **Export and restore keep every account.** The auth battery exported
  only the canonical `auth_users`, `auth_sessions` and `auth_passkeys`
  tables, so an app wired with the documented
  `NewEntityUserStore(db, "users")` exported no users or sessions, and a
  restore brought back no accounts. Every app also lost 2FA enrolments
  (2FA accounts fell back to password-only), OAuth links (OAuth-only
  accounts were locked out), and each user's name and verified flag.
  Exporters now follow the configured stores' tables and cover 2FA
  enrolments and OAuth links. When the user table is also an app entity,
  the entity's export carries the columns auth adds to it, so names
  survive there too. Magic-link and reset tokens stay out on purpose.
- **Development email logs redact links in HTML bodies.** An HTML
  template writes `&token=` in an `href` as `&amp;token=`, which
  `LogSender` and `RedactBody` did not recognise, so a live reset or
  magic-link token reached the log.
- **A print document whose component panics answers 500.** The panic
  fallback, with its message, was served as the page or printed into
  the PDF with a 200.
- **The cache battery honours every privacy signal.** It stored a
  response that middleware outside it had marked `Cache-Control:
  no-store` or `private` or given a `Set-Cookie`, and replayed it to
  other callers. It also read only the first `Cache-Control` line, so
  `max-age=60` followed by `no-store` was stored; every line counts now,
  on the request too.
- **Redis cache prefixes can no longer collide.** Doubling `:` in a
  prefix was not enough: `WithPrefix("u")` with key `:alice:x` and
  `WithPrefix("u:alice")` with key `x` named one Redis key, and
  `Clear` on `u` deleted the other namespace's entries. A prefix's `%`
  and `:` are now percent-encoded. Entries under a prefix holding `:`
  are cold after the upgrade.
- **The site search page says when the index is unavailable.** A
  failed index build drew a query as zero matches, while the query
  endpoint answered 503. The page now shows a "search index
  unavailable" notice.
- **`service install` refuses a data directory with a line break.**
  `GOFASTR_SERVICE_DATA_DIR` is written into the systemd unit's
  `ExecStart` line, and a newline in it could add a directive to the
  unit. A value holding a control character is now refused.
- **A control keeps a selected value its options don't list.** A
  `FilterToolbar` select or pills facet whose URL value no option
  listed drew nothing chosen, and Apply dropped the filter;
  `headless.Select` (and the `FilterRows` field and operator selects)
  let the browser pick the first option. Both now draw the value as its
  own chosen option, labelled with the value. `ui.SegmentedControl`
  given such a value checks no segment instead of the first, so the
  group no longer submits a value nobody chose. An empty `Selected`
  still starts on the first segment. The `FilterToolbar` sort control
  keeps an unlisted sort the same way.
- **Generated forms accept decimal and float numbers.** An app spec
  `entity_form` drew every number field with the browser's default step
  of 1, so a price of 9.99 failed native validation. `decimal` fields
  now step by 0.0001, `float` by `any`, and `int` by 1.
- **A stalled ACP client parks one goroutine per session.** Each
  `session/prompt` sent to a client that had stopped reading parked a
  goroutine waiting to write its response, without bound. The next
  prompt on a session now waits until the previous turn's response is
  written.
- **`gofastr verify` reads `%%s` in SQL as a literal.** GOFASTR1401
  took SQLite's `strftime('%%s', 'now')` inside a `fmt.Sprintf` for a
  value interpolated into quotes and failed the build. `'%s'` still
  fires.
- **Migrations handle schema-qualified tables.** With
  `Table: "x.widgets"`, the column, key and existence probes read the
  search path's `widgets` (Postgres) or failed on the dot (SQLite), so
  a new column was never added; default index and primary-key names
  carried the dot and failed to create, and an index replace dropped
  nothing. Probes now split the schema on both engines, default names
  use the table's base name (`idx_widgets_title`), and drops name the
  schema. `ReadLiveColumns`, `ReadLiveColumnsBulk` and
  `TableExistsBulk` resolve `schema.table`. On SQLite a key-change
  rebuild renamed to `main.widgets` and wrote `REFERENCES aux.parents`,
  both syntax errors, and the owner-key repair found nothing on a
  qualified table; both now work on `main.` and attached tables, with
  indexes, triggers and the AUTOINCREMENT counter kept in the table's
  schema, and the foreign-key check covers every attached schema. A
  `REFERENCES app.a` in a generated migration was read as a reference
  onto `app`, so `migrate down` dropped the parent first and Postgres
  refused it.
- **A quoted mixed-case Postgres column is not taken for the entity's.**
  A legacy `"Title"` was read as the entity's `title`, so the column was
  never added and every insert failed. Only folded columns count now;
  the framework adds `title` beside `"Title"` and never drops it.
- **SQLite table lookups ignore case.** `TableExistsBulk` reported an
  existing `tasks` as absent for `Tasks`, and the owner-key repair
  could not find it.
- **`SaveSnapshot` replaces the file atomically.** It rewrote the file
  in place, so a crash mid-write left a torn snapshot. It now writes a
  temp file beside it and renames it over, keeping an existing file's
  mode and writing through a symlink.
- **BREAKING: `ui.DateTimeFieldConfig.Step` is a `float64`.** The
  step is in seconds, so `0.001` lets a datetime field show and keep
  milliseconds; an `int` step could not. An untyped constant still
  compiles; an `int` variable converts with `float64(n)`.
- **A belongs-to cascade through a key the write cannot set is
  refused.** With the foreign key `ReadOnly`, `Hidden`, or the owner
  column on an update, the nested child was still written and handed
  back as attached while the parent kept its old reference: a PATCH
  could edit an unrelated draft note and return its body, and a POST
  left an orphan. It now answers 400
  `validation.cascade_key_not_writable` before any child is written,
  and the child read-back goes through the child's read scope.
- **BREAKING: an upsert onto an existing key announces
  `entity.updated`.** Both arms emitted and staged `entity.created`, so
  a rolled-back upsert onto an existing row still announced a create.
  The upsert now probes the key inside its transaction. AfterCreate
  hooks still fire on both arms.
- **An upsert onto an existing row keeps the columns it was not sent.**
  The conflict arm set every column, filling an omitted one with its
  default, so `{"title":"edited"}` reset `status` to `draft`.
- **Cascade child events wait for the operation inside a caller's
  transaction.** Under `App.InTx` or a handler bound to a `*sql.Tx`,
  each child announced as it was written, so a cascade that failed
  later announced rows that never committed.
- **Batch writes send `Cache-Control: no-store` and audit the
  request.** The three `_batch` routes cached per-caller bodies and
  wrote audit rows with no client IP or user agent.
- **`WithMCPGate` holds whatever the option order.** It set the gate on
  the server present when it ran, so `WithMCPGate(...)` followed by
  `WithMCPServer(s)` served `s` with no gate: every tool, resource and
  prompt open to anonymous callers. `NewApp` now installs the gate on
  the final server after all options run.
- **MCP JSON-RPC envelopes follow JSON-RPC 2.0 on every transport.** A
  body that is not JSON answers `-32700` (new `mcp.ErrParse`); a wrong
  `jsonrpc` version, a repeated or case-folded envelope key, or an `id`
  that is not a string, number or null answers `-32600` (new
  `mcp.ErrInvalidRequest`), all of which answered `-32602` before, and a
  `true` or object id was echoed back. A valid request with no `id`
  member is a notification whatever its method: HTTP and SSE answer
  `202` with no body and stdio writes nothing, where HTTP used to answer
  every message and stdio went by the `notifications/` prefix. `"id":
  null` is a request and gets an answer, and so does an id-less `{}` or
  wrong version, with `-32600` and a null id.
- **An MCP enum holding objects or arrays compares their numbers by
  value.** `reflect.DeepEqual` compared a decoded `float64` or
  `json.Number` with the declared `int`, so `{"n":1,"tags":["a",2]}` was
  refused by an enum listing it.
- **`prompts/get` refuses a `null` argument.** It reached the handler as
  `""` and passed the required-argument check.
- **`resources/read` carries the resource's `_meta`.** An MCP App's
  `_meta.ui` (csp, permissions) reached `resources/list` only, so a host
  reading the widget's contents saw no CSP.
- **The MCP App widget client completes the spec handshake.**
  `ui/initialize` sent only `appCapabilities`; it now sends `appInfo`
  (from a new optional third `connect` argument, defaulting to
  `gofastr-mcp-app` `0.0.0`) and `protocolVersion: "2026-01-26"`, which
  spec hosts require. A failing `ui/resource-teardown` handler answers
  with code `-32603`, not the string `"E_HANDLER"`.
- **rtc: a graceful `Close` clears this replica's peers from the others.**
  `Close` emptied its room list before reading it, and the empty
  roster it meant to publish carried no node id, which every receiver
  drops. Other replicas kept the closed replica's peers until the
  remote TTL (45s), counted them against `MaxPeers`, and routed signals
  to them. The empty rosters now go out and land at once.
- **BREAKING: llm.md documents and server actions honor region guards.**
  An outlet, area or fill guard that redirects moves the whole page, but
  the `/llm-pages.md` index, `ScreenLLMMDHandler`, the pattern llm.md
  doc and server action calls ran only the screen's policy chain: the
  index listed a guarded page's title and description, the docs served
  its full content, and its actions ran for a visitor the page sent to
  sign-in. They now run the region guards through the new
  `(*App).PatternPolicy`. A dynamic route under a guarded layout is
  withheld from pattern-level documents, since a guard decides per URL;
  its per-URL llm.md still serves. An action naming its page runs that
  page's full policy phase, eager resolvers included, and a failed
  resolver refuses it with the page's status. A region guard's failed
  resolver read answers the page's error everywhere: a deferred part
  request (`X-Gofastr-Part`) served its fill while the page answered
  404, and now resets; the page render no longer runs `Load` first.
  `ScreenLLMMDHandler` now takes the app: `ScreenLLMMDHandler(a, screen)`.
- **The sitemap judges each locale on its own.** It ran a page's policy
  once with no locale and listed every prefixed variant, so a page a
  policy blocks under `/es/` stayed listed and named as an alternate.
  Each locale is now checked in the context its prefixed request
  carries, and only readable variants and alternates are listed.
- **A SQLite migrate or seed lease can no longer be clobbered by its
  last holder.** Renewal and release matched the lock row by id alone,
  so a holder whose lease had expired and been taken extended or deleted
  the new holder's row, and two processes migrated at once. They now
  match the expiry the holder last wrote; the stale holder logs "lock
  lease lost" and its run stops before the next migration.
- **A SQLite migrate run on a one-connection pool keeps its lease.** The
  lease was taken and renewed through the pool while the runner held
  the only connection, so the heartbeat starved and another process
  took the lease mid-run. It is now taken, renewed before each
  migration and released on the runner's pinned connection.
- **An encoded `Precache` entry no longer slips past the PWA deny
  list.** The filter compared the raw spelling, so `/a%70i/me?v=1` was
  kept while the router served it as `/api/me`: the worker fetched it
  with the installing user's cookies and later answered from that
  cache. Entries are decoded and cleaned before the check; a malformed
  escape is dropped.
- **Export, restore and erasure reach schema-qualified Postgres tables.**
  An entity declared with `Table: "app.widgets"` was skipped as absent:
  the presence probe compared the whole dotted name to `table_name`, and
  the quoting made one identifier of it. Each part is now matched and
  quoted on its own.
- **A table declared in two spellings exports once.** Two entities, or
  an entity and an exporter, whose tables differ only in case were two
  archive sources for one table; the second was skipped as absent and
  its columns were lost. They are now one source, named after the
  entity that sorts first. An archive that holds both entries fails
  import with "not a live entity or registered exporter".
- **Import refuses a source whose table is missing**, naming the source
  and the table, before any row is written.
- **Index drops run before column changes in a generated migration.** A
  unique index moved to a new column, or an indexed column dropped with
  its index, failed because the column change ran first. Changed indexes
  are now dropped first and created last.
- **A `Unique` field added to an existing table gets its index.** The
  `ADD COLUMN` carried no uniqueness; it is now followed by
  `<table>_<column>_key`.
- **SQLite table rebuilds keep views, triggers and routines.** The
  rename now runs under `PRAGMA legacy_alter_table`, so a view or
  another table's trigger that names the table survives, and every
  routine's `Up` re-runs after the rebuild.
- **Postgres view changes handle kind switches and dependents.** Turning
  a plain view materialized, or back, failed on the live object of the
  other kind; it is dropped first, at boot and in the generated file.
  A changed materialized view's declared dependents are dropped and
  recreated with it.
- **A Postgres text-to-integer retype with a default applies.** The old
  text default could not be cast; it is dropped before the retype and
  the declared default set after.
- **Defaults render as the value they hold.** A float default wrote
  `0.000000` for `1e-07` and lost precision elsewhere; it now uses the
  shortest exact form. A scalar default on a JSON field is JSON-encoded.
- **Boot reads only the folded spelling of a Postgres entity table.**
  `ReadLiveColumnsBulk` and `TableExistsBulk` matched without case, so a
  hand-made quoted `"Widgets"` beside the entity's `widgets` could stand
  in for it.
- **Includes, scopes, filters and self-joins work on schema-qualified
  tables.** Scoped and owner-scoped many-to-many includes, pivot scopes,
  nested relation filters and self-joins quoted `app.widgets` as one
  identifier and failed.
- **Constraint errors on a schema-qualified table name the field.** A
  unique, foreign-key or not-null refusal on `app.widgets` came back as
  a generic error instead of naming the column.
- **Upserts work on a table named `excluded`.** CRUD upsert, the webhook
  `SQLStore` subscriber upsert, `PostgresSearch.Index` and
  `PgVectorStore` inserts alias their target now. Postgres refused the
  statement as ambiguous, and SQLite resolved `excluded.col` to the
  table and silently kept the old values.
- **A webhook store without leases dead-letters at `MaxAttempts`.** The
  fallback path for a store that does not implement `LeasedStore` never
  counted attempts, so a failing endpoint was retried forever.
- **Webhook and outbox workers hand back what their lease cannot
  cover.** A worker kept sending a claimed batch after the lease ran
  short, so another replica could claim the same delivery and both sent
  it. The webhook worker now releases a delivery, attempt refunded, when
  less lease is left than the HTTP client's timeout (capped at half the
  lease); the outbox relay releases the rest of a batch when one handler
  run plus its settle would pass the lease.
- **SQLite webhook claims no longer race.** Two workers could claim the
  same delivery; the claim now runs in a `BEGIN IMMEDIATE` transaction
  on one connection.
- **A replayed durable-schedule job still blocks an overlapping tick.**
  Occurrence retention pruned the row of a failed job, so replaying it
  let the next tick run beside it. Only occurrences of done jobs are
  pruned now.
- **A late `Nack` leaves a finished DBQueue job alone.** A `Nack` that
  arrived after `Ack`, a dead-letter or a replay put the job back in the
  queue.
- **BREAKING: a blank optional field clears on update.** An update
  dropped `""` for a number, date, enum, uuid, relation or JSON field,
  so emptying the control kept the stored value while the save reported
  success. It now writes null; on create the default still applies. A
  blank `Image` or `File` on update still keeps the stored file. A JSON
  client sending `""` for a field it means to leave alone now clears it:
  omit the key instead.
- **The entity UI can save a record untouched.** An optional Int drew
  `0` for null and saved it; an optional enum had no way back to empty;
  a relation a read hook masked sat in the form as a blank value that
  every save cleared; a required enum holding a value the field no
  longer lists saved its first listed value instead; an optional bool
  stored NULL saved `false`; a color the picker cannot hold (`brand-red`,
  `#A1B2C3`, empty) saved as the picker's lowercase or black. The Int is
  now a number field that may be blank, the enum has an empty choice, a
  required enum with an unlisted value asks for a listed one, a NULL
  optional bool draws Not set / Yes / No, a color draws the picker only
  for a stored lowercase `#rrggbb` and a text input otherwise, and the
  masked relation changes through its own Replace button. New `ui.PickerConfig.Form` and
  `headless.ComboboxPick.Form` name the form a picker's value submits
  with.
- **A form's datetime-local value saves.** crud refused
  `2026-10-27T10:00` with `not_timestamp`; it now reads that spelling,
  with optional seconds and fraction, as UTC, as the filter does. The
  entity UI drew a stored instant in its own zone without seconds, so an
  untouched save on a Postgres session in another zone moved it, and it
  cut seconds and fractions. It now draws UTC with seconds or
  milliseconds when present.
- **A generated create form's checkbox saves its answer.** An
  `entity_form` bool posted `on`, which a bool field refused, and an
  unticked box sent nothing. It now posts `true` or `false` and starts
  at its declared default.
- **`gofastr migrate down` refuses a count it cannot read.** `down all`
  rolled back one migration and `down 3x` rolled back three, each
  reporting success. A count that is not a positive integer now exits 1
  before anything is rolled back.
- **`gofastr migrate up --create-db` checks its arguments first.** With
  no `migrations/` directory or no database URL, it created the
  Postgres database and then exited 1, leaving an empty database.
- **A record with a JSON field can be edited.** The entity UI drew a
  stored document as Go text (`map[plan:pro]`) in the edit form, the
  record view and list cells. The browser's JSON check then refused the
  form, so no edit of the record could be saved. It now shows the
  document as JSON.
- **A stored decimal saves untouched.** A `Decimal` input stepped by
  0.01, so a stored `19.995` failed the browser's step check and blocked
  Save. It now steps by the column's four fraction digits, or finer
  when the stored value has more. The `money` input keeps its cent step
  and widens it the same way for a finer stored value.
- **BREAKING: generated clients and CLIs keep big integers.** The Go
  entity client, the Go SDK, the entity CLI and the `--from-openapi`
  CLI decoded numbers inside an `any` (json fields, batch data,
  `--json` bodies, json-field flags, printed responses) as float64, so
  `9007199254740993` was read, printed and sent back as `…992`. They now
  decode as `json.Number`; code asserting `float64` on such a value
  calls `Int64()` or `Float64()` instead.
- **A generated CLI's table output drops control bytes.** `-o table`
  printed record text, the next-cursor line and API error bodies raw,
  so a stored `\x1b[2J` cleared the screen of whoever listed it. C0
  (but tab), DEL and C1 are now dropped from those lines; JSON output
  was already escaped.
- **`gofastr pack` refuses a field it cannot represent.** A
  `schema.Field` with a `WireName`, `RawType` or other key the spec has
  no field for, a type with no spec name, a computed `Max` or `Min`, or
  an unknown `AutoGenerate` was dropped from the packed spec without a
  word. Pack now fails naming the field and key.
- **`gofastr generate` refuses a field named like a generated method.**
  A field whose Go name matched a method the generator writes on the
  same type, such as `UnmarshalJSON`, generated code that did not
  compile. It is now refused with the clash named.
- **`--from-openapi` CLIs follow a query array's style.** `style: form,
  explode: false` was sent as repeated parameters; it now sends
  `tags=a,b`. A style the CLI cannot send (`spaceDelimited`,
  `pipeDelimited`, `deepObject`, a non-`simple` path or header
  parameter) is refused at generate time.
- **A typed timestamp condition matches the stored row.** A timestamp
  column's `Eq`, `Gt`, `Gte`, `Lt` and `Lte` bound a `time.Time` or
  RFC 3339 text as given. SQLite compares the column as text, so
  `…10:00:00Z` missed the row stored as `…10:00:00.000000Z` and ranges
  compared spellings. They now bind in the layout crud stores.
- **`EagerLoad` with a registry decodes its rows.** It returned a Date
  as a timestamp, a JSON field as text and, on SQLite, a decimal as a
  number. It now decodes rows as `?include=` does.
- **Included rows keep a JSON field's stored keys.** `?include=`
  deep-converted a related row's JSON document to the entity's key
  case, so `{"user_id":1}` came back as `{"userId":1}`.
- **A mixed-case include target reads right on Postgres.** Included
  rows from a target with mixed-case column names came back keyed by
  the folded column name, so Date, JSON and decimal decoding missed
  them. They now carry declared field names and decoded values.
- **A number past float64 range is a field error.** `1e400` in a
  non-JSON field answered `invalid_json` for the whole body. It now
  answers a validation 400 naming the field (`not_finite`, or
  `not_integer` on an int), and a JSON field stores the number as
  written.
- **Retrying a failed OAuth unlink drops the stored provider token.**
  When the token delete failed, the link was already gone, so the retry
  answered 404 before reaching the token and the refresh token stayed
  stored for good. An unlink of an unlinked provider now drops its
  tokens first.
- **Two magic links for one new address both sign in.** Redeemed at
  once, both missed the lookup and both created the account; the loser
  spent its single-use token on a 500. It now signs in to the account
  the other one made.
- **Cross-replica presence heartbeats run.** `SetFanout` never set the
  15-second heartbeat interval, so the loop exited at once: a dropped
  announcement never healed, an idle remote member fell out of
  `PresenceRoster` after 45 seconds, and a crashed replica's members
  never fired `SetOnPresenceChange`. Detaching the fanout now also
  fires it for each topic that lost remote members.
- **A slow WebSocket write no longer closes a healthy peer.** The pong
  clock started before the Ping waited for the write lock, so a data
  write longer than `PongTimeout` left the peer no time to answer. It
  starts when the Ping is written.
- **A Kiln seed the database refuses no longer wipes the live data.**
  A failed `add_seed` rebuilt the runtime database from the journal,
  dropping every row written through the CRUD API. A seed now inserts in
  one transaction, so a refused one lands no rows and nothing is
  rebuilt, and a row key that is not a field of the entity is refused up
  front as a validation error.
- **Deleting a Kiln field removes it from the entity's seeds.** The
  column stayed in the seed rows, so the next rebuild or freeze failed
  on a column the table no longer had. `delete_field` and an
  `update_entity` that drops a field now strip it, and `undo` restores
  it.
- **Kiln freezes two seeds for one entity into one block.** The frozen
  spec held two `seed:` blocks for the entity, which `gofastr generate`
  refuses. They merge now; two seeds that weight one column differently
  are refused at freeze.
- **Semantic search's HTTP routes keep large integers exact.** `/index`
  and `/query` decoded numbers as float64, so a document indexed with
  tenant `2^53+1` matched a `meta_match` on `2^53`. They decode with the
  new `handler.UseNumber()` option to `UnmarshalStrict`, and the WAL and
  snapshot carry the `json.Number` values.
- **A crash in the middle of a semantic replace no longer loses the
  document.** The remove and the add were two WAL records, so a crash
  between them replayed only the remove. A replace is one record now.
- **A zero-vector document no longer breaks pgvector queries.** Its
  cosine distance is NaN, which failed JSON encoding and answered an
  empty 200. It scores 0, as on the in-memory store, and a response the
  route cannot encode answers 500.
- **Filtered hybrid queries keep in-scope keyword matches.** The keyword
  leg cut its candidates before applying the filter, so out-of-scope
  matches ranked above could push every in-scope one out. The window
  now widens until the filter is met.
- **A restarted semantic `Watcher` drops files deleted while it was
  down.** Its first scan removes its own documents under its roots
  whose files are gone.
- **Semantic keyword terms keep subscript and other Unicode digits.**
  `H₂O` split into `h` and `o` in the in-memory keyword index, and the
  64-term query cap could cut a term at `H₂`.
- **A password change or reset spends the user's outstanding magic
  links.** A link mailed before the change, the one someone with access
  to the mailbox may hold, still signed in afterwards. Both now purge
  the links by the canonical address and emit `magiclink_tokens.purged`;
  a reset whose purge fails answers 500 and asks for a new link, and a
  change logs the failure and stands. The OAuth unlink guard also took
  its row lock only when a per-request dialect probe succeeded; the
  auth stores now detect the dialect once per store.
- **BREAKING: an MCP App widget carries its client inline.** The chat
  host renders a widget in a frame on its own origin, so the client's
  `<script src="/__gofastr/mcp/app/widgetclient.js">` loaded from the
  host, not the app, and the widget never initialized. A test with the
  host and the MCP server on one origin hid it. `mcp.WidgetDocument` now
  inlines the client, and `mcp.WidgetClientScriptURL`,
  `mcp.WidgetClientHandler` and the route are removed. Hand-written HTML
  inlines `mcp.WidgetClientJS()`. The script guard reports the right
  offset after a non-ASCII rune and also checks the inlined client.
- **An MCP integer argument is judged on its text.** `9007199254740993e0`
  and `…993.0` were refused as fractions, while `1.0000000000000001` and
  `1e-999999999` rounded to whole floats and passed an integer schema.
- **An empty MCP resource keeps its `text` or `blob` key.**
  `resources/read` and an embedded resource dropped an empty `text` or
  `blob`, which left the content item matching neither union member.
- **BREAKING: `app.ScreenLLMMD` and `ScreenLLMMDWithMeta` take a
  context.** A pattern document's `llm.md` (`/p/:id/llm.md`), the
  fallbacks `ScreenLLMMDForPath` serves when a resolver or Load fails,
  and `ScreenLLMMDHandler` linked unprefixed pages under `/es/`. They
  now carry the localized canonical, hreflangs and prefixed links.
- **Static export writes `llm.md` for every locale.** The page documents
  and `llm-pages.md` were written once, for the default locale; each
  locale tree now gets its own, and links to copied static files stay
  unprefixed as the pages' do.
- **The exported PWA worker installs when a page path holds `?` or `#`.**
  The precache list named pages and `llm.md` files in their decoded
  form, so `/items/a?b/` was fetched as `/items/a` with a query, and the
  404 failed the whole install. Paths are escaped now, and a link to an
  escaped page gets its trailing slash.
- **A Postgres unique index named like a field no longer names that
  field in a 409.** The conflict mapper read an index name as a MySQL key
  alias, so `CREATE UNIQUE INDEX Email ON acct(token)` reported a
  conflict on `Email`. Only MySQL reads key names now.
- **Export keeps binary columns binary.** A BLOB or `bytea` value was
  exported as text and came back as U+FFFD runs in a text column. Binary
  values are written as `{"$bytes":"<base64>"}` in archive format
  `gofastr-data-v2`; `v1` archives still import, and any other JSON
  object or array in a column value is refused.
- **Restore advances a battery table's Postgres sequence.** A raw table
  registered for export with a SERIAL key kept its sequence at 1 after
  import, so the next insert failed on a duplicate key.
- **Two anonymizers on one table both apply.** Erasure ran each battery
  anonymizer as its own UPDATE, so once the first had scrubbed the
  column the second matched on, the second changed nothing; a delete
  after an anonymizer missed the same way. Every eraser's rows are now
  read first, deletes run before anonymizers, and the anonymizers on a
  table run as one UPDATE. `RowsAffected` is the rows each matched.
- **Rows a battery eraser removes lose their audit diffs and files.**
  Erasure scrubbed the audit log and deleted stored objects only for the
  entity rows it erased itself. Entity rows a battery deletes or
  anonymizes are now scrubbed too, and lose every file for a delete or
  the scrubbed file columns for an anonymizer.
- **Erasure locks the rows it reads stored-object keys from.** On
  Postgres a concurrent update could swap a file in between the read
  and the delete, and the new file survived. The read is `FOR UPDATE`.
- **Storage keys have one spelling.** The local backends
  (`battery/storage` and `upload.NewLocalStorage`) accepted `a//b.png`,
  `a/./b.png`, `/a/b.png` and, on a case-insensitive disk, `A/b.png` for
  `a/b.png`, so erasing a row that named the alias deleted another
  user's file. A key that is not in its clean form, a leading `/` and
  `.` are refused with `ErrInvalidKey` on every operation (the upload
  serve handler answers 400), and Delete refuses a folded spelling as
  Save does. Keys the framework mints were already clean.
- **An A2A run stops when another replica settles and trims its task.**
  A task cancelled on another replica and removed by retention before
  this replica's poll saw it kept its handler running. The run now ends
  when the task is gone.
- **An A2A task past its timeout cannot complete.** A handler that
  called `Complete` after `TaskTimeout` marked the task completed; it now
  ends failed with "task timed out", and every task mutation after the
  deadline returns an error wrapping `context.DeadlineExceeded`.
- **A2A push notifications arrive in order per config.** Deliveries ran
  in parallel, so an appended artifact chunk could reach the receiver
  before the one it extends. Each config now gets its events one at a
  time, in order. An event that reaches the queue after a newer one for
  the same config (a `WORKING` publish overtaken by `CancelTask`) is
  dropped, so a terminal state is always the last thing a receiver sees.
- **ACP `session/new` refuses a session ID the connection holds.** An
  agent that returned an ID already live or draining replaced that
  session's state; the call now answers an internal error and keeps it.
  `session/load` does the same when the loader returns a session whose
  ID differs from the one requested.
- **An ACP client used after the connection closes writes nothing.**
  `Update` and `RequestPermission` blocked on an output nobody read;
  they return "acp: connection closed", and so does a call that was
  queued behind a stalled write when the connection closed.
  `RequestPermission` also returns when its `ctx` ends, whether it is
  queued behind a stalled write or its own request is the write stuck on
  a client that stopped reading. See [ACP](framework/docs/content/acp.md).
- **Kiln's ACP `session/load` replays only that session's chat.**
  Loading a session replayed every message in the journal, other
  sessions' prompts included. Messages an ACP turn writes carry
  `acp_session`, and a load replays its own. The replay is written
  after the journal lock is released, so a client that stops reading
  no longer blocks every edit.
- **A cancelled Kiln ACP prompt stops.** Once the model's stream had
  returned, the turn journaled its text and ran every tool call despite
  the cancel; it now checks after the stream and before each call.
- **A site-search page built with no sections is found by its title.**
  `sitesearch.Build` indexed the title only alongside the first
  section, so a page passed with a title and no sections matched
  nothing. It now gets an empty lead section, as extraction gives
  every page.
- **`gofastr generate app --out=` with no value is refused.** The empty
  value wrote into the module root, as `--out=.` does. It is an error
  now that names `--out=.` for the module root.
- **`gofastr build --help` lists `--no-generate` and `--no-contracts`.**
  Both flags worked but were missing from the help text, which also
  left out the contract gate.
- **A page served in place of a guarded screen keeps the screen's
  metadata to itself.** A policy's `RenderAlt` rendered the alternate
  under the guarded screen's title, description and article flag, in
  the full page, the nav partial's head and `X-Gofastr-Title`, and the
  static export. The alternate now carries only its own. A component's
  `HeadHTML` is read from the loaded instance, not the registered one.
- **The PWA worker denies sensitive paths behind a locale prefix.**
  `/es/api/me` routes to `/api/me`, but the deny list and the precache
  filter knew only the unprefixed form, so a credentialed response
  could enter the shared cache. Each denied path is now also denied
  under every locale tag, the default included.
- **The sitemap runs a page's policy the way a request does.** It ran
  the policy chain without the page's resolvers, so a policy reading
  resolver state left a public page out. The new `(*App).PagePolicy`
  installs the route match and resolver state, runs the eager
  resolvers and the policy chain, and applies region-guard redirects;
  the sitemap lists a page on Allow, or on RenderAlt with no error. A
  resolver that fails when a region guard reads it fails the page here
  too.
- **A cascade write honors the child's read scope.** A nested HasOne,
  HasMany or many-to-many child came back in the response whole, so an
  anonymous POST read a draft child its own GET would hide. A hidden
  child now comes back as its key only, as a direct write does; the
  parent's AfterCreate and AfterUpdate hooks see it that way too.
- **A cascade child keyed by a `WireName` links.** The child's key was
  read with the parent's wire names, so a child whose key had a
  `WireName` left the foreign key NULL or failed the insert. A
  many-to-many link by id now answers with the child's wire key.
- **An upsert that changes nothing returns the row's id under a read
  scope.** The DO NOTHING arm's read-back applied the read scope and
  failed with `sql: no rows in result set`; it answers the id alone, as
  the DO UPDATE arm does.
- **A streamed list that fails midway stays invalid JSON.** A row scan
  or cursor error closed the envelope with a `total`, so a truncated
  stream parsed as a complete, shorter list. The stream now stops
  without closing it.
- **A queue's memory Replay keeps a job that failed again.** Replay
  deleted the dead record by job ID once the job was back on the queue,
  so a job that died a second time before Replay finished lost its new
  record. Each dead record now carries a generation, and Replay deletes
  only the one it read.
- **A stale Redis Nack or Reclaim no longer pushes a second copy.** A
  worker whose claim had moved on pushed the job back and then lost
  the delete race, so two workers ran it. A client that implements the
  new optional `CompareAndMover` capability (`MoveIfEqual`, one Lua
  script) checks, pushes and deletes atomically; others narrow the
  window with push-then-conditional-delete. See `gofastr docs queue`.
- **An inbound webhook keeps its dedupe key.** The ingest handler wrote
  the key after the enqueue through `UpdateEnvelope`, and a worker
  that settled first wrote back the keyless snapshot it had loaded,
  so the sender's redelivery ran twice. The shipped stores implement
  the optional `RegisterDedupeKey`, which writes only the key, and an
  `UpdateEnvelope` with an empty key keeps the stored one. Clearing a
  key goes through `RegisterDedupeKey(id, "")`.
- **A failed inbound envelope's retry is recorded.** The settle and
  processing fences treated `failed` as terminal, so the queue's retry
  of a failed envelope ran without counting its attempt or recording
  its result. Only `processed` is fenced now.
- **A stale webhook retry no longer replaces a live lease.** A worker
  that overran its lease wrote its backoff over the lease the next
  claimant held, and a third worker claimed the delivery while the
  second still sent it. A retry or `dead` settle from a claimed
  delivery now lands only while the row holds that claim's lease, so a
  stale give-up no longer dead-letters a delivery an operator replayed.
  A stale `dead` settle no longer overwrites a delivered row, and a
  stale success still lands: the receiver has the payload.
- **The memory webhook stores no longer recreate a reaped row.**
  `UpdateDelivery` on a missing delivery and `UpdateEnvelope` on a
  missing inbound envelope inserted it; both are no-ops now, as the
  SQL stores' UPDATEs always were.
- **An outbox replay restarts the handler grace.** `Replay` and
  `ReplayConsumer` kept the delivery's original `created_at`, so on a
  replica without the consumer's handler a replay past the grace was
  abandoned on its first pump. The reset now stamps `created_at` too.
- **Cron catch-up after long downtime keeps the newest ticks.** The
  bounded search counted forward from the last run, so after more than
  five years down it kept the oldest ticks and skipped every newer one,
  the current tick included. It now counts back from now.
- **Outbox retention keeps a replayed delivery.** Under Postgres READ
  COMMITTED a parent could complete over the delivery a concurrent
  Replay had just reset, and retention then deleted the pending child
  the relay was about to send. Retention now keeps any parent with a
  delivery that is not dispatched.
- **An Int field refuses an inexact fraction.** `1.0000000000000001`
  rounds to the float `1`, and crud stored it as `1`. Integrality is
  now checked on the number's text; `1.0`, `1e3` and `-2.50e1` still
  pass.
- **A Date field reads back as a date.** Reads returned
  `2026-10-27T00:00:00Z`, which a resave then refused. Get, list,
  cursor, stream, `?include=`, group counts and typed reads return
  `2026-10-27`; a typed `time.Time` field still decodes it. Hooks see
  the date string too. An in-process write takes a `time.Time` or
  RFC 3339 text at exact midnight for a Date field, so a typed read
  saved back through the repository keeps its date.
- **A JSON field holding a string reads back as JSON.** A document whose
  top-level value was `"123"` read back as `123`, and a resave changed
  its type.
- **A typed create keeps a column with a digit.** `MarshalEntity` named
  `Line2` `line2`, which matched no column, so `line_2` was dropped.
  In-process writes (`CreateOne`, `UpdateOne`, `UpsertOne` and the
  batch calls) now resolve a key through the wire name and the camel
  form of each column, and two keys that name one column are refused,
  as the HTTP body path refuses them.
- **An export no longer drops rows after a NULL key.** SQLite lets a
  non-`INTEGER` primary key hold NULL; a page that ended on one asked
  for `id > NULL` next, matched nothing, and the export finished with a
  valid checksum and the rest of the table missing. The export now
  fails naming the table.
- **A table named after a client member is refused.** The JS SDK binds
  each resource on the `Client`, so a table whose property was `token`,
  `baseURL`, `do` or an `Object.prototype` name replaced that member: a
  `token` table sent `Bearer [object Object]` on every request. The
  generator now refuses such a table, and refuses Go endpoint names that
  declare a method beside `Client.Token`, `Client.HTTP` or
  `Client.BaseURL`.
- **A nested repo update reaches a child with its own key.** The nested
  patch struct always carried `ID json:"id"`, so updating a child keyed
  on another column fell into the server's create branch. It carries the
  child's key field now.
- **An optional OpenAPI request body is optional in the generated CLI.**
  The CLI demanded `--file`, and the body schema's required fields, even
  when the operation's `requestBody` is not `required`. A `required` that
  is not a boolean, or a `content` that is not an object, fails the
  generate instead of reading as absent.
- **The OpenAPI CLI sends the token the document's `security` names.**
  It took the first supported scheme by name and ignored `security`. It
  now follows the top-level requirement, and an operation whose own
  `security` names another scheme sends the token under that scheme's
  header.
- **The generated Go client decodes `?include=` responses.** An
  included relation arrives as a record in the key's place, and decoding
  it into the string field failed. The field still holds the key, and
  the record lands in the entity's `Included` map.
- **`gofastr generate sdk --only=` clears the settings list.** An empty
  `--only=`, `--exclude=` or value flag fell back to the `gofastr.yml`
  entry instead of overriding it.
- **The OpenAPI CLI picks a binary body's media type the same way every
  run.** It took whichever type map iteration gave first; it prefers
  `application/octet-stream`, then the first type by name.
- **The A2A SQL store honors an offset with no limit.** `ListTasks`
  with `ListQuery{Offset: 1}` and no `Limit` returned every task, since
  `OFFSET` was written only beside a `LIMIT`. It skips the offset now,
  as the memory store does.
- **CRUD reads inside `App.InTx` see the transaction's own writes.**
  `GetOne`, `ListAll`, `CountAll`, the aggregates, `?include=` loads
  and typed queries ran on the pool, so a row created earlier in the
  same transaction read as missing, and on a one-connection pool
  (SQLite in memory) the read waited on the transaction until its
  deadline. Reads now run on the transaction the context carries.
- **`softdelete.SoftDelete`, `Restore` and `ForceDelete` join the
  ambient transaction.** They wrote on the pool, so the change
  committed on its own even when the caller rolled back.
- **An upsert with an empty body inserts a row of defaults.** On an
  entity whose only required column is an auto-increment key,
  `UpsertOne` with nothing to write rendered `INSERT INTO t () VALUES
  ()`, a syntax error on both dialects. It renders `DEFAULT VALUES`
  now.
- **A request path can no longer add links to a page's `Link` header.**
  The `rel="alternate"` markdown link was built from the decoded path,
  so a path holding `>, <https://evil.test/mcp>; rel="service"` closed
  the URI and advertised the attacker's MCP endpoint. The escaped path
  is used now.
- **The PWA offline page's navigation links are no longer precached.**
  Every root-relative `href` on the offline page joined the precache,
  `<a href="/dashboard">` included: the install stored the signed-in
  user's page, and the worker served it to the next user offline. Only
  assets are precached now, and an asset URL is read from its HTML
  spelling (`&amp;` was requested literally, failing the install).
- **The PWA cache version follows the static file actually served.** A
  file present in both the static dir and the embedded FS is served from
  the dir, but its fingerprint was read from the embedded copy, so a
  redeploy that changed only the dir's file kept the old offline cache.
- **`/es/llm-pages.md` links the Spanish pages.** The index served
  under a locale prefix linked `/about/llm.md`, the default-language
  doc. Its links now carry the prefix. A page's own `/es/…/llm.md`,
  and the markdown an `Accept: text/markdown` request negotiates, now
  carry the Spanish canonical, the hreflang alternates and prefixed
  body links, as the HTML head does.
- **A sitemap key holding `/` stays one segment.** `StaticPaths`
  returning `{"id":"a/b"}` was listed as `/orders/a/b`, a page nobody
  serves, and a space went out unescaped. It is now `/orders/a%2Fb`.
  The static export renders that page with the key `a/b` and writes it
  where a static host serves `/orders/a%2Fb`; it substituted the value
  raw, and a catch-all's components the same way.
- **An ACP prompt error no longer reaches the client.** A
  `Session.Prompt` that returned an error answered
  `session/prompt: <error text>`, so paths and driver text crossed the
  wire. It now answers `session/prompt: internal error`, as
  `session/new` and `session/load` do.
- **An A2A subscribe follows the task's current run.** A
  `SubscribeToTask` that arrived as a run paused, with a resume starting
  the next run before the snapshot was read, forwarded the paused run's
  events: the stream ended on a `WORKING` snapshot and the completion
  never arrived. The subscribe now re-reads until the run holds still.
  On the replica whose run asked for input, a subscribe made after
  another replica resumed the task attached to the local run and closed
  when that run's handler returned, or waited on it while the handler
  was still unwinding; it now follows the store to the task's end.
- **A2A task retention keeps the task `ListTasks` shows first.** On a
  status-timestamp tie the SQL stores deleted by id ascending while
  `ListTasks` and the memory store ordered by update time and id
  descending, so the task listed as newest could be the one deleted.
- **A `bool` field sent as `"true"` is stored as a boolean.** Validation
  accepts `"true"`, `"false"`, `"1"` and `"0"`, but the value reached the
  database as the string, and SQLite kept `"true"` as text that
  `?enabled=true` never matched. Every accepted spelling now binds as
  `true` or `false`.
- **Two screens whose names differ only in case no longer share phrase
  keys.** `PostsList` and `postslist` generate distinct types and files
  but both wrote `<app>.screens.postslist.*` keys, so the same slug with
  different English made `i18n.Define` panic when the app started. The
  later screen in the spec now gets `postslist_2`; other keys are
  unchanged.
- **`gofastr pack` refuses a bad output flag.** `-o` with no file name,
  or a misspelled flag such as `--output`, was ignored: the YAML went to
  stdout and the command exited 0. Both now fail with exit code 1.
- **A `--from-openapi` CLI no longer follows a redirect with its
  credentials.** Its HTTP client followed 3xx answers and re-sent the
  bearer token or API-key header to whatever host the redirect named.
  It now answers with the 3xx response instead of following it.
- **A pgvector document replace is one transaction.** Re-indexing a
  document deleted its old chunks and then inserted the new ones, so an
  insert that failed (metadata holding NaN, a NUL byte in the text) left
  the document with no chunks at all, and two replicas replacing the same
  document at once could leave chunks of both versions. The new chunks
  are now checked first, and the delete and insert commit together under
  a per-document advisory lock. The keyword index changes only after the
  commit.
- **Long Postgres table names keep their search indexes.** A search or
  pgvector table whose name is 63 bytes long got no GIN or `doc_id`
  index: the index name, cut at 63 bytes, was the table's own, so
  `CREATE INDEX IF NOT EXISTS` skipped it. Such a name now ends in a
  hash of the table name. Shorter names keep the index name they had.
- **Two Kiln page patches at once no longer lose one.** Each
  `update_page_element` read the page, patched a copy and stored the
  whole page, so two patches built from the same version both answered
  OK and the second erased the first's edit. The second is now refused
  with `kind: "conflict"` and the refetch hint. A session journal that
  already recorded such a pair still loads, the later patch winning.
- **Deleting a seeded Kiln entity deletes its seeds.** The seeds stayed
  behind, and `freeze` refused the world for seeding an entity it does
  not have. `undo` brings the entity and its seed rows back.
- **Kiln answers `conflict` for a path the app already serves.** A route
  that overlaps another (`/x/{id}` beside `/{name}/y`), a page on an
  entity's CRUD mount (`/api/posts`), and an entity, or a table rename,
  whose mount is a page's path failed the rebuild with
  `kind: "internal"`. Each is refused with `kind: "conflict"` naming the
  other path.
- **A SQLite key change no longer strands the tables that reference it.**
  Moving an entity's primary key (from `id` to `code`) rebuilt its table
  on boot or in a generated migration, while a child's
  `REFERENCES parent(id)` and a many-to-many pivot's foreign key kept
  naming the old column, and every later write to them failed with
  "foreign key mismatch". Boot and `migrate generate` now refuse the
  change and name those tables; a reference onto a column that stays
  unique is kept.
- **Dropped tables go in dependency order when DDL holds a literal.** A
  string literal that read like `REFERENCES t(id)`, in a `CHECK` or a
  default, counted as a foreign key when a generated migration ordered
  its `DROP TABLE`s, so a parent could be dropped before the table that
  references it.
- **A batch item reads its numbers as a single create does.**
  `{"items":[{"enabled":1}]}` was refused as not a boolean, and a number
  sent for a string field was stored as its text.
- **A multipart Float field takes only the canonical decimal spelling.**
  `0x1p4` and `1_000` were stored as `16` and `1000`; the same text in a
  JSON body is refused.
- **An A2A skill can no longer store a part with no content.**
  `DataPart(nil)` or `Part{}` passed to `Working`, `Artifact`,
  `Complete` or a pause was stored, and every later `GetTask`,
  `ListTasks` and stream of that task failed to decode it. The call now
  returns an error and the task is unchanged. A data part whose value
  encodes as `null` (a nil map, slice or pointer, or a raw `null`) counts
  as no content too.
- **Paused A2A tasks are bounded per owner.** A skill that asks for
  input or auth on every send kept one row per send forever, outside
  `TerminalTaskRetention`. Past `Config.PausedTaskRetention` (default
  64, negative disables) the oldest paused tasks are canceled, and
  terminal retention trims them.
- **ACP keeps required fields when they are empty.** An empty text
  block, a resource link with no name, a diff that clears a file
  (`newText: ""`) and a terminal item were sent without the field the
  schema requires.
- **An entity MCP tool refuses an integer it would store rounded.** An
  `int` argument past 2^53 spelled with a point or an exponent
  (`9007199254740993e0`) reached the route as `9007199254740992` and was
  stored, where the route itself refuses that body. Tool handlers now get
  such a number as the `json.Number` the caller sent.
- **Entity MCP tools take a `decimal` field as text.** The tool schema
  asked for a JSON number, which the route refuses for a decimal, so a
  required decimal could not be written through MCP at all. It now asks
  for the string the route stores (`"12.34"`).
- **A JSON field keeps an integer past 2^53 exactly.**
  `{"n":9007199254740993}` was stored exactly and read back as
  `9007199254740992`; an integer past int64, or one spelled with an
  exponent (`9007199254740993e0`), was rounded before it was stored.
  Such an integer now reads as `int64`, or as the `json.Number` it was
  written as when `int64` cannot hold it; every other number still reads
  as `float64`. A Before-hook sees that `json.Number` too, and a typed
  hook that edits another key in the field writes it back unchanged.
  `TypedQuery` reads it the same way into an `any` field, so reading a
  row and saving it back no longer rounds the number.
- **A CRUD body with a second JSON value after the object is refused.**
  `{"name":"first"}{"name":"second"}` wrote the first object and dropped
  the rest; it is now a `400`.
- **A `bool` field takes `1` and `0`.** The integer `1` was refused with
  `must be a boolean` while `1.0` and `"1"` were accepted.
- **SQLite `FieldEquals` no longer matches an array or object field** by
  its JSON text.
- **Concurrent semantic writes of one document cannot mix two
  versions.** Two `Add` or `Remove` calls for the same document could
  interleave and leave chunks of both.
- **Semantic metadata holding a nested map or slice survives a
  restart.** The write-ahead log could not encode it, and the old
  document was already gone. Metadata the log cannot encode now fails
  `Add` and leaves the old document in place.
- **Semantic WAL replay refuses entries from another embedder.** Add
  entries record the embedder's name, so replaying a log under a
  different model fails instead of mixing vectors from two models.
  Entries written before this release carry no name and replay as
  before.
- **pgvector hybrid search compares large integer metadata exactly.**
  Keyword hits are filtered in SQL, as vector hits are, instead of
  through a `float64` recheck that let tenant `2^53+1` match `2^53`.
- **A reranker can change the top result.** Without MMR it saw only the
  K candidates already chosen; it now sees up to `max(4K, 50)` and K is
  applied after it.
- **Site search skips content inside a hidden or ignored ancestor** when
  it picks a page's main content.
- **A site search call waiting on an index build returns when its
  context ends.**
- **An A2A file part with no bytes keeps its task readable.**
  `{"raw":""}` was accepted, then stored as `{}`, a part with no content
  field: on a SQL store every later `GetTask` and `ListTasks` that held
  the task failed. An empty file now encodes as `{"raw":""}`.
- **ACP `RequestPermission` works from every handler.** A permission
  request made during `session/load`, or through a `Client` kept from a
  load and used inside another request, waited forever: the handler ran
  on the connection's reader, the goroutine that delivers the client's
  answer, and the connection stopped answering. Handlers now run beside
  the reader, and frames that arrive meanwhile are answered in order
  after it. At most 256 frames or 16 MiB wait; past that a request is
  refused with an internal error. A `session/cancel` still stops a
  running turn at once. Input EOF cancels the waiting handler and drops
  the frames still waiting behind it, since nobody is left to answer.
- **An ACP tool-call patch can clear a field.** `ToolCallUpdateFrame`
  dropped a title set to `""`, an empty content list and empty
  `rawInput`/`rawOutput` maps, and the client kept the old values because
  an absent field means "unchanged". Fields set to empty are now sent.
- **An MCP notification stream closes when its session is revoked.**
  Delivery checked the user the stream connected with, and the MCP
  transport dropped the re-check the auth middleware installs, so a
  stream kept receiving after its session was deleted or its token
  revoked. Each delivery now re-establishes the caller through
  `handler.RecheckPrincipal` and closes the stream when that fails, as
  the entity event stream does. Tool handlers now see that check too.
- **The anonymous MCP stream cap counts per host.** The seat key kept
  the source port, so each HTTP/1.1 stream had a bucket of its own and
  one host could hold any number. Behind a proxy all anonymous streams
  now share one cap.
- **A duplicate key after an out-of-range number is refused.**
  `handler.CheckObjectKeys` read numbers as `float64` and stopped at
  `1e309`, so `{"arguments":{"x":1e309},"name":"safe","name":"danger"}`
  passed and MCP `tools/call` ran `danger`.
- **An MCP tool's `[]string` type union is enforced.** A schema written
  `"type": []string{"string", "null"}` skipped type validation and
  accepted any value; it is now checked like the `[]any` spelling.
- **`app_routines` no longer probes the database dialect per call.** The
  probe ignored the caller's context, so on an exhausted pool a canceled
  call held for about 15 seconds. The tool reads the dialect the boot
  probed.
- **An erasure dry run reports what the erasure would do.** It counted
  each eraser on its own, so a row two of them reach counted twice, and
  it passed where the real run failed on a constraint. It now runs the
  real erasure in a transaction it rolls back, holding the same row
  locks while it runs, and reads no stored keys and deletes no files.
- **Erasure works across tables that reference each other.** Two tables
  that reference each other, and a table with an `ON DELETE RESTRICT`
  reference to itself, made the erasure fail. References between rows
  being erased are now emptied before the deletes; a `NOT NULL` one
  fails the erasure and names the relation, and so does one that runs
  through the primary key, which the delete matches on.
- **Erasing a SQLite row with a NULL primary key is refused.** Staging
  never finished on such a row; the erasure, dry run included, now
  returns an error naming the table.
- **Erasure keeps a stored file another row names with `\u` escapes.**
  On SQLite a variants JSON that spells the key with `\u` escapes missed
  the shared-key check, so the file was deleted under the other row.
- **A raw table with no primary key exports every row.** Export paged it
  by an `id` column it does not have: SQLite exported one row and
  Postgres failed. It is read in one statement, and its manifest
  `primary_key` is `""`. A registered raw exporter with an empty
  `PrimaryKey` takes the same path instead of a guessed `id`, which on
  SQLite stopped after the first page under a valid checksum, and a
  comma-joined `PrimaryKey` is a composite key rather than a panic.
- **A Postgres import of ids below the sequence minimum succeeds.** A
  table whose restored ids were all 0 or negative failed the sequence
  reset; the sequence now restarts at its minimum.
- **A2A: a `CancelTask` served by another replica stops the task.** With
  a shared store the cancel only wrote the store, so the replica running
  the task kept its handler going until its next write or `TaskTimeout`,
  and that replica's streams ended without `TASK_STATE_CANCELED`. The
  running replica now polls the store while the handler runs, cuts the
  handler's context when it finds a terminal state it did not write, and
  sends that state to its subscribers. When both replicas cancel at once
  and the other one's write wins, the losing cancel sends it.
- **A2A refuses a JSON-RPC id that is not a string or a number.** An id
  of `true`, `{}` or `[]` was dispatched and echoed back; it now gets
  -32600 with a null id.
- **ACP `Serve` returns when its context ends while input is idle.** The
  read loop checked the context only after a line arrived, so an agent on
  a quiet stdin did not stop on shutdown.
- **ACP `PlanUpdate(nil)` sends `"entries": []`.** An empty plan omitted
  the required field, so the client kept showing the old plan.
- **BREAKING: the auth stores share a mixed-case users table with the
  entity layer on Postgres.** `EntityUserStore`, `EntitySessionStore`,
  `EntityTwoFAStore`, role updates, the `<users>_oauth_links` table and
  the passkey and magic-link token stores quoted a declared name such as
  `Members` as written. AutoMigrate's DDL is unquoted and Postgres folds
  it, so the store and the entity layer used two tables: accounts the
  admin listed were not the ones that could log in. The stores now fold
  the name the way AutoMigrate does, and a dotted name means
  `schema.table` as it does for AutoMigrate. When `EnsureSchema` finds a
  case-kept table or case-kept mapped columns from an older version, or
  a dotted name an older version created as one literal table, it stops
  with an error listing the quoted `ALTER TABLE` statements to run,
  rather than boot on an empty table. After the rename, the old
  case-kept index names are left beside the new ones and can be dropped.
- **Mixed-case tables work with transitions, aggregates, soft delete and
  setup on Postgres.** A state transition, `SUM`/`AVG`, the ambient
  transaction's event confirmation, `softdelete.SoftDelete`/`Restore` and
  the setup battery's admin step quoted the declared spelling of a table
  or column, which names nothing on Postgres when AutoMigrate stored it
  folded. `SoftDelete` also failed on SQLite, which has no `NOW()`.
- **The admin audit page reads a mixed-case `AuditTable` on Postgres.**
  `EnsureAuditTable` creates the table quoted with its case kept; the
  page's count and list queries named it unquoted, so Postgres looked for
  the folded name.
- **Erasure scrubs a mixed-case custom audit table on Postgres.** With
  `WithEraseAuditTable("AuditTrail")`, erasure folded the name and failed
  with `relation "audittrail" does not exist`, though `EnsureAuditTable`
  had created `"AuditTrail"`.
- **A 409 names the field on a mixed-case Postgres table.** Postgres
  reports a folded table's constraints, columns and relation in lower
  case (`members_email_key`, `"teamid"`), and the mapper compared them
  with the declared spelling, so the response named no field.
- **A rolled-back update in a caller's transaction is not announced
  under camel wire keys.** The confirm query that tells a committed
  update from its rollback checks the announced values, but it mapped
  the record's keys back by snake-casing them, so a mixed-case column or
  a `WireName` field dropped out. With every column dropped, the row's
  existence alone confirmed the update and a rolled-back write emitted
  `EntityUpdated`.
- **A WebSocket whose app stops reading is reclaimed.** With the read
  buffer full, the pump could not read the peer's Pongs or Close, and the
  keepalive skipped its checks, so a stalled connection stayed open for
  ever. After `ReadIdleTimeout+PongTimeout` an app that has called `Read`
  has its connection closed. An app that never calls `Read` (push-only)
  has incoming data dropped, so pings and Close keep working. A stall
  shorter than that bound no longer closes the connection as it ends:
  the pong clock restarts, since the peer's Pong waited behind frames
  the pump could not read.
- **WebSocket `Read` returns the last message before the error.** A
  message the pump handed off just before it stopped could lose a random
  `select` to the terminal error and vanish.
- **WebSockets follow RFC 6455 on malformed input.**
  - A text message that is not valid UTF-8 closes with 1007.
  - A Close reason that is not UTF-8 is echoed as 1002.
  - A payload length in a longer form than it needs closes with 1002.
  - Nothing is written after our own Close frame: a write queued behind
    Close gets `ErrClosed`, and a Ping answered after Close gets no Pong.
- **A reconnecting StateChannel client hydrates.** A snapshot carried
  the source's version, which could equal the sequence of an event the
  client had already applied, so the reconnect's snapshot was rejected
  and the page stayed stale. A snapshot now sorts above every sequence
  the channel sent. The reducer from `createSequencedReducer` takes the
  message's generation as a second argument (`reduce(env,
  info.generation)`); the first snapshot of a newer generation always
  applies, so a restarted server or another replica cannot strand the
  page, and a late message from an older generation is refused.
- **An evicted SSE stream frees its handler.** Evict-oldest and
  same-id replacement closed the old stream's channel, but a handler
  blocked writing to a client that stopped reading never saw it and
  stayed resident with no seat. The evicted response's write is now
  cut with a past deadline.
- **Presence reports a departure from another replica's TTL.** The
  sweep compared rosters that both left out expired members, so a
  member whose replica stopped refreshing never fired
  `OnPresenceChange`. Cap eviction of an expired roster had the same
  gap, and so did a replica's empty announcement that removed its own
  expired roster before the sweep reached it.
- **`fanout.PostgresFanout.Close` stops its fallback-row read.** The
  SELECT for a payload too large for NOTIFY ran with no context, and the
  dispatcher outlived Close holding a pool connection. Close now cancels
  the read and waits for the dispatcher to return.
- **MCP CRUD callers no longer share one rate-limit bucket.** A CRUD
  tool call reached the entity route from httptest's placeholder address,
  so a per-IP limiter put every MCP caller in one bucket and one caller
  could spend everyone's budget. The route now sees the `/mcp` caller's
  address and its `X-Forwarded-For` and `X-Real-IP` headers, which a
  trusted-proxy limiter reads only when that address is a trusted proxy.
- **`app_routines` reads the ledger under the call's context and says
  when it could not.** The read ran on a background context, so a call
  whose client had gone kept a connection waiting on a locked ledger. A
  failed read reported every routine `missing`, as if none had been
  applied; it now reports `unknown`. A ledger table that does not exist
  yet still reads as `missing`.
- **MCP integer enums compare exactly.** A tool argument above 2^53 was
  checked against the schema's `enum` through float64, so its neighbour
  matched a declared value and the declared value itself, which arrives
  as a `json.Number`, matched nothing. An in-process `CallTool` argument
  of any Go numeric type (`int32`, `uint64`, `float32`) compares by value
  too. Numbers compare by exact decimal value in any spelling (`1e2`
  equals `100`) from their text, so an argument like `1e999999` costs no
  more than its length.
- **A gated prompt's registration is not announced to callers its gate
  refuses.** `RegisterPrompt` sent `notifications/prompts/list_changed`
  to every stream; it now carries the prompt's gate, as tool and
  resource-template registration do.
- **Erasing one user's data no longer deletes every row of a table named
  `d`.** The erasure delete correlated each table with its staging table
  under the alias `d`, so in an owner-scoped table actually named `d` the
  subquery compared the staged keys with themselves and every user's rows
  matched. The delete now aliases the target table and names it inside
  the subquery.
- **Export, import and erasure find mixed-case entities on Postgres.**
  AutoMigrate creates tables and columns with unquoted names, so Postgres
  stores `Widgets.UserName` as `widgets.username`; the export, import and
  erasure quoted the declared spelling and failed with "relation does not
  exist". They quote with the new `query.QuoteFolded`, which lowercases
  each part of the name and then quotes it. A battery table is found
  as its battery created it: its own spelling first (battery/queue's
  quoted `"Jobs"`), then the folded one, never another case variant.
  On SQLite the table probe ignores case too, so erasure no longer
  skips a table whose declared name differs in case from the one
  SQLite stored.
- **Erasure scrubs the audit copy of a row whose key holds control
  bytes.** The audit log stores record ids with control bytes stripped,
  and the erasure matched the raw id, so an admin's audit entry for that
  row kept its diff. The id is now matched in its stored spelling, raw
  and as its `sha256:` digest.
- **An import restores rows that reference each other.** Two rows of a
  self-referencing table that name each other have no insert order that
  satisfies the foreign key, and the import failed. The row that closes
  the cycle is now inserted with that reference empty and updated once
  the table's rows are in. Two tables that reference each other restore
  the same way: the reference into the table written later is set once
  every table is in. Two such references sharing a column (a tenant)
  each keep it, a row with a NULL key that holds one is refused, and an
  update that sets no row fails the import.
- **Erasure handles a raw table with no primary key.** A `migrate.Table`
  with no key and a foreign key into an owned table made the erasure
  fail while staging it. Its rows are now matched through the foreign
  key, counted in the dry run, and deleted before every other table, so
  an `ON DELETE SET NULL` reference cleared by an earlier delete no
  longer leaves the row behind while the report counts it.
- **Erasure staging tables never reuse an app table's name.** A staging
  table named `gofastr_erase_0` hid an app table of that name for the
  rest of the transaction; staging names now skip every table in the
  graph.
- **A webhook client with a plain custom transport checks the address it
  dials.** An `Options.HTTPClient` whose transport was an
  `*http.Transport` with no proxy and no dialer of its own (set for TLS
  or pool settings) got the resolve-then-deliver wrapper, which resolved
  the target once to check it and let the transport resolve again to
  connect, so a name that rebound between the two reached an internal
  address. Such a transport is cloned with the dial-time guard, as
  `core/a2a` push delivery now does, and still speaks HTTP/2 when it
  would have on its own; `netguard.GuardPlainTransport` is the shared
  helper.
- **The Web Bot Auth rate-limit docs no longer promise a cap on one
  sender.** `AgentRateLimitKey` keys by a URL the signer chooses, so a
  sender with a domain and a key gets a fresh budget per path, query or
  subdomain. The docs said verification never widened what a caller
  could do; they now say the agent limiter splits a shared egress, and
  the example keeps an IP-keyed ceiling in front of it.
- **BREAKING: MCP tool results keep big integers exact.** The CRUD
  tools decoded the route's response with every number as a float64, so
  an id or count above 2^53 reached the agent rounded, and a module's
  reverse call through the broker got the same rounding. Results carry
  each number as the route wrote it; `crud.Redispatch` returns
  `json.Number` where it returned float64.
- **MCP write tools take what the HTTP route takes.** The create tool
  required the owner and tenant columns the server stamps, so an agent
  could not create a row the route accepted; a `json` field only took an
  object; and an optional field refused `null`, so an update could not
  clear it. The owner and tenant columns are no longer required, a
  `json` field takes any JSON value, and an optional field takes `null`.
- **MCP echoes a numeric request id exactly and answers no
  notification over HTTP.** The JSON-RPC envelope read an id into a
  float64, so an id above 2^53 came back rounded and the client could
  not match the reply. `ServeHTTP` and the `ServeSSE` POST answered a
  notification with a JSON-RPC reply; they answer `202 Accepted` with no
  body, as stdio already sent nothing.
- **BREAKING: typed hooks keep JSON integers exact.** A typed hook's
  value decoded the request body with every number as a float64, so an
  integer above 2^53 in a `map[string]any` or `any` field (an external
  id, a counter) was rounded, and the hook's merge wrote the rounded
  number back to the row. Integers in such fields now arrive as int64,
  other numbers as float64, and they merge back the same way, matching
  the untyped body. A hook that asserted `.(float64)` on a nested
  integer gets an int64.
- **Typed query bulk writes join the caller's transaction.**
  `UpdateAll`, `DeleteAll` and the soft-delete spelling of `DeleteAll`,
  plus `Count` and `All` on a typed query, ran on the pool even inside
  `App.InTx` or a hook's transaction, so a rollback kept their writes
  and on SQLite they waited behind the transaction's own lock. They run
  on the transaction the context carries.
- **A many-to-many link that breaks a constraint answers 409.** A
  cascade write swallowed any unique violation on the pivot insert, so
  a link refused by a constraint beyond the link's own key reported
  success on SQLite with the parent committed and no link, and on
  Postgres the aborted transaction answered 500. The insert runs under
  a savepoint; a violation is accepted only when a recheck finds the
  link (a concurrent request wrote the same pair), and any other is a
  409 that rolls the write back.
- **The many-to-many existence check reads the pivot's scope.** A
  registered pivot's tenant, owner and soft-delete filters now apply
  when a cascade write checks whether a link exists. Read unscoped,
  another tenant's row stood in for the caller's, the caller's link was
  never written, and a soft-deleted link counted as present. Re-linking
  a pair whose row is soft-deleted now answers 409 instead of reporting
  a link nobody can see.
- **`softdelete.SoftDelete` works on SQLite.** It set `deleted_at =
  NOW()`, which SQLite does not have, so every call failed there; the
  time is now bound as a parameter in UTC.
- **A2A streams and push notifications no longer leak work.**
  - A streaming send refused for its seat cap, or one whose client left
    before the first event, left the task SUBMITTED with its run claimed
    and never started, costing the owner one run slot each time. The
    seat is admitted before anything is created, and a claimed run
    always starts.
  - A CancelTask that arrived before the run started was lost and the
    handler ran anyway; the task now settles CANCELED without calling
    it. A stream whose first event was still being written when the
    cancel landed ends on the CANCELED event instead of closing after
    SUBMITTED.
  - Resubscribing could repeat an artifact append the snapshot already
    held, doubling its parts on the client. Events carry the task
    version their write produced, and a stream skips those its snapshot
    reflects.
  - Resubscribe's snapshot and poll reads ignored the client leaving, so
    a hung store read pinned the handler and its seat; they run under
    the request context.
  - Push deliveries to a hung receiver queued without limit. A delivery
    with no free slot is dropped with a Warn line.
  - A plain `*http.Transport` on the push client is cloned with the
    guarded dialer, so a name that resolves again between the check and
    the dial cannot reach an internal address. Proxies and custom
    dialers keep the per-request check. HTTP/2 receivers still get
    HTTP/2.
  - Retention trimming left the trimmed tasks' push configs behind;
    they are deleted with the task, and the next trim deletes a config
    registered while its task was being trimmed, once it is ten minutes
    old (a younger one may belong to a send still creating its task).
  - `MemoryStore.UpdateTask` handed back slices and maps it shared with
    the stored record, so a caller's edit changed stored state.
- **ACP refuses what it used to let through.** `session/load` on a
  session with a prompt turn in flight answers -32600: replacing the
  session let a second prompt run beside the first and cut
  `session/cancel` off from it. The same holds for a session
  `SessionOverflowEvictOldest` evicted whose canceled turn has not
  returned yet. A prompt still running when `Serve`'s
  input closes is canceled. A frame with a method but no `jsonrpc:
  "2.0"`, or an id that is not a string, number or null, gets -32600
  with a null id before dispatch. An answer to a permission request
  without `jsonrpc: "2.0"` settles that request as an error, never as
  an approval.
- **`gofastr init .` scaffolds in place without replacing a project.**
  It named the module `local/.`, which `go mod init` refuses, so every
  in-place init failed after writing its files; the project is now named
  after its directory. It also overwrote `main.go`, `entities.go`, `.env`
  (with a new signing key) and the rest in a directory that already held
  them. It now refuses, writing nothing, and names the files; `--force`
  overwrites them. An existing `.gitignore` is kept and gains the
  entries it lacks. `init <name> --reinit` no longer stops at the
  existing directory it is meant to refresh, and an unknown flag such as
  `--noentty` is an error (exit 2) instead of a default scaffold.
- **BREAKING: `/api/llm.md` links the page index only when the app
  serves one.** The index's Pages section always linked `/llm-pages.md`,
  which only a UI host built with `uihost.WithPublicLLMMD` serves, so an
  API-only app (or a host without the opt-in) published a link that
  answered 404. The app now passes the path its route table holds, and
  the section is left out when there is none.
  `crud.RegistryLLMMD` and `crud.RegistryLLMMDHandler` take a trailing
  `pagesIndex string` for that path; pass `""` from a hand-rolled mount
  with no page index.
- **The cache battery no longer serves one language to every reader.**
  `i18n.Middleware` (what `WithI18n` installs) now sends
  `Vary: Accept-Language, X-Locale` when the locale came from the
  headers, and `CacheMiddleware` reads `Vary` set by middleware outside
  it as well as the handler's. Before, the first reader's language was
  stored and replayed to everyone, and a CDN in front of the app did
  the same. Under `WithLocalePrefixes` the router strips `/es` before
  middleware runs, so `/es/pricing` and `/pricing` shared one entry;
  the key now carries the URL's locale. A locale resolver adds the
  headers it reads too, `Cookie` for `i18n.CookieLocale`.
- **BREAKING: the magic-link, password-reset and email-verification
  plugins refuse their in-memory token store in production.** Each
  plugin minted its own `MemoryMagicLinkTokenStore` when `TokenStore`
  was unset, so a link emailed from one replica answered "invalid or
  expired" on the next, and a restart dropped every pending link. In
  production mode (`DevMode: false`) `Init` now refuses that default,
  as the session and 2FA stores already do. Set each plugin's
  `TokenStore` (`auth.NewSQLMagicLinkTokenStore(db)`), or set
  `AuthConfig.AllowInMemoryStores: true` on a single node.
- **Typed after hooks redact multi-word fields.** An `OnAfterCreate` or
  `OnAfterUpdate` hook that cleared `CardNumber` wrote `""` under a new
  `card_number` key, while the response kept `cardNumber` with the
  original value. A typed hook's change now lands on the key the payload
  already uses, which also fixes a Before hook's change to a digit
  column such as `line_2` being dropped.
- **JSON and `data-cui-spa` forms pass CSRF and show refusals.** An
  `enctype="application/json"` form behind `middleware.CSRF` always got
  403: the runtime sent a JSON body with no token the middleware reads.
  Intercepted forms now send the page's token as `X-CSRF-Token`. A
  refused submit used to leave the page unchanged; it now puts the
  server's field errors in place, or toasts the status, as a
  `data-cui-rpc` form does, and a 2xx answer delivers its toast and
  screen-cache invalidation headers.
- **Client navigation keeps non-ASCII paths and layers.** A redirect
  answered to a client navigation sent its target raw in
  `X-Gofastr-Location`, so `/new/café` reached the router as
  `/new/cafÃ©`; it is now percent-encoded like the full page's
  `Location`. A `{param}` screen group re-rendered its layer on every
  click inside a project whose name the browser encodes (`my proj`),
  because the origin in `X-Gofastr-From` was compared un-decoded; it is
  now read in routing form. `X-Gofastr-Swap` percent-encodes a layer key
  holding non-ASCII or `%`, and the runtime decodes it, so such a
  navigation swaps below the kept layer instead of reloading the page.
- **Toasts keep their accents.** `ui.AddToast` wrote raw UTF-8 into the
  `X-Gofastr-Toast` header, which the browser's Fetch API reads as
  ISO-8859-1, so a Spanish "Creación guardada" toast rendered as
  "CreaciÃ³n guardada". The header now carries non-ASCII as JSON `\u`
  escapes, which the runtime's `JSON.parse` already reads, and
  `ui.InvalidateScreens` does the same for `X-Gofastr-Invalidate`.
- **Cursor pages with `?include=` on a key relation reach every row.**
  On an entity keyed on `(team_id, position)`, `?include=team_id`
  replaced the stored `team_id` with the related row before the cursor
  was encoded, so the next page started from an object: SQLite skipped
  the rest and Postgres failed. Includes now run after the cursor is
  taken.
- **A belongs-to cascade works on a parent keyed on several columns.**
  Creating a post keyed on `(author_id, slug)` with a nested `author`
  answered 500: the dependent-relation pass asked for the parent's
  single key column before checking the relation type.
- **A blank value in a composite key column is refused.** A create or
  upsert storing `""` in one column of a multi-column key succeeded, and
  the row could then never be fetched, updated or deleted, since a
  record path refuses an empty part. It now answers 400 naming the
  column.
- **A reopened widget fires its action once.** Closing a server-rendered
  widget and opening it again left the first mount's click and submit
  listeners on the reused root, so one click on its button sent the RPC
  twice and the second answer removed the widget. Each mount's listeners
  now go when it is dismissed.
- **Accept-Language honours `*` and refuses a bad `q`.** `en;q=0, *`
  served English, the one language the reader refused, because `*`
  matched nothing and negotiation fell back; it now picks a catalog
  locale the header did not refuse. An entry with a `q` outside 0 to 1
  (`q=garbage`, `q=NaN`, `q=2`) kept the default `q=1` and outranked
  every well-formed entry; it is now skipped.
- **A failed CRUD write inside `app.InTx` leaves nothing behind.** A
  `CreateOne` refused by its `AfterCreate` hook kept its row, so a caller
  that handled the error and committed stored the row it was told had
  failed; on Postgres any failed statement aborted the whole
  transaction. A CRUD operation that joins a transaction now runs under
  a savepoint, like a nested `app.InTx`.
- **rtc rooms across replicas announce each peer once.** A roster beat
  that listed a peer before its own join mirror arrived made the local
  sockets hear two joins, and `rtc.js` rebuilt the connection it had
  just made. A replica's expiry or an old seat's leave mirror announced
  a leave for a peer that had reconnected on another replica with the
  same id. Local sockets now hear only what changed in the merged view,
  and when replicas disagree on an id the latest seat stands for it. A
  peer that moves to another replica also frees its seat here, so it no
  longer counts against `MaxSocketsPerUser`.
- **rtc signals are never dropped in silence.** A burst of ICE frames
  past the room's 64-event queue lost signals while every socket stayed
  open, so a call could wait for ever on an answer that never came.
  `stream.StateChannel.CloseOnOverflow(true)` now also deepens the queue
  to 4096 events and closes every connection an event past that would
  have reached, so the clients reconnect and renegotiate. The
  webmcp-remote-assist example turns it on for its signaling channel.
- **BREAKING: a static export ships the app's own
  Content-Security-Policy.** `App.ExportStatic` wrote a fixed policy
  whatever `framework.WithSecurityHeaders` set, so a font or image host
  the live app allowed was blocked on the static site. The export now
  carries the configured policy in each page's `<meta>` (one per
  comma-separated policy, without `frame-ancestors`, `report-uri` and
  `sandbox`, which a meta ignores) and in full in `_headers`. The build
  refuses a policy holding a `'nonce-…'` source, which a file would give
  every visitor, or a control byte, quote or angle bracket.
- **Static exports keep redirects.** An `app.Redirect` route left no
  file, so an old link answered 404. Each exact redirect now gets a page
  at its old path that refreshes to the target, in every locale tree,
  plus a `_redirects` rule (308) for Netlify and Cloudflare Pages. In a
  locale tree the target follows the rule links follow: a static file,
  or a target that already names a locale, keeps its path, and a page
  takes the tree's prefix. A pattern redirect is skipped with a warning.
- **The exported sitemap lists only exported pages.** A page the export
  skipped (an excluded route, a page answering a status other than 200)
  stayed in `sitemap.xml` and sent crawlers to 404.html. When a
  screen's `StaticPaths` lists its pages in a different order on each
  call, the sitemap is kept whole with a warning, instead of keeping one
  page's entry by another page's verdict.
- **A rebuild picks up a changed `ExtraDirs` file.** `Builder.Watch`
  rebuilds into the same directory, and the previous build's copy of an
  extra file kept the new one out. Which file wins is now decided by the
  build and the static source, never by what the output directory held.
  An extra file can no longer replace the `_headers` or `_redirects` the
  build writes: `_headers` carries `frame-ancestors`, which a page's
  `<meta>` cannot.
- **A static dir's `index.html` files are exported.** Every `index.html`
  in the static source was dropped; only one at the path of a page the
  build rendered is now refused, and the build log names it.
- **Locale trees link static files at their real path.** With
  `WithLocalePrefixes`, a Spanish page's `<a href="/files/report.pdf">`
  was rewritten to `/es/files/report.pdf`, but static files are exported
  once, to the root, so the link answered 404. A link to a copied file,
  or to a path no route takes, keeps its root path. An escaped link to a
  route whose path is not ASCII (`/caf%C3%A9` for `/café`) is a page
  link, and takes the prefix.
- **A nested `ui.Tabs` shows its own panel.** The outer strip's active
  index also picked the inner strip's panel and tab with the same index,
  so choosing the outer second tab showed two inner panels at once.
- **`ui.Select` selects an option whose value is empty.** "All" (value
  `""`) marked `Selected` rendered unselected, so the form submitted the
  first option instead. `headless.Select` with an empty `Selected` and
  no placeholder now selects the empty-value option.
- **Combobox and Carousel ids stay their own.** Both wrote the ids they
  derived into the caller's options or slides, so a second component
  built from the same slice repeated the first one's ids.
- **`ui.FilterToolbar` refuses a hidden `sort` beside its sort control.**
  The collision check missed the defaulted `SortName`, so a carried
  `sort=old` submitted beside the control's value and the server read
  the stale one.
- **The sidebar's default nav name is translated.** With no `NavLabel`
  the landmark read "Primary" in every language; it now uses the
  `ui.nav.primary` phrase.
- **Every theme toggle on a page shows the same choice.** Choosing Dark
  in one pill group left the others marking the old scheme.
- **A `cascade_write` link writes a registered pivot through the
  pivot's own create.** The link row skipped the pivot entity's access
  permission, its `CRUD: false` and its tenant and owner stamps, so a
  caller the pivot route refused could add the row through the parent,
  and a multi-tenant pivot row landed with no tenant and read back as
  no link.
- **A client navigation to a `RenderScreen` recovery screen sets its
  title and head.** The partial arm sent no `X-Gofastr-Title` and no
  head carrier, so the previous page's tab title, language and meta
  stayed on the screen.
- **Negotiated markdown answers a refused page with the HTML's
  status.** `Accept: text/markdown` on a page whose policy blocks
  answered 200 with the withheld doc, and on one whose policy redirects
  answered 200 where the HTML answers 303. The block's status and the
  redirect now apply to both. `app.ScreenLLMMDResult` gained `Decision`.
- **The plain navigator drops its screen cache on a session change.**
  A navigation answer carrying `X-Gofastr-Session` left the cached pages
  of the previous identity replayable on Back; the envelope navigator
  already cleared them.
- **An error page is never cached.** A navigation across layout chains
  that fetched a 500 page cached it, so Back replayed the error after
  the server recovered.
- **A prefetched layout-less page leaves the layout shell.** A
  prefetched page with no layouts was swapped into the `<main>` of a
  page inside a layout, keeping the old page's chrome around it.
- **An intercepted overlay applies its signal seed.** The pane kept the
  answer's seed script inert, so its page signals read undefined.
- **`WithIdempotency` warns when its in-memory store runs beside
  `WithFanout`.** The default store keeps keys per process, so on
  several replicas a retry that reaches another one runs again. Set
  `IdempotencyConfig.Store` to a shared store.
- **BREAKING: loading content speaks the reader's language.** The route
  graph in `manifest.js` rendered each route's `Loading.Show` once, so a
  Spanish reader saw the default language's "Loading…". With a
  translator, the UI host now builds one `manifest.js` body and route
  graph per catalog locale, a page links its own locale's body by `?v=`
  hash, and the PWA worker precaches each one, listing the locales of the
  translator its request carries rather than the process default. The cache is keyed by the
  new `i18n.CatalogLocale(ctx)`, which only answers locales the catalog
  holds, so a forged `X-Locale` cannot grow it.
  `app.LoadingManifest.HTML` is now a method, `HTML(ctx) string`, that
  renders `Show` under ctx; it was a string field.
- **Kiln `undo` reverts the last world edit.** It cut one journal entry,
  which after an HTTP tool call was the `tool_result` envelope, so the
  edit stayed. It now cuts back to the edit, with its envelope. A failed
  apply restores the world from the journal. Seed rows go into the
  entity's table (`blogPosts` → `blog_posts`), not one named after the
  entity. A delete refused because its target is gone no longer spends
  the plan. Every world edit, `undo` and `reset_session` now refreshes
  the build-mode page, not only some edits; a theme or app-config edit,
  a nav edit, an undo of either, and a reset load the page again, since
  they change its stylesheet and sidebar. A page that reconnects after
  losing the event stream loads again too, so a shell edit made while it
  was away shows, and so does a page whose first connection finds a
  newer world than the one it was rendered from. `world_get` masks
  `jwt_secret`, `seed_password` and a credentialed `db_url` as
  `[redacted]`, as `GET /kiln/world` already did.
- **BREAKING: Kiln refuses hooks, routes and pages it would never run.**
  A hook with an unknown `when`, a hook action of `respond_json`, a hook
  with no entity, a route action other than `noop` or `respond_json`, or
  a page path without a leading `/` was stored and did nothing. Each is
  now refused with `kind: "validation"`. A session journal that already
  holds one still loads. A route pattern `net/http` cannot parse
  (`/x/{oops`), a page path with a parameter (`/users/{id}`), and an
  element edit that adds an unsupported action failed the rebuild with
  `kind: "internal"`; they are refused the same way.
- **Postgres prefix search finds every prefix.** The English config
  stems the query as well as the document, so `"pagina"` found nothing
  while `"pagin"` found "pagination". The last term now also matches an
  unstemmed `tsv_prefix` column, which `EnsureSchema` adds and fills on a
  table created before it. SQLite gets the same through an unstemmed
  FTS5 companion table, matched with FTS5's own prefix scan, so every
  word the prefix starts counts (reopening the index under a case variant
  of its table name finds the companion and does not fill it twice); a
  row both legs match sums their
  scores, as Postgres sums `ts_rank`. Query text splits into terms the way documents do, so `"won't"`,
  `"load-balance"` and `"H₂O"` no longer fail to match. On Postgres the column is
  filled in committed batches, so searches on other replicas keep running
  while an upgraded table is backfilled. A trigger computes the column
  on every insert and on every update of the text or fields, so a
  replica still running the previous release keeps it current during a
  rolling upgrade; it needs CREATE FUNCTION and CREATE TRIGGER on the
  table's schema.
- **Semantic search's keyword leg survives a restart.** `Open` with
  `Options.Keyword` (or a `PgVectorStore`) now rebuilds the keyword
  index from the stored chunks, so hybrid queries in a fresh process or
  a second replica no longer lose their keyword half; a store that cannot
  be read fails `Open` rather than leave the keyword index empty. A batch naming one
  `ID` twice keeps the last document instead of indexing both.
  `Filter.MetaMatch` compares values as JSON on every store: `5` equals
  `5.0` (a `json.Number` too), a named string type equals the plain
  string, `[]` matches only an empty array, and `null` matches a stored
  null, not a missing key.
- **Site search ignores a sitewide `noindex`.** A staging deploy's
  `uihost.WithRobotsMeta("noindex")` left the site's own search empty.
  Only a page's own robots directive now drops it from the index, and one
  inside a `data-search-ignore` element does not.
- **Migrations compare foreign keys.** `DiffSchema` adds a missing
  foreign key, re-adds one whose columns, target or `ON DELETE` changed,
  and drops one no relation declares (a destructive change that
  `ApplySchemaDiff` holds behind `AllowDestructive`). SQLite rebuilds the
  table. Changing a Postgres primary key that other tables' foreign keys
  reference now drops and re-adds those keys around the change, and a
  table rebuild that moves the key onto a new column keeps its rows. A
  foreign key onto the old key that no relation declares is not dropped
  on the way: boot and a generated migration refuse the change and name
  it, and `DiffSchema` reports its drop as destructive. A key's target
  carries its schema, so a key onto `x.parents` is not taken for one
  onto the search path's `parents`, and a schema-qualified child keeps
  its key through a parent's key change.
  Views drop before the table changes they depend on and are created
  after, in `DependsOn` order, and a materialized view's dependent views
  are recreated around it on boot.
- **Every setup step runs, and every replica leaves setup.** A step
  after the one that made `Complete` true (`HealthStep` after
  `AdminStep`) never ran, because the wizard swapped to the app as soon
  as `Complete` flipped. On several replicas, the ones that did not run
  the wizard answered 503 for ever. They now re-check `Complete` at most
  once a second and swap, answering the request that heals them with a
  307 to the same path. `AdminStep` and an app spec's `seed_email` store
  the address in the auth battery's canonical form, so
  `Ops@Example.com` can sign in.
- **Email clients show one body, and a sent mail is not sent twice.** A
  message with text and HTML bodies was `multipart/mixed`, so clients
  showed both; it is now `multipart/alternative` (inside `mixed` when
  there are attachments). A failed `QUIT` after the relay accepted the
  message returned an error, and a retrying queue delivered it again.
  A template file saved with CRLF line endings no longer leaves `\r` in
  the subject.
- **PDFs past 1.5 MB render, and `BaseURL` brings the app's tokens.**
  `chromepdf` navigated to a `data:` URL of the whole document, which
  Chrome caps at 2 MiB. It now sets the document as the content of
  `about:blank` and waits for its load event. The app.css that
  `print.Config.BaseURL` makes absolute was always blocked by the
  document's CSP; its origin is now added to `style-src`. Name that host
  in chromepdf `Options.AllowedHosts` for the PDF to fetch it.
- **BREAKING: erasure follows foreign keys.** `EraseUserData` deleted
  the user's owner-scoped rows only, so a row in another table that
  referenced one (another user's comment on the user's post, a list's
  items, a pivot row) either blocked the delete or was left pointing at
  nothing. Such rows are now erased too, children first, and reported in
  `EraseReport.Cascaded`. A relation declared `ON DELETE SET NULL` is
  not followed. One declared `RESTRICT` or `NO ACTION` refuses the
  erasure, dry runs included, while a row the erasure does not remove
  still references a row it would. The audit trail's `diff` of the
  user's rows, and of rows the erasure removed, becomes
  `{"erased":true}`. Keys are staged in temporary tables, so a user with
  a million rows binds no million parameters.
- **A data import restores what it exported.** Sources are now written
  parents first, a pivot after both its ends and a self-referencing
  table parent row first, so foreign keys no longer refuse the restore;
  many_to_many pivot tables are exported at all. Integers past 2^53
  restore exactly instead of as the nearest `float64`. On Postgres an
  auto-increment column's sequence moves past the restored ids, and the
  battery-table presence check reads the current schema only.
- **BREAKING: generated clients type an integer id as a number.** An
  entity that declares `{name: id, type: int}` got `ID string` in the Go
  client and `id: string` in `client.d.ts`, but the server answers a JSON
  number, so every decode failed after the row was saved. It is now
  `int` (record methods and `BatchDelete` take `int`) and `number`.
  Generated repos, typed `Exists`, cascade children and
  `On<Entity>Deleted` callbacks take an `int` id too; they took a
  string, so `repo.Get(row.ID)` did not compile.
  A `json` field was `map[string]any`, which failed the whole list when
  one row held an array; it is now `any` (`unknown` in TypeScript).
- **BREAKING: an OpenAPI CLI takes a bare origin.** `servers[0].url`'s
  path (or a relative URL such as `/api/v3`) now becomes the API prefix
  every request carries, and its origin the default server, so `--url
  http://localhost:8080` reaches `/api/v3/...`. Pass `--api-prefix` (now
  accepted with `--from-openapi`) to replace it. A parameter's
  `description` becomes its flag's help text, and an operation's
  parameter overrides a path-level one with the same `name` and `in`.
  An explicit `--from-openapi` naming the spec a settings entry names
  takes that entry's `api_prefix`, and a stored server URL that already
  ends in the prefix does not get it twice.
- **A generated CLI sends the stored token only to its own server.**
  `--url` or `<APP>_URL` naming another origin sent the token `login`
  stored to that host. The stored token now goes only to the origin it
  was stored with, and stderr says why it was withheld; `<APP>_TOKEN`
  still goes wherever the call does. A host spelled with non-ASCII
  letters gets no stored token, since case folding could map it onto
  another host; give its `xn--` form. `--cursor ""` now fetches the first
  keyset page instead of dropping the flag.
- **`service install` and `restart` wait out a slow start.** The daemon
  binds its control socket before it builds the app and answers once
  the app is ready, so on launchd and Task Scheduler the first status
  probe timed out, and an app that took more than 5 s to boot was
  reported as not coming up while it served fine. A probe that times
  out now counts as starting, up to the documented 15 s. On Windows,
  `stop` waits the same 15 s for a starting daemon to answer before
  ending its task, instead of killing it mid-migration. `service logs
  -f` follows a log that was deleted and written anew, instead of
  re-printing the deleted one.
- **BREAKING: `columns` is a reserved transition key.** A move keyed
  `columns` overwrote the JS resource's upload column map.
- **A timestamp keyset walk ends on SQLite.** A `time.Time` cursor value
  was written in a different spelling from the stored text, so on SQLite
  a walk over `created_at` could serve the same page forever. Cursor
  times are now written in the layout crud stores (UTC, six fraction
  digits), and crud stores every Timestamp it writes in that layout, a
  client's `…00Z` or `…+02:00`, an upsert's auto-generated column and
  a `time.Time` a BeforeCreate hook sets included, so a keyset on a client-written
  Timestamp no longer repeats its first page on SQLite. A Date keyset
  token carries the date, which the next request's check accepts. A
  Timestamp filter binds its value in that same layout
  (`filter.TimestampLayout`, `filter.FormatTimestamp`), so
  `?created_at=…Z` and `?created_at=…+02:00` match the row they name
  on SQLite. A filter value with no zone, and a bare date, read as UTC,
  the zone every stored Timestamp is in; on a Postgres session whose
  `TimeZone` is not UTC they used to mean session-local time.
- **BREAKING: a cursor field must not hold NULL.** `entity.Define`
  panics, and the app spec refuses, a cursor field that is not a key
  column, not `Required` and not framework-written: keyset paging skipped
  its NULL rows, and the dialects sort NULLs at opposite ends. A NULL
  that still reaches the last row of a page answers 500 instead of a
  cursor that returns empty pages.
- **Offset pages break ties on the key.** The list endpoint,
  `?stream=true`, `ListAll` and a typed query's `Offset` now order by the
  `?sort=` columns and then the primary key, so `?sort=status&page=N` no
  longer serves some rows twice and others never on Postgres. With no
  `?sort=`, an entity with timestamps lists oldest first (`created_at`,
  then the key) and one without lists in key order.
- **The same filter query builds the same SQL.** Filter keys were walked
  in map order, and `?score=5&score_gt=3` kept or dropped the equality
  depending on that order. Both predicates are now kept, and flat and
  relation filters walk keys sorted.
- **BREAKING: a filter value must be of its field's type.** `?n=abc` on
  an Int answered 500 on Postgres and an empty 200 on SQLite; every
  filter surface (flat filters, `?where=`, the DSL, `?rel.field=`,
  `?include=rel(...)`, `ListOptions.Filters` and a cursor token) now
  answers 400 naming the field and type, `LenientFilters` included. A
  Bool takes exactly what `strconv.ParseBool` accepts, so `yes`, `on`
  and `maybe` are refused. A JSON value must be JSON, and a Relation
  column holding an integer key is checked as an Int. The table of
  accepted values is in entity-declarations.md.
- **BREAKING: `_like` and DSL `contains` ignore case on both dialects.**
  They compile to `LOWER(col) LIKE`, the `?q=` shape, at every depth;
  before, Postgres matched case and SQLite did not. SQLite folds ASCII
  letters only. `contains` on a non-text field is now refused.
- **Overlapping saves under folding keys no longer overwrite each
  other.** On a case-insensitive filesystem, a save of `Report.txt`
  still copying while `report.txt` was saved passed the folded-key check
  and its rename then overwrote the other object. Both local backends
  now repeat the check at the rename, under one lock.
- **A canceled request still rolls back its uploads.** A client that
  disconnected during a create or update's hooks or insert canceled the
  context the compensating delete ran under, the local backends refuse
  a canceled Delete, and the saved files stayed with no row referencing
  them. The same held for an image whose renditions failed. Both
  cleanups now run detached from the request, bounded at 30 s.
- **Deleting a missing local upload reports `os.ErrNotExist`.** The
  scrubbed error dropped it, so a repeated `DeleteFileField` or an
  erasure whose rendition was already gone failed instead of counting
  the object as deleted.
- **A slow outbox handler's outcome is recorded.** The 10 s window for
  the settle writes opened before the handler ran, so a handler that
  took longer settled under an expired context, stayed pending and was
  delivered again until it dead-lettered. The window now opens when the
  handler returns.
- **A stale outbox settle no longer clears a newer claim.** A relay whose
  lease expired and was re-claimed still wrote its outcome, clearing the
  re-claimant's lease so a third relay could claim the delivery while
  the second ran it. Every settle now matches on the `claimed_until` its
  own claim wrote; the current claimant's outcome decides the row.
- **A cron tick fires once across replicas.** `PostgresAdvisoryLease`
  released its lock as soon as the tick's jobs finished, so a replica
  whose timer fired a moment later ran the same minute again. It now
  records the claimed minute in a `gofastr_cron_ticks` table (created on
  first use) and a replica that finds it claimed skips the tick. A
  custom lease gets the same guarantee by implementing the new
  `cron.TickLeaderElection`. The guarantee is at most once.
- **A webhook delivery past its attempt budget is not sent again.** A
  worker that died between claim and settle left the row to be
  re-claimed, and the budget was only checked after a failed send, so a
  `MaxAttempts: 1` delivery was POSTed once per crash. A claim already
  past the budget is now marked `dead` without a request. That
  dead-letter never replaces a `success` the outrun worker recorded.
- **A durable cron schedule fires each repeated wall minute once.** When
  clocks went back, `"0,30 1 * * *"` in New York fired 01:00 and 01:30
  again on the EST side, because the walk remembered only the last fire.
  A repeat is now read from the zone data, however heartbeats split the
  night.
- **`MemoryQueue` and `RedisQueue` hold a job until its `ScheduledAt`.**
  Both handed a delayed job out at once. Memory workers now sleep until
  the earliest held job is due, and `Close` drops jobs not yet due with
  a WARN naming the count. Redis keeps them on a `<queue>:delayed` list
  that `Dequeue` promotes from; it is read whole on each call, so it
  suits hundreds of delayed jobs, not millions. A due job is pushed onto
  the main list before it leaves the delayed one, so a worker that dies
  mid-move leaves a duplicate, not a lost job.
- **`CORS` answers only real preflights.** Every `OPTIONS` was treated
  as one, so behind an allow-list a plain `OPTIONS` with no `Origin` got
  a 403 and with "*" a 204, and the app's own `OPTIONS` handler never
  ran. A preflight now needs `Origin` and `Access-Control-Request-Method`.
- **`CORS` sends `Vary: Origin` on every allow-list response.** Only
  matched origins got it, so a shared cache could store the no-ACAO
  response and replay it to an allowed origin.
- **The log battery's panic recovery re-raises `http.ErrAbortHandler`.**
  It reported net/http's abort signal as a panic and wrote a 500 over a
  half-sent body; it now lets the server drop the connection, as
  `core/middleware.Recovery` does.
- **Fields whose names end in an operator suffix filter.** `?opt_in=`
  and `?members.check_in=` were cut into a field `opt` or `check` and
  refused; a suffix now counts only when the name before it is a field.
- **Long-lived streams end when their caller loses access.** The
  principal was resolved once at connect, so a revoked session or an
  expired token kept receiving until the client left. The rtc room
  socket, the SSE broker, island streams (which also re-run the topic
  gate), A2A task streams, the CRUD event stream's tenant filter and the
  harness MCP GET stream now re-check on each delivery or heartbeat and
  close when the check fails.
- **Stream writes have a write deadline.** A client that stopped reading
  parked the handler for the life of the connection on kiln's live SSE,
  dev livereload, the harness web, REST and WebSocket streams, island
  streams and the CRUD event stream. Each write now runs under a fresh
  deadline, cleared once the write is flushed. Kiln's live SSE also cuts
  an event name at its first CR, LF or NUL, so a name can no longer
  write its own `data:` line.
- **32-bit builds compile.** `GOARCH=386` failed on constants past a
  32-bit int or uint in core/i18n plural operands, core/schema and crud
  unsigned bounds, the headless counter, the entityui number-input bound
  and bench-resources. CI now cross-builds linux/386.
- **HTTP/2 streams survive an idle gap.** `stream.SSEWriter`, A2A task
  streams, the core/mcp notification stream and the harness MCP GET
  stream left each write's 10 s deadline armed. On HTTP/2 that deadline
  is a stream timer that resets the stream when it fires, write pending
  or not, so a stream quiet for 10 s died before its next heartbeat. The
  deadline is now cleared after each flush.
- **Outcomes are recorded after a shutdown starts.** The queue's ack and
  nack, the inbound webhook envelope's outcome, the migrate seed ledger
  and entityui bulk chunk results were written on the context the work
  ran under, so a shutdown landing as the work finished dropped the
  record and the work ran again. They now settle on a detached, bounded
  context. The outbound webhook's delivery state write and claim release
  were detached but unbounded, so a store blocked on a dead database
  held the worker past `Stop`; they are now bounded too.
- **The harness web fetch checks the address it connects to.** It
  checked a separate DNS lookup, so a rebinding host passed as public
  and was dialed at an internal address. The SSRF check now runs on the
  connected IP. Webhook and A2A push clients built on a clone of
  `http.DefaultTransport` now get the same dial-time guard, which used
  to skip them because the clone carries the environment proxy; the
  guarded clone drops that proxy, as `netguard.GuardedTransport` does.
  A transport with a `net.Dialer` of its own keeps it and gets the
  resolve-and-check guard instead.
- **Response wrappers expose the connection.** The cache middleware's
  recorder, the theme editor, the webmcp observer, the idempotency
  middleware and the timeout middleware gained `Unwrap`, so
  `http.ResponseController` reaches flush, hijack and deadlines through
  them. The cache and idempotency middleware no longer store a response
  that was flushed or hijacked. Tracing sets its response headers before
  the handler runs, since a later change never reached the client.
- **Owner-only directories no longer rewrite shared parents.** The log
  file sink, the desktop data dir, semantic persistence, the harness
  SQLite store and `App.ExportData`'s directory tightened an existing
  directory such as `/var/log` to 0700 after `MkdirAll`. Only a
  directory the call created is tightened now; the export directory is
  still tightened, and a symlink there is refused. A pre-existing
  semantic WAL with looser permissions is now made owner-only.
- **Huge field bounds no longer turn negative.** A String field's `Max`
  or `Min` past the int range went through `int(...)`, which is
  implementation-defined, so OpenAPI and JSON Schema published a
  negative or nonsense `maxLength`. `schema.LengthBound` now clamps it to
  [0, MaxInt32], and migrate and kiln's migrate write `TEXT` instead of a
  `VARCHAR(n)` wider than Postgres accepts (10485760). The entityui
  record form clamps a number field's bounds to ±2^53, where a huge
  `Max` had landed below `Min` and refused every value.
- **Ids are used exactly.** The RBAC admin and entityui bulk actions
  trimmed spaces from ids, so `" u1 "` edited `u1`.
- **SQLite search works on a table in an attached database.**
  `SQLiteFTSConfig{Table: "aux.docs"}` created its table but every query
  failed, because FTS5 read `"aux"."docs" MATCH` as a column. The bare
  name now sits where FTS5 takes the table, and the boot-time probes
  read the attached schema's catalog.
- **`html.Classes` renders in sorted order,** so the `class` attribute is
  the same bytes on every render.
- **`ui.CodeTabs` and `ui.Markdown` take a `Ctx`,** so their code blocks'
  accessible names, line counts and copy controls follow the request's
  language.
- **The static builder writes through `os.Root`.** A symlink planted in
  the output tree failed no check and redirected the write outside it.
- **The log webhook sink refuses redirects.** A 307 or 308 replayed the
  batch and every configured header, such as an `X-API-Key`, to whatever
  host the redirect named.
- **The harness bash tool's timeout kills the process group.** A
  backgrounded child holding the output pipe kept the call waiting for
  its whole life.
- **The audit scrub catches camelCase and kebab-case keys.** `resetToken`
  and `reset-token` were stored in the clear while `reset_token` was
  dropped. Keys now fold to snake case before the sensitive-suffix match.
- **The email dev log redacts more secrets.** A token, code, key, secret
  or password query parameter was kept when the keyword sat inside the
  name (`?auth_token_v2=`), when the `&` was HTML-escaped, or when the
  URL had no scheme. From, To, CC, Subject and headers are scrubbed of
  control bytes, and header values are redacted like the body.
- **Charts skip readings that are NaN or infinite.** `LineChart`,
  `Sparkline`, `BarChart` and `PieChart` wrote `NaN` into SVG
  coordinates, or scaled every bar to one infinite value. A non-finite
  reading is treated as one that never arrived, as `Gauge`, `Heatmap` and
  `Scatter` already did. `LineChart` labels now sit over the points of
  the longest series.
- **`TypedQuery.UpdateAll` writes a field whose column has a digit
  segment.** `MarshalEntity` spells a `Line2` field `line2`, which
  `UpdateAll` matched to no column; the `updated_at` restamp then counted
  as a write, so the call reported the rows it touched having changed
  only the timestamp. It now names columns as `CreateOne` and `UpdateOne`
  do, leaves the caller's map alone, and returns "no fields to update"
  for a body that sets nothing.
- **A grant refresh that keeps failing waits between tries and lets the
  app stop.** With the database down, a replica's refresh of a role
  another replica changed retried at once, forever, and the fanout stop
  waited on it, so shutdown hung. Each wake now reloads the roles it
  found once and leaves a failure for the next try 5 seconds later. See
  [scaling](framework/docs/content/scaling.md).
- **An update that keeps a `belongs_to` reference saves when the caller
  can no longer read its target.** Once an invoice's customer was
  archived out of the caller's read scope, the edit form, which re-sends
  every field, answered 404 on any change to the invoice until someone
  re-picked the customer. A reference the update leaves as stored, every
  column of it, is no longer checked; one that changes in any column
  still is. See [security](framework/docs/content/security.md).
- **A SQLite restore keeps timestamps filterable.** The export wrote every
  time the driver parsed in RFC 3339 with trailing zeros trimmed, so
  crud's `...05.120000Z` came back `...05.12Z` and a date came back
  `...T00:00:00Z`. SQLite compares the text, so after a restore an
  equality filter missed those rows and same-second rows reordered under
  a cursor. A battery's `DATETIME` changed spelling too. The export now
  reads a SQLite time column as stored and writes a Postgres instant in
  the layout crud uses; an import rewrites an older archive's declared
  `Timestamp` and `Date` columns in it. See
  [data export](framework/docs/content/data-export.md).
- **A blank Bool on a multipart form is not false.** The multipart parse
  read an empty value as false, so saving a record through a form with an
  upload field wrote false into every unset optional Bool (the entity form
  draws a stored NULL as a select whose empty choice submits ""), and a
  required Bool left blank passed. A blank now means "not provided", as on
  the JSON path: an update stores NULL, a create applies the default, and a
  required field answers 400. See [uploads](framework/docs/content/uploads.md).
- **A re-granted wildcard holds on every replica.** `Revoke("teams:*")`
  tombstones the wildcard and its expansion, and `Grant("teams:*")` lifted
  only the expansion, so peers and restarts expanded the surviving
  `teams:*` tombstone and denied what the origin allowed. Grant now lifts
  the same tombstones Revoke writes. A grant inside a wildcard revocation
  it does not lift (`Grant("teams:read")` after that revoke) returns
  `access.ErrRevokedByWildcard` and writes nothing, where it used to hold
  on the origin until its next reload. `GrantStore.CheckGrant` runs the
  same checks without writing; the admin RBAC page uses it and answers
  409. See [access control](framework/docs/content/access-control.md).
- **A cascade child's keys are named like its parent's.** In process, a
  nested child built with `MarshalEntity` lost every field whose derived
  key is not its column (`line2` for `line_2`), since the children were
  read by the HTTP rule while the parent was not. Over HTTP, two child keys
  naming one column (`bodyText` and `body_text`) were settled by map order;
  they now answer 400 `validation.keys_fold`, as the parent's keys and
  batch items do. See
  [entity declarations](framework/docs/content/entity-declarations.md).
- The experimental harness web client's `Server.Stop` closes an open
  `/events` stream once its context ends. `http.Server.Shutdown` never
  closes an active connection, so Stop returned at the deadline and left
  the stream's handler, seat and bus subscription running. Stop still
  returns the context's error when it had to close connections.
- A kiln journal append that fails part way (a full disk, a failed
  fsync) no longer corrupts the journal. The fragment stayed in the
  file, the next append completed it into a line of invalid JSON, and
  the next boot refused to replay the session. The failed append's bytes
  are now cut back off; if that fails too, appends are refused until
  restart. See [kiln](framework/docs/content/kiln.md).
- `world_get` with path `_plans` returns a copy of the plans. It
  returned the session's own map, encoded after the read lock was
  released, so a concurrent `propose_plan` or `approve_plan` could crash
  kiln with a concurrent map write.
- `gofastr migrate force` takes exactly one version, parsed whole.
  `force 3x` read 3 and marked migration 3 applied without running its
  SQL, and with two versions the last one won. See
  [migrations](framework/docs/content/migrations.md).
- **BREAKING: `gofastr sdk` refuses an entity whose `client.d.ts` type
  name is already declared.** The name can be taken by the runtime
  (`Resource`, `Client`, `CursorPage`, and the other runtime types) or
  by another entity (`order_input` beside `order`'s `OrderInput`). The
  second declaration used to ship beside the first, so the client failed
  to compile or TypeScript merged the two interfaces. Rename the entity.
  See [SDK](framework/docs/content/sdk.md).
- `App.Shutdown` tears down once. A second call found the server
  already detached and stopped every battery again; called concurrently
  (a signal handler plus a deferred Shutdown), it closed batteries under
  requests the first call was still draining. A later or concurrent call
  now waits for the first to finish, or for its own context to end. See
  [deploy](framework/docs/content/deploy.md).
- `App.Shutdown` no longer restarts the process modules it drains. The
  supervise loops kept running and read each drained exit as a crash,
  so they started a new child during or after shutdown. The drain now
  closes the supervisor first, and runs before batteries stop rather than
  after, so a child's in-flight reverse calls still reach them. A child
  still starting when the supervisor closes is killed instead of
  installed. See
  [process modules](framework/docs/content/process-modules.md).
- An entity, view or routine declared from a module's or battery's
  `Init` is migrated and seeded. Start ran its only migrate-and-seed pass
  before `Init`, so the table never existed: Start succeeded and the
  module failed at its first query. Start now migrates and seeds again
  once `Init` has declared something new. A host binary's `migrate
  generate` verb calls `app.InitPlugins()` before `app.MigrationPlan()`
  to include them. See [modules](framework/docs/content/modules.md).
- A process module that stops reading stdin no longer hangs shutdown,
  or a disable or upgrade drain. The drain wrote `module.drain` before
  it closed stdin or started the kill timer, and that write blocked on a
  full pipe whatever its context said; a stuck runtime drain kept the
  module in Draining for good. The budget now runs from the drain call
  and the kill ends the write. The call waits at most half the budget
  for an answer, so a child that never answers still has time to exit
  at EOF. `moduleproto.Peer.Call` also returns once its context ends,
  the peer closes or the peer faults, even while its request write is
  blocked; the write keeps the call's inflight slot until it returns.
  See
  [process modules](framework/docs/content/process-modules.md).
- A process module whose host peer faulted now restarts. A malformed,
  over-cap or unsolicited frame left the child running in `Ready` while
  every call to it failed, because nothing watched the peer's fault. The
  supervisor now kills the child on a fault and counts it as a crash, so
  the module backs off, restarts, and opens its circuit if it keeps
  faulting. See
  [process modules](framework/docs/content/process-modules.md).
- **Signal controls work inside a modal, drawer or popover.** The
  runtime's signal delegate skipped widget roots, and the widget's own
  listener handles only `data-cui-rpc` and close, so
  `data-cui-signal-set`, `-inc` and `-toggle` did nothing there:
  `ui.Tabs` in a drawer never switched and a `headless.Button` with a
  signal action was dead. On a node that also carries `data-cui-rpc`,
  the signal mutation wins inside a widget as it does outside one. See
  [reactivity](framework/docs/content/reactivity.md).
- **A superseded navigation no longer destroys a loading region's
  original content.** When a second click replaced a navigation whose
  `data-cui-loading` content was showing, the first navigation dropped
  the hidden nodes it had parked, so a second navigation that then failed
  or was aborted left the region on the skeleton, and its input values,
  listeners and island state were gone. The newer navigation now takes
  the parked nodes over, and a region no apply replaced gets them back.
  See [layouts](framework/docs/content/layouts.md).
- **A second click no longer clears the first click's busy marks off
  the regions both navigations change.** The superseded navigation's
  cleanup removed `aria-busy` from every region it had marked, so the
  outlets and slot the winner had marked read as idle while its request
  was still in flight. Each mark now records the navigation that set it,
  and only that navigation clears it.
- **A state field with no `Default` starts at its first `Initial` value.**
  A create or upsert that left the state out stored NULL, a state no move
  starts from, so the record could never move, and a Required state field
  failed every entityui and admin create, since the form never sends the
  state. Define now sets the field's `Default` to that value. See
  [states](framework/docs/content/states.md).
- **An in-process include filter is read as written.** `TypedQuery.Include`,
  `GetOne` and `ListAll` pasted the include names into a URL unescaped, so a
  filter value with a space (`comments(body=hello world)`) panicked, a `+`
  matched a space and a `#` cut the rest of the filter off.
- **A redelivered inbound webhook job for a processed envelope does not
  run the handler again.** A crash before the queue's ack, or a lease
  expiry under a slow first run, redelivered the job, and `ProcessInbound`
  called the business function a second time. BREAKING:
  `MarkEnvelopeProcessing` on both bundled inbound stores returns
  `(bool, error)`, false when it marked nothing. See
  [webhooks](framework/docs/content/webhooks.md).
- **A DI constructor can resolve the services it needs.** The container
  held its lock while a constructor ran, so a factory that called
  `Resolve` or `Inject` blocked every container call for good. A
  constructor now runs outside the lock, still once per type: a second
  caller waits for the first and shares its value, and a constructor
  that panics releases the waiters with an error. A constructor that
  needs its own type still waits on itself.
- **The documented notify wiring sends without an error.** The examples
  in the notifications guide, the package doc and the agent recipe set a
  template for one channel while registering another, so every `Send`
  returned `no template for ("order.shipped", "log")`, after the email
  had already gone out. Each now sets a template per channel, and the
  guide says a `Send` error can follow other channels' delivery. See
  [notifications](framework/docs/content/notifications.md).
- **A static export keeps the app's `_headers` and `_redirects`.** The
  build refused a static-dir (or static FS) `_headers`, and a
  `_redirects` when the app had redirects, as if each were a rendered
  page, so the app's caching headers and legacy redirects never reached
  the host. The app's file is now appended after the generated rules,
  which stay first. A `Builder.ExtraDirs` copy is still refused. See
  [static export](framework/docs/content/static-export.md).
- **BREAKING: a process module's artifact upgrade waits for its own
  migrations.** The upgrade kept the previous artifact's
  `MigrationsAppliedAt`, so a new version that added tables reached Ready
  against the old schema. A new artifact digest now clears the stamp;
  the supervisor drains the module and holds it in `InstalledDisabled`
  until the migration coordinator runs for the new artifact, and `Apply`
  refuses to stamp a row that has moved to another artifact. Run the
  coordinator as part of every module deploy. See
  [process modules](framework/docs/content/process-modules.md).
- **A panicking presence hook no longer ends the process.** The
  heartbeat's expiry sweep called the `SetOnPresenceChange` callback
  with no recover, so an app hook that panicked on a departed remote
  roster crashed the replica. The remote-roster merge and the fanout
  `stop` called it bare too, so one panic skipped the topics after it.
  Every path now recovers and logs per topic, as the local join and
  leave paths already did. `recovercallback` now follows a callback
  read through a package-local accessor, the spelling that hid the
  heartbeat site. See [presence](framework/docs/content/presence.md).
- **`ExportData` streams each table instead of holding it in memory.**
  The export collected every page of a table into one slice, then built
  the whole `.ndjson` file in a buffer before writing it, so
  `WithExportPageSize` bounded nothing and a large table could exhaust
  memory. Each page now reaches the file, hashed on the way, before the
  next is read. The archive format is unchanged. See
  [data export](framework/docs/content/data-export.md).
- **Back and Forward through a layout with outlets restore the page's
  own signals.** The envelope navigator seeded its cache entries with
  the `route.*` keys alone, so Back to the first page kept the quantity
  the page you left had set, a page re-captured on leave lost the seed
  its own answer carried, and a fetched whole document (a cross-chain
  swap or a static host's answer) applied its route keys at most.
  It now reads the `#gofastr-signals-page` names as the plain navigator
  does, and a re-captured page keeps the seed it arrived with. Globals
  keep the value the user gave them. See
  [signal store](framework/docs/content/signal-store.md).
- **A navigation answered 200 with a body that is not HTML no longer
  blanks the page.** The navigators refused a non-HTML body only on an
  error status, so a 200 `text/markdown` or JSON answer swapped an
  empty `<main>` in, or left the address bar on the link. Any answer
  not typed `text/html` now toasts naming its status, the URL goes back
  and the page stays, an untyped 200 included. On the site, the docs
  pages' links to sibling pages (`a2a.md` → `/docs/a2a`) and to repo
  files (`../../../battery/admin/` → the GitHub source) are rewritten,
  so they no longer land on the raw-markdown route or a 404. See
  [core-ui/ARCHITECTURE.md](core-ui/ARCHITECTURE.md).
- **Back to a visit's first page keeps a shared nested layout.** On a
  page with no outlets, `/group/a` → `/group/b` → Back replayed the
  first page's cache entry, keyed at `<main>`, over the group layout:
  its DOM was rebuilt and lost its listeners and typed input. The
  replay now applies only the entry's cell for the deepest layer both
  pages share, with the entry's page metadata and page signals. See
  [core-ui/ARCHITECTURE.md](core-ui/ARCHITECTURE.md).
- **Email headers that are not ASCII are encoded.** The SMTP sender
  wrote a localized `Subject` (`¡Confirma tu correo!`), a display name
  in `From`, `To` or `Cc`, and an attachment filename as raw UTF-8,
  which a relay without SMTPUTF8 may reject or mangle. They now go out
  as RFC 2047 encoded words and RFC 2231 `name*=` / `filename*=`
  parameters; ASCII values keep their bytes. See
  [email](framework/docs/content/email.md).
- **`Unique` set or cleared on an existing column reaches the schema.**
  Only `CREATE TABLE` and `ADD COLUMN` wrote a field's uniqueness, so a
  column made `Unique` later kept accepting duplicates, and clearing it
  left the constraint. Boot now creates `<table>_<column>_key` when no
  single-column unique index covers the column; rows that already repeat
  a value fail the boot with an error naming the column. `migrate
  generate` records each table's unique columns in the snapshot
  (`unique_columns`) and creates or drops the index; on SQLite a clear
  rebuilds the table. See
  [migrations](framework/docs/content/migrations.md).
- **`scripts/pr-review-findings.sh --gate` reads every review thread.**
  Its GraphQL query fetched the first 100 threads with no cursor, so an
  open thread past them passed the gate. It now pages through all of
  them and counts one line per open thread.
- **Writing a table's default schema into `Table` no longer drops it.**
  After `widgets` became `main.widgets` (SQLite) or `public.widgets`
  (Postgres), `migrate generate` read the table as new and the recorded
  one as removed, and wrote a `CREATE TABLE IF NOT EXISTS` (a no-op)
  followed by `DROP TABLE widgets`, which took the rows. The two
  spellings are now one table, and the change generates nothing. See
  [migrations](framework/docs/content/migrations.md).
- **`migrate generate` finds the children of a schema-qualified SQLite
  table.** A SQLite foreign key names its parent without a schema, and
  generate compared that bare name with the parent's qualified `Table`,
  so it saw no child. Dropping `main.ap` and its child together dropped
  the parent first, and the migration failed with `FOREIGN KEY
  constraint failed` while the child held rows. The rebuild guards that
  read the same references missed them too, and a bare reference in
  `aux.b` was read as one onto `main.a`. A bare parent now resolves into
  the child's schema.
- **A generated app stops when auth fails to start.** The app generator
  emitted `authMgr.Init(fwApp)` and dropped its error, so a production
  app with `JWT_SECRET` unset served its screens with no `/auth/login`.
  The generated `app.go` now exits with Init's error. Regenerate, or
  wrap the call yourself in an app you own. See
  [app specs](framework/docs/content/app-spec.md).
- **A generated `entity_form` posts to the entity's table and offers its
  related records by key.** The form submitted to `<api_prefix>/<entity>`
  while the routes mount at the table, so an entity with `table:` set
  answered 404, and the generated e2e test called the same wrong path.
  A relation select fetched its target by entity name and gave each
  option the row's `id`; a target with `primary_key: [currency_code]`
  has no `id`, so every option posted `undefined`. The select now reads
  the target's key column from its table's route. Regenerate to pick it
  up. See [app specs](framework/docs/content/app-spec.md).
- **A generated app's Sign out, login and signup forms follow
  `app.auth.base_path`.** The battery mounted its routes at `base_path`,
  but the generated Sign out button posted to `/auth/logout`, so signing
  out failed and the session stayed. The `login_form` and `signup_form`
  defaults and the generated e2e test's login named `/auth` too. They now
  post under `base_path`. BREAKING for app specs: `base_path` must be an
  absolute path with no trailing slash, such as `/account`, and
  `gofastr generate` refuses `/auth/`, `auth`, `//host`, `/a//b` and
  `/..`. See [app specs](framework/docs/content/app-spec.md).
- **A strict OpenAPI validator accepts list responses.** The list
  route's 200 is `oneOf` `ListResponse` and `CursorPage`, which had no
  required keys, so every list body matched both and `oneOf` refused it.
  `ListResponse` now requires `data`, `total`, `page`, `perPage` and
  `totalPages`, and `CursorPage` requires `data`, `cursor` and
  `hasMore`. The generated `client.d.ts` typed `CursorPage.total` as
  always present; cursor mode never sends it, so it is optional now. With
  `strictNullChecks`, code that read it as a number now gets a type
  error instead of `undefined` at run time.
  See [cursor pagination](framework/docs/content/cursor-pagination.md).
- **The OpenAPI spec lets an optional field be `null`.** The route takes
  `null` for an optional field and stores it, so reads return it, but
  the spec typed the field `string` (or `integer`, and so on), and a
  strict validator refused the write and every read of the row after
  it. An optional field's type is now `[T, "null"]` and its enum lists
  `null`; a required field, a generated value and a key column stay
  non-null. The create schema's state field under enforced states stays
  non-null, as the route refuses a null state there; the MCP create
  tool offered one and no longer does. `gofastr generate cli
  --from-openapi` reads a `[T, "null"]` type as `T`; it made such a
  field a string flag. See [core packages](framework/docs/content/core-packages.md).
- **`migrate down` refuses a migration that has no Down.** It ran the
  empty Down, which succeeds, and deleted the tracking row, so the
  version read as pending while its Up's changes stayed and the next
  `up` ran it again. `Down` now returns `ErrNoDown` naming the version
  before it rolls back anything. See [migrations](framework/docs/content/migrations.md).
- **A SQLite table rebuild keeps the table's triggers.**
  `ApplySchemaDiffWithOptions` rebuilds a SQLite table for a retype or a
  key change, and boot does for a key change. The `DROP TABLE` took the
  table's triggers with it: the live apply put none back, and boot put
  back only the declared routines, so an audit trigger stopped firing
  with no error. Both now recreate the table's triggers right after the
  rebuild, in the order they were made, with any column rename earlier
  in the change set kept in their bodies. See
  [migrations](framework/docs/content/migrations.md).
- **A refreshed or shared pane link opens the pane with its content.**
  Back and Forward re-clicked the keyed trigger, so its RPC filled the
  pane, but a page load only opened the pane on its generic content. A
  load now replays the trigger the same way, without a history entry.
  Both match the trigger by key and pane: one key could label a
  record's view and its edit trigger, and the first one in the page
  opened. See [pane host](framework/docs/content/pane-host.md).
- **A cancelled harness turn no longer cancels every turn after it.**
  `CancelTurn` cancelled the engine's root context, which every turn
  starts from, so after one cancel (even one sent while idle) each new
  turn began already cancelled. It now ends the running turn alone. A
  turn cancelled mid-stream or while connecting ends with reason
  `cancelled`, not `error`. See
  [harness architecture](framework/docs/content/harness-architecture.md).
- **A harness session export holds every event.** The export read the
  log in one `EventsSince` call, which the SQLite store caps at 10,000
  rows, so a longer session exported a bundle missing its tail with no
  error. It now pages through the log until a page comes back empty.
- **A heavy-JS plugin mounts after a client-side navigation.** The
  plugin host broker scanned for `data-fui-plugin` markers only at boot
  and tore down every instance on `gofastr:navigate`, so a page reached
  by navigation or an island swap showed the fallback forever, and a
  plugin in a retained layout region was destroyed. The broker now
  mounts markers as they arrive and tears down only the instances whose
  marker left the DOM.
- **A `ShowWhen` field draws on an entity with no States.** The record
  form compared the condition's field with the States field before
  checking the entity had States, so any `ShowWhen` on such an entity
  panicked and the create and edit forms showed "Couldn't load this
  section".
- **A create form sets a required key the caller assigns.** entityui
  and the admin treated every one-column primary key as server-owned and
  left it off the create form, so an entity keyed on a required `code`
  with no generator failed validation on every create. A `Required`
  one-column key with no `AutoGenerate` now draws as an input on create,
  read-only after, and blank on a Duplicate, the way a composite key's
  columns do. See [entityui](framework/docs/content/entityui.md).
- **Removing a filter chip keeps the other terms' meaning.** The list
  rebuilt the filter from the kept chips without parentheses, so
  removing `active = true` from `(status = "open" or status = "paid")
  and amount > 100 and active = true` left `status = "open" or
  status = "paid" and amount > 100`, which matches every open row, and
  "Every match" bulk actions acted on that wider set. A top-level `or`
  also split into chips that were rejoined with `and`. Groups now keep
  their parentheses in chips, in the query box and in nested terms,
  and an `or` filter is one chip.
- **Widgets open and poll on embedded screens.** The embed middleware
  listed `/__gofastr/widget` as a runtime path, but widgets serve their
  style, state and chrome from `/core-ui/widget/<name>/`, so it answered
  403 to every one of them before the widget's own gate ran. Those three
  routes now pass for a widget offered on the grant's surface page and
  stay refused for a widget scoped elsewhere, whatever session gate it
  carries. New `(*embed.Host).SetWidgetReach` takes the check;
  framework/uihost installs it from the widget registry.
- **A custom 404 screen built on `component.ContextOnly` renders its
  body.** The `WithNotFoundScreen` adapter called `Render`, which
  `ContextOnly` answers with "", so the page body was empty; it now
  dispatches `RenderNotFound`, then `RenderCtx`, then `Render`. An app
  with no root layout ignored the configured screen and served the
  built-in body; it now renders the screen in the bare `<main>`, falling
  back to the built-in body when the screen panics.
- **macOS deep links reach the app.** The darwin shell registered its
  GetURL handler for the Apple Event code `'GURT'` instead of `'GURL'`,
  so a URL on the app's scheme opened from a browser or `open` never
  reached `Config.DeepLink`.
- **The `window` capability acts on the calling window.** `title`,
  `setTitle` and `snapshot` always used the main window, so a Settings
  page that mirrors `document.title` through `setTitle` renamed the main
  window, and a secondary window's `snapshot` returned the main window's
  pixels. A call from a window that has closed now answers `not_found`.
- **A file picker keeps the user's choice however long they take.**
  The macOS open, save and folder panels gave up after 10 seconds while
  still on screen, and on every platform the app's 30-second request
  timeout and 60-second write timeout cut a bridge call short, so a
  path picked after that was never granted or never reached the page.
  The call route now has an 11-minute budget, and the macOS panels wait
  10 minutes like the permission alert.
- **BREAKING: pgvector replicas share one keyword leg.** `Open` filled a
  `MemoryKeyword` from the table once, so a document written through one
  replica never reached another replica's hybrid keyword leg until that
  replica restarted. `PgVectorStore.Keyword()` answers the keyword leg
  with Postgres full-text search over the same table, and `Open` refuses
  `MemoryKeyword` with a `PgVectorStore`. Replace
  `Keyword: semantic.NewMemoryKeyword()` with `Keyword: store.Keyword()`;
  [semantic-search.md](framework/docs/content/semantic-search.md) has the
  matching rules and the GIN index for a large table.
- **BREAKING: a pgvector table refuses a second embedding model.** A
  pgvector column fixes the dimension only, so an index opened with a new
  model of the same dimension ranked old and new vectors as one space;
  the file-backed store already refused that. `EnsureSchema` now creates
  `semantic_models`, `Open` records the table's model and dimension and
  returns `*ModelMismatchError` for another while the table holds rows,
  and each write checks the record in its own transaction. An emptied
  table takes the new model. `Open` before `EnsureSchema` fails with an
  error naming it.
- **`webhook.Bridge` queues one delivery per event across replicas.** With
  a fanout attached, every replica re-emits the events of the others, and
  each replica's bridge queued its own delivery under a fresh id, so an
  endpoint behind N replicas received N copies of each change. The bridge
  now skips events that arrived from another replica (`event.IsRemote`);
  the emitting replica queues them into the shared store.
- **SMTP login works against relays without CRAM-MD5.** `SMTPSender`
  tried CRAM-MD5 first and fell back to PLAIN on the same connection, but
  `net/smtp` closes the connection when a relay refuses a mechanism, so
  relays offering only PLAIN and LOGIN (SES, Gmail) never authenticated
  and every send failed. The sender now picks one advertised mechanism
  (CRAM-MD5, then PLAIN, then LOGIN, which Office 365 offers alone) and
  makes one attempt. LOGIN runs only over TLS or to localhost, as PLAIN
  does.
- **A node-tree `textarea` shows its value.** `core-ui/noderender` built
  the element with no content and passed `value` through as an attribute,
  which a textarea ignores, so an edit form drawn from a node tree (Kiln,
  an app spec's raw `textarea` block) showed an empty box and saving it
  blanked the field. `value` (or `text`, then `content`) is now the
  element's text.
- **`kiln agent` exits cleanly when its serve fails to start.** A serve that
  died before answering (an unopenable journal, a bad flag) made the launcher
  sit until the 10-second readiness poll ran out and then abort with Go's
  "all goroutines are asleep - deadlock!" and a goroutine dump. It now prints
  the serve's error and exits 1 at once.
- **A Kiln seed row without an id gets one.** `add_seed` inserted only the
  row's own keys, so a row with no `id` stored a NULL primary key (SQLite
  allows NULL in a `TEXT PRIMARY KEY`) and the row could not be fetched,
  edited, deleted or referenced; `created_at` and `updated_at` stayed NULL
  too. Seeds now fill an omitted or null UUID id and timestamps the way a
  CRUD create does, and keep any value the row gives.
- **Kiln's `reset_session` and `undo` drop tables linked by foreign keys.**
  The rebuild dropped tables in name order with foreign keys on, so a parent
  table with linked rows (`authors` under `books`) refused the drop; the
  failure was logged, the reset answered OK, and re-adding the entities
  served the old rows. The drops now run with foreign keys off on one
  connection that is set back before it returns to the pool, and a rebuild
  that still fails is reported by `reset_session`, `undo` and boot.
- **Kiln's ACP agent remembers earlier prompts in a session.** Each prompt
  sent the provider only its own text, so a follow-up such as "now add a
  title field" reached a model that had never seen the request before it.
  A session now keeps its conversation, tool calls and results included,
  and runs one prompt at a time even when a second connection loads it. A
  tool call that a cancelled prompt never ran gets a `cancelled` result,
  so the next request never carries a call without its result.
- **Kiln's `delete_field` and `update_entity` change the live table.**
  Kiln migrated additively, so a deleted required field kept its
  `NOT NULL` column and every later create failed. A dropped field's
  column now leaves the table. A field that is made optional or no longer
  unique, or a column SQLite cannot drop in place, rebuilds the table
  with its rows. Rows the rebuilt table refuses refuse the edit and
  leave the table as it was.
- **A Redis worker that dies inside `Dequeue` no longer loses the job,
  with a client that implements the new `queue.PopRecorder`.** `Dequeue`
  popped the job and wrote the processing entry in two round trips, and a
  worker killed between them left the job on no list. `RPopHSet` (a
  four-line Lua script, in [queue](framework/docs/content/queue.md#optional-poprecorder))
  records the job as it pops it, and `Reclaim` puts back a record older
  than the visibility timeout. A client without it behaves as before.
- **A cron schedule that never fires no longer enqueues on every tick.**
  For a spec like `0 0 30 2 *` (February 30), the next run was the zero
  time, which is always due. `Register` on both schedulers now refuses a
  spec with no firing in the next five years, and an in-memory schedule
  whose next firing lies past that search stops and logs a warning.
- **An event-bus unsubscribe releases the handler, and cron catch-up holds
  only the ticks it keeps.** Cancelling a `Subscribe` left the handler in
  the slice's backing array and kept an emptied event type in the map, so
  a long-lived bus kept every cancelled SSE closure alive. A durable cron
  schedule waking after an outage collected every missed tick before
  trimming to `MaxCatchUpOccurrences`; it now keeps a ring of that size.
- **Sent email carries `Date` and `Message-ID`, and the dev log scrubs
  every line.** `SMTPSender` wrote neither header, which direct delivery,
  threading and dedupe need. Each message now gets a `Date` and a random
  `Message-ID` on the sender's domain; a `Headers` entry of either name
  replaces it. `LogSender` printed bodies and attachment names raw, so an
  escape sequence in a template reached the terminal.
- **Removing a routine with no `Down` is refused instead of writing an
  empty migration.** `GeneratePlan` wrote a migration of only `;` and
  advanced the snapshot, while the routine stayed in the database. It
  now returns an error naming the routine. Declaring it again with a
  `Down` and generating records the drop, and the removal then carries
  it.
- **A view on a SQLite table no longer fails boot's key-change rebuild.**
  The rebuild's final `RENAME` re-parsed every view and failed with
  `error in view …: no such table`. Boot now runs it under
  `PRAGMA legacy_alter_table`, as the live apply and generated files do,
  and turns the pragma back off afterwards.
- **Declared renames run in order of the old name.** Map order picked
  the order of the `RENAME COLUMN` statements and of an app spec's rename
  errors, so the same declaration could generate a different migration or
  error text on each run.
- **Two versions booting at once no longer leave a table without the
  newer one's columns.** Auto-migrate reads the live schema before it
  takes the migrate lock. When both boots saw a table missing and the
  older version created it first, the newer boot's `CREATE TABLE IF NOT
  EXISTS` did nothing and its new columns never landed. A table read as
  missing is now read again under the lock and converged like any
  existing table.
- **A response JSON cannot encode is an error, not an empty success.**
  `handler.Respond` wrote `200` and then dropped the encode error, so a
  NaN or a channel in a handler's result answered an empty 200. It now
  encodes first and answers the generic 500. An MCP tool whose result,
  or whose `structuredContent`, cannot be encoded gets the "internal tool
  error" reply instead of a successful call with empty text.
- **MCP echoes a numeric id past float64's range.** An id such as `1e400`
  failed the strict decode before the id was rebuilt from its literal, so
  the client got an invalid-request reply with a null id. The envelope now
  decodes numbers as their literal from the start.
- **A 405 `Allow` header names `HEAD` on a GET route.** The mux answers
  HEAD on every GET pattern, but `Allow` listed GET alone. A gated GET
  drops its HEAD with it.
- **Two image variants that name the same file are refused.** Variants
  with the same `Suffix` (or a `Suffix` equal to another's width) and the
  same `Format` saved over each other while the variants column listed
  both, so a srcset served the wrong bytes. `VariantSet.Process`,
  `ProcessTo` and `imagefield.New` now return an error naming both
  indices, and `image.CheckVariantNames` runs the same check.
- **A resize target past `MaxPixels` is an error, not a runtime panic.**
  `Resize`, `Placeholder` and `NewGradient` allocated whatever size they
  were asked for, and the aspect arithmetic could overflow. A target
  whose area exceeds the image's `MaxPixels` (`DefaultMaxPixels` for
  `FromImage`) now returns `image.ErrTargetTooLarge` from
  `VariantSet.Process`, `ProcessTo`, `Placeholder` and `NewGradient`.
  `Resize` panics with an error wrapping it. An image decoded with a
  small `MaxPixels` can no longer be upscaled past that cap.
- **A lazy `Tree` branch set `Expanded` starts collapsed.** It rendered
  an open, empty group that nothing loaded until the reader closed and
  reopened it. With no `Children`, `Expanded` is now ignored, so the
  first toggle opens the group and loads it.
- **An app installed from a locale page launches in that locale.** Every
  page linked one `/manifest.webmanifest` whose `start_url` had no
  locale prefix, so a reader who installed from `/es/` landed on `/`. A
  locale page now links `/es/manifest.webmanifest`, whose `start_url`
  carries the prefix; `id` and `scope` are unchanged, so it is still one
  app. The worker precaches each locale's manifest and the static export
  writes one per locale tree. BREAKING: `UIHost.PWAManifestJSON` takes a
  `context.Context` first.
- **A computed-signal cycle no longer freezes the page.** With `a`
  reading `b` and `b` reading `a`, every write ping-ponged until the
  stack overflowed, re-running each reducer over a thousand times.
  `store.Computed` now panics on a declaration that closes a cycle and
  names the path, and the runtime never re-runs a node inside its own
  recompute, which stops a cycle written by hand in the attributes.
- **SMTP bodies go out quoted-printable.** The text and HTML bodies were
  written as given, so a template saved with Unix line endings put bare
  LFs under CRLF headers and a minified one-line HTML body passed SMTP's
  998-octet line limit. Each body part now carries
  `Content-Transfer-Encoding: quoted-printable`, with every line ending
  made CRLF, and a single-part message declares `MIME-Version`. The
  boundary check reads the encoded body and refuses a line that starts
  with a delimiter. `Email.Headers` can no longer set `Content-Type`,
  `Content-Transfer-Encoding` or `MIME-Version` (the send fails with
  `ErrSendFailed`), and notify's `Extra["headers"]` refuses
  `Content-Transfer-Encoding` as it already refused the other two.
- **The batch docs no longer promise event delivery in input order.**
  A `_batch` emits its lifecycle events in input order, but `EmitAsync`
  starts every subscriber on a new goroutine per event, so a subscriber
  can receive them in any order. `hooks-and-transactions.md`, `events.md`
  and the `BatchCreateMany` doc comment now say so.
- **A generated `e2e_test.go` fails when sign-in leaves no session.** It
  signed in as the seeded admin and then accepted any 200 from each gated
  screen, following redirects, so with no session the login page passed
  for every screen. The signed-in client now stops at a redirect to the
  login screen's route and the screen check reports the 303. Meridian's
  own e2e test had the same gap and gets the same client.
- **A widget RPC declared `PATCH` is registered as `PATCH`.** BREAKING.
  `widget.Mount` had no `PATCH` case and registered every method it did
  not know as `POST`, so the runtime's `PATCH` got 405 and a `POST`
  reached the handler. `Mount` now registers `PATCH`, and panics before
  registering any route on a method other than `GET`, `POST`, `PUT`,
  `PATCH`, `DELETE` or `""` (which still means `POST`), lower-case
  spellings included.
- **A widget mounted on a router group publishes the URLs it is served
  at.** The `/__gofastr/widgets` catalog listed the declared style,
  state and chrome paths without the group prefix, so a widget mounted
  on `root.Group("/v2")` was fetched at URLs with no route. The catalog
  now publishes the prefixed URLs, which `Definition.StyleURL`,
  `StateURL` and `ChromeURL` also return. `gofastr static` writes each
  widget's files at those URLs, and prefixes them for a base path
  whatever they are, a custom `StylePath` outside `/core-ui/widget/`
  included.
- **An embed grant reaches a `RequireSession` widget's RPCs on its own
  surface.** The session check uihost installs read the widget's name out
  of a `/core-ui/widget/<name>/` URL. An RPC path (or a custom or
  group-prefixed state path) names no widget, so a grant was refused there
  even with the path in the surface's `Reach`. Each widget gate now hands
  the check its own definition, which `widget.GatedWidget(r)` returns, and
  the grant passes when that widget is offered on the grant's surface, the
  rule its state and chrome already followed.
- **`cmd/affected` selects what still imports a package the change
  deleted.** A package whose last file was deleted is no longer in the
  `go list` set, so nothing owned the change and a test still importing
  the package was not selected. Each changed file's directory now counts
  by its import path, which the remaining importers still list.
- **A failed navigation goes back to the page still shown.** Starting a
  navigation while another was loading, then having the second one
  fail, left the address bar and the router on the first one's
  destination, a page that never rendered. Both navigators now roll
  back to the page the DOM shows.
- **Back to the first page of a visit shows that page.** When the first
  page had no outlet and a later navigation brought one in, the module
  that caches outlet pages loaded one navigation late and filed the page
  then on screen under the first page's URL, so Back to the first page
  replayed the wrong one. It now caches nothing until its own first
  navigation answers.
- **A macOS window snapshot after a cancelled one shows the page now.**
  A snapshot whose context was cancelled, or that timed out, still
  finished later, and the next snapshot of any window returned that
  stale image. Every snapshot after it then ran one capture behind. The
  next snapshot now waits for the abandoned capture to finish and
  discards it.
- **Replicas naming one pgvector table two ways share its document
  locks.** The per-document replace lock hashed the configured table
  name, so a replica set to `embed_chunks` and one set to
  `public.embed_chunks` took different locks, and two replaces of one
  document could keep chunks of both versions. The lock now hashes the
  table's OID, resolved in each replace's transaction.
- **A generated migration that changes a view and drops a column the old
  view reads applies.** `migrate generate` replaced a changed view only
  after the table changes, so the column drop ran while the old view
  still read it, and both Postgres and SQLite refused it. A changed view
  and the declared views that read it are now dropped before the table
  changes and created again after them. On Postgres a changed view is no
  longer replaced in place: grants on it go with the drop, and a view
  outside the plan that reads it makes the drop fail.
- **A toast swapped in by an island no longer stops the stack's other
  toasts from closing.** The arm pass ran on the swapped region alone and
  cancelled the timer of every toast outside it, so the stack's toasts
  stayed up for good. A timer is now dropped only when its row has left
  the page, and a row that replaces one under the same id arms its own
  timer.
- **Two list rows with the same title open their own row menus.** A row
  menu's id was a hash of its label ("Actions for {{title}}") and items,
  so two rows titled alike shared trigger and panel ids, and the second
  row's trigger opened the first row's menu. Each row menu is now keyed
  by its row, the way its copy-link span already was.
- **A number field's − and + step their own input when another field on
  the page shares its name.** The input's id defaults to the field name,
  and the buttons found it with a page-wide `getElementById`, so the
  second of two forms with a "replicas" field stepped the first form's
  input. The buttons now look for the input in their own row.
- **A multiselect chip's × unchecks its own option when another
  multiselect on the page is for the same field.** Option ids derive from
  the field name by default, and the × found its checkbox page-wide, so
  removing a chip in an advanced filter form unchecked the quick filter's
  option while the form still submitted the value. The × now looks inside
  its own multiselect.
- **A disabled tab can no longer be chosen with the mouse.**
  `headless.Tab{Disabled: true}` kept the tab out of the arrow keys but
  still wrote its signal and moved the selection on a click, showing its
  panel. A disabled tab now renders without its signal write or its href,
  and the tabs module ignores clicks on it.
- **Removing a tag that holds "×" announces the tag as written.** The tag
  input read the removed chip's text and stripped the first "×", which
  was the tag's own, so "3×5" was announced as "35× removed". The
  announcement now reads the chip's hidden value. The submitted value
  was always right.
- **An embed's new-tab link no longer reports an error.** A
  `target="_blank"` link inside an embed opened its tab, then also
  showed "This panel cannot open links." and sent
  `link-navigation-blocked` to the customer's `onError`. The frame
  read `window.open`'s null return as a refused popup, but with
  `noopener` it is always null. The link now opens its tab and
  reports nothing.
- **A destroyed embed no longer reports that it never loaded.** The
  loader's 20-second no-load watchdog outlived `handle.destroy()`, so
  a single-page host that unmounted an embed before it loaded got
  `onError({code: 'no-ready'})` and a console warning for a frame it
  had already removed. `destroy()` now stops the watchdog.
  `embed.md` documents the handle `mount` returns.
- **`?sort=` takes a comma-separated list.** `sort=title,-created_at`
  sorts by both fields. The generated CLI's `--sort` and process-module
  sort arrays already sent that form, and every such request got a 400
  naming `title,-created_at` as an unknown field. The 16-field limit
  counts the fields across every `sort` value.
- **The MCP list tool sends each filter in the shape the route reads.**
  A JSON field's argument was sent as its first array element
  (`{"payload": [1, 2]}` filtered on `1`) or as Go's `map[x:1]` for an
  object; it is now sent as JSON. An empty `_in` array dropped the
  filter and listed every row; it is now refused. An `_in` element
  holding a comma (`["Doe, Jane"]`) was split by the route into `Doe`
  and ` Jane`; it is now refused. The tool schema gives `_in` arrays
  `minItems: 1`.
- **The generated JS client refuses a filter list element holding a
  comma.** It joined a filter array with commas, so
  `title_in: ["Doe, Jane"]` reached the server as `Doe` and ` Jane`.
  The client now throws before sending such a list.
- **The entityui filter rows keep the query box's meaning on Apply.**
  The box was put in parentheses beside the rows only when it held
  ` or ` with spaces, so a row `status != "open"` and the box
  `name = "zeta"or(name = "alpha")` listed the open alpha order. The
  box is now always grouped beside rows. A term whose value a row
  would trim or drop (`name = ""`, `name = " x "`, a value over 200
  characters) now stays in the box instead of becoming a row, so
  applying the page unchanged keeps the filter.
- **A `where()` value holding an operator word parses.** The fluent
  DSL searched each operator across the whole term, ` in ` and
  ` contains ` first, so `title="Drive in Cinema"` split inside the
  quotes and was refused. A term now splits at its leftmost operator.
- **SQLite search accepts every `FieldEquals` key the other backends
  match.** `SQLiteFTS` refused any key outside `^[A-Za-z0-9_]+$`, so a
  `tenant-id` scope that worked on Memory and Postgres errored on every
  SQLite search. The key is now a bound parameter matched through
  `json_each`, like the value.
- **Memory search matches whole words, like the SQL backends.** It
  split the query on whitespace and counted substrings, so `error`
  found `xerror` and `foo,bar` missed "foo bar"; an app built on the
  Memory default gained and lost matches on Postgres or SQLite. It now
  splits words the way they do and matches the last term as a prefix.
  It still does not stem.
- **The site search page says when a query is too long.** A `q` over
  256 bytes was dropped and the page drew the empty state, as if
  nothing had been typed, while the query endpoint answered 414 for the
  same text. The page now shows a "query too long" notice.
- **A batch item reports the refusal the single write sends.** An item
  refused for a state change it may not make, a unique or foreign-key
  conflict, a NOT NULL field, a transition, a reentrant move or a
  missing tenant came back as `internal server error`, while
  `POST`/`PATCH` on one row said what was wrong. Both now share one
  error classifier, so the item's `error`, `reason`, `fields` and
  `field_reasons` match the single write's body.
- **A failed batch on a handler bound to a `*sql.Tx` undoes its own
  writes.** With `ch.DB` set to a caller's transaction, a batch ran its
  items straight on it, so the items before the one that failed stayed
  written for a caller that handled the error and committed. Each
  operation now runs under a savepoint there, as it does inside
  `App.InTx`. Such a handler still publishes its events before the
  caller commits; `hooks-and-transactions.md` says so and points at
  `db.WithTxQueue`.
- **MCP write tools work behind CSRF.** With CSRF on every route, a
  cookie-authenticated `_create`, `_update`, `_delete` or transition
  tool always got a 403: the `/mcp` request passed the check, and the
  internal request the tool re-dispatched carried the CSRF cookie but
  not the token. `CSRF` now records the token a request passed with
  (`middleware.VerifiedCSRFSubmission`), and the tool presents it under
  the configured header, where the inner check compares it with the
  cookie again.
- **SQLite webhook leases and due times compare in UTC.** SQLite stores
  a timestamp as text with its offset, and the claim compared that
  text. When two workers' clocks carried different zones, or a lease
  spanned a DST change, a live lease read as expired and the same
  delivery went out twice, or a due retry waited out the offset. The
  webhook SQL stores now bind every timestamp in UTC, and opening a
  `SQLStore` on SQLite rewrites the due times of pending rows an
  earlier release stored in the writer's zone.
- **The read-hook redaction examples delete wire keys.** The
  `AfterList` and `AfterGet` examples in the hooks guide deleted
  `"password_hash"` and `"internal_notes"`, but read-hook rows are keyed
  by wire names, camelCase by default, so the delete found nothing and
  the value shipped. The examples now delete `"passwordHash"` and
  `"internalNotes"`, and the guide says which keys the rows carry. A
  hook copied from the old examples needs the same change.
- **GOFASTR1808 reads a function fallback as one argument.** The
  fallback scan counted `(` but not a nested function token, so the
  inner commas of `var(--spacing-lg, var(--spacing-md, 8px))` or
  `clamp(4px, 2vw, 8px)` split the fallback, and the check reported
  drift against the last piece and `gofastr gen styles` refused to
  write the sheet. A fallback that is a function is not judged.
- **BREAKING: owned sheets refuse statement at-rules (GOFASTR1825).**
  `@import`, `@charset` and `@namespace` in a `*.style.css` compiled
  into app.css after the kit's rules, where the browser ignores them,
  and no check said so. GOFASTR1825 now reports them and
  `gofastr gen styles` stops on the sheet. Delete the line; load a web
  font with `style.FontFaceCSS`. `@layer` statements are allowed. The
  contract catalog holds 82 rules.
- **Registering a theme copies its extensions.**
  `style.RegisterThemeOverride` and `uihost.RegisterThemeVariant`
  copied the theme's four maps but kept the caller's `Extensions`
  token sets, so writing one after registering changed the served CSS
  under the hash already issued for it. `AllThemeOverrides` handed out
  the same shared sets. All three now store or return a copy from the
  new `Theme.Clone`, which copies the maps and every extension.
- **Docs search says when it cut the list, refuses a short term, and
  skips code comments as headings.** `gofastr docs --grep` printed
  its 50-hit cap as the match total; it now prints "More than 50
  matches" when there were more. A term under three characters
  returned zero hits as success, from the CLI and from
  `framework_docs_search`, so "too short" read as "not found";
  `docs.Search` and `docs.SearchWithLimit` now return
  `docs.ErrSearchTermTooShort`, the CLI exits 1 and the MCP tool
  answers a tool error. A `#` line inside a fenced code block (a shell
  or YAML comment) was taken as a heading, named as the heading of the
  hits after it and counted toward heading relevance; it is now an
  ordinary line, and a topic's title skips fences too.
- **rtc recovers from a failed `ws` module load and keeps its own
  status across a reconnect.** The module cached the first
  `loadModule('ws')` promise, so one transient load failure left every
  later `connectRoom` on the page at `closed:error` until a full page
  load; it now asks the loader each time, and the loader already drops
  a failed load. After a signalling reconnect `room.self.status` was
  empty while peers saw the resent document; the module puts it back
  on the new `self` before `onSnapshot` runs.
- **BREAKING: `__gofastr.desktop.available` is a boolean.** It was the
  checking function itself, so `if (desktop.available)` passed in a
  plain browser and the page went on to call the bridge. It is now a
  getter that reads the host marker, as the generated d.ts and the
  desktop docs already said. `desktop.available()` now throws a
  TypeError: read it as a property.
- **Test-selection gates fail instead of passing quietly.** CI's
  browser-suite scope step tested `[ -n "$(go run ./cmd/affected …)" ]`,
  which drops the selector's exit status, so a selector error skipped
  the suite with a notice; it assigns the output first now, and
  `cmd/repolint`'s new `selector-status-discarded` rule refuses the old
  spelling in workflows and build scripts. An edit to the coverage-floor
  table in `scripts/coverage-floors.sh` widens `cmd/affected` to every
  package, where it used to check no floor at all. `scripts/dupl.sh`
  refuses a baseline that is not one non-negative integer; `-gt` errored
  on it inside `if` and the gate printed its success line.
- **A graceful shutdown mid-batch no longer spends the outbox's attempt
  budget on deliveries that never ran.** The claim counts an attempt on
  every row of a batch; on shutdown the relay left the unstarted rest
  leased, and once the lease ran out the budget sweep could dead-letter
  them (with `WithMaxAttempts(1)`, after one shutdown). The relay now
  hands them back with the attempt refunded, as it already did when its
  lease ran short.
- **`email.SMTPConfig` takes an IPv6 host.** `Host: "::1"` dialled
  `::1:587`, which Go's dialer refuses; the address is built with
  `net.JoinHostPort`. A host written in brackets (`[::1]`) reaches the
  TLS server name and the PLAIN/LOGIN host check without them.
- **A durable cron schedule registered in a fixed-offset zone no longer
  skips its first run.** A zone the `tz` column cannot store
  (`time.FixedZone`, `time.Local`) evaluates in UTC, but the first
  watermark came from the registration zone, so `0 2 * * *` registered at
  03:00 -05:00 waited for 07:00 UTC, which is no firing in UTC, and the
  catch-up moved the first run to the next day. The first watermark is
  now taken in the zone that persists.
- **A long Postgres queue table name gets scheduler tables of its own.**
  `<table>_scheduler_schedules` and its siblings were cut at Postgres's
  63-byte limit; from a 52-byte table name the three tables shared one
  name (from 63 bytes, the queue table's), and from 40 bytes an
  occurrence index was skipped as an existing name. A
  name whose cut collides now ends in a hash of the table name, as the
  queue's own indexes do; a cut that collides with nothing keeps the
  name earlier releases created.
- **`queue_dead_letter_total` counts dead-lettered jobs.** The collector
  read a `dead` status no backend writes; every backend files a job that
  spent its attempts under `failed`, so the metric stayed 0.
- **`Scheduler`'s `Job` keeps a payload's encoding error.** A payload
  `json.Marshal` refuses (a `NaN`, a channel) registered fine and every
  run received `null`; `Register` now returns the error and registers
  nothing, as `DurableScheduler`'s builder already did.
- **`If-None-Match` lists revalidate everywhere.** The UI host's
  `/__gofastr/*` assets and the webmcp script, manifest and
  instructions compared the header to the ETag as one string, so a
  client sending a list or a `W/` tag got the whole body again, and
  the SDK download matched a substring and ignored `*`. They now share
  the new `static.IfNoneMatch`, the RFC 9110 comparison `core/static`
  already used.
- **A sitemap past 50,000 URLs becomes a sitemap index.** `SitemapXML`
  wrote every page into one URL set, which a search engine rejects
  whole past 50,000 URLs or 50 MiB. Past either limit `/sitemap.xml`
  now names parts at `/sitemap/1.xml`, `/sitemap/2.xml`, …, and the
  static export writes them as `sitemap/<n>.xml`. The new
  `UIHost.SitemapFiles` returns every file. The live server also kept
  no render, so each anonymous GET ran every `StaticPaths` and the
  page policy for every path; it now keeps one render for a minute
  (none under `gofastr dev`).
- **The static export's sitemap lists only pages it wrote.** It
  rendered the sitemap twice and paired the entries to find skipped
  pages; when a `StaticPaths` answered in a different order or count
  the second time, it warned and kept the whole document, so
  `ExcludeRoutes` pages, redirected paths and pages it never wrote were
  listed. `SitemapFiles` now takes a `keep` filter the export fills
  from the pages it wrote, asked about each page by its routing path,
  and a hreflang alternate naming a skipped page goes as well.
- **The HTTP cache middleware evicts a URL's stored `GET` after an
  unsafe request to it.** Every method but `GET` passed straight
  through, so after a `DELETE` or `PUT` the anonymous `GET` of the same
  URL answered `HIT` with the old body until the TTL, while the docs
  called the middleware RFC 9111-compliant (§4.4 requires the
  invalidation). Any method but `GET`, `HEAD`, `OPTIONS` and `TRACE`
  now evicts that URL's entry on every locale once its handler ran.
  Lists that showed the record are not evicted.
- **Back after an entityui delete fetches the record page again.** The
  delete navigated away with `data-cui-rpc-navigate`, which refreshes
  only the destination, so Back replayed the deleted record from the
  runtime's screen cache. A successful RPC whose method is not `GET`
  now evicts the page it ran on before it navigates.
- **The blog example's search drops deleted posts.** `/posts/search`
  answered from an index the seed filled once with fixed documents, so
  a soft-deleted post stayed searchable, and came back after a restart.
  The index is now built from the live rows at start and kept by
  `AfterCreate`, `AfterUpdate` and `AfterDelete` hooks that run once
  the write commits.
- **Web bot auth keeps an IPv6 literal's brackets in `@authority`
  when it drops a default port.** `Host: [2001:db8::1]:443` produced
  `2001:db8::1`, so a signature over the compliant `[2001:db8::1]`
  never verified. Brackets were restored only when a non-default port
  stayed.
- **The auth form-error redirect keeps an escaped `?` or `#` in the
  Referer's path escaped.** It returned the decoded path, so a Referer
  of `/login%3Fnext=/admin` came back as `/login?next=/admin`, a query
  the redirect then extended with `&error=…`, though the Referer's
  query is meant to be dropped. The redirect now uses the escaped path;
  the protocol-relative refusal still reads it decoded.
- **A webhook worker stopped mid-batch hands back the deliveries it
  had not started, attempt refunded.** On a `LeasedStore` they stayed
  claimed with an attempt spent until the lease ran out, so a restart
  waited out the lease, and a delivery at `MaxAttempts` 1 was
  dead-lettered without a send.
- **A replica's presence and rtc goodbye is its last message on the
  fanout.** Island presence `stop` and the rtc `Signaler`'s detach
  published the empty roster before stopping the heartbeat and the
  publish queue, and the queue's stop did not wait for a publish in
  flight, so an older roster could land after the goodbye and restore
  the departed members on every peer until the TTL.
  `fanout.PublishQueue`'s `stop` now waits for the publish in flight
  (at most the 5-second publish deadline on a stalled backend) and
  starts none after it returns; both callers stop the heartbeat and
  the queue before the goodbye.
- **A keyset list page honours and validates `?fields=`.** With
  `?cursor=` present the list returned every visible column and
  answered 200 to an unknown field the offset list refuses with 400. A
  cursor column the projection leaves out is still read to build the
  next cursor and is dropped from the rows.
- **An entityui list's `cols` param and saved-view columns stay within
  the columns the list offers.** A list built with `Columns("name")`
  rendered `?cols=internal_notes` because the check only asked for a
  visible field. A `cols` param naming a column the list does not
  offer is now ignored whole, and a saved view opened on a narrower
  list shows only the columns that list offers. The offered set is
  the menu's: `Columns(...)`, else `Display.Columns`, else the default,
  which leaves out system fields such as `created_at`, JSON fields and
  `Where`-pinned ones, so a typed `?cols=` naming one of those no
  longer shows it.
- **A user erasure deletes the per-account login limiter's rows.**
  With `LoginRateLimitPerAccount` on a `SQLRateLimitStore`, the attempt
  and block rows are keyed by the limiter's scope, `account:` and the
  email, and `EraseUserData` left them until the window sweep.
  `AuthManager.Init` now registers both tables by email
  (`IdentityEmail`), following the limiter's `Scope`; other limiters'
  rows on the same store stay.
- **A user erasure overwrites the embed grants `SQLBurnStore` keeps
  for that user.** A burn row keeps the grant it issued, and the
  grant's claims name the subject, so `EraseUserData` left a signed
  credential naming the erased user until `Prune`. The row now records
  the subject in a `subject` column (added on open to a table an older
  build created; its existing rows keep an empty subject until they
  are pruned), and the erasure overwrites the grant and the subject
  while keeping the row, so the nonce stays spent and its replay is
  refused.
- **A replica on an old build no longer runs it under a newer
  artifact's generation.** When one replica registers a new artifact,
  the row's digest moves and the generation bumps with the module still
  enabled. A replica whose descriptor still named the old executable
  respawned it after the upgrade drain. It now drains its child and
  holds the module in `InstalledDisabled` (routes answer 503) until its
  own deploy registers the matching artifact.
- **A generation bump re-arms a `Failed` process module.** The docs
  name the bump as the way out of `Failed`, but the supervisor reset
  the restart circuit and left the slot `Failed`. `BumpGeneration`, a
  grant revoke or an artifact change now respawns it, and the new
  child goes through the same digest checks.
- **`migrate generate` carries `required` and `default` changes on an
  existing column.** The snapshot diff compared column types only, so
  a field made optional kept its `NOT NULL` and a changed default
  never reached the table. Generate now compares each column with the
  `CREATE TABLE` the snapshot recorded: Postgres gets `SET`/`DROP NOT
  NULL` and `SET`/`DROP DEFAULT` with their inverses as the `Down`;
  SQLite rebuilds the table, under the same refusal as any rebuild
  when a `CASCADE` or `SET NULL` foreign key references it. A snapshot
  holding a default in an older rendering (`0.000000` for `1e-07`)
  converges on the declared one. Boot does not compare either.
- **`gofastr semantic query` and `stats` no longer truncate a running
  `watch`'s WAL.** Both opened the same directory as a second writer,
  and `Close` snapshotted and truncated the log the watcher was still
  appending to. An index opened with `Options.Path` now takes an
  exclusive lock on the directory (a `lock` file) until `Close`, and a
  second writer, in this process or another, gets `ErrIndexLocked`.
  The new `Options.ReadOnly` loads the snapshot and every whole WAL
  record without a lock and writes nothing (`Add`, `Remove` and
  `Snapshot` return `ErrReadOnly`); `query` and `stats` open that way.
- **A semantic write that failed after its WAL record no longer loses
  the document at the next snapshot.** A keyword delete refused
  mid-replace left the store without the document, and the next
  snapshot wrote that store and truncated the record that would have
  restored it. The record now applies again before the next write and
  before any snapshot; while it still fails, writes, `Snapshot` and
  `Close` return the error and the WAL keeps it for replay.
- **The remote `gofastr semantic query` and `stats` send a token.**
  With `GOFASTR_URL` set they sent no `Authorization` header, so a
  mount with `WithAuthToken` answered 401. Set
  `GOFASTR_SEMANTIC_TOKEN`; it is sent as a bearer token to the
  `GOFASTR_URL` server only, and a redirect is refused.
- **A cancelled semantic write on `PgVectorStore` stops waiting.** The
  replace dropped cancellation before its transaction, so a caller
  that gave up kept waiting on another replica's document lock while
  every other write on the replica queued behind it. The transaction
  gets the caller's context; a cancel rolls it back.
- **The semantic watcher retries a failed removal.** It forgot a
  deleted file before `Remove` returned, so a failed removal left the
  document in the index for good.
- **`FlatStore` deep-copies chunk metadata.** A nested map or slice
  was shared with the caller and with returned hits, so writing into
  either changed what the store filters and snapshots on.
- **A whole-number `decimal` bound written as a short decimal is that
  decimal.** `Min: 1e30` was read as its binary expansion,
  1000000000000000019884624838656, so `"1e30"` failed its own minimum.
  A bound of up to 15 significant digits is now read as written; a
  whole-number bound that needs more (2^60) keeps its exact binary
  value.
- **An upsert accepts a blank optional field as create and update do.**
  `{"id":"k1","qty":""}` with an optional `Int` answered 422 "must be an
  integer" on upsert and succeeded on create. A blank non-text value now
  reads
  as not provided when the upsert inserts, and clears the column when
  it updates an existing row; a blank `Image` or `File` keeps the stored
  file, and a blank in a key column is never dropped (an `Int` key part
  still refuses it).
- **A screen a plugin registers in `Init` gets its server actions.**
  The UI host compiled actions only at `Mount`, which runs before
  `App.Start` calls plugin `Init`, so a screen registered there
  rendered its `data-action` buttons and every click answered 404.
  `ValidateBoot` now compiles them after every `Init`, and the
  action-id collision and embed server-action refusals cover those
  screens too, so a late screen that trips either stops the boot.
- **A widget's `/state` omits a signal with no JSON form.** A
  `SignalSource` returning NaN or Inf failed the whole encode after
  the headers went, so `/state` answered an empty 200 and every
  signal kept its server-rendered value. Each signal now encodes on
  its own; one that fails is logged and left out, as a signal whose
  `Read` fails already was.
- **`widget.Mount` no longer claims to be idempotent.** A second
  `Mount` of one widget on one router registers its routes again and
  the router panics on the conflict; the doc now says to mount a
  widget once per router.
- **A cron spec rarer than five years has a next firing.**
  `cron.Schedule.Next` searched five years of minutes, so
  `"0 0 29 2 */7"` (February 29 on a Sunday, next in 2032) read as
  never firing, and both queue schedulers refused it. `Next` now skips
  the dates the day fields refuse and searches 401 years, one full
  Gregorian cycle; a spec that names no date (`"0 0 30 2 *"`) is
  answered from its fields at once. The schedule gains `MatchesDay`,
  `SkipDay`, `Satisfiable`, `FixedTime` and `cron.HorizonYears`, and
  the refusal reads "never fires".
- **A wildcard durable cron runs through a DST change, as vixie cron
  does.** Every spec skipped the repeated hour when clocks went back,
  so `"*/5 * * * *"` in New York stopped from 01:00 to 01:59 EST. The
  once-only rules now apply to fixed-time specs alone, those whose
  minute and hour fields both name values. A spec whose minute or hour
  begins with `*` fires through both copies of the repeated hour, and
  no longer fires at a spring-forward transition for a minute the jump
  skipped.
- **A durable `Every` below one millisecond is refused.** Ticks closer
  than a microsecond collapse to one Postgres timestamp, and the
  occurrence insert broke the tick's unique constraint, which ended
  every scheduler pass before the schedules after it. `Register`
  refuses the interval, and a stored row with one is logged and
  skipped while the rest fire.
- **The harness websocket assembles fragmented messages.** It ignored
  the FIN bit, so a client library that split a command ran the first
  half as a command of its own and closed on the continuation. A text
  frame without FIN now opens a message that continuation frames
  extend, with control frames allowed between them. A stray
  continuation, a text frame inside a message, a fragmented control
  frame, a control frame over 125 bytes, or a message over 16 MiB
  closes the socket.
- **`featureflag.Bool` on a request context reads that request's App.**
  `Flags` and `SetFlagStore` install the App's evaluator as the
  process-wide default, so with two Apps in one process a handler in
  the first read the second's store. The default middleware now puts
  each App's evaluator on the request context
  (`featureflag.WithEvaluator`, read back with `EvaluatorFrom`), and
  `Bool` prefers it to `Default()`. An app without the default chain
  adds `App.FlagContextMiddleware()`.
- **Kiln's `delete_entity` removes the entity's hooks.** It dropped
  the entity and its seeds and kept its hooks, so a frozen world named
  a hook on an entity it no longer had, and the app spec generator
  refused the spec. The hooks now go in the same edit, and `undo`
  brings them back.
- **A Kiln seed row can set a `json` field to an object or array.**
  `add_seed` bound the decoded value as-is, database/sql refused the
  map or slice, and the seed failed with an internal error where a CRUD
  create of the same row succeeds. The value is now stored as its JSON
  text, as CRUD stores it, and reads back as the value seeded.
- **A Kiln page patch read before a delete and re-add no longer
  replaces the new page.** A re-added page restarts at version 1, so a
  patch read from the old page matched it and swapped the old tree in.
  Each `add_page` now gives the page a `gen`, and a patch built from
  another one answers `kind: "conflict"`.
- **Kiln's freeze gives every unnamed page its own screen name.** A
  page without a name took one from its path and the root was `home`,
  so `/` and `/home` both froze to `home` and the generator refused
  the spec. A derived name that a named page or an earlier path holds
  now gains a suffix (`home_2`).
- **An embed frame reloaded after the nonce's minute gets its grant
  back.** The loader re-sends the same nonce on every `ready`, and the
  exchange checked the nonce's expiry before the burn store, so a
  reload after the first minute of a 15-minute grant answered 401. A
  nonce whose only fault is its age now passes every other check and
  looks up the grant its first exchange stored, while that grant
  lives. It never mints one: a nonce that was never exchanged still
  answers expired.
- **BREAKING: a JSON field takes no filter.** SQLite compared the stored
  JSON text and Postgres compared JSONB values, so one filter matched
  different rows on each engine. Every filter surface now answers 400 on
  a JSON field (`?field=`, `_ne`, `_in`, `?where=`, the DSL, relation and
  include filters, the MCP list tool), and OpenAPI, the generated CLI and
  the SDK readmes stop offering one.
- **BREAKING: a list cursor falls back to the anchor row's key when its
  sort values pass the cap.** A long Text sort value minted a next cursor
  over the 16 KiB decode cap, so the following page answered 400. The
  cursor carries the sort values and the key when they fit, and the key
  alone when they do not; the server then reads the sort values back from
  that row. A single `CursorField` pages on that field plus the key, so
  rows tied on it are no longer skipped. A key-only cursor whose anchor
  row is gone answers 400 with reason `cursor_anchor_gone`; a key too
  long for the cap answers 500 with `cursor_key_too_long`. Define refuses
  a Hidden or NoQuery key behind a `CursorField`, and a `CursorFields`
  list naming one column twice. A cursor minted before
  the upgrade on a single `CursorField` other than the key is refused
  once. See
  [cursor pagination](framework/docs/content/cursor-pagination.md).
- **BREAKING: `pagination.NewCursorPage` returns `(CursorPage, error)`.**
  It refuses, with `pagination.ErrCursorTooLong`, a row whose cursor
  would pass the decode cap, instead of handing out a token the next
  request refuses.
- **BREAKING: the SDK schema hash includes the ordered primary key.**
  Swapping a composite key's columns reversed the generated client's
  positional key arguments while `sdkdocs` still reported it in sync.
  Every app's hash changes once; run `gofastr generate sdk` after
  upgrading.
- **An `Int` field is `BIGINT` on Postgres.** Validation accepted any
  int64 and Postgres stored a 32-bit `INTEGER`, so `2147483648` saved on
  SQLite and failed on Postgres. An `INTEGER` column an older build made
  stays and reports in sync; the upgrade note has the `ALTER` for wider
  values.
- **Integer foreign keys are `INTEGER` on SQLite.** They were `TEXT`, so
  `?parent_id_gt=2` left out 10 and a sort put 10 before 2. Any integer
  spelling of the parent key (`INT`, `int8`, `SMALLSERIAL`, …) counts.
- **Long index and constraint names stay distinct on Postgres.** Names
  past 63 bytes were truncated by Postgres, so two indexes sharing a long
  prefix got one name and `CREATE UNIQUE INDEX IF NOT EXISTS` skipped the
  second, leaving a table with no uniqueness check. Long default index
  names and `<table>_<col>_key` names now end in a hash of their parts;
  shorter names are unchanged. A name already taken by another table's
  index fails boot with the column and index named. Clearing Unique on
  Postgres drops the constraint the catalog holds on that exact column.
- **A `NoTransaction` migration runs each statement on its own.**
  Postgres ran a multi-statement body as one implicit transaction, so
  two `CREATE INDEX CONCURRENTLY` statements failed.
- **Data import refuses an archive with no key.** An archive missing a
  primary-key column, or with a null key, wrote rows nothing could reach.
- **Data export finds `main.`-qualified SQLite tables and writes an
  aliased table once.** A schema-qualified table on SQLite was skipped as
  absent, and `users` beside an exporter's `public.users` exported one
  table twice, so the restore failed on duplicate keys.
- **BREAKING: a CRUD handler bound to your own `*sql.Tx` announces events
  after commit only.** Its events went out before the caller's Commit,
  and even after a Rollback, and a failed batch item left earlier ones
  written. Run the write with `db.WithTxQueue(ctx, tx)` and call
  `RunAfterCommit` after Commit; with no queue the event is dropped with a
  warning.
- **The ambient-transaction probe keeps its two-minute limit.** A
  statement holding the transaction's connection blocked the probe, and
  its commit check, past the limit and past shutdown.
- **Erasure reaches rows a database cascade removes, and stores outside
  SQL.** A profile row deleted by `ON DELETE CASCADE` from a battery-owned
  user kept its uploaded files and audit diffs. Battery delete erasers now
  seed the same cascade plan, matched to entity tables by catalog
  identity on both dialects. The new `datexport.StoreEraser` runs after
  the SQL commits: the semantic index's snapshot and WAL are rewritten
  without the user's chunks, and `RedisQueue`, `MemoryQueue` and the
  in-memory `Scheduler` drop the user's jobs and schedules. Results land
  in `EraseReport.Stores`; a store that fails is reported, never counted
  as erased. `DurableScheduleBuilder.UserID` and `ScheduleBuilder.UserID`
  stamp the jobs a schedule fires. See
  [data export](framework/docs/content/data-export.md).
- **BREAKING: Redis erasure needs `queue.ScriptRunner`.** The erasure is
  one Lua script over the queue's five keys, and each move a worker makes
  between keys is a script that checks its source still holds the job, so
  a running worker cannot write an erased job back. A client without
  `Eval` fails the erasure. On Redis Cluster give the queue a hash-tagged
  name.
- **`Harness.Shutdown` writes queued session events before it closes the
  store.** It cancelled the writes and returned nil with events missing.
  It now drains within 10 s and returns the store's errors.
  **BREAKING:** `Harness.CreateSession` returns `(ids.SessionID, error)`
  and refuses with `ErrShutdown` once Shutdown has begun.
- **An rtc signal lost on the way to another replica closes the sender.**
  A failed or dropped cross-replica publish left both sockets open and
  the call waiting for an answer that never came. `core/fanout` gains
  `PublishQueueReporting`.
- **`gofastr generate cli --from-openapi` reports an `allOf`
  self-reference.** It overflowed the stack instead of returning the
  "$ref cycle" error.
- **App spec generation refuses two items with one Go name.** A helper
  named `register_generated` redeclared `RegisterGenerated` and the app
  failed to compile after a successful generate. The check covers the
  whole package and import names.
- **Generated UI configs pass the request context.** Pricing cards,
  forms, text areas, sidebars, content rows, action buttons, data tables
  and sections said their built-in words in English on every locale.
- **`App.Shutdown` ends open streams when the drain starts.** An SSE
  response, a `core/stream` WebSocket (closed with 1001) or a hijacked
  connection held Shutdown to its deadline. A stream is known by its
  response, not its `Accept` header. `stream.WithUpgradeHook` is new. A
  WebSocket whose connection dies during the close handshake ends at
  once instead of waiting out `CloseTimeout`.
- **`gofastr dev` lets the app shut down.** It killed the app 100 ms
  after SIGTERM on every save; it now waits up to 5 s and returns as soon
  as the app exits. A signal during a build kills the build's process
  tree. The watcher skips dot-directories, and `dist`, `gen` and `tmp` at
  the project root unless the build imports from them.
- **Kiln ends options before the chat message.** A message starting with
  `-` reached omp, claude-code or a custom agent as a flag.
- **A recycled pid no longer reads as a live daemon or run.** `service
  stop`, `service uninstall` and `localrun.List` pair the pid with the
  process start time; the daemon reports its own.
- **`PageHandler` takes colon-style paths.** Given `/thing/:id` it set
  `id=":id"` on every request.
- **The static export writes a `StaticPaths` key holding `/`, and refuses
  two pages that write one file.** The key is checked after it is encoded
  for disk; `..`, `.`, `\` and NUL are still refused. Two files that
  differ only in case, or only in Unicode normalization (`café`
  precomposed or with a combining accent), count as one file: a redirect's page, a
  static-source or `ExtraDirs` copy (`_HEADERS` included), a contributor
  file and a rail script are checked the same way. A build that writes
  any two such spellings, an asset and a rail script or a contributor
  file and the generated `_headers` among them, or two directories on
  their paths, fails and names both. A static file replaces a generated
  one only when spelled the same. A static-source, static-dir or
  `ExtraDirs` name holding `\`, and an `ExtraDirs` mount holding one, is
  refused, and so is a contributor's `_headers` or `_redirects`. A
  rebuild after a case-only rename keeps the page, and every file and
  directory the build writes is renamed to its spelling when an earlier
  build left it in another case. One behind a symlink in the output
  directory is not renamed; the build fails and names it, as it does
  for a symlink whose target names a directory in an old spelling.
- **A rebuilt static export writes the same PWA worker.** The worker's
  tree hash read a locale tree's `manifest.webmanifest` only once an
  earlier build had written it, so a second build over unchanged
  content rotated the worker's cache and every installed client
  re-downloaded the site. The hash leaves out the files the PWA surface
  writes, under their exact names, after renaming a leftover in another
  case that is the same file, and any symlink or hard link to one the
  build generates (an alias of a static-dir manifest stays). On a
  case-sensitive disk a file such as `MANIFEST.webmanifest` is a file
  of its own and stays in the hash.
- **BREAKING: `registry.RegisterStyle` checks its name.** A name holding
  `"` added attributes to every instance. It now takes
  `^[a-z][a-z0-9-]{0,63}$`, as `RegisterBehavior` does.
- **A dismissed `ui.Banner` with no `DismissID` stays dismissed.** Its
  identity came from a process-wide counter, so the next render, an
  island refresh or a page navigation, showed it again. It is now named
  by its message.
- **A theme missing a breakpoint fails at boot, not on a request.**
  `app.WithTheme` compiles every owned style sheet against the theme and
  names the one that fails; at serve time a sheet that does not compile
  is logged and served empty.
- **`SectionMenu`'s default label is translated.** It said "Menu" on
  every locale.
- **Pane-host keys act on the focused host, and an open drawer takes
  focus.** Escape and the Tab trap went to the first overlay host on the
  page. A drawer rendered open on a phone-width load now moves focus
  inside, unless a modal holds it, and Tab from outside goes into it.
- **A backslash in a filter value keeps the filter.** `C:\reports` made
  the rebuilt DSL unparseable and the list showed unfiltered. `dsl.Quote`
  is the one serializer.
- **A cleared saved-view filter stays cleared.** The next sort, page or
  columns click brought it back.
- **Related-list counts match the list.** The badge counted without the
  child list's default view and filters.
- **Auto-derived ids are unique on a page.** Two same-titled sections,
  tabs or carousels emitted the same `id`, so `aria-labelledby` and
  `#features` hit the first. Repeats get `features-2`, a component's
  derived ids are reserved with it, and an explicit id always wins. Slots
  that render at once settle in slot order. An island renders under its
  own id, so a section in island `cart` is `cart--features` in both the
  page and the RPC answer, and one in a deferred outlet takes the
  outlet's address, marked so no island name reaches it
  (`_l-shell-aside--features`); a link to an auto-derived id inside
  either needs the prefix. A namespace keeps letters, digits
  and single inner hyphens and escapes any other byte as `_` and hex
  (island `cart.v2` gives `cart_2ev2--features`), so two namespaces never
  share ids and an island id with a quote never reaches an attribute.
  `ui.Carousel`, `ui.Repeater` and `ui.Counter` with no `ID` take their
  id from the same scope, and HTML seeded into a signal carries the
  settled ids. See core-ui/ARCHITECTURE.md.
- **Local-entity number fields read the page's language.** `3,5` on a
  Spanish page and `12,34,567.8` on an Indian English one were refused.
- **The API docs' mobile drawer lists entities registered after `Mount`.**
  `interactive.SectionMenuDrawerFunc` builds a drawer's items per render.
- **examples/site's status pill keeps its wording through hydration.**

- **A directory app spec keeps every file's hooks and merges `app:` key
  by key.** Hooks in a second file were dropped, and a later file's
  `app:` replaced the earlier one whole, so a theme-only file turned
  auth off. A key that two files both set is now an error naming both
  files.
- **The generated CLI, SDKs and typed client call the path the app
  mounts.** An entity named `BlogPost` or `Blog-Post` was called at
  `/api/BlogPost` and answered 404; every client now uses the table name
  (`/api/blog_post`). The generated CLI command for a CamelCase entity
  is now `blog-post`, not `blogpost`.
- **Cron leader election releases the minute's lease after the claim.**
  The lease was held until every job in flight on the leader finished,
  so one long job skipped every tick of every job on every replica.
- **Following a link with a pane drawer open on a phone leaves the next
  page scrollable.** The drawer's scroll lock outlived the navigation.
- **Process modules.** `Peer.Close` after a protocol fault cancels the
  inbound handlers still running. Notifications to a child that stopped
  reading are capped at `Limits.Inflight`; past the cap `Notify` returns
  `ErrInflightCap` instead of parking another goroutine. A module that exits inside the drain deadline takes its
  descendants with it.
- **Single-key shortcuts stay out of editors.** `/` (GlobalSearch) and
  `?` (the shortcut sheet) fired inside a `contenteditable` region. A
  shortcut bound to `k` no longer fires on Shift+K.
- **Traditional Chinese readers get the Traditional catalog.** zh-TW and
  zh-HK fell back to Simplified Chinese or English. `EnvLocale` keeps a
  POSIX `@latin`/`@cyrillic` modifier.
- **Contracts and lint gates.** A `"*/*"` string no longer blinds the
  comment stripper to the code after it. GOFASTR1810 catches a kit class
  reached through `[class~=…]`, `[class^="fui-"]` or a CSS escape. The
  inline-script and inline-style gates catch a tag built by
  concatenation. The `runtimeprose` and `layerprefix` allow markers are
  comment-anchored like every other marker, and
  `//check-novar:ignore-file` must start a comment. GOFASTR1802 and
  GOFASTR1803 read an app's `.js` files, and 1802 fires on any
  `location… =` assignment. check-csp and GOFASTR1805 treat an inline
  `<script type="importmap">` as a script, since CSP applies to it. GOFASTR1407 counts
  `jsonexact.Unmarshal` and `Decode` as raw-JSON sinks. `LintNumberFill`
  reads `say()` fill params, and the intercept layer-limit toast formats
  its count in the page's language.
- **Cursor paging.** A key-only cursor's anchor lookup applies only the
  access scopes, so a row that left the `?q=` or `?where=` search no
  longer answers `cursor_anchor_gone`. The DSL's `after()` refuses a
  `CursorField` that is neither the only key nor unique
  (`dsl.after_not_unique`): it skipped rows tied on the sort value.
- **Erasure.** The in-memory queue erases a job held back by a disabled
  module's gate; it ran later. An in-memory `Scheduler` feeding a
  `DBQueue` can no longer re-insert the erased user's job after the SQL
  delete. `GrantStore.Grant` and `Revoke` each run in one transaction.
- **Includes and filters.** Repeating a scoped include whose filter set
  already had its own node keeps both nodes. A Timestamp or Date filter
  of year 0000 is refused instead of failing on Postgres.
- **Dotenv expands each value once.** A single-quoted literal or a `\$`
  escape stopped being literal when another line referenced it.
- **Markdown leaves intraword underscores alone.** `XDG_DATA_HOME`
  rendered `XDG<em>DATA</em>HOME`, and an underscore in a URL could
  break the link.
- **YAML.** A trailing comma in a flow list no longer adds an empty item
  (in an app-spec enum it became an allowed value). A compact nested
  sequence (`- - a`) parses as a nested list.
- **The migration statement splitter keeps a multi-action `CREATE RULE …
  DO INSTEAD (a; b)` whole.**
- **Headless and kit components.** Escape on an open combobox closes
  only the combobox, not the modal or intercept overlay around it
  (`data-hui-combobox-inline` marks a combobox that lives inside its
  owner, as CommandPalette's does). A keyboard-grabbed sortable item
  that loses focus is dropped back, and a cancelled drag no longer POSTs
  the reorder. Lightbox zoom and pan reset on Prev and Next. Six kit
  components and the Avatar status dot speak the page's language
  (`AvatarConfig.Ctx`). `preset.BottomSheet`'s doc drops the ~50vh claim;
  it caps at 75vh.
- **Runtime.** A refused optimistic commit restores the displaced member
  only when nothing committed since. A component action script that
  failed to load retries on the next use. Reveal sections show after
  Back restores a page from the cache. A failed widgets.js load at boot
  stops the catalog timer and keeps the catalog, so a later open
  finds its widget. `data-cui-window-drag` starts a window drag
  only on a primary-button press outside a control, and
  `data-cui-window-no-drag` marks a region inside it that never drags. A
  keyed view transition plays into a layout chain the current page does
  not declare. A deferred part that fails after commit retries once,
  then sets `data-cui-loadstate="error"`. A `data-cui-intercept-page`
  link no longer leaves the closed pane's URLs in history.
- **Declarative redirects keep the query string**, on full loads and
  client navigation. The error partial no longer carries the failed
  route's title in `X-Gofastr-Title`. Per-caller markdown (content
  negotiation, `/llm.md`) is sent `no-store` with `Vary: Cookie`. Strict
  boot checks pages whose route has a constraint (`/notes/:id:int`).
  Site search skips pages whose robots meta says `none`.
- **App spec and CLI.** `theme init --out=<path>` writes the package
  of the Go files already in the directory, else the directory's name. `gofastr init .` over an existing `go.mod` uses
  its module path and refuses a `--module` that disagrees. Re-init
  advises `gofastr init <dir> --reinit --force`, which now overwrites an
  AGENTS.md whose markers are missing; it named an `agents init` flag
  that does not exist. Two screens whose routes differ only in param name
  (`/posts/{id}`, `/posts/:slug`) are refused; the second was
  unreachable. `gofastr pack` keeps negative numbers and a card's children.
- **Generated apps.** The generated e2e test expects 403 from a screen
  that needs a role the seeded admin lacks. `--from-openapi` refuses two
  operationIds that fold to one Go name instead of emitting a CLI that
  does not compile. client.d.ts declares `NoBlob`, and an entity
  named `no_blob` is refused.
- **Kiln.** `kiln freeze` reads the journal read-only, so it can no
  longer truncate an entry `kiln serve` just wrote, and writes an empty
  object in a form core/yaml accepts. `agent.Loop.Run` stops dispatching
  a turn's tool calls once its context is cancelled. SIGTERM to `kiln
  serve` waits for the agent's process group to be killed.
- **Harness.** Context files (`AGENTS.md` and the like) are read inside
  the repo with `os.OpenRoot`, regular files only, capped at 256 KiB: a
  symlink to `~/.ssh/id_ed25519` went to the provider. The secret redactor and export's redaction keep the
  event valid JSON; events were dropped from export silently. Grep honours
  cancellation and skips FIFOs and devices. Quiet mode refuses `find`'s
  `-fprint0`, `-exec` and `-delete` in any spelling or quoting. The TUI tracks each pending
  permission prompt on its own. Stream resume (`lastEventId`,
  `Last-Event-ID`) replays the events since the given id over SSE, the WebSocket and MCP
  (`resources/read` of `harness/v1://session/{id}/events?since=N`). The stdio MCP server handles requests concurrently, so
  `harness.cancel_turn` reaches a `wait=turn` call. Sub-agent tool calls
  are persisted on the parent's log, tagged with the sub-agent. The
  session log's bus subscription waits for the store instead of
  dropping, so a burst past 256 queued events no longer leaves holes in
  the log that resume and export read. The MCP server's `initialize`
  no longer advertises `resources.subscribe`, which it never handled.
- **Service.** On macOS a stop whose drain overruns, or a battery
  `OnStop` error, no longer exits 1, so launchd does not restart the
  daemon `service stop` reported stopped. `service uninstall` and macOS
  `service stop` wait up to 15 s for a daemon that is still starting,
  then go ahead; on macOS that stop boots the job out of launchd, which
  would otherwise restart it.
- **Web Bot Auth.** Directory fetches hold one slot per registrable
  domain, so hanging `jwks_uri` hosts cannot pin every slot, and a full
  fetcher answers `ErrResolverBusy`. A failed refresh keeps serving the
  stale entry for a back-off instead of refetching on every request.
- **StripMetadata keeps every frame of an animated PNG** that carries a
  tEXt or eXIf chunk.
- **MCP tool refusals reach the agent.** `contracts_fix`,
  `contracts_explain`, `contracts_list` and the CRUD list/get argument
  refusals answered "internal tool error" and logged at Error; they now
  answer invalid params with the reason.
- **A webhook POST that `Stop` cancels is refunded**, not counted as a
  failed attempt, so a delivery on its last attempt is no longer
  dead-lettered.
- **A panicking `Fanout.Publish` no longer kills the process.** The
  publish queue lost its panic guard in the last round; the payload is
  dropped and reported as a failed publish.
- **The cache middleware treats `X-API-Key` as a credential**, so a GET
  it authenticates is never cached under the shared key. It checks every
  value of each credential header, not only the first.
- **The relay refuses upstream protocol switches.** It strips the
  inbound `Connection` and `Upgrade` headers, and a vendor 101 closes
  the upstream socket and answers 502; the socket was left open.
- **rtc notices a signal the receiving replica dropped.** Each sender
  numbers its signals per target, and a gap closes the target's socket
  with `signal_lost`, as the publish side already did.
- **Log battery.** The webhook sink no longer retries 3xx and 4xx
  responses. Attrs added after `WithGroup` nest under the group, and
  nested groups nest, the way `slog`'s JSON handler writes them.
- **A cancelled SMTP send stops.** Cancelling after the connection was
  up left the send waiting on the relay, and it could still deliver and
  return success.
- **entityui.** List cards draw a `Display.Card` field that is not a
  shown column, and draw the card's subtitle, which the custom header
  hid. CSV export writes a negative number in a numeric column
  as a number; a non-number there is still quoted. Decimal and Float
  values show the column's scale (the `s` of `NUMERIC(p,s)`), four
  places by default, as entityui.md says; they showed two. The Activity
  tab starts after a reused key's earlier purge or hard delete, so it no
  longer shows the purged record's history.
- **Local-entity forms keep machine values dot-decimal.** On a
  comma-decimal page, range, select, radio and hidden values were parsed
  with the page's decimal mark and written back with it; only text,
  search and textarea controls use it now.
- **`gofastr dev` and `gofastr desktop`.** On Windows, shutdown waits
  for the app to exit before removing its binary directory, so nothing
  is left in %TEMP%. A save that adds the first import of a root `gen/`
  or `vendor/` package restarts the app once, not twice.
- **The Windows and macOS file dialogs validate like Linux**
  (`desktop.CheckFileDialog`), and Windows returns its conversion and setter
  errors. Every shell now refuses an extension containing `;` and a
  default name containing `\`. On Windows a `*` extension matches
  everything, and so does a filter with no extensions.
- **BREAKING: two indexes that share a name are refused.** Index names
  belong to the schema on both engines and every index is created with
  `IF NOT EXISTS`, so the second table's index was skipped and a
  `Unique` one let that table take duplicates. A default name collides
  too (table `a_b` on `c` and table `a` on `b_c` are both `idx_a_b_c`).
  Boot, `DiffSchema` and `migrate generate` now name both tables; give
  one index its own `Name`.
- **A `String` whose `Max` changed migrates on Postgres.** Neither
  `DiffSchema` nor `migrate generate` compared the `VARCHAR` width, so
  a `Max` raised from 50 to 200 left `VARCHAR(50)` in place, validation
  passed a 150-character value and the insert failed. Both now emit
  `ALTER COLUMN … TYPE VARCHAR(n)` (a destructive change, like every
  retype); the live Postgres column read carries the width. SQLite
  ignores widths and is unchanged.
- **`tenant.WithMultiTenant` adds the tenant column.** It runs after
  `entity.Define`, which is where a multi-tenant entity's column is
  injected, so it set `MultiTenant` and `TenantField` on an entity with
  no such field: auto-migrate created no tenant column and the first
  create failed on the column CRUD writes. It now adds the column, and
  renames the `tenant_id` that `Define` injected to a custom `Field`.
- **BREAKING: a misspelled `-- +migrate` directive is refused.** The
  parser skipped a directive it did not know and kept the section above
  it open, so `-- +migrate down` under `Up` put the Down statements into
  `Up`: the rollback ran as part of the apply (a `DELETE` or `DROP` with
  it) and the version was recorded applied. An unknown directive, and a
  comment that reads as one with its spacing or case off
  (`--+migrate Down`, `-- +Migrate Down`), now fail the parse; so does a
  prose comment such as `-- +migrated from v1`. `migrate generate`
  refuses a SQL body holding any such line through the parser's own
  test, the new `migrate.IsDirectiveLine` (`core/migrate`), so it no
  longer writes a file the runner refuses to load.
- **Tables and columns named past 63 bytes migrate on Postgres.**
  Postgres cuts such a name to 63 bytes and the catalog holds the cut
  one, which the migrator compared with the whole name. A long table
  read as absent on every boot and never gained a column; a long column
  read as missing, so the next boot with a column to add failed on a
  duplicate `ADD COLUMN`, `DiffSchema` proposed dropping it, and a long
  key column read as a key change. A foreign key onto a long table, or on
  a long column on either side, diffed as a destructive drop plus an add
  on every run. The catalog is now read by the cut name.
- **BREAKING: `migrate generate` refuses a SQLite rebuild of a table
  another table references.** SQLite empties a table before dropping it,
  and with foreign keys on, a child row still pointing at it fails the
  `DROP`. The generated file applied on a database whose child was empty
  and failed with `FOREIGN KEY constraint failed` wherever it held rows.
  It was refused only for `CASCADE` and `SET NULL` keys; every key the
  snapshot records now counts. Write the rebuild by hand, or apply it with
  `ApplySchemaDiffWithOptions`.
- **A Postgres view whose columns changed boots.** Boot applies a plain
  view with `CREATE OR REPLACE VIEW`, which refuses a body that drops,
  renames or retypes a column, so such a view failed every boot from
  then on while SQLite booted. Boot now drops the view, and the plan's
  views that read it, and creates them again when Postgres refuses the
  replace. A view outside the plan that reads it still stops the boot.
- **A failed unlock no longer leaves the Postgres migration lock held.**
  The unlock's error was dropped and the connection went back to the
  pool still holding the session lock, so after a `NoTransaction`
  migration failed inside its own `BEGIN` (an aborted session refuses
  the unlock) every other replica waited on the lock for the life of
  the process. Such a connection is now retired, which ends its session
  and releases the lock. `cron.NewPostgresAdvisoryLease` had the same
  release and gets the same fix: a failed unlock no longer keeps every
  other replica from leading.
- **Keyword table, column and index names migrate.** The migrator wrote
  every name bare while CRUD quotes them, so a field named `order`,
  `group` or `limit` failed `CREATE TABLE` on both engines, and one
  named `user`, `end`, `desc`, `to`, `offset` or `do` booted on SQLite
  and failed on Postgres. A name that is a keyword on either engine is
  now written quoted and folded (`"order"`) in every statement the
  migrator writes, and the recorded DDL a snapshot holds is read back
  either way. Other names stay bare, so existing DDL and snapshots are
  byte-identical, and a snapshot that holds a keyword name bare diffs
  as unchanged.

### Security
- **The trash view's Restore and Delete permanently routes honor read
  hooks.** `entityui.RestoreHandler` and `PurgeHandler` wrote any
  trashed row the caller's scope reached by id, including one a
  `BeforeList` hook hides from the trash view itself; the bulk restore
  already dropped such a row. Both now read the row with
  `crud.WithReadHooks` first and answer 404 for a hidden one (#645).
- **A record's Activity tab no longer prints a value a read hook
  masks.** The change list drew each audit row's stored before and
  after values for every field but `NoQuery` ones, so a field an
  `AfterGet` hook rewrites (`****` on the record and in the JSON API)
  showed its written values to anyone who could open the record. A
  field the hook rewrites on that record is now left out, locked fields
  included (`framework/entityui`). The hook is also run once over a
  probe copy of the row with every empty field filled, so a hook that
  masks only a non-empty value is caught while the record's own value
  is empty; a hook that fails on the probe keeps the probed fields out.
  The admin's Audit log page and the dashboard's recent activity
  (`UI.Changes`) leave out the fields the hooks mask on a probe row too:
  they printed the stored values the admin's own record screen masks.
- **The log file sink tightens an existing file to `FileMode`.** The
  `0600` default applied only when the sink created the file, so a
  `server.log` an older build, a `touch` or a provisioning step left
  `0644` stayed world-readable, while every file the sink rotated in
  was `0600`. Opening now clears the bits `FileMode` lacks (it never
  adds any) on a file this process owns. A file another user owns (a
  provisioned `0664 root:app` log the app appends to through its group)
  keeps its mode, and a chmod the OS refuses is one stderr warning, not
  a failed open; Windows skips the tighten. Set
  `FileOpts.FileMode: 0o640` if a shipper reads the file through its
  group.
- **Slow-query lines mask SQL string literals.** `slowquery.TrimSQL`
  (the `sql` attr of every `slow query` line) printed a statement
  verbatim, so a value spelled inline (`ALTER ROLE … PASSWORD '…'`,
  `SET app.key = '…'`, a hand-built `WHERE token = '…'`) reached every
  log sink. The line now uses the masker behind tracing's
  `db.query.text` (shared in `internal/sqlredact`): string and number
  literals print as `?` and comments are dropped; placeholders and
  identifiers stay. `slowquery.WithDialect("postgres" | "sqlite3" |
  "mysql")` picks the engine's quoting rules; without it the line masks
  under all of them, so a SQLite `"double-quoted"` string, a MySQL
  `'a\'b'` escape or `#` comment, and a PostgreSQL nested `/* /* */ */`
  comment no longer leak, at the cost of PostgreSQL quoted identifiers
  printing as `?`. Tracing's `db.query.text` now nests PostgreSQL
  comments, reads MySQL `#` comments and `$é$` dollar tags, and masks
  under every engine's rules for a `db.system.name` it does not know.
  The 240-byte cut also no longer splits a UTF-8 rune.
- **The log console sink escapes control and bidi runes in the message
  and keys.** Only attr values were escaped; the message, attr keys,
  the time and level fields, and an entry that was not JSON were
  written raw, and slog escapes C0 only, so a C1 CSI or a right-to-left
  override in a message built from request data reached the terminal
  live. A quote in a message also printed as `\"`.
- **The log webhook sink no longer writes its URL to stderr.** When a
  batch was given up on a transport error, the warning quoted net/http's
  error, which carries the full URL, so a Slack incoming-webhook secret
  or a `?token=` query landed in journald and the container log. The
  warning (and `HTTPErrorReporter`, which shares the sink) now names
  only `scheme://host`.
- **A static export no longer publishes a symlink out of the static
  dir.** The UI host reads the static dir through `os.Root` and answers
  404 for `static/config.json -> ../config/prod.json`, but the export
  copied it with `os.Open`, so the target's bytes went out with the
  site. Both the static dir and a disk-backed static FS now skip such a
  link (and any entry that is not a regular file) and log it; an
  in-tree symlink still ships. A symlinked directory that stays in the
  tree (`static/images -> img`) is walked under its own name, as the
  live host serves it; one that leaves the tree is skipped, and so is a
  link back to a directory the walk is in (`loop -> .`). A symlinked
  directory used to fail the build halfway with an empty file left
  behind.
- **`core/static` refuses a hidden file named with a backslash.** The
  dot-segment and config-file checks split on `/` only, so
  `/sub%5C.env` read as one harmless segment, while a disk `FS` on
  Windows opens it through `os.Root`, where `\` is a separator, and
  served `sub/.env`. `static.Refuses` (and the static export's
  publish filter that uses it) now treats `\` as a separator.
- **A caller's `/openapi.json` view no longer names entities they cannot
  read through a relation.** The view drops such an entity's schema and
  paths, but a readable entity's relation field still carried
  `x-relation: <name>`. It now names a target only when the view
  includes it; the full document is unchanged.
- **A junk-key flood no longer resets an exhausted rate-limit key.**
  With the default `RefillBy`, eviction at `MaxKeys` shed the exhausted
  bucket first, so a per-username login throttle came back with a full
  budget after a burst of made-up usernames. The buckets with the most
  tokens left go first now.
- **`router.Param` refuses an empty catch-all segment.** A `{name...}`
  value with a leading or doubled `/` only arrives as `%2F`, and
  `/files/%2Fetc/passwd` handed the handler the absolute path
  `/etc/passwd`. The value is cut there, as a smuggled `..` already is.
- **A scope added after an `OrWhere` covers the whole WHERE.**
  `query.Select` wrote its connectors flat, so
  `Where(a).OrWhere(b).Where(tenant)` built `(a) OR (b) AND (tenant)`,
  which SQL reads as `a OR (b AND tenant)`. A tenant, owner or
  soft-delete scope, or a `Cursor`, added to a host-built query after an
  `OrWhere` bound only to the last condition, and rows matching `a` came
  back from every tenant. An AND after an OR now wraps everything before
  it: `((a) OR (b)) AND (tenant)`. A chain with no `OrWhere` before a
  `Where` builds the same SQL as before.
- **A `BeforeGet` read scope binds record writes.** A row
  `GET /<entity>/{id}` answered 404 for, because the caller's own
  BeforeGet hooks excluded it, could still be changed with PUT, PATCH,
  DELETE, a transition or a `_batch` item. Those writes now answer the
  404 a missing id gets, before anything is written; restore, purge and
  the in-process helpers under `crud.WithReadHooks` follow, and
  `UpsertOne` refuses a hidden existing key as it refuses another owner's
  row.
- **`RedirectPattern` no longer opens an off-origin redirect through a
  tab, CR or LF in a catch-all value.** `GET /old/%09/evil.example`
  against `/{rest...}` answered `Location: /\t/evil.example`, which
  browsers read as `//evil.example`. A resolved target holding any
  control byte is no redirect, the 308 is gated by
  `handler.IsSafeRelativePath`, and every Location the UI host writes
  percent-encodes tab.
- **The `WithHeadHTML` / `SEOScreen.HeadHTML` scrub runs to a fixpoint**,
  so removing an inner match can no longer splice a `<script>`, a
  meta-refresh or an event handler back together. Head HTML still
  changing after 8 passes is dropped and logged once.
- **Page markdown is `no-store` with `Vary: Cookie`** (the
  `Accept: text/markdown` twin, `/<page>/llm.md`, `/llm-pages.md`), like
  its HTML page: a shared cache could replay a signed-in reader's page.
- **`WithExtraScripts` holds the `RegisterExternalScript` rules**: an
  off-origin, protocol-relative or malformed src is dropped with a
  warning.
- **BeforeList/BeforeGet read-scope hooks now cover every read surface.**
  `_events` no longer delivers rows they hide (or hidden rows' delete
  ids); cascade writes no longer link, update or echo child rows the
  child's hooks hide, and a hidden child or belongs_to target answers
  404 like a missing one (closing an existence oracle); a 409/422 state
  refusal no longer names a hidden row's stored state.
- **Headless RPC sinks are same-origin.** `MenuItem.RPC`, `RPCAttrs`
  `data-cui-rpc` (including `/\host`), `TreeNode.LazyPath`, SortableList
  `RPCPath`/`ConflictRPC` and a static Combobox `NoScriptAction`
  (`javascript:` shipped) are refused unless same-origin.
- **sdkdocs no longer serves SDK downloads that cover gated entities.**
  `gofastr generate sdk` covers every entity unless `--only` or
  `--exclude` narrows it, while the docs site documents only `Public` entities by default.
  The `/sdk/` routes served the artifacts whole, so an anonymous caller
  read a gated entity's table, fields and transitions from
  `client.d.ts` while its reference page answered 404. The four
  download routes now answer 404 unless every entity the manifest names
  is one the site documents. The same applies when the manifest is
  missing. A file must also match the SHA-256 the manifest records for
  it, so a `sdk-go.zip` left in dist by an earlier run with more targets
  no longer serves beside a narrower manifest. The overview page says the downloads are withheld and shows
  the `--only` command. Generate for the documented entities, or widen
  `Config.Entities` or set `IncludeGated`.
- **A scoped API token no longer passes a permission or role gate on its
  owner's roles.** `RequirePermission` now also needs a scope granting
  the permission, so a `customers:read` token minted by an admin no
  longer passes `RequirePermission("posts:write")`. `MCPRequireRole`
  (the control and log tools) and the default `/.debug` check need
  `role:<role>` in the token's scopes; `access.RoleScope("admin")`
  spells it. With `WithDebugAuthorize` installed, a token also needs
  `role:admin` before the custom check runs. A Decider still sees every
  role the caller holds. See `access-control.md`.
- **A password reset or account claim whose passkey, session,
  magic-link, token, OAuth unlink or password purge fails still runs the
  other purges** before
  it fails, so one failing store no longer leaves old sessions or tokens
  working. A failed claim leaves the account unverified, so the next
  proof retries it.
- **print's built-in gates refuse an API token without `print:read`**
  (`print.Scope`).
- **An OAuth link that lands during an account claim, reset or password
  change is taken back.** The link callback re-checks the session and
  password hash after the upsert and unlinks with a 401 if either
  moved, and `claimAccount` purges sessions before and after its
  unlink. A password change that a reset overtakes deletes the
  changer's fresh session.
- **Presence: another replica can no longer push live topics out of the
  cross-replica roster.** A full topic table takes a new topic only in
  place of one whose every entry has expired. A member's leave is
  queued under the same lock as the roster it reads, so an older
  heartbeat can no longer resurrect them.
- **Process modules: a revoke or upgrade cancels a spawn still starting
  under the old grants.** That child is killed instead of installed
  beside the new one, and its late failure no longer marks the new one
  failed.
- **Sandbox probes test what they claim.** P3 identifies the planted fd
  by device and inode, so the child's own descriptors no longer read as
  a breach on Linux. P4 proves each refusal against listeners the runner
  reached itself, and reports unreachable with none. P6 counts only
  `EAGAIN` and `EMFILE` as a cap, holds every child alive, and reports a
  timeout or a missing binary as unreachable. P3 and P6 have not run on
  Linux CI yet.
- **An abandoned or failed process-module spawn closes every fd it
  opened**, including both ends of the stdio pipes and the artifact.
- **Linear-time markdown for unclosed `[` and `](`.** Input with many
  unclosed brackets took quadratic time to render.
- **`Accept-Language` entries over 35 characters, or with characters
  outside a-z, 0-9 and `-`, are skipped.** A huge header no longer
  allocates megabytes per request.
- **`.env` expansion stops at its budget while it builds the value.** A
  line with many `${VAR}` references used to allocate the whole value
  first.
- **A `sensitive` config value is redacted in its escaped spellings
  too**: `%q`, `%+q`, JSON and URL encodings, up to two layers deep.
- **Email headers are folded.** No header line passes 998 octets, and
  long subject words and attachment names are encoded so they split
  cleanly.
- **`gofastr desktop keygen` refuses to write through a symlink** at the
  key or `.pub` path, and never leaves half a pair. An existing `.pub`
  is now refused instead of overwritten. New
  `fileperm.WriteNewOwnerOnly`.
- **The desktop updater refuses a mislabelled bundle.** `install`
  refuses an archive whose Info.plist version differs from the feed's
  or whose bundle identifier is another app's, and `gofastr desktop
  feed` refuses a `--version` that doesn't match the bundle.
- **`gofastr desktop build` on macOS refuses a `--version` that is not
  major.minor.patch** (new `desktop.ValidateAppVersion`). A bundle whose
  own version does not parse reports that, instead of "the update feed
  is invalid", and its scheduled update checks stop.
- **An exported static redirect page refuses unsafe schemes.** Its
  meta refresh, canonical link and anchor carried a `javascript:` or
  `data:` target live; a refused target becomes `#`.
- **The OpenAPI docs page link refuses unsafe schemes.** A base path of
  `javascript:` or `data:` produced a live `href`; it now falls back to
  `/openapi.json`.
- **CI's secret scan catches `key: "value"`** (YAML, struct literals),
  not only `key = "value"`.
- **Uploads refuse HTML or SVG markup saved as UTF-16 or UTF-32**, which
  the content check missed because the markup is NUL-interleaved and an
  XML parser decodes and renders it.
- **An ACP server with `AuthMethods` refuses `session/new`,
  `session/load` and `session/prompt` until `authenticate` succeeds.**
- **The blog example hides draft posts, and comments on them, from
  anonymous auto-CRUD reads**; callers holding `posts:write` still see
  them.
- **Deleting a record, or replacing its file, deletes the stored
  object.** A hard delete, a purge, `TypedQuery.DeleteAll`, and an update
  or upsert that replaced an `Image`/`File` value left the old object
  (and its renditions) in storage, still served at `/uploads/<key>` to
  anyone holding the link. They are now deleted after the write commits,
  unless another row of the table still names the key; a soft delete
  keeps them for a restore. See
  [uploads](framework/docs/content/uploads.md#when-stored-objects-are-deleted).
- **CRUD multipart writes refuse a cross-site sender; `_batch` and
  transitions take JSON only.** The write gate took
  `multipart/form-data` as a JSON stand-in, and a browser sends that type
  cross-site with no preflight, so a page on a sibling subdomain could
  create records, run `_batch` (which reads the body as JSON whatever the
  label) and fire transitions as the signed-in user. A multipart create
  or update now passes `handler.IsCrossSiteRequest` or answers 403, and
  `_batch` and transitions answer 415 to anything but
  `application/json`. See [uploads](framework/docs/content/uploads.md).
- **The admin's permission checks ask the Decider.** The
  `modules:manage` levers and the grant and revoke checks read the
  caller's permissions from `Config.Policy` alone, so an
  `access.Decider` that denied an admin one of them was never asked. Its
  deny now refuses; its allow still admits nothing the policy does not.
  See [admin](framework/docs/content/admin.md#process-modules).
- **The cross-site guard refuses a same-site request with a `null`
  Origin.** `handler.IsCrossSiteRequest` took `Sec-Fetch-Site:
  same-site` plus an absent or `null` Origin as safe, and a page on a
  sibling subdomain sending `Referrer-Policy: no-referrer` posts exactly
  that, so it passed every guard built on it: auth's form guards
  (login CSRF included), the admin and entity UI restore, purge and
  views posts, and Kiln's reset. A same-site request now needs an Origin
  naming the request's own host. See
  [security](framework/docs/content/security.md).
- **A cross-owner update or delete reaches the row owner's `_events`
  stream, not the writer's.** With the owner column Hidden (the default
  for an injected `OwnerField`), an update's record left it out, and a
  delete's payload is the key alone, so the event and its outbox row
  took the owner and tenant from the writer's context: Alice's
  `AllowCrossOwner` edit of Bob's row went to Alice's stream, Bob's
  missed it, and the outbox named Alice for erasure. The write now
  reads the row's owner and tenant before changing it, on update,
  delete, batch, transition, restore and purge. See
  [entity declarations](framework/docs/content/entity-declarations.md).
- **The `_events` read-scope filter compares values by column type.**
  It compared text, while List and Get bind the scope value typed, so a
  `neq` scope written `"1"` on a Bool column delivered every `true` row
  the list hides, and a Decimal `neq "1.50"` delivered Postgres's
  `"1.5000"`. Bool, Int, Float, Decimal and Timestamp values now compare
  as the SQL side reads them. See [events](framework/docs/content/events.md#read-scope).
- **A write's response runs the `BeforeGet` scope GET applies.** An app
  that scopes reads with a `BeforeGet` hook and grants writes more
  widely handed the writer every column of a row GET answers 404 for: a
  no-op `PATCH` of another team's row read it. Create, update, `_batch`
  create and update, and transition responses now run the chain after
  the commit and answer with the row's id when its clauses exclude the
  row or the hook errors; the write itself stands. A `BeforeGet` hook
  therefore also runs on those writes. See
  [hooks](framework/docs/content/hooks-and-transactions.md).
- **An entity endpoint's MCP twin asks for the API token scope its
  route does.** `auth.RequireAPIScopes` holds the HTTP route to
  `<table>:<verb>`, but the `MCPHandler` twin is registered straight on
  the MCP server, so an editor's `customers:read` token called a `posts`
  endpoint twin its route refused. The twin now needs `<table>:read`
  (GET, HEAD) or `<table>:write` (any other method) from a token caller
  before it runs or shows in `tools/list`, under `MCPGate` and `Public`
  too; sessions are not narrowed. See
  [entity declarations](framework/docs/content/entity-declarations.md).
- **`tenant.ResolveMiddleware` re-resolves the tenant on a held
  stream.** The CRUD event stream closed when a re-check placed the
  caller in another tenant, but a host middleware that called
  `SetTenantID` once replayed the connect-time tenant on every re-check,
  so a member removed from a tenant kept its create, update and delete
  feed until disconnect. `tenant.ResolveMiddleware(resolve)` sets the
  tenant from a server-side lookup and runs it again on each
  `handler.RecheckPrincipal`, so the stream closes at the next check.
  See [multi-tenant](framework/docs/content/multi-tenant.md).
- **A TURN refresh re-checks each RTC peer before minting its
  credential.** A joined socket re-checks its principal on every frame
  and every 30 seconds when idle, but a TURN refresh landing in that
  window still sent a revoked peer a fresh credential. The refresh now
  re-checks every peer first and closes the ones that no longer hold.
  See [rtc](framework/docs/content/rtc.md).
- **`App.CrudHandlerForEntity` is wired like `App.CrudHandler`.** It
  left out child hooks, the image pipeline and
  `WithStripUploadMetadata`, so the entity UI and `/admin/entities`,
  which build their handlers through it, cascaded writes past child hooks, saved
  images with no renditions and kept their EXIF metadata, GPS included.
  Both now build through one constructor.
- **A nested include runs its rows' read hooks when a `WireName` is in
  the way.** The child read hooks looked up every level's rows with the
  root entity's wire keys, while each level's rows sit under the key its
  parent entity's wire map gives the relation. A `WireName` on either
  entity, for a field sharing the nested relation's name, hid the rows
  from the lookup, and `?include=author.card` served the cards' stored
  values past their `AfterList` and `AfterGet` masks.
- **A list serves every field under its `WireName`.** Since v0.85.0 a
  list that selected every visible column took a cached key list built
  without `WireName`, so `GET /cards` served `card_number` where
  `GET /cards/{id}` and `?fields=` served `pan`. An `AfterList` hook
  deleting the documented key removed nothing and the value shipped, an
  aliased JSON column went out as a string, and a field renamed with an
  API version's `Rename` kept its old name on the list.
- **A schema-qualified `Through` no longer skips its join entity's
  gates.** The join entity was found by an exact table-name match, so
  `Through: "public.memberships"` against a `Table` of `memberships`
  found none, and includes and nested filters joined the table with no
  owner, tenant, soft-delete or read predicate on the links. Boot and
  every read now refuse a `Through` and a join entity's `Table` that
  differ only in naming a schema; spell them the same. See
  [entity declarations](framework/docs/content/entity-declarations.md).
- **A `has_one` or `has_many` cascade create refuses a child link it
  cannot write.** The child's create skips a `ReadOnly` or `Hidden`
  field, or one it does not declare, so a post created with nested notes
  whose `post_id` was hidden stored the notes with no parent while the
  response showed them attached. The nested create now answers 400
  `validation.cascade_key_not_writable` and rolls the parent back, as
  the `belongs_to` arm already did.
- **A cascade write's response masks grandchildren.** The body ran the
  read hooks of the children it nested but not of the rows nested under
  them, so a field the grandchild's entity masks in `AfterList` or
  `AfterGet` came back raw from a `POST` or `PATCH` that wrote two
  levels. Every written level now runs its own entity's read hooks. See
  [hooks and transactions](framework/docs/content/hooks-and-transactions.md).
- **An evicted `_events` stream leaves at once.** Under
  `SeatOverflowEvictOldest` an evicted stream parked in a write to a
  client that stopped reading held its handler until the 10-second write
  deadline fired, so a caller reconnecting in a loop kept more handlers
  than its seat cap. Eviction now cuts the parked write with a past
  deadline, as the SSE broker does. See [events](framework/docs/content/events.md).
- **Entity UI screens read a related entity only where `?include=`
  could.** The relation picker, relation labels, facets and headings
  checked the related entity's session, owner, tenant and RBAC gates but
  not its exposure, so any posts writer could list every user's id and
  name through `_pick?field=author_id` while users had CRUD off, and a
  `posts:write` token read them without `users:read`. They now go
  through the new `crud.CanReadRelatedScoped`, which also refuses an
  entity with `Exposure.CRUD` false and a token without `<table>:read`.
  Labels, facets and a named CSV export (`_export.csv?_id=`) also leave
  out a row a resource-aware Decider refuses by id. See
  [entity UI](framework/docs/content/entityui.md#the-read-gates).
- **A narrow API token no longer reads the A2A tasks a stronger credential
  ran.** Tasks were keyed by the owner id alone, so a `customers:read`
  token listed its user's session tasks with their artifacts, and could
  resume, cancel and subscribe to them. A task records its creator's
  scopes (`TaskRecord.Scope`), and a scoped caller reaches only the tasks
  made under the same scopes. `WithA2A` wires it; a hand-built server sets
  `a2a.Config.Scopes`. The SQL store adds a `scope` column on open. A
  custom store that filters `ListTasks` by `ListQuery.Scoped` declares
  `a2a.ScopeFilter`; one that does not is walked, so a scoped list's
  `totalSize` and page token count only the caller's tasks. See
  [A2A](framework/docs/content/a2a.md#auth-and-owner-scoping).
- **An access decider binds a generated screen's role gate.** The
  `authPolicy` the app spec writes into `app.go` read the user's roles
  itself, so an `access.DeciderMiddleware` deny never reached a screen with
  `access: { role: ... }`, and a scoped API token passed on its owner's
  roles. It composes `auth.SessionPolicy` and `auth.RolePolicy` now; an app
  generated earlier replaces the function by hand. Meridian carries the
  same function. See [app spec](framework/docs/content/app-spec.md).
- **A session-scoped harness token lists only its own sessions.**
  `GET /v1/sessions` (live and `?past=true`), the MCP
  `harness.list_sessions` tool and the `harness/v1://sessions` resource
  listed every session, past ones with the first 80 bytes of their
  first prompt. Each now keeps the sessions the token's claims allow.
  See [harness architecture](framework/docs/content/harness-architecture.md#rest-api).
- **An OAuth sign-in no longer survives an account claim that overtakes
  it.** A returning sign-in refreshed its link's profile with an
  upsert, so a claim that removed the link between the lookup and the
  refresh had the squatter's link put back, and the session minted
  afterwards outlived the claim's purge. The refresh is UPDATE-only
  through the new `OAuthProfileRefresher` (implemented by
  `EntityUserStore`; a custom store without it keeps the link-time
  profile), and the callback resolves the link again after minting and
  answers 401 when it no longer names the account. See
  [auth](framework/docs/content/auth.md#proving-the-mailbox-claims-an-unverified-account).
- **An API token or passkey created across a reset no longer survives
  it.** `POST /auth/tokens` and `POST /auth/passkey/register` read the
  caller before the body, and a reset revokes tokens and passkeys once.
  A request held open with its body withheld until the reset finished
  stored a lasting credential after that purge. Both now read the
  password hash before the body and, when it moved or the request's
  session is gone, undo what they stored and answer 401. The undo runs
  detached from the request, so a client that hangs up after the insert
  cannot cancel it. See [auth](framework/docs/content/auth.md).
- **A stale password change no longer overwrites a completed reset.** A
  change verified the current password, computed the new hash, then wrote
  it keyed on the user id alone, so a reset that landed in between was
  replaced by the stale request's password and the change handed out a
  fresh session. The write now needs the verified hash to still be stored
  (new optional `PasswordSwapper`, implemented by `EntityUserStore`), and
  a change that loses answers 422 like a wrong current password. See
  [auth](framework/docs/content/auth.md).
- **OIDC no longer borrows `email_verified` for a different address.** An
  id_token with an email but no `email_verified` took the flag from
  userinfo even when userinfo named another address, so the id_token's
  email came out verified and the callback could auto-link that
  address's account. The userinfo flag now counts only when its email is
  the one kept. See [auth](framework/docs/content/auth.md).
- **A password reset overtaking a passkey sign-in leaves no session, and
  a failed session purge is no longer reported as success.** A passkey
  sign-in that recorded its use just before a reset minted its session
  afterwards, for a key the reset had removed. It now re-reads the key
  after minting, and the reset removes passkeys before it revokes
  sessions. A reset or password change whose session purge failed
  answered `{"updated": true}` while the old sessions kept working; the
  reset answers 500 `auth.reset_incomplete` and the change 500
  `auth.change_incomplete`. See [auth](framework/docs/content/auth.md).
- **A sibling subdomain can no longer plant the OAuth state cookie.** The
  browser-binding cookie was `gofastr_oauth_state` even over Secure, so a
  page on another subdomain could set it for the parent domain with its own
  flow's state and sign a victim into the attacker's account at the
  callback. With `SessionSecure` on it is now `__Host-gofastr_oauth_state`;
  DevMode keeps the plain name. A flow started before the upgrade and
  finished after it fails once and is retried. See
  [auth](framework/docs/content/auth.md).
- **A soft-deleted user no longer signs in.** With `Scope.SoftDelete` on
  the users entity, `EntityUserStore` looked accounts up by id or email
  alone, so a trashed account kept its password login and every session,
  API token and JWT. The store now skips a row with `deleted_at` set, and
  `GET /auth/me` answers 401 for a session whose account is gone. See
  [auth](framework/docs/content/auth.md).
- **BREAKING: the MCP control tools and the log MCP tools need the
  `admin` role.** `app_module_enable`/`app_module_disable`
  (`WithMCPControl`) and battery/log's `log_*` tools asked only for a
  signed-in caller, so on an app with open sign-up any account could turn
  a module off for everyone or read every caller's paths and IPs. They now
  run behind the new `framework.MCPRequireRole("admin")`;
  `framework.WithMCPControlGate` and `log.Config.MCPGate` replace it.
  `MCPRequireUser`, `MCPRequireRole` and `/.debug` now honour an access
  `Decider`'s deny. `auth.RoleGateCapability` moved to
  `access.RoleGateCapability`. See
  [agent-ready](framework/docs/content/agent-ready.md) and
  [log](framework/docs/content/log.md).
- **The bodyless 2FA routes refuse a cross-site POST.** `/2fa/disable`,
  `/2fa/backup-codes` and `/2fa/enroll` read no body, so another site's
  hidden form could submit them with a stepped-up session cookie and turn
  the user's second factor off or burn their backup codes. They now run
  the cross-site guard login and logout use. See
  [auth](framework/docs/content/auth.md).
- **BREAKING: an API token reaches the admin only with `admin.AdminScope`.**
  The gate read the token's owner and its roles, never the token's
  scopes, so a `customers:read` token an admin minted could read the
  audit log and toggle process modules. A token now needs
  `"admin:access"` (or `"admin:*"` or `"*:*"`); sessions are unaffected.
  See [admin](framework/docs/content/admin.md).
- **BREAKING: the admin's platform pages refuse a tenant-scoped caller.**
  Queue jobs, auth accounts, role grants and process modules carry no
  tenant, so a tenant's admin could list, replay, grant, assign roles to
  and toggle modules for every tenant. The Jobs, User roles, Roles and
  Modules pages and their posts now answer 403 to a caller with a tenant,
  and the sidebar, palette and dashboard leave them out. See
  [admin](framework/docs/content/admin.md).
- **The admin's audit page and activity respect owner scope.** On an
  entity with an `OwnerField`, they named another owner's record from
  its stored copy and listed its old and new values, though the record
  screen refuses it. Such a record now shows by its id alone. See
  [admin](framework/docs/content/admin.md).
- **A lower admin tier can no longer strip or self-assign roles above
  it.** User roles checked only the roles a save sent, so a support-tier
  admin could clear a superadmin's roles, and a role the policy grants
  nothing (one checked by name with `auth.RequireRole`) passed the tier
  check for anyone. The check now covers every role a save adds or takes
  away, and a permissionless role needs the caller to hold it or the
  wildcard. See [admin](framework/docs/content/admin.md).
- **BREAKING: `RevokeGrants` removes the grants it names.** It replaced
  the module's grant set with them, so the admin's Revoke on
  `invoices:write` left a read-and-write module write-only. It now keeps
  the rest, refuses an empty list (which used to revoke everything), and
  writes through the new `ProcessModuleStore.RemoveEffectiveGrants`, a
  compare-and-swap so concurrent revokes both stick. See
  [process modules](framework/docs/content/process-modules.md).
- **The relay keeps the app's headers and the vendor's hints apart.**
  It forwarded `X-Gofastr-Embed`, the embed grant an embed frame sends
  on every fetch, to the vendor; every `X-Gofastr-*` header is now
  stripped inbound. An upstream `103 Early Hints` is no longer
  forwarded, so its unfiltered `Link` preload stays out of the browser,
  and the security headers the app set before the relay ran survive
  it (ReverseProxy cleared them). `Service-Worker-Allowed` is stripped
  outbound, so vendor script cannot scope a worker over the whole app.
  See [relay](framework/docs/content/relay.md).
- **BREAKING: a process module's response headers are allow-listed.**
  The proxy copied every header the child sent, so an untrusted module
  could label a `text` body `text/html` and run script on the host
  origin, set host cookies, or replace the host's Content-Security-Policy.
  Only cache and language headers cross now, `Vary` adds to the host's,
  `Content-Type` is always the body kind's with `nosniff`, and a status
  outside 200–599 answers 503 instead of panicking. See
  [process modules](framework/docs/content/process-modules.md).
- **`golang.org/x/net` is v0.62.0.** v0.59.0 carries an advisory in
  `html.Parse`, which `battery/sitesearch` calls to extract page text.
  The bump brings the other `golang.org/x` modules to their current
  releases, in the root module and in `battery/print/chromepdf`.
- **`middleware.RateLimit` keeps an exhausted key denied.** Its idle
  reap and its `MaxKeys` shed both read the refill clock, which a denied
  request does not move, so any traffic from other keys dropped a key
  that had spent a `1`-per-hour budget and its next request got a full
  bucket. An hourly login limit became a five-minute one. The reap now
  drops only buckets that have refilled to capacity, and the shed goes
  nearest-to-full first. See
  [security](framework/docs/content/security.md).
- **A process module that stops reading cannot pile up host goroutines.**
  A served reverse call released its serve slot when the handler
  returned and then waited on the write, so a child that sent paced
  calls and never read stdin parked one host goroutine per call with no
  bound. At most `Limits.Inflight` answers now wait behind a blocked
  write; the next one faults the peer with `moduleproto.ErrStalled`, and
  a faulted peer starts no handler for a frame it reads after the fault.
  An answer leaves the count when its write starts, before the child can
  read it, so a child with `Limits.Inflight` calls open is never taken
  for one that stopped reading. See
  [process modules](framework/docs/content/process-modules.md).
- **BREAKING: a custom CSRF cookie name gets the `__Host-` prefix on
  HTTPS.** `middleware.CSRF` prefixed only its default name unless the
  host set `HostPrefixWhenSecure`, so the documented production config
  (`CookieName: "fui_csrf"`) sent a plain cookie. The token is not tied
  to a session, so a sibling subdomain could fetch one, plant it as a
  `Domain` cookie and post it with the victim's credentials.
  `HostPrefixWhenSecure` is removed: every name, `auth.WithCSRFCookieName`
  included, becomes `__Host-<name>` on a secure request, and
  `NoHostPrefix` opts out. `CSRF` panics on a `CookiePath` other than
  `/` unless `NoHostPrefix` is set, since the browser drops a `__Host-`
  cookie with any other path. See
  [security](framework/docs/content/security.md).
- **A quoted local part cannot rewrite the SMTP envelope.** The SMTP
  sender wrote `net/mail`'s unquoted local part into `RCPT TO:<…>`, so
  `"attacker@evil.test> ORCPT=rfc822;x"@company.test` closed the path
  early: a DSN-capable relay reads `attacker@evil.test` as the recipient
  and the rest as an `ORCPT` parameter. The envelope
  now quotes a local part that needs it, and refuses one holding `<`, `>`
  or `@`. See [email](framework/docs/content/email.md).
- **An idempotency `Principal` that returns `""` caches nothing.** Every
  such request shared one namespace, so a second caller who sent the
  same key and body got the first caller's stored response. The
  documented `WithIdempotency` example returned `""` for every request,
  since its `auth.SessionFrom` lookup runs before the app's auth; the
  example now keeps the default, which keys on the request's
  credential. Return a constant such as `"anon"` to share a namespace on
  purpose. See [idempotency](framework/docs/content/idempotency.md).
- **A generated MCP tool runs against the host the caller reached.** The
  tool re-dispatched its CRUD request with `httptest`'s `example.com` as
  `Host`, so host middleware (a site or tenant chosen by host, a host
  allowlist) served it as some other site. See
  [mcp](framework/docs/content/mcp.md).
- **Web Bot Auth signs the path as sent.** `@path`, `@request-target`
  and `@target-uri` read the percent-decoded path, so a signature over
  `/a/b` also verified on `/a%2Fb`, a different route, and a bot that
  signed `/docs/a%20b` as RFC 9421 says failed to verify. They now carry
  the path before decoding. See
  [Web Bot Auth](framework/docs/content/web-bot-auth.md).
- **A harness REST call runs as its token's identity class.** The REST
  transport stamped every request `human` with a fresh client id, so an
  `agent` token could answer a permission prompt that needs a human.
  It now dispatches as the verified token: its `identity_class`, and
  one client id for every request made with it, so a turn's originator
  names the token that started it. WebSocket already used the token's
  class. See
  [harness architecture](framework/docs/content/harness-architecture.md).
- **Harness quiet mode prompts for a Bash call that sets `env` or
  `cwd`.** Quiet mode and argv-glob rules matched the command string
  alone, so `{"cmd":"git diff","env":{"GIT_EXTERNAL_DIFF":"sh -c …"}}`
  ran its program with no prompt. A Bash call's argv summary now spells
  the `cwd` and `env` in front of the command, which no quiet-listed
  prefix or bare-command rule matches. `git branch` is quiet only when
  it lists, so `git branch -D main` prompts. See
  [harness architecture](framework/docs/content/harness-architecture.md).
- **BREAKING: `gofastr harness --listen` binds loopback only.** The
  chat page at `/` embeds a 24 h human-class token and takes no auth,
  and its `Host` check does not stop a direct peer, so
  `--listen 0.0.0.0:PORT` or a LAN address handed the token to anyone
  on the network who sent `Host: localhost`. A bind whose address is
  not loopback now fails at startup; forward the port over SSH to reach
  it from another machine. The docs described a LAN mode gated on TLS
  and `--auth-token-file`; neither flag existed, and that text is gone.
- **A server action on a dynamic screen runs its `Requires` resolvers.**
  An action POST that named no page, or a page of another screen, was
  judged by `(*App).PatternPolicy`, which ran the screen chain and the
  region-guard check but skipped the eager resolvers. A screen like
  `/projects/:id` whose access comes from a `Requires(project)` resolver
  refusing with `ErrNotFound` ran its actions for any caller holding a
  self-minted session. `PatternPolicy` now answers Block for a dynamic
  pattern that requires a resolver, as it does for one under a region
  guard, so such an action must name a page of its screen, and the
  llm.md index and pattern doc withhold that screen's metadata.
- **A static export leaves out gated widgets' chrome.** The export wrote
  every registered widget's chrome HTML to
  `/core-ui/widget/<name>/chrome`, including widgets with
  `RequireSession` or `RequireAuthenticated` whose live endpoint refuses
  an anonymous caller, so their slot HTML was public on the static host.
  Such a widget now gets its style sheet (ungated live too) and no chrome
  file.
- **`fs.writeText` stays inside its grant.** The fs allow-list checked a
  path's cleaned form, then resolved the raw path and passed whenever
  that resolution failed, which it always does for a file that does not
  exist yet. A save target spelled `dir/link/../name`, which cleans to
  the grant while the OS follows the link, or one whose missing parent
  became a symlink after the grant, was written outside the granted
  directory. A path must now arrive in the exact clean form the dialog
  returned (the dialogs return that form), and a missing file resolves
  through its deepest existing parent.
- **Desktop events and deep links reach only the app's own pages.** The
  battery evaluated every event, posted window message, menu navigation
  and deep-link navigation in whatever page a window showed. A window
  that had followed an external link to another site handed that site
  each of them, an OAuth callback's code included, once the page
  defined `window.__gofastr.desktop._dispatch` or
  `window.__gofastr.navigate`. Each script now runs inside
  `if (location.origin === "<app origin>") { ... }`, checked in the
  page when the script runs.
- **Cancelling a Kiln agent turn kills what the adapter started.** The turn
  ran in its own process group, but cancelling it killed only the adapter's
  own pid, so a shell it had backgrounded kept running and could keep
  calling Kiln's tools and changing the world. Cancel, supersede and the end
  of a turn now kill the whole group on Unix.
- **The desktop updater holds redirects to its https rule, and deep links
  keep their escaped path.** A feed, signature or archive URL that
  redirects to plain http off 127.0.0.1 now fails before the target is
  asked. The default deep-link mapping keeps the path escaped, so
  `%3F` and `%23` no longer start a query or fragment, and the navigate
  grammar refuses percent-encoded dot segments (`%2e%2e`) for every
  caller. A deep link that reaches an app with no `DeepLink` config is
  dropped instead of panicking, and a capability named `available`,
  `call`, `on`, `off` or `manifest` is refused at `Register`, since the
  bridge would overwrite the runtime's own `NS.desktop` member.
- **An A2A unknown-skill refusal lists no skill ids.** A message naming
  a skill the server does not have, or naming none with several
  registered, was rejected with `available:` and every registered id,
  entity skills the MCP gate withholds from the caller included. The
  refusal now names only the skill asked for (`no skill named "x"`), or
  says `the message names no skill`; the agent card lists the callable
  skills.
- **Name and extension comparisons fold ASCII only.** Go's
  `strings.ToLower` maps U+212A KELVIN SIGN onto `k` and U+0130 onto
  `i`, and several comparisons folded both sides with it. A reference
  URL such as `/docs/api/entities/%E2%84%AAiosk` served the `kiosk`
  page, and a `sdkdocs.Config.Entities` name matched the same way.
  `upload.ValidateExt` let `photo.gİf` through an allow list of
  `gif`. A migration dropping two tables whose names folded alike left
  one of them out of the drop plan. The generated
  CLI's entity selection and the GOFASTR1808 fallback comparison
  folded the same way. All of them now fold A–Z only, and an
  upper-case ASCII spelling still matches.
- **An MCP notification stream closes when its caller changes tenant.**
  The stream re-checked the caller before each delivery and ran the
  gates on the result, but a gate that does not read the tenant kept
  sending the connect-time tenant's resource URIs and `list_changed`
  notices to a member removed from it. The stream now closes when the
  re-check (`tenant.ResolveMiddleware` installs one) returns another
  tenant, as a tenant-scoped CRUD event stream already did.
- **BREAKING: `admin.AuthPrincipal(am, member)` checks that a bulk
  job's creator still belongs to its tenant.** The principal re-read the
  creator's roles before every chunk but stamped the confirm-time tenant
  unchecked, so a queued bulk delete or update kept writing tenant A's
  rows after its creator left A. It now asks `member`
  (`admin.TenantMember`) first and stops the run on false or an error.
  A tenant run with a nil `member` is refused; `admin.AnyTenant` admits
  every run, for an app whose users never leave a tenant.
- **The UI host and the static export no longer publish dotfiles.**
  `uihost.WithStaticDir` and `SetStaticFS` served every file under
  them, `.env`, `.git/config` and `web.config` included, and the
  static export copied the same files, and every `Builder.ExtraDirs`
  file, into the public tree. Both now refuse what `core/static`
  refuses: a path with a dot segment, or a server config file name
  (`web.config`, `global.asax` and their kin, any case). A refused
  request falls through to page rendering, and the export logs each
  file it skips. Files under a leading `.well-known/` stay public
  (RFC 8615) unless they are dotfiles themselves, and the export keeps
  a root `.nojekyll`. An export that needs another dotfile, such as an
  Apache `.htaccess`, copies it into the output after the build. The
  new `static.Refuses` exposes the check.
- **An embed origin with a Unicode host is refused instead of folded
  onto an ASCII one.** `NormalizeOrigin` lowercased the host with
  `strings.ToLower` before the check that refuses non-ASCII bytes, and
  Go folds U+0130 to `i` and the Kelvin sign to `k`, so an allowlist
  entry `https://İ.example` was accepted as `https://i.example`. The
  fold is ASCII-only now, and such an entry fails at boot like any
  other Unicode host.
- **A CONNECT request no longer confirms a disabled module's
  methodless or CONNECT route.** For CONNECT, net/http names the
  trailing-slash redirect by its bare path, which matched no gate key,
  so `CONNECT /admin/panel` answered 307 to `/admin/panel/` where every
  other request gets a 404. The router now gates that redirect on the
  route's methodless and CONNECT keys. Routes registered with
  `Get`/`Post` and the other method helpers were not affected.
- **A module's migrations run under the module role alone.** The
  coordinator built the role's DSN from the admin DSN with the role's
  login in place, and kept the rest of the query. lib/pq sorts its
  key=value pairs, so an admin `user` or `password` in the query could
  outrank the role's login, and any key it does not know, such as
  `options` or `search_path`, went to the server as a startup setting
  that overrode the role's. The role DSN now keeps only the keys that
  say where the server is and how to reach it (`host`, `port`,
  `dbname`, the `ssl*` settings and a few more, listed in
  process-modules.md) and drops the rest.
- **The signed-in `/openapi.json` and `/api/llm.md` follow a scoped
  token's scopes.** A token holding `customers:read` got the schema and
  paths of every entity that declares no read permission, while
  `RequireAPIScopes` refused the reads. Both now keep only the entities
  whose `<table>:read` scope the token or embed grant holds. Sessions
  are unaffected.
- **A `RequireAuthenticated` widget refuses a scoped API token.**
  `TokenMiddleware` loads the token's owner as the user, which satisfied
  the gate, so a leaked token read the widget's state, chrome and RPC
  whatever its scopes. The gate now refuses a token as it refuses an
  embed grant, and as `auth.RequireSession` does.
- **A `many: true` relation field's id follows the target's scopes on
  write.** Define derives no relation for a Many field, so the
  belongs-to scope check never read its column, and a create or
  update stored another owner's id there. The id is now checked as a
  belongs-to's is: a target the caller cannot read answers 404, and an
  update that keeps the stored id passes.
- **The harness websocket refuses a malformed handshake.** `/v1/ws`
  answered 101 to any request with an Upgrade header and a non-empty
  key, POST included: the upgrade-desync shape `core/stream` already
  refused, where a proxy that does not see an upgrade waits for an
  ordinary response and reads the frames as one. It now answers 400
  unless the request is GET with a `Connection: Upgrade` token,
  version 13 and a 16-byte key.
  `core/stream` exports the check as `CheckUpgradeRequest`, which
  `Upgrade` runs too.
- **BREAKING: the print battery refuses a root `PathPrefix`.** `"/"` trimmed to
  `""`, so the routes mounted at the root while the embed reservation
  still named `/print`, and the embed gate did not refuse a grant on
  the PDF route, which starts headless Chrome. `RegisterRoutes` (and
  so `Init`) now fails on a prefix that is not a path below the root,
  `"/"` or one with no leading slash, so the reservation always names
  what is mounted.
- **BREAKING: a desktop window stays on the app's origin.** The
  runtime let an external link navigate the window in place, and no
  shell had a navigation policy, so a foreign page ran in the app's
  window and looked like the app. Each shell now asks the battery
  before a main-frame navigation commits (macOS
  `decidePolicyForNavigationAction`, Windows `NavigationStarting`,
  Linux the main-frame response in `decide-policy`), and the battery
  allows only its loopback origin and `about:blank`. An external link
  does nothing. `Config.DangerouslyAllowForeignNavigation` restores the
  old behaviour, for an in-window OAuth flow say. A custom `Shell`
  enforces the new `WindowConfig.AllowNavigation`;
  `desktoptest.Harness.AllowsNavigation` checks it in a test. On Linux
  the refused request still reaches the other host; nothing it returns
  renders. The Windows hook has no test runner yet.
- **BREAKING: one client can no longer hold every island stream.** A
  session costs one same-origin POST and the caps counted streams per
  session and per replica, so 256 minted sessions of 16 streams took all
  4096 default slots and every other visitor's stream answered 429. A
  per-client cap now counts the signed-in user, or an anonymous caller's
  address (IPv6 by /64), across its sessions: 256 by default,
  `island.WithPeerStreamCap` to change it. Behind a reverse proxy, list
  it with `uihost.WithTrustedProxies` (`island.WithTrustedProxies`), or
  every anonymous visitor shares the proxy address's 256 streams; the
  first such refusal logs a warning. `Manager.ConnectSession` takes the
  client key as a second argument (`Manager.ClientKey`; `""` for a
  server-side subscription).

- **BREAKING: idempotency replays are keyed on the tenant and the held
  scopes.** A credential that switched its server-side tenant and resent
  the same key, path and body got tenant A's response, and tenant B's
  write never ran. `WithIdempotency` now runs inside the app's middleware,
  after tenant resolution, and keys a replay on the resolved user, tenant
  and sorted scopes (`IdempotencyConfig.Scope`), so a narrower token gets
  its own replays. Responses cached before the upgrade miss once.
- **BREAKING: A2A tasks are bound to the tenant they ran in.** A user
  who left a tenant could still read that tenant's task results. Tasks
  record the creator's tenant, list, get, push-config and stream calls
  check it (`a2a.Config.Tenant`), and the store reads on the request
  context. A custom `a2a.Store` must persist the tenant.
- **BREAKING: `RequireAPIScopes` reads the resource from the route's entity.** With
  a table named `v1`, a record id was checked as the scope resource, and
  `/api/v1/_batch` asked for `_batch:write`. Every route an entity
  registers (CRUD and its subroutes, `Endpoints`, EntityUI's
  `_bulk`/`_export.csv`/`_pick`, the admin entity API) now requires
  `<table>:<verb>` wherever it is mounted, through the new
  `router.ResourceGroup`, `router.RouteResource` and `crud.EntityRouter`.
  A custom mount whose path differs from its table (`/people` on
  `persons`) now needs the table's scope.
- **The `_events` stream re-checks read scope at delivery.** The first
  private row emitted after an `Unrestricted` role was removed still
  reached the subscriber.
- **The sdkdocs drawer follows `Config.Policy`.** An anonymous caller
  under a denying policy got the drawer's list of every documented
  entity.
- **Embed framing keeps every CSP policy.** A second
  `Content-Security-Policy` field was dropped and a comma-joined second
  policy was rewritten as part of `frame-ancestors`. Each policy now keeps
  its own directives.
- **The semantic watcher stays inside its root.** A symlink to
  `~/.ssh/id_ed25519` was indexed and searchable, and one to `/dev/zero`
  read without bound. The watcher opens regular files through `os.Root`,
  never blocks on a FIFO, and stops at `MaxFileSize`.
- **The duplicate-registration notice goes through a bounded queue.** A
  stalled mail provider piled up one goroutine per duplicate signup. The
  notice and password reset share a queue with a 30 s deadline per send,
  stopped by the new `CorePlugin.OnStop`.
- **`gofastr dev` on a non-loopback bind hides build output and
  livereload.** A LAN peer read the hold page's build log (absolute paths,
  quoted source) and could open unlimited `/__livereload` streams.
  `GOFASTR_DEV_LIVERELOAD_EXPOSE=1` keeps livereload on.
- **webmcp-remote-assist closes an expired credential's sockets and
  limits incoming frames.**

## [0.95.0] - 2026-10-26

### Added
- **An icon catalog sourced from Feather and Lucide (#599).**
  `framework/ui/icons` holds 291 outline drawings and 13 aliases on the
  24-unit grid, each with a PascalCase constant (`icons.Search`,
  `icons.ArrowLeft`) and a category. `sources.json` pins every file to
  its upstream URL, retrieval time and SHA-256, and
  `THIRD_PARTY_NOTICES.md` plus `LICENSE.feather` and `LICENSE.lucide`
  carry the licenses. `ui.Icon` reads the catalog, so the hand-drawn
  framework icons are replaced by their upstream drawings, and every
  existing name still resolves. `ui.RegisterIcon` overrides only the
  exact name it is given: overriding `check` leaves the `success`
  alias on the catalog drawing. `go generate ./framework/ui/icons`
  rebuilds the catalog offline from the checked-in SVGs and fails on a
  checksum mismatch.
- **A searchable icon browser.** `gallery.RenderIconCatalog` renders
  the catalog with a search box and category filter that live in the
  URL query, and each tile copies the icon's name. The gallery shows
  it at `/components/icon`. Two kit options came with it:
  `CopyButtonConfig.Tile` draws a whole-tile copy control with the
  glyph above the label, and `FilterToolbarConfig.ClientNavigation`
  submits the toolbar's GET form through the client navigator instead
  of a full page load. Leave `ClientNavigation` off when a shared
  layout also renders content that depends on the query, because the
  client navigator keeps the layout.
- **An experimental Linux desktop shell (#561).** `battery/desktop/linux`
  runs the app in GTK 3 and WebKitGTK 4.1 on linux/amd64 and
  linux/arm64, built with `CGO_ENABLED=0`. It supports native menus,
  file and folder pickers, permission dialogs, clipboard text,
  secondary windows and PNG snapshots. `gofastr desktop build` on Linux
  writes a plain ELF executable to `dist/<Name>`. Tray icons, deep-link
  registration, custom title bars and transparent windows are not
  supported yet; a configured tray returns an error. See
  `framework/docs/content/desktop.md` for the runtime packages.

### Changed
- **Editing a release note no longer re-runs most of the test suite.**
  The retired-markup check that test binaries and `gofastr dev` run
  over rendered pages loaded the whole migration registry, so `framework`
  and `framework/uihost` embedded every `internal/upgrade/releases/*.yml`
  file, and a change to any of them selected 104 packages and 10 browser
  CI jobs. The check now reads `internal/retired/retired_table.gen.go`,
  a table of the retired kit class and attribute names generated from
  the registry by `go generate ./internal/retired`. A release-note edit
  now selects 14 packages and 2 browser jobs, `gofastr-cli` and
  `meridian` (both build the `gofastr` CLI, which ships the registry for
  `gofastr upgrade`). `TestTableIsCurrent` fails with the regenerate
  command when the table is out of date: a breaking note added or
  dropped a kit name, the change text of a note that retires one was
  edited, or a release file holding one was renamed. Regenerating it
  then runs every package that renders through the check. The check
  flags exactly the names it flagged before. The version checks the CLI
  shares with the registry moved to `internal/semver`.
- **`cmd/gofastr` is a package per subcommand (#589).** It was one
  `package main` with 1,413 tests, so a change to any subcommand, or to
  any of the 224 packages its tests reached, reran all of them: 640s of
  a CI run. `main.go` now parses the command line and calls
  `cmd/gofastr/internal/<name>cmd` (`initcmd`, `generatecmd`, `devcmd`,
  `verifycmd`, …). The app spec parser is `internal/appspec`, the app
  generator `internal/appgen`, and the helpers their tests share
  `internal/clitest`. Each test moved with the code it covers, so a
  change to one subcommand runs that package's tests, the cross-command
  tests left in `cmd/gofastr`, and the packages that run the built
  binary. The commands, their flags and `-ldflags "-X main.version=…"`
  are unchanged.
- **`cmd/check-embed/embedcheck` type-checks its fixtures once per run
  (#589).** The tests share one load of the fixture module, plus one
  load of the mutated fixtures, instead of loading it in every test: the
  package runs in about 3s, down from about 40s. The fixture module
  moved out of the package to `cmd/check-embed/testdata`, so editing a
  fixture no longer selects `cmd/gofastr` and the other packages that
  import `embedcheck`.
- **`internal/upgrade`'s tests are split by what they read (#589).**
  The parser tests and the release-recipe gates stay in
  `internal/upgrade`. The `examples/` check is
  `internal/upgrade/examplescan`, one scan shared by the v0.86.0 and
  v0.87.0 assertions. The release-note scans are
  `internal/upgrade/releasetest` (v0.86.0 on; new release tests go
  there), `releasetest/mid` and `releasetest/old`. A CHANGELOG edit or an
  `examples/` edit no longer reruns every scan, and none of these
  packages takes more than about 5s, down from about 18s for the one
  package.
- **`cmd/affected` tells the command it wraps what changed (#589).** A
  command run after `--` gets the change set in
  `GOFASTR_AFFECTED_FILES`, one repo-relative path per line. When the
  run tests every package (`-all`, `GOFASTR_TEST_ALL=1`, a `go.mod`
  change, a base it cannot resolve, or a change set longer than 64 KiB)
  the command gets `GOFASTR_TEST_ALL=1` instead.
- **A pull request builds one generated app (#589).** `cmd/gofastr`'s
  tests generated and compiled about 65 apps per run: every example
  twice, and one small app per generator feature, each build compiling
  most of the framework. A pull request now builds one, in
  `TestUnionApp` (`internal/appgen`), whose app spec
  (`internal/appgen/testdata/union/gofastr.yml`) uses every generator
  feature that fits in one app. The others call `clitest.VariantBuild`
  and run on every CI run that is not a pull request, under
  `GOFASTR_TEST_ALL=1` (`./scripts/test-all.sh` sets it), or when the
  change touches an input they name, such as an example's
  `gofastr.yml`.
  `TestExampleAppSpecsGenerateAndCompile` is gone:
  `TestExampleAppSpecsBoot` builds each example the same way before it
  boots it. On a framework change, the `cmd/gofastr` package's tests
  take about 49s instead of 110s and `internal/appgen`'s about 28s
  instead of 79s (local run, warm build cache).
- **framework/ui's repo-wide gates moved to
  `framework/ui/internal/treegate` (#589).**
  `TestNoRemovedAliasTokenReferences` reads every non-test Go and JS
  file under `framework`, `core-ui`, `battery`, `kiln`, `cmd/gofastr`
  and `examples`, so a change anywhere there reran framework/ui's 68
  browser tests. It, and the inventory checks against `doc.go` and
  `framework/docs/content/ui-new-components.md`, now live in a package
  of their own that such a change selects instead. That package runs in
  under a second.
- **A CHANGELOG edit no longer reruns the `framework` and
  `framework/docs` tests (#589).** Every pull request edits
  CHANGELOG.md, and three checks that read it lived in those two
  packages, which take about 2.5 and 1.5 minutes in CI. The check that
  README.md and CHANGELOG.md state the contract rule count moved to
  `framework/contracts/internal/docgate`, so a README edit no longer
  reruns `framework` either. The two checks that CHANGELOG.md and the
  upgrade registry agree on which releases are breaking moved to
  `internal/upgrade`, next to the CHANGELOG check already there. A
  CHANGELOG edit now selects those two packages, which run in under a
  second.
- **The docs site deploys when a release is published, not on every
  push to main.** `pages.yml` ran on each merge, so the site showed
  docs for code no release contained, under the previous release's
  version number. `release.yml` now calls it once the publish gate has
  passed and the release exists, and it builds the site from that tag.
  A refused gate deploys nothing. A manual run of `pages.yml` redeploys
  the latest release, or the tag you name.
- **Three tests stop waiting on nothing, and two groups stop running
  where they don't belong.** `TestExamplesAreHMRReady/backoffice`
  waited 60s for a `/healthz` that backoffice never serves before
  trying `/`; it now polls both and takes about 5s. A held request made
  `TestPlaceholderPaintsWhileSourceIsPending` stall 17s in
  `httptest.Server.Close`, and Chrome's HTTP cache made
  `TestWidgetChromeCtx_FailedFetchAfterNavKeepsNewCache` wait 20s for
  an identical in-flight GET; each now takes 1–3s. Three Chrome-driven
  `framework/ui` tests that import `framework` and `framework/uihost`
  now carry the `chromium` build tag like their siblings, so a
  `framework/uihost` change no longer reruns the whole `framework/ui`
  package in the main test job (the chromium-ui job still runs them).
  `kiln/integration`'s live agent tests, which need `KILN_LIVE=1` and
  never run in CI, now also need `-tags kilnlive`, so a `cmd/gofastr`
  change no longer pulls the kiln browser job into a pull request.
- **The axe gates load each page once for both color schemes.**
  `framework/testkit/axetest` adds `ScanSchemes`, which switches the
  scheme on the page already loaded and scans again, and `WaitReady`,
  which waits until a page has loaded and every runtime module it asked
  for has registered. `Prepare` no longer sleeps 150 ms: it returns once
  the scheme has taken effect. `Scan` injects the axe engine once per
  page instead of on every scan, and only reuses the copy it injected:
  a `window.axe` the page defines itself is replaced, so a stub cannot
  report a failing page as clean. `ScanSchemes` with no schemes is an
  error, never an empty pass. The site and Meridian gates use them in
  place of a second page load per scheme and fixed 400 ms and 450 ms
  waits.
- **The site's browser tests wait for what they check, not for a fixed
  time.** `pageReady()` in `examples/site` now waits for the page to
  settle (`axetest.WaitReady`), and a new `waitFor` helper polls the
  state a test reads next, failing after 10 s with the condition that
  never held. Fixed sleeps remain only where a test checks that
  something does not happen within a window, each marked with a
  comment.
- **The site's code-sample compile checks reuse the Go build cache.**
  They built each sample with an empty cache, so every sample compiled
  the framework from scratch. They now share the normal cache and skip
  writing a binary, and still fail when a sample stops compiling.
- **Browser tests start one Chrome per test binary, not one per test.**
  `internal/chromedptest.Context` starts a headless Chrome the first
  time a test binary needs one and reuses it for every later call with
  the same allocator options; a call with other flags (WebMCP, an
  explicit browser path) gets its own. Each call still gets its own
  window in its own BrowserContext, so cookies, storage and HTTP cache
  never carry from one test to the next, and a tab opened from the
  returned context shares that test's state. If the shared Chrome dies,
  or a test's window cannot be cleaned up, the next call starts a new
  one; the old one closes once the other tests' windows on it are done.
  Starting Chrome waits up to the startup allowance (90s by default,
  `StartTimeout`) for both the DevTools URL and the websocket dial that
  follows it; chromedp's own dial limit is 10s, which a cold Chrome on
  a loaded runner can miss. A small `sh` watcher kills Chrome and
  deletes its profile when the test binary exits, including on a panic
  or a `-timeout` kill. On Windows, without `sh`, or with an option
  that installs a func or a writer (`chromedp.ModifyCmdFunc`,
  `chromedp.CombinedOutput`), every call gets its own Chrome as before. framework/headless's
  `behaviorBrowserCtx` and core-ui/runtime's `boundaryBrowserCtx`, which
  said they shared one browser but started a new one per test, now use
  it. Code that boots Chrome itself (36 test files, `axetest`, the UI
  eval runner) passes the same 90s to both start waits, and
  `TestHandBootedChromeKeepsLaunchAllowance` checks every `.go` file
  for both. The passkey and sitesearch browser tests, which had copied
  one boot helper, use `chromedptest.Context`. The eval runner starts its Chrome once per capture run: its
  tabs came from a context that never ran, so each screenshot had
  started a Chrome of its own. Each shot opens a window in a browser
  context of its own, so no cookie or storage entry carries from one
  shot to the next.
- **The CLI's full test run compiles fewer generated apps.** Tests in
  one package that need the same generated app now build it once and
  share it (`clitest.SharedTree`); a test that edits files works on a
  copy, and each boot keeps its own database. The example apps are
  built once each and also run the repo vettool, which replaces appgen's
  separate vettool pass over the same seven examples. Five appgen tests
  that only proved their feature compiles now check the emitted source
  and leave the build to `TestUnionApp`; each was checked by breaking
  the generator and watching `TestUnionApp` fail. Under
  `GOFASTR_TEST_ALL=1` the directories the toolchain builds, vets or
  tests in drop from 82 to 61: `cmd/gofastr` 32 to 26,
  `internal/appgen` 42 to 30, `internal/packcmd` 4 to 1 (with
  `generatecmd` at 3 and `initcmd` at 1, unchanged). On a busy machine
  (load 20–40) `cmd/gofastr` ran in 110s instead of 120s and `appgen`
  in 60s instead of 65s. A pull request builds nothing it did not build
  before.
- **The shipped JavaScript drops `var` and the pre-ES2015 idioms, and
  a lint keeps them out (#598).** The runtime, the behavior modules, the
  plugin host, the MCP widget client and the dev tools now use `const`
  and `let`, spread instead of `.push.apply` and `Array.prototype.*.call`,
  rest parameters instead of `arguments`, and class methods instead of
  `Ctor.prototype.x = …`. `core-ui/check.LintLegacyIdioms` fails on any
  of those four shapes. It, the no-var lint and the runtime shape lints
  now read every shipped `.js` file in the repo; before, they read
  `core-ui/runtime` and the registered behavior modules only. Behavior
  is unchanged.
- **Faster tests and CI (#598).**
  - battery/auth's tests hash passwords at bcrypt's minimum cost; the
    two tests that time the hash keep the production cost.
  - `TestDocExamplesCompile` compiles the doc snippets with
    `go list -export` and no longer links a binary for each.
  - battery/sitesearch's 10,000-page test compresses the export once,
    across all cores.
  - The process-module probe timeout test runs on a one-second budget
    instead of waiting out the 15 s default.
  - The six core-ui/runtime test files that import framework/uihost,
    webmcp, battery/auth or localentity moved to
    `core-ui/runtime/hosttest`, so a change to those packages runs a
    7 s suite instead of the runtime's 6-minute one. CI runs it as the
    `core-ui-runtime-hosts` shard.
  - The two site axe shards are one `site-axe` shard again: each
    scanned for about 85 s once #595 loaded every page once for both
    color schemes.
  - The `chromium-ui` shard runs only the tests the `chromium` tag adds
    (`scripts/tagged-tests.sh`); the test job already ran the rest. It
    also picks up core-ui/interactive and the acme-site and meridian
    `siteheader` packages, whose tagged tests no CI job ran. A repolint
    rule, `chromium-shard`, fails when a package with chromium-tagged
    tests is missing from the shard.
  - A browser shard decides whether the change reaches its suite before
    it installs Chrome and the `gofastr` binary, and skips both when
    it does not.
  - Nine packages that read `gofastr.settings.yml` now declare it, so
    editing it selects their tests.

### Fixed
- **A redirected answer is no longer taken as the request's own
  (#577).** fetch follows a 3xx itself, so a runtime request whose
  session had expired got the sign-in page back as a 200 and used it.
  `data-cui-rpc` and optimistic actions now fail: a form keeps what
  the reader typed and shows the submit-failed toast, and any other
  element sends the reader to the redirect target through the client
  navigator. Polls, widget chrome, state and discovery, `fetchPage`,
  server actions, deferred parts, the SSE session re-mint and the
  desktop bridge ignore the redirected body, and the embed handshake
  refuses redirects. A sortable reorder whose save redirects reverts,
  and a selection Copy fails instead of copying the sign-in page.
- **A generated PWA app's e2e test finds its icons.** The emitted
  `e2e_test.go` runs the app from a temporary directory, so every icon
  it checks under `/icons/` answered 404 and the suite failed on a
  freshly generated app. It now copies the app's static directory there
  before it starts the server.
- **Reopening a widget panel no longer waits behind its own slow fetch
  (#598).** Chrome's HTTP cache runs one request per URL at a time, so a
  second open of a panel whose chrome fetch was still in flight waited up
  to 20 s for the first. The runtime's chrome fetch now passes
  `cache: 'no-store'`; the runtime's own chrome cache already dedupes
  the request.
- **A form's submit button keeps its overrides.** The runtime's form
  bridge sent the form's own `action`, `method` and `enctype` and
  dropped the clicked button's name and value. It now carries the
  button's value and its `formaction`, `formmethod`, `formenctype` and
  `formtarget`. A form with another browsing target, a dialog or image
  submit, or a plain-text POST stays with the browser. When the runtime
  module fails to load, the native replay keeps the original button, and
  a form the bridge would POST with no `method`, or send as PUT or
  PATCH, stays on the page with the warning: the browser would have sent
  it as a GET with its fields in the URL. A
  form inside a widget that the widget does not handle now goes
  through the document bridge. A `data-cui-spa` form, or a JSON form,
  submitted with GET used to fail with the submit-failed toast because
  fetch refuses a GET with a body. It now navigates to the action with
  the fields as the query, through the client navigator. A marked form
  with no `method` still POSTs.
- **`gofastr init <name>` explains an existing directory (#590).** It
  said to `cd` in and run `gofastr init .`, which a duplicate launch on
  Windows turned into a second init over the first. It now says the run
  wrote nothing and to inspect the existing directory first.
- **`battery/print/chromepdf` gives a slow Chrome start the whole
  render `Timeout`.** chromedp waits 20 s for Chrome to print its
  DevTools URL and 10 s for the websocket dial after it, so a render
  on a loaded host failed with "websocket url timeout reached" or
  "could not dial …: context deadline exceeded" while most of its 30 s
  `Timeout` was left. Both waits now take `Options.Timeout`.

## [0.94.1] - 2026-10-24

### Security
- **BREAKING: Go 1.27.3 is the floor.** `go.mod` declares `go 1.27.3`,
  up from `go 1.27.0`. v0.94.0 built the repo with 1.27.3, but its
  `toolchain` line binds only GoFastr's own builds, so an app still
  built on Go 1.27.0 to 1.27.2 and shipped GO-2026-6809 in
  `crypto/x509`, which the SMTP sender's TLS handshake reaches. Now an
  app needs 1.27.3: `go get` raises its `go` line, and the default
  `GOTOOLCHAIN=auto` downloads the toolchain. With `GOTOOLCHAIN=local`,
  install 1.27.3 first. `gofastr init` writes `go 1.27.3` with no
  `toolchain` line, and `gofastr init .` raises a module below the
  floor.

## [0.94.0] - 2026-10-23

**BREAKING** for error helpers, translated text and entity keys.
`gofastr upgrade` lists each change.

**Translation.** Every message the framework writes is an `i18n.Phrase`
in the request's locale, and every JSON error body carries a stable
`reason` code. Error helpers, refusals, screen titles, battery text
callbacks, and desktop, setup and admin words take phrases or the
request context. Numbers, dates and plurals follow CLDR, pages carry
`<html lang dir>` from the request, and kit CSS sets logical
properties. Generated apps and Meridian translate, a locale can live in
the URL (`/es/pricing`), and a language switcher picks it.

**Composite keys.** `EntityConfig.PrimaryKey` is a `[]string`, so an
entity can key on several columns. HTTP, MCP, the admin, the screens
and the generated clients name a record by one path segment per key
column, and relations, join entities and foreign keys
(`Relation.ForeignKey` is a list) reach composite keys. A key column
must be a string, text, integer, enum, UUID or relation. The schema
diff migrates a changed primary key.

v0.94.0 also adds unique jobs, and a record page for a missing or
hidden row answers 404 where it answered 200: a test that expects 200
for another user's record now gets 404.

### Added
- **Unique jobs.** A job with `Job.UniqueKey` set is refused with
  `queue.ErrDuplicateJob` while another job of the same type holds the
  key, and nothing is enqueued. The key is free again once that job
  succeeds or is dead-lettered. `DBQueue` holds it with a partial unique
  index, so it holds across replicas and does not abort the caller's
  transaction; existing tables gain the `unique_key` column on start.
  `Replay` of a dead job whose key a newer job holds returns
  `ErrDuplicateJob`; the admin's replay answers 409 for it, and "Replay
  N failed" skips it and replays the rest. `RedisQueue` refuses a keyed
  job with `queue.ErrUniqueUnsupported`.
- **Error codes and translated framework messages.** Every JSON error
  body the framework writes carries `reason`, a stable code such as
  `unique_taken` or `unknown_filter`, beside `error` or `message`, which
  is now in the request's locale. Field failures add `field_reasons`, one
  code per message in `fields`. This covers `core/handler`, crud (single,
  batch and state-move results), auth's JSON bodies, entity bulk actions,
  saved views and status overrides, desktop preferences and semantic
  search. Plain-text refusals (`429`, `503`, the recovery `500`, ...) and
  battery screens translate too. Each message is an `i18n.Phrase`
  (`i18n.Define(key, english)`), and its code is the key's last segment,
  so a locale file translates one by naming its key
  (`validation.required`, `error.not_found`, `filter.unknown_filter`).
  New: `i18n.Phrase`, `i18n.Define`, `i18n.Phrases`, `i18n.Error`,
  `i18n.NewError`, `i18n.Wrap`, `i18n.Phrased`, `i18n.ErrorText`,
  `i18n.HTTPError`, `i18n.HTTPErrorFor`, `i18n.StatusPhrase`,
  `handler.Fail`, `handler.ViolationBodies`, `schema.Violation` and the
  `schema.Reason*` phrases. A wrapped error's text keeps its frame
  (`nested filter "author.age_like": ...`) and its code is the innermost
  cause's. OpenAPI lists `reason` and `field_reasons` on its error
  schemas; the generated Go client, JS SDK and CLI decode them. See
  `gofastr docs i18n` → "Framework messages and error codes".
- **`rawtext` analyzer.** `make analyze` refuses English that skips the
  catalog in `core/`, `framework/`, `battery/`, `core-ui/` and
  `examples/meridian`. A deliberate site carries
  `//gofastr:allow(rawtext) <why>`. It reports:
  - **Responses:** a bare `http.Error`, `i18n.Define` in a function body
    or with computed text, `handler.Errorf` and
    `handler.Error{Message: ...}`, a response helper that takes a
    `msg string`, a JSON envelope whose `error` or `message` is prose or
    `err.Error()`, a status-bearing JSON struct body (RFC 9457) whose
    `title` or `detail` is prose, and a constant JSON body written
    straight to the response with a prose message.
  - **Markup (`uitext`):** `render.Text` of a prose constant; an
    `aria-label`, `title`, `placeholder` or `alt` attribute written as a
    constant; a constant in a reader-facing field of a `framework/ui`,
    `framework/headless`, `framework/entityui` or `core-ui` type
    (`Label`, `Heading`, `Hint`, `Help`, and any field ending in
    `Label`, `Title`, `Text` or `Placeholder`); a `fmt.Sprintf` whose
    format is English ("Signed in as %s"); and a `ScreenTitle` or
    `ScreenDescription` returning a constant. A local variable,
    parameter or package-level variable holding English, a same-package
    helper whose string parameter reaches page text, and an index write
    into an attribute map (`attrs["aria-label"] = ...`) count at those
    sinks.
  - **Numbers (`numtext`):** a number turned into Go's digits
    (`strconv.Itoa`, `fmt.Sprint`, ...) passed as a phrase parameter or
    written into page text; pass the number or `i18n.Number`.
  - **Desktop shells (`nativetext`):** a constant passed to
    `objc.NSString` or used as a Win32 menu row, window or message-box
    title.
  - **Components without Ctx (`ctxless`):** a UI component literal whose
    `Ctx context.Context` field is unset while the function holds a
    request context. It found 55 sites in the admin, setup, entityui,
    `framework/ui` and Meridian; each now passes its context.

  `core-ui/check.LintRuntimeProse` is the runtime JavaScript twin:
  literal prose written into a toast (the `_toastOrFallback?.(` spelling
  included), a `textContent` (`+=` too), an `aria-label` or a `title`.
- **Every component word is a phrase.** All of `framework/ui`'s labels,
  hints and screen-reader text are `i18n.Phrase` vars in
  `framework/i18nui` (`i18nui.PaginationNext`, `i18nui.KeyCtrl`, ...),
  under the same `ui.*` keys a catalog already used, so a catalog
  translates them as before. Newly translatable: keycap words in
  `ShortcutHint` (`ui.key.*`, so German can say `Strg`), the pricing
  card's badge and default button, and the code block's labels and copy
  button. `PricingCard`, `ShortcutHint`, `Button` and `CodeBlock` take a
  `Ctx` field for the reader's locale. `i18n.Fill` fills `{{name}}`
  placeholders in a plain string, and `i18nui.HTML` does it for a line
  that carries markup.
- **Framework pages speak the reader's language.** The fallback 404, 405,
  500 and offline pages, the uihost problem documents (now with `reason`
  beside `title` and `detail`), the API 404, the setup wizard, the
  desktop settings screen and sidebar, the static-export preview banner,
  the process-module UI renderer, local entities' validation messages
  and the passkey prompts all read from phrases.
- **Runtime strings.** The runtime's own words (a failed request's toast,
  a navigation that could not load, the embed error, local-entity
  validation) are phrases registered with `runtime.RegisterStrings`. The
  host writes the ones the reader's locale translates into a
  `#gofastr-strings` JSON block, and the runtime's `say(key, english,
  params)` reads it. A test holds every `say` call in the repo's
  JavaScript to a registered phrase with the same English.
- **`i18n.CheckCatalog`.** Compares a locale's catalog with every phrase
  the program defines and reports unknown keys, placeholder sets that
  differ from the English, and phrases missing under a `Require`d
  prefix. Each plural form is checked on its own, and may leave out only
  the count. `i18n.Skip` leaves out the app's own data keys (`entity.`,
  `nav.`). See `gofastr docs i18n` → "Checking a catalog".
- **Mounted widgets speak each reader's language.** A widget mounted
  once at startup renders its body per request, and the framework's
  words in it follow that request's locale: `ui.ConfirmAction`'s
  buttons, `ui.Lightbox`'s controls, `ui.NotificationBell`'s empty
  text, `ui.ShortcutSheet`'s title and `ui.CommandPalette`'s
  placeholder and key hints. `ui.ShortcutSheetFunc` and
  `ui.CommandPaletteFunc` build the whole config per request, so the
  rows and commands translate too; the admin shell mounts both.
- **Numbers, dates and lists in the reader's locale.** `i18n.Number`,
  `i18n.Percent`, `i18n.Currency`, `i18n.Date`, `i18n.Time`,
  `i18n.DateTime`, `i18n.List` and `i18n.ListOr` write a value the way
  the request's locale does (`1 234,5` in French, `٤٬٨٢٠` in Egyptian
  Arabic, `5 mars 2026`), from CLDR 47 tables generated into the package.
  `i18n.MinFraction` and `i18n.MaxFraction` set the decimals.
  `i18n.Decimal` formats an exact decimal string, a money amount from
  the database, without going through a float. `i18n.NoGrouping` keeps
  a year or an id in one run of the locale's digits. NaN and infinity
  take the pattern's signs (`$∞`). Plural forms follow CLDR 47's rules
  for every locale (a French million is `many`), and a `B` time pattern
  names the part of the day (zh-Hant's `下午3:04`). A number passed as
  a phrase parameter is formatted in the reader's locale, and
  `i18n.FillContext` does the same for a plain template.
- **Components write numbers and dates the reader's way.** Pagination,
  counts, steps, ratings, tags, the section menu, dropdown badges,
  `ui.Ago` past a month, entity list and record cells, admin dashboard
  counts and charts all format through the request's locale. `ui.BarChart`,
  `ui.LineChart`, `ui.PieChart`, `ui.Gauge`, `ui.Heatmap`,
  `ui.ScatterChart` and `ui.Sparkline` take a `Ctx` field for it, and
  their words ("No data yet", point and cell titles) are phrases. The
  command palette's `Esc` key chip is translatable. Headless
  `Strings.ForRequest(ctx)` carries the locale into headless components,
  and `data-cui-signal-mode="number"` keeps a live counter formatted
  after the client updates it (the `headless-numbers` module, loaded
  only where a page uses it).
- **Right-to-left pages.** Every full page carries `<html dir>` from the
  new `i18n.Dir(lang)`, so Arabic, Hebrew, Persian and the other
  right-to-left scripts render `dir="rtl"`. The document language is the
  negotiated locale when no route sets one, and the `doc-dir` runtime
  module keeps `dir` right across client navigation. The setup wizard
  and the print shell declare the reader's language and direction too.
  The design system's CSS uses logical properties, so the sidebar,
  indents, chips, the select chevron, switches, toggles, the segmented
  indicator, the timeline and the nav progress bar mirror. Corner and
  edge positions (notifications, back-to-top, widget corners, `Edge`
  drawers) and tooltip sides mean start and end; code blocks stay left
  to right; an anchored popover lines up with its trigger's start edge.
  See `gofastr docs i18n` → "Direction".
- **Component defaults in the reader's language.** The default labels of
  `ui.AvatarGroup`, `ui.ContentRow`, `ui.FilterChipBar`, `ui.Gallery`,
  `ui.ListDetail`, `ui.NetworkRetryBanner`, `ui.StepRail`,
  `ui.TableOfContents`, the sidebar drawer, `interactive.SectionMenu`
  and `interactive.PhoneSheet` are phrases. Their configs (and
  `ui.SidebarConfig`, `ui.AnimatedCounterConfig`, `ui.SliderConfig`,
  `ui.JSONViewerConfig`, `ui.AnchoredRailConfig`) take a `Ctx` field;
  `ui.Slider`'s and the headless range controls' value outputs,
  `ui.JSONViewer`'s counts, `ui.AnchoredRail`'s item counts,
  `ui.FilterRows`' row numbers, `ui.FileDropzone`'s size hint and the
  admin process-module generations write the locale's digits. The
  admin activity feed's hover time is in the reader's date format.
- **Contracts rule GOFASTR1824** refuses a physical side in the design
  system's CSS (`margin-left`, `left: 8px`, `text-align: right`, a
  corner radius, an asymmetric four-value shorthand) where a logical
  property exists. See `gofastr docs contracts`. The contract catalog
  holds 80 rules.
- **Pages outside the app shell, mail and desktop shells in the
  reader's language.** `App.Start` negotiates the locale ahead of the
  server wrappers and the setup wizard, so the setup wizard, the sign-in
  gate, the service pages and the magic-link confirmation page speak it
  and declare it with the new `i18n.DocLang(ctx)`. The deploy-skew
  reload notice and the PWA offline page are phrases; the offline page's
  `lang` matches its body. Password-reset, email-verification and
  magic-link mail render under the requester's locale, and
  `notify.Recipient.Locale` renders a notification in its recipient's.
  New `i18n.Match(tr, tags...)` and `i18n.EnvLocale(tr)` pick a locale
  with no request; `desktop.ShellContext` uses them for a shell's own
  menus, About box, permission prompt, accelerator names and the
  settings window's title; the settings window's number hints ("Between
  1 and 180.") are phrases with the locale's digits. After a client-side
  navigation, and on Back to a page reached that way, the runtime's own
  words follow the page shown (`data-cui-strings`). An action's default
  success toast says `runtime.done`, and a failed RPC shown in a text
  signal reads "Request failed (500)" in the page's language, where it
  read "Error: 500". See
  `gofastr docs i18n` → "Beyond the page body".
- **Generated apps translate.** Every word an app spec writes for a
  reader is an `i18n.Phrase` declared at the top of the file that says
  it, keyed `<app>.screens.<screen>.<slug>`, `<app>.chrome.<slug>` or
  `<app>.auth.<code>`; field labels and enum values read the `entity.*`
  keys through `i18nui`. Screens render with `RenderCtx(ctx)`, and the
  header, footer and sidebar take the request context. The generator
  writes `i18n.go`, whose `translatorOption()` loads `locales/<tag>.json`
  and adds the `en-XA` pseudo-locale under `GOFASTR_PSEUDO_LOCALE=1`,
  `i18n_test.go`, which runs `i18n.CheckCatalog` over every locale file
  so `go test` fails on a stray key or a dropped `{{placeholder}}`,
  and `locales/README.md`. It never writes a `<tag>.json`, so
  `generate --force` keeps translations. `gofastr pack` reads each
  phrase back as its English. Meridian's pseudo-locale switch is
  `GOFASTR_PSEUDO_LOCALE` too. See `gofastr docs app-spec` →
  "Translations".
- **Meridian's screens speak Spanish.** Every word its screens and
  chrome show is a phrase, from the marketing pages to the admin's
  revenue report, and `locales/es.json` translates all of them; its
  catalog test fails on a `meridian.*` phrase with no Spanish.
- **The language in the URL.** `framework.WithLocalePrefixes()` serves
  each catalog locale under its own path: `/es/pricing` is the pricing
  screen in Spanish, the fallback locale keeps the plain paths, and
  `/en/pricing` answers 308 to `/pricing`. The router strips the prefix
  before matching, so routes and screens are written once. Links, form
  actions, island and poll URLs and redirects in a `/es/` response gain
  the prefix on the way out (`core-ui/urlprefix`); the runtime keeps
  navigation inside a locale client-side, sends its own requests under
  the page's prefix, and loads a new document when the locale changes.
  Pages carry an `hreflang` alternate per locale plus `x-default`, the
  sitemap lists each page once per locale, robots rules repeat under
  every prefix, and `gofastr export` writes a tree per locale. A
  one-off link to another language uses `i18n.LocalePath`. Boot refuses
  it without `WithI18n`, beside `WithLocaleResolver`, and with a route under
  a locale segment. Lower-level pieces: `i18n.PathLocales`,
  `i18n.WithPathLocale`, `i18n.PathLocale`, `i18n.PathPrefix`,
  `Router.PreRoute`, `urlprefix.Middleware` and
  `UIHost.SetPathLocales`. App specs turn it on with
  `app.locale_prefixes: true`; Meridian does, so its Spanish lives
  under `/es/`. See `gofastr docs i18n` → "Locale in the URL".
- **A language switcher.** `ui.LanguageMenu` lists every catalog locale,
  each named in its own language (`i18n.LanguageName`: "Español",
  "Português (Brasil)"), and links the current page in each. Its rows
  are `rel="alternate" hreflang` links, which the runtime re-points at
  the current page after each client-side navigation, so the menu can
  live in a header or footer that stays on screen. Generated apps with
  `app.locale_prefixes` put it in the marketing footer (sitefooter
  gains a `Language` slot), and so does Meridian.
- **Record ids name every key column.** `Entity.KeyColumns()` lists an
  entity's primary-key columns, and crud reads, writes, upserts, the
  keyset cursor's tiebreak, event payloads, audit `record_id`s and hook
  ids all name a record through them. A string id is `entity.JoinKey` of
  the key values (`entity.SplitKey` and `entity.RowKey` read it back),
  which for a one-column key is the value itself, so nothing changes for
  existing entities. `SplitKey` refuses any spelling of a composite id
  but the one `JoinKey` writes (`a/b` names `a` and `b`; `%61/b` is
  refused), so one record has one id. See `gofastr docs
  entity-declarations` → "Record ids".
- **Composite primary keys.** `EntityConfig.PrimaryKey: []string{"student_id",
  "course_id"}` keys a table on several columns: each gets `NOT NULL` and
  the table a `PRIMARY KEY (student_id, course_id)` constraint, with no
  injected `id`. A raw `migrate.Table` marks several columns `PrimaryKey:
  true` for the same key. Two versions
  of one entity must declare the same key, and `Define` panics on a
  composite key column that is `Hidden` or `NoQuery`, since the API could
  not name its records. `ExportData` pages a composite key by row value,
  and the manifest's `primary_key` joins its columns with commas.
- **Composite keys over HTTP, MCP and the admin.** A composite-key
  entity's record routes take one path segment per key column,
  `/api/enrollments/{student_id}/{course_id}`, each value path-escaped,
  and its state moves hang off that path. OpenAPI declares one string
  path parameter per key column, `/api/llm.md` lists them, the MCP get,
  update, delete and move tools take one argument per key column, and a
  batch update item names its record by every key column, each in its
  field's own type. entityui and
  the admin link, read, edit, bulk-act on and export such records at
  `<base>/<v1>/<v2>`; the create form sets the key columns and later
  edits show them read-only. New: `crud.KeyPath`, `crud.TransitionPath`,
  `crud.PathRecordID`, `CrudRouteOptions.Key`. A key value holding `/`
  is escaped in its segment and reaches its screen as it reaches REST
  and MCP: the screen router splits the escaped path and decodes each
  segment, and a pattern redirect carries such a value as one segment,
  with a `?`, `#` or (in a catch-all) `%` in it escaped, so it never
  turns into a query or a fragment of the target.
  The login `next=` from `auth.SessionPolicy` and the admin gate, the
  stale-markup reload, and `cache.CacheMiddleware`'s key all use the
  escaped path, so `/a%2Fb` and `/a/b` stay two pages.
  New: `app.RoutePath` and `app.EscapeRoutePath`. See
  `gofastr docs entity-declarations` → "Composite keys over HTTP and MCP".
- **Composite keys in the generators.** An app spec entity takes
  `primary_key: [student_id, course_id]`, and `gofastr pack` writes it
  back. A record screen's `{id}` mounts as one segment per key column
  (`/enrollments/:student_id/:course_id`), and the generated
  `e2e_test.go` addresses the record by every key column. The typed
  repository's `Get`, `Update`, `Delete`, `Exists` and `Transition`, the
  Go client's record methods and moves, and the CLI's record verbs take
  one argument per key column; the JS SDK takes an array of the values
  (`api.enrollments.get([s, c])`), typed as a tuple in `client.d.ts`.
  Each escapes a value into its own path segment the way
  `entity.JoinKey` does, and `<Entity>ID(a, b)` spells the id that
  batch deletes and lifecycle subscriptions use. `gofastr generate cli
  --from-openapi` reads such an app's document: a body property named
  exactly like a path parameter, as the key columns are in the PUT body,
  takes no flag of its own. `seed.count` rows never repeat a key, and
  the generator refuses a count an enum-only key cannot cover. A seed
  reference (`"@currencies.label=Euro"`) resolves to the target's key
  column rather than `id`. New:
  `framework.JoinKey` and `framework.RowKey`. An entity keyed on `id`
  keeps every generated signature. See `gofastr docs app-spec` →
  "Composite keys (`primary_key`)".
- **Join entities and three-way associations.** A foreign key has one
  column per column of the key it references (`entity.CheckRelationKeys`):
  a `belongs_to` checks its target, a `has_one` or `has_many` its source,
  and a `many_to_many` needs one-column keys on both ends. An enrollments
  entity keyed on
  `(student_id, course_id)` with a `grade` column belongs to students and
  courses, each side has many of it, and `students` can declare a
  `many_to_many` to courses through it; `?include=`, nested filters and
  `EagerLoad` work across all of them, and a three-column key does the
  same for a three-way association. When a registered entity owns a
  `many_to_many`'s pivot table, reads through the pivot apply its soft
  delete and owner, tenant and read scopes, and boot refuses the
  relation if that entity lacks either link column. A `cascade_write`
  link inserts the pivot row through that entity's `belongs_to` scope
  checks. When several
  versions own the pivot and none is the source's version or
  unversioned, the read is an error rather than a pick. A `cascade_write`
  into a composite-keyed child is refused at boot. A key that is a
  relation to an integer key holds that integer: ids, writes and
  cascade links take it in canonical decimal on both dialects. New:
  `entity.CheckRelationKeys`, `entity.PivotEntity`, `entity.ValueFields`,
  `entity.RelationHoldsInt`, `crud.CrudHandler.WireKey`.
  See `gofastr docs entity-declarations` → "Relations and composite
  keys".
- **Foreign keys to composite keys.** A relation names one column per
  key column of the end it references:
  `entity.BelongsTo("order", "orders", "order_store", "order_no")` lists
  both in `Relation.ForeignKey`. The migrator emits one `FOREIGN KEY` over
  the tuple and an index on it; boot refuses a column count that differs
  from the key's, a repeated or undeclared column, a column typed
  unlike its key column, and a column a transition of any declaration
  over the table writes, at registration. `?include=`, nested filters, `EagerLoad`, the
  record page's related tabs and the admin's related lists join on the
  whole tuple. A write that sets some of the columns and not the others,
  or nulls only some, is a 400 `validation.partial_foreign_key`, and the
  scope check on a reference looks up the whole tuple as the row will
  hold it: a column the write does not take from the body reads the
  field's default or the caller's tenant on create and the stored value
  on PATCH, an upsert checks both its insert and its conflict update,
  and a write that cannot know such a column (the upsert's conflict
  update, the bulk `UpdateAll`, a create through an `AutoIncrement`
  column) refuses moving part of the reference with a 400
  `validation.kept_foreign_key`; the upsert and a scoped `UpdateAll`
  read the tenant and owner columns as the caller's. A tuple with a null column names no
  row. The columns are spelled as their
  fields are declared. An app spec
  names the columns as a list, `foreign_key: [order_store, order_no]`.
  A `cascade_write` over several columns and a `many_to_many` with a
  composite-keyed end are refused at boot; link the latter through a
  join entity. New: `entity.ErrForeignKeyShape`,
  `migrate.ForeignKey.Columns`.
- **Primary-key change migrations.** The schema diff detects a changed
  key and emits it as destructive: Postgres swaps the key constraint,
  SQLite rebuilds the table. Boot changes the key of an empty table only
  and refuses a populated one, pointing at `gofastr migrate generate`; a
  populated table with no primary key at all is refused the same way.
  The snapshot records `primary_keys`. See `gofastr docs migrations` →
  "Primary-key changes".
- **`query.DeleteBuilder.Returning`.** A DELETE built with `query.Delete`
  can end in `RETURNING` the deleted rows' columns, as an UPDATE already
  could.
- **`Matching` on `query.UpdateBuilder` and `query.DeleteBuilder`.**
  `b.Matching("id")` builds the `SELECT id FROM <table> WHERE …` of the
  rows the write would reach, with the same conditions and their
  arguments.

### Changed
- **BREAKING: `EntityConfig.PrimaryKey` holds the key.** `Entity.PrimaryKey
  string` is gone; set `EntityConfig.PrimaryKey []string` (nil means
  `id`) and read `Entity.KeyColumns()`, or `Entity.KeyColumn()` where one
  column is needed. `CrudHandler.PrimaryKey` is `[]string`, and a handler
  that leaves it empty reads its entity's key. A raw `migrate.Table` with
  several `PrimaryKey` columns no longer panics.
- **BREAKING: `Relation.ForeignKey` is `[]string`.** It lists one column
  per key column of the end it references, so a one-column relation is
  `ForeignKey: []string{"author_id"}`; the `BelongsTo`, `HasOne` and
  `HasMany` constructors are unchanged. `migrate.ForeignKey.Column` is
  gone: name the local columns in `Columns`, `{Columns:
  []string{"user_id"}, RefTable: "users"}`. The SDK schema hash of an app
  with no composite foreign key does not change.
- **BREAKING: a column name may not be another field's wire key.**
  `Define` panics when one field's `WireName` is another field's column
  name (`body_text` published as `content` beside a `content` column
  published as `summary`): a body naming the column, as entityui's forms
  do, wrote the other field.
- **BREAKING: a key column is a string, text, image, file, integer,
  enum, UUID or relation.** `Define` panics on a key column, `id` included, of type
  `Float`, `Decimal`, `Bool`, `Date`, `Timestamp` or `JSON`, or one that
  sets a `RawType` other than an integer type on an `Int` column. A record
  id is the key's text, and those values have several spellings:
  Postgres reads `2024-1-2` as the `DATE` keyed `2024-01-02`, and a
  `CITEXT` or Postgres `UUID` column matches either case, so one row
  answered to many ids. `App.View` with `Columns` panics on the same key,
  and the admin's `Config.Entities` fails `Init` naming a raw table with
  one; both skip `Define`, and `Entity.CheckRecordKey` is the check they
  share.
- **BREAKING: the saved-view delete route takes its id in the query.**
  It is `POST <write base>/_views/_delete?id=<id>`, not
  `.../_views/_delete/{id}`, which collided with a composite key's
  record routes (`<write base>/{a}/{b}/_restore`). Mount
  `SavedViewsHandler` at `POST <write base>/_views/_delete`; the admin
  and entityui's delete form already use the new route.
- **BREAKING: error helpers take phrases.** `handler.WrapError(code,
  reason, err)` takes an `i18n.Phrase`; `handler.Fail(code, reason,
  params)` builds one. `handler.ValidationError` and
  `crud.NewValidationError` take `map[string][]schema.Violation`, and
  `crud.ValidationError.Fields()` returns it. `handler.Error.Fields` is
  now `Violations`, beside new `Reason` and `Params`.
  `schema.ValidationResult.Errors` holds `[]schema.Violation` per field;
  `Violation.Error()` is the old English message. `handler.WriteError`
  takes the request (`WriteError(w, r, err)`) so it can answer in the
  caller's locale. `handler.Errorf` is unchanged and sends its text as
  written, with no code.
- **BREAKING: refusal messages are phrases.** `decide.Block(status)`
  drops its message; `decide.BlockWith(status, reason, params)` names
  one, and `app.Decision` and `app.RenderResult` carry `Reason` and
  `Params` instead of `Message`. `auth.WithBlock(status)` drops its
  message; `auth.WithBlockReason(status, reason)` names one.
  `print.AccessPolicy` returns `(status, i18n.Phrase)`. `rtc.HTTPError`
  carries `Reason` and `Params` instead of `Message`.
  `middleware.RateLimitConfig.ErrorMessage string` is
  `Message i18n.Phrase`.
- **BREAKING: query parsers answer with their own code.** A crud list
  given a bad `?field_<op>=`, `?sort=`, `?where=`, `?filter=`,
  `?include=`, `?fields=` or nested `?rel.field=` answers the parser's
  message and code (`unknown filter "x"`, `unknown_filter`), where it
  wrapped it in "invalid filters: ..." or "invalid query: ...". A bad
  cursor keeps its "invalid cursor: ..." text and answers the decoder's
  code (`cursor_format`). The `filter`, `dsl` and `pagination` errors
  and crud's include, nested-filter, projection and cursor errors are
  `*i18n.Error`; their `Error()` text is unchanged.
- **BREAKING: `entity.Unique` says "is already in use"**, the same phrase
  (`validation.unique_taken`) crud's conflict answer uses, where it said
  "must be unique". `entity.Required` and `entity.Unique` write their
  message in the context's locale.
- **BREAKING: `i18nui` validation keys removed.** `TranslateValidation`
  and the `ui.validation.*` keys (`KeyValidationRequired` through
  `KeyValidationUnique`) are gone: validation messages are the
  `validation.*` phrases, and a catalog translates them under that key.
  The summary's `ui.validationSummary.title` and
  `ui.validationSummary.problem` keys stay, as the
  `i18nui.ValidationSummaryTitle` and `i18nui.ValidationProblem`
  phrases. The
  entity bulk, saved-view and override refusal keys
  (`ui.entity.bulkNone`, `ui.entity.overrideDenied`, ...) move to
  `entityui.*` phrases (`entityui.bulk_none`, `entityui.override_denied`)
  whose placeholders use the double-brace spelling (`{{cap}}`,
  `{{entity}}`).
- **BREAKING: `i18nui` keys are phrases.** The `i18nui.Key` type, its
  `Key*` constants, `Defaults`, `T`, `TVars`, `TVarsHTML` and `AllKeys`
  are gone. Each key is now a phrase var named without the `Key` prefix
  (`i18nui.KeyPaginationNext` is `i18nui.PaginationNext`); say it with
  `.Text(ctx)` or `.Text(ctx, params)`, and use `i18nui.HTML` where
  `TVarsHTML` took markup. The catalog keys are unchanged.
- **BREAKING: placeholders are `{{name}}`.** Component phrases,
  `headless.Strings` fields and `localentity.Messages` spell a
  placeholder `{{name}}` where they spelled `{n}`, `{label}` or `%s`. A
  catalog entry or a `Strings` value that still uses the old spelling
  shows it literally.
- **BREAKING: `ui.CheckStrings` and `ui.StringsRefusal` are removed.**
  `i18n.CheckCatalog` checks every phrase, components included.
- **BREAKING: smaller removals and signatures.**
  `headless.Strings.MultiSelectRemoveLabel` is gone; the multi-select
  chip's remove control uses `RemoveLabelled`, as the tag input does. `localentity.DefaultMessages` is
  gone; an empty `Messages` field uses the `localentity.*` phrase.
  `entityui.AccessDeniedTitle` is an `i18n.Phrase`, not a string
  constant. `ui.ShortcutList(ctx, items)` and `uinoderender.New(ctx,
  resolve)` take a context. `ui.LightboxConfig.Ctx` and
  `ui.ConfirmActionConfig.Ctx` are gone: the mounted widget takes the
  locale from each request.
- **BREAKING: `print.Battery.PrintLink` takes the request context.**
  `PrintLink(ctx, doc, params, label)`; an empty label is "Print" in the
  reader's language.
- **BREAKING: plural rules come from CLDR.** `i18n.PluralRule` and
  `Translator.RegisterPluralRule` are gone; every locale CLDR knows has
  its rule built in.
- **BREAKING: the document language follows the request.**
  `App.LangForPath(path)` is `App.LangFor(ctx, path)`, and
  `UIHost.LangForPath` is `UIHost.LangFor(ctx, path)`: with no route
  language set, `<html lang>` is the negotiated locale, where it was
  the app's default. `App.SkipLabelForPath(path)` is
  `App.SkipLabelFor(ctx, path)`, and the default skip label is the
  `app.skip_to_main` phrase.
- **BREAKING: numbers and dates in markup changed shape.** English
  numbers now carry grouping (`4,820`, where `4820` was written), entity
  timestamps read `Mar 5, 2026, 2:05 PM`, `ui.Ago` past a month writes
  `Mar 5, 2026` where it wrote `2026-03-05`, and the admin's capped
  count reads `10,000+` where it read `10k+`. A headless counter's value
  span carries `data-cui-signal-mode="number"` and an animated counter
  `data-hui-counter-to`. `headless.StepsProps` gains a `Strings` field.
- **BREAKING: logical CSS.** Kit CSS that set `left`, `right`,
  `margin-left`, `padding-right`, `border-right` and the like now sets
  the logical property. An app override written with the physical
  property still lands on a left-to-right page but hits the wrong side
  on a right-to-left one; write the logical property instead.
- **BREAKING: screen titles, descriptions and SEO take the request
  context.** `app.ScreenTitler` is `ScreenTitle(ctx context.Context)
  string`, `app.ScreenDescriber` is `ScreenDescription(ctx
  context.Context) string`, `uihost.ScreenSEO` is `ScreenSEO(ctx
  context.Context) uihost.SEO` and `uihost.ScreenSchema` is
  `ScreenSchema(ctx context.Context) []seo.Thing`, so an OG title or a
  JSON-LD description can be a phrase. Add the parameter to every
  screen; `gofastr upgrade` names each one. `App.Register` reads titles
  and descriptions once with a bare context, which gives the English. A
  component still declaring a no-argument method panics at
  registration, or at its first render when no registration sees it (a
  `RenderAlt` result, a not-found or error body, `RenderScreen`): that
  method compiles, but the page would lose its title or SEO without a
  word. `app.RefuseRetiredScreenMethods` is the check, for code that
  holds a component outside the router.
- **BREAKING: battery text callbacks take a context.** `BodyTemplate`
  on `auth.PasswordResetConfig`, `auth.EmailVerificationConfig` and
  `auth.MagicLinkConfig` is `func(ctx context.Context, url string)
  string`, and `auth.MagicLinkConfig.ConfirmPage` is `func(ctx
  context.Context, d ConfirmPageData) []byte`. `desktop.PlanMenuBar` and
  `desktop.PlanTrayMenu` take a context first, `Battery.OpenSettings`
  takes one, and `desktop.WindowConfig.OnSettings` is `func(ctx
  context.Context)`. `gofastr upgrade` names each callback still
  written with the old signature.
- **BREAKING: desktop menu, tray and preference words are phrases.**
  `desktop.MenuItem.Title`, `desktop.Tray.Tooltip`,
  `desktop.Preference.Label` and `desktop.Preference.Help` are
  `i18n.Phrase`. The shell renders titles and the tooltip in the OS
  language it already uses for its own menus; the preferences screen
  and the `preferences` capability render label and help in the
  request's language. `Tray.Title` stays a string: apps write a live
  countdown there. `desktoptest` `ClickMenu` and `ClickTray` match a
  title's English on every shell, the fake and the native drivers
  alike, so a suite passes whatever language the machine is set to;
  `desktop.MenuPlan` and `MenuItemPlan` carry that `English` beside the
  drawn `Title`.
- **BREAKING: setup wizard words are phrases.** `setup.Field.Label`,
  `setup.Config.Title` and the new `setup.Step.Title` are `i18n.Phrase`;
  the zero Phrase shows the field's or step's `Name`, or "Setup".
- **BREAKING: `auth.ErrPasswordEmpty` and `auth.ErrPasswordTooShort`
  are `*i18n.Error`.** `errors.Is` still matches them; their text is
  "password is empty" and "password too short", without the `auth:`
  prefix.
- **BREAKING: admin, entityui and theme picker words are phrases.**
  `admin.Page.Title`, `admin.Card.Title`, `admin.Link.Label`,
  `admin.Metric.Label`, `entityui.Tab.Label`, `entityui.SidePanel.Title`,
  `entityui.Action.Label` and `ui.ThemeChoice.Label` are `i18n.Phrase`,
  drawn in the request's language. An extension whose label is the zero
  Phrase shows its `Key`, as an empty string did. `gofastr upgrade`
  names each field still set to a string.

### Fixed
- **A record page for a missing or hidden row answers 404.** entityui's
  Record (a generated record screen, the admin record page) drew its
  "Not found" state with a 200, while the JSON API gave the same caller
  404. The page answers 404 now, on the full page, a client navigation
  and a drawer, and so does a Create duplicating such a row. The status
  comes from the new `app.SetStatus(ctx, code)`, which any component
  can call while it draws; a screen's `ScreenStatusCode` still outranks
  it. A drawer that gets the 404 drops the record's cached page before
  it falls back to a navigation, so a deleted record never replays from
  the screen cache. The record's `llm.md` and its negotiated markdown
  (`Accept: text/markdown`) answer 404 too, and a static export skips
  such a page and its `llm.md` the way it skips a 404 outlet. A screen
  registered as a value, not a pointer, now renders its per-URL doc the
  way its page does; it fell back to the route's pattern doc, which
  never ran the render that sets the status. Every llm.md and negotiated
  markdown answer now follows the page's steps: a resolver whose error
  `Load` dropped, or an outlet declared `FallbackNotFound` that nothing
  fills, answers 404 where it answered 200 with the screen's body, and
  a region guard that redirects serves the withheld doc where it served
  the screen's body. An exact route's `/llm.md` is built per URL, as the
  static export already built it, not from the pattern doc, so a
  `ScreenLoader` screen's doc carries its loaded content in place of
  the placeholder line. The doc renders under its page's request and
  route match, not the `/llm.md` URL's, so a list's record links point
  at `/customers/7`, not `/customers/llm.md/7`, and a catch-all's
  resolver reads `foo`, not `foo/llm.md`. The embed content route
  keeps answering 200 with the not-found body, because the embed loader
  mounts only a 2xx.
- **`App.NoLLMMD` turns off every page doc.** On an app that set
  `NoLLMMD`, every `llm.md` route stayed unmounted, but three places
  still served or linked a doc: `Accept: text/markdown` under
  `WithMarkdownNegotiation` got the page's doc, the default `/llms.txt`
  linked `/llm-pages.md`, and a page's `Link` header advertised its
  `llm.md` as a markdown alternate. The request now gets the HTML page,
  `/llms.txt` lists no index, and the header names no doc, the last
  also for a screen that sets its own `NoLLMMD`.
- **An overlay's index link has one slash.** `/llm-pages.md` linked a
  drawer or sheet registered at `/tools/` to `/tools//llm.md`; it links
  `/tools/llm.md`, the route uihost mounts, as pages already did.
- **Postgres text-to-integer retypes apply.** A column whose declared
  type moved from text to an integer rendered
  `ALTER COLUMN ... TYPE INTEGER` with no `USING` clause, which Postgres
  refuses. The change casts the column now: numeric text converts, and
  anything else fails the migration.
- **App spec edit forms load their record.** An `entity_form` with
  `mode: edit` PUT to a literal `{id}`, drew its fields empty, and could
  not submit while a required field stayed blank. It renders the record
  page now (`appUI.Record`, with `fields:` as `.Omit` of the rest), which
  loads the record, keeps the key read-only and saves it. A record screen
  whose route continues past `{id}` (`/posts/{id}/edit`) passes the
  route up to `{id}` as the page's base, so Save lands on `/posts/<id>`
  instead of `/posts/<id>/<id>`.
- **`auto_generate: none` is no generation in an app spec.** The
  generated form left the field out and seed rows skipped it, as if the
  server filled it. Both read `none` the way the entity does now. Seed
  rows also fill a field with no `type`, which is a string; a required
  one left out aborted startup.
- **The admin layout renders with no signed-in user.** Its account menu
  panicked with "Avatar requires Name" when uihost's strict boot check
  drew the layout with no one signed in; the menu is left out instead.
- **Lists and records mounted at the root render.** entityui joined its
  links as `base + "/" + id`, so a list or record whose base is `/`
  built `//create` and `//42`, which the link guards refuse, and the
  section drew "Couldn't load this section". Links join with one slash
  now.
- **entityui reads a field by its `WireName`.** crud returns a field
  that sets `WireName` under that name, and entityui looked it up by the
  field's `Name`, so lists, record screens, titles and exports drew it
  empty, and a title column that sets one failed to render its link.
- **An integer key has one spelling.** A record id for an integer key
  column takes its value in canonical decimal only and binds it as an
  integer. `/api/seats/01` named the row keyed 1, since both databases
  coerce `"01"`, under an id a resource-aware Decider, asked about the
  id string, did not recognise; it now answers 404, as do `+1` and `1.0`.
  entityui's named CSV export (`_export.csv?_id=01`) reads ids the same
  way, through the new `entity.KeyValues`.
- **A record id matches its row's own spelling.** A key column whose type
  the database chose, under a view's `SELECT` or an `Unmanaged` table,
  can compare a second spelling equal: a Postgres `UUID`, `CITEXT` or
  nondeterministic collation matches either case, a SQLite `NOCASE`
  column too, and a `CHAR(n)` matches the value with or without its
  padding. Every read and write by id on an `Unmanaged` entity now reads
  back the key columns of the row the statement reached and keeps it
  only when its own id, as `entity.RowKey` renders it, is the one asked
  for; a write that reached another row fails and rolls back. A row
  swapped in under another spelling between two statements is refused
  the same way, and so is a write whose key equality matches several
  rows (a SQLite `PRIMARY KEY (code COLLATE BINARY, …)` on a `NOCASE`
  column); a delete on a SQLite virtual table checks its rows before it
  runs, since SQLite cannot return rows from one. `/claims/550E8400-…/x` answers 404 for the row keyed in
  lowercase, a `CHAR(5)` row answers to its padded id, and the media
  check on an update reads the same row. entityui's bulk reads and named
  exports drop a row whose own id was not asked for, and return each row
  once.
- **A raw `App.Table` has no CRUD.** `migrate.Table.ToEntity` left
  `Exposure.CRUD` unset, which every reader takes as on, so the admin's
  `AllEntities` listed a migration-only table with write routes and
  `App.EntityCRUDMounted` reported routes that were never mounted. It
  sets `CRUD` false now.
- **entityui's record base ignores a trailing slash.** A record screen
  with no `Base` at `/invoices/42/` based its links at `/invoices/42`.
- **Deleting a saved view returns to the list.** The delete toasted
  "View deleted" twice, once from the form and once from the handler's
  toast header, and left the deleted view open on screen. It now toasts
  once and navigates to the list without the view.
- **`EagerLoad` joins on each entity's own key.** It assumed every parent
  and every belongs_to or many_to_many target was keyed on `id`, so an
  entity keyed on another column loaded nothing for those relations. It
  now reads the key from the entity and the registry, and returns
  `entity.ErrCompositeKey` for a composite one. Included and eager-loaded
  rows also keep a mixed-case field (`Sku`) on Postgres, which reports
  the folded column name (`sku`) the allow-list used to miss.
- **A relation field to an integer key failed on Postgres.** Its column
  was `TEXT`, and Postgres refuses a `TEXT` foreign key to an `INTEGER`
  key, so the `CREATE TABLE` failed. The column now takes the target
  key's integer type on Postgres, following a target key that is itself
  such a relation; SQLite keeps `TEXT`. A write stores the reference in
  canonical decimal (`"01"` becomes `1`), and a value that names no
  integer is a 400 `not_integer`.
- **`?include=` joined on `id`.** A `belongs_to` or `many_to_many`
  include to a target keyed on another column loaded nothing; it now
  joins on the target's key. Includes, nested filters and `EagerLoad`
  also stopped quoting relation columns, so a mixed-case one
  (`authorId`) matches the column Postgres folded when the unquoted DDL
  created it.
- **A nested include used the first hop's wire names at every depth.**
  `?include=student.enrollments` renamed each enrollment's columns with
  the student's `WireName`s and casing, not the enrollment's own. Each
  hop now converts with its own target's.
- **A child `AfterList` hook that replaced rows failed when the child's
  key had a `WireName`.** The fold-back looked the key up under the
  parent's casing of the column, while the included rows carry the
  child's wire names, so it found no key. It now reads the child's.
- **`EagerLoad` refused a `many_to_many` built with
  `entity.ManyToMany`.** It validated `Relation.ForeignKey`, which a
  many-to-many leaves empty, and failed with `invalid FK ""`.
- **A generated SQLite rebuild migration had a partial `Down`.** The
  rebuild has no inverse, but the other changes in the same file kept
  theirs, so a rollback "succeeded" into a table matching neither schema.
  A migration holding a rebuild now ships no `Down`.
- **A screen policy can send a client-side navigation to another
  site.** A policy redirecting to an absolute URL, an identity
  provider's login for instance, answered the runtime's fetch with a
  303. fetch follows that itself, the provider refuses CORS, and the
  reader got a "could not load" toast and stayed put. uihost now names
  the URL in `X-Gofastr-Document` on the partial and the whole-page
  fetch, and the runtime loads it as a page after checking it is
  http(s). A document load still gets the 303.
- **Audit rows name records whose key column is not `id`.** The audit
  trail's create and update rows always read an `id` column for
  `record_id`, so an entity keyed on `user_code` logged its `id` column,
  or nothing when it had none, while its delete rows and the Activity
  tab used the `user_code` value. Every row now reads the key column,
  under its `WireName`, its camelCase form, or its own name.
- **`DBQueue` index names on long table names.** Postgres cuts
  identifiers at 63 bytes, so with a table name of 62 or 63 bytes the
  dequeue and lane indexes got the same name and the second was never
  created. A name over the limit now ends in a hash of the table name.
- **The bulk bar names a move in the reader's language.** It drew the
  transition's declared English; it now reads the
  `entity.<entity>.transitions.<key>` catalog entry, as the record page
  does.
- **The pseudo-locale covers entity labels.** Column headers, status
  badges, view tabs and entity names fell back to plain English under
  `i18nui.AddPseudo`'s tag, so overflow checks never measured them. Their
  English fallback is now pseudo-localized there too.
- **Bar chart labels stay inside their slot.** The wrap budget assumed
  6.2px a character at 12px text; accented or capitalized labels run
  wider and spilled into the next label or off the plot edge. Lines now
  wrap at 7px a character with 4px of air between neighbours, and a
  slot too narrow for four characters (12 bars at 320px) keeps fewer
  instead of overflowing; the bar's `<title>` holds the full label.
- **App spec screens never overwrite each other's file.** An entity's
  screens all write `screen_<entity>_crud.go`, but the file-name check
  read each screen's own name: it refused two screens of one entity
  whose names differ only in case, and let a standalone screen named
  "Posts Crud" replace the posts screens' file. It now checks the file
  each screen writes, refuses two screens that share a name and so
  would declare one Go type, and counts the `<entity>_create` screen a
  `create: true` list adds in both checks.
- **Sidebar sections stay current in a static export under a base
  path.** `gofastr export` with a `BasePath` prefixed a link's href but
  not its `data-cui-match-prefix`, so after the first client-side
  navigation the section the link owns never read as current.

### Security
- **A blank reference skipped the belongs_to scope check.** A write
  whose foreign key was the empty string was treated as naming no row,
  so it could point at a row keyed on `''` that the caller cannot read.
  Only a null reference skips the check now.
- The repo builds with Go 1.27.3 (`toolchain go1.27.3` in go.mod, and in
  the go.mod `gofastr init` writes; the `go 1.27.0` floor is unchanged).
  1.27.3 fixes GO-2026-6809 in crypto/x509, which govulncheck reaches
  through the SMTP sender's TLS handshake. Build apps with 1.27.3.
- **A sensitive primary key stays out of the audit log's `record_id`.**
  The default scrub dropped an `api_key` or `token` column from the
  audit diff, but the same value went into `record_id` unchanged when it
  was the record's key, on delete always and on create and update for a
  one-word name. Without a `Redact` callback, a key whose column or wire
  name the scrub treats as sensitive is now stored as `sha256:<hex>` on
  every row, and the Activity tab hashes the id the same way to find
  them. Rows written before the change keep the raw id and no longer
  show in that tab. Create and update rows now strip control bytes from
  `record_id` the way delete rows already did. The CRUD handler hands
  its After hooks the record id it read from its own key columns
  (`crud.AuditRecordIDFor`), so a key two API versions give different
  wire names, even each other's, still names the right record, and a
  key sensitive under any version's wire name is hashed.
  `framework.AuditDigestID` tells a digest from a record id; the admin
  audit page uses it so a digest is never looked up, and so never
  titles or links a record whose own id is that digest string.

## [0.93.0] - 2026-10-20

**BREAKING** for generated clients, endpoints and SQLite decimals.
`entities/client/client.go` and the generated CLI are rebuilt from
`entities/` on every build, and only `custom.go` in the CLI is yours.
Custom endpoints move to `gofastr.settings.yml` with a linked typed
handler, `kiln freeze` writes them in that shape, and the Go client,
JS SDK and CLI each gain a typed call for them. An `Endpoint` route now requires a signed-in caller unless it sets
`Public`, which replaces `MCPPublic`. The `sdk` and `cli` settings
entries take YAML lists and refuse unknown keys. On SQLite, a decimal
column answers as a JSON string, as it already did on Postgres. The
generated clients upload Image and File fields. v0.93.0 also adds HTTPS
serving, `InTx` options, tracing spans for SQL, jobs and outbound HTTP,
content-hash static URLs and admin email previews. `gofastr upgrade`
lists each change.

### Added
- **Custom endpoints in `gofastr.settings.yml`.** An `endpoints:` entry
  names an entity, a method, a path relative to the entity, and a linked
  handler (`entities/refunds.go#RefundOrder`). `gofastr generate` reads
  the handler's input and output types from the Go source and writes
  `entities/endpoints.gen.go`, which registers the route with typed input
  and output schemas. The Go client and SDK gain `c.RefundOrder(ctx, id,
  client.RefundOrderRequest{...})`, the JS SDK `api.orders.refundOrder(id,
  {...})` typed in `client.d.ts`, and the generated CLI
  `orders refund-order <id> --flag ...`. OpenAPI documents path, query and
  body inputs separately and the response as the returned entity schema,
  the SDK docs site names the typed calls, and the SDK schema hash covers
  the endpoints. Input fields travel by their
  `handler.Bind` tags; generation refuses a name that collides with a
  resource method, a state move or a CLI verb, and a return type the
  client selection leaves out. `mcp: true` adds the MCP tool and
  `public: true` serves anonymous callers. New `gofastr generate
  endpoints` rebuilds only the registration. See `gofastr docs endpoints`.
- **The generated clients upload files.** For an entity with an Image
  or File field, the Go client and SDK gain `Create<Entity>WithFiles`,
  `Update<Entity>WithFiles` and `Patch<Entity>WithFiles`, which take an
  `<Entity>Files` struct of `*FilePart{Name, Body}` beside the write
  struct, plus `DoMultipart` for any other multipart route. The JS SDK
  sends a create, update or patch whose Image or File value is a `Blob`
  as multipart, and `client.d.ts` types those fields `string | Blob`.
  The generated CLI gains a file flag per such field, `--logo
  ./acme.png` or `--logo -` for stdin. Every part goes by its column
  name, which is what the server matches. Each client refuses, before
  sending, a file over the server's 32 MiB per-file limit, a body over
  its 64 MiB limit, and a `null`, which a multipart body cannot carry;
  the Go client also refuses a field set in the body and sent as a
  file. A JS batch refuses a `Blob`, since a batch sends JSON. Read-only
  file fields get no upload slot or flag. See `gofastr docs sdk` and
  `gofastr docs app-cli`.
- **`App.EntityEndpoints` queues endpoints for an entity registered
  later.** The next `App.Entity` or `TryEntity` call for that name merges
  them into its config, so they pass the same route and MCP checks.
  Calling it after registration panics, and `Start` fails while endpoints
  wait on a name nothing registered. See `gofastr docs
  entity-declarations` → Endpoints from another file.
- **`gofastr pack` recovers `hooks`, `middleware`, `plugins`, `helpers`
  and `endpoints`.** The first four come back from their wiring in
  `app.go` and `main.go` plus the names and descriptions in `stubs.go`, so
  a stub whose body you wrote still packs; endpoints come back from the
  `endpoints:` section of `gofastr.settings.yml`, and the check counts a
  linked handler file as hand-written. Pack also recovers nav icons,
  `app.output_dir`, `paragraph` blocks and `entity_form` blocks.
  `examples/ecommerce/app` packs with nothing unrecovered. When the
  app's `.env` is missing, pack says the spec has no admin seed password
  instead of leaving the check to report `app.go` and `main.go` as
  changed. See `gofastr docs app-spec` → Packing.
- **`App.InTx` takes transaction options.** `framework.TxIsolation(level)`
  sets the isolation level and `framework.TxReadOnly()` makes writes
  fail. The SQLite driver ignores the read-only flag, so on SQLite the
  transaction runs on its own connection with `PRAGMA query_only` set,
  cleared before the connection goes back to the pool. A nested `InTx`
  given options returns `framework.ErrTxOptionsNested` without running.
- **Content-hash URLs for static files.** `host.AssetURL("/logo.svg")`
  returns `/logo.svg?v=<hash>` for a file in the static directory or
  static FS, and the host serves a request at the file's current hash
  with `Cache-Control: public, max-age=31536000, immutable`. A plain or
  stale URL keeps the old behavior, and `gofastr dev` still serves
  static files `no-store`. A path with no file behind it comes back
  unchanged.
- **Email previews in the admin.** `admin.Config.EmailPreviews` lists
  the app's mails, each with a `Render` func that builds it from sample
  data. `/admin/emails` shows a tab per mail with its subject, text body
  and HTML body; the HTML loads in a frame from `/admin/_email/<name>`,
  served under a `sandbox` CSP with no scripts. A render error or panic
  draws a generic notice and logs the error's type.
- **`html.IFrame`.** An `<iframe>` with a required `Src` and `Title`,
  sandboxed by default: an empty `Sandbox` applies every restriction, and
  `Unsandboxed` is the named opt-out. `ExtraAttrs` cannot set `src`,
  `srcdoc`, `sandbox` or `title`. An iframe inside `ui.AspectRatio`
  now draws the kit's hairline border instead of the browser's inset one.
- **The app can serve HTTPS.** `framework.WithTLS` takes certificate
  files (read again when they change, so a renewal needs no restart),
  `AutoCert` for Let's Encrypt over TLS-ALPN-01, or a caller-built
  `*tls.Config`. TLS 1.2 is the minimum and HTTP/2 is offered over TLS;
  a config returned by `GetConfigForClient` below TLS 1.2 is raised to
  it. The startup banner, the first-run setup link and the local run
  record use `https://` URLs.
- **`framework.WithHTTPProtocols`** passes an `http.Protocols` to the
  server, for HTTP/2 without TLS (h2c with prior knowledge) behind a
  proxy. Unset, the server keeps net/http's default.
- **Spans for SQL, queue jobs and outbound HTTP.** `tracing.OpenDB`
  (package `core/tracing`) opens a database whose statements and
  transactions each get a client span, with literals in `db.query.text`
  replaced by `?` (backslash escapes in Postgres `E'...'` strings and in
  MySQL strings included, and double-quoted text on SQLite and MySQL). `tracing.Transport` wraps an `http.RoundTripper` with a
  client span per request; it sends no `traceparent` unless given
  `tracing.PropagateTraceContext()`. Handler calls on `MemoryQueue` and
  `DBQueue` run under a `process <job type>` consumer span, and the
  webhook battery's default delivery client uses the tracing transport.
  A failed call's span records the error's type, never its message.

### Changed
- **BREAKING: an app spec's `endpoints:` entries are `gofastr.settings.yml`
  endpoints.** Each takes `name`, `entity` (required), `method`, a `path`
  relative to the entity's route (`{id}/publish`), a `handler` link
  (`entities/posts_endpoints.go#PublishPost`), `description`, `mcp` and
  `public`. `generate app` merges them into the settings file, writes each
  linked handler as a typed stub that answers 501 until you write it, and
  writes `entities/endpoints.gen.go` and the client calls; it no longer
  writes `http.HandlerFunc` stubs into `stubs.go` or `Router().Handle`
  lines into `app.go`. An entity's own `endpoints:` list is refused with
  the move to make. `gofastr upgrade` finds both spellings. A route with no
  entity belongs in owned Go (`fwApp.Router().Handle`). `--add` merges a
  fragment's endpoints into the settings file, and its `--json` report
  lists the settings file and the rebuilt `endpoints.gen.go` and
  `client.go` under a new `updated` key instead of `skipped_existing`. See
  `gofastr docs app-spec` → Custom endpoints.
- **BREAKING: `kiln freeze` writes endpoints in the settings shape and
  refuses what has none.** Entity and scaffold endpoints freeze to the
  top-level list with a relative path and an
  `entities/<entity>_endpoints.go#<Name>` link. A world route, a scaffold
  endpoint with no entity, one whose path sits outside its entity's route
  (`/<table>/…`), or one that repeats another's route stops the freeze,
  naming it; delete it and register the route in
  owned Go after generation. See `gofastr docs kiln`.
- **`entity_detail` refuses `text:` and `fields:`.** The record page draws
  the entity's `display:` block, so both were accepted and never rendered.
- **BREAKING: `Endpoint` routes require a signed-in caller unless the
  endpoint sets `Public`.** The HTTP route answers 401 with no user on the
  request and sends `Cache-Control: no-store` when gated. Public or not,
  it refuses with 403 a cross-site write a plain HTML form could send (no
  `Content-Type`, urlencoded, multipart or `text/plain`). Before, an
  endpoint's HTTP route had only the middleware the app put in front of
  it, while its MCP twin already required a user. OpenAPI marks every
  endpoint operation but a `Public` one with the security schemes.
  `Endpoint.MCPPublic` is
  renamed `Public` and now opens both doors. `gofastr upgrade` lists every
  endpoint and each `MCPPublic`. See `gofastr docs entity-declarations`.
- **BREAKING: the generated CLI and `entities/client` are rebuilt from
  `entities/`.** `gofastr generate cli` writes `*.gen.go` files that say
  DO NOT EDIT; every run rebuilds them, removes the ones the entities no
  longer produce (a dropped entity, a deselected verb) and keeps
  `custom.go`, the one file that is yours. `customCommands` there can
  replace `login` and `logout` too. A second run no longer needs
  `--force`. A `cli` entry in `gofastr.settings.yml` (keys `binary`,
  `api_prefix`, `from_openapi`, the lists `only` and `exclude`, and
  `verbs`, one list or a map from entity to its list) makes plain
  `gofastr generate` and `gofastr build` rebuild the CLI; such a run
  rewrites only changed files and prints nothing when the CLI is current.
  `gofastr generate cli` reads the same entry, flags winning key by key,
  and a run no entry drove prints the entry to paste. The `main.gen.go`
  header no longer repeats the flags.
  New `gofastr generate client` rebuilds `entities/client/client.go`, and
  `gofastr generate`, `gofastr build`, `--watch` and `generate app --add`
  run it whenever that file exists, so the client follows a new entity or
  field. The first run on a CLI from an earlier release removes its old
  `.go` files and stops on any you edited; an edited `client.go` from an
  earlier release is left alone with a warning. Move the edits into
  `custom.go` or another file of package `client`, then run the generator
  once with `--force`. `gofastr upgrade` lists both. See `gofastr docs
  app-cli` and `gofastr docs codegen`.
- **BREAKING: the `sdk` entry in `gofastr.settings.yml` reads `only`,
  `exclude` and `targets` as YAML lists.** A comma string such as
  `only: posts,comments` is refused with the list spelling
  (`only: [posts, comments]`); before, `targets` as a string was dropped
  without a word. An unknown key or a value of the wrong type fails
  `gofastr generate`, `gofastr build` and `gofastr generate sdk` with the
  entry named, where it used to be ignored. `gofastr upgrade` finds the
  comma strings. See `gofastr docs sdk`.
- **A nested `App.InTx` runs under a savepoint.** Called inside another
  transaction, `InTx` used to join it outright, so an error it returned
  left its writes in place and, on Postgres, a failed statement aborted
  the whole transaction. It now rolls its own writes back to a savepoint
  on error and leaves the outer transaction usable; on success the
  savepoint is released and the writes commit with the outer owner.
  Events from CRUD writes inside a rolled-back savepoint are dropped.
  A savepoint whose release fails is rolled back too.
- **`framework.SetupRunner.SetupURL` takes the scheme.** It is now
  `SetupURL(scheme, addr string)`, so the setup link the app prints is
  `https://` when the app serves TLS. A custom runner adds the argument.

### Fixed
- **A decimal column reads back as a string on SQLite.** OpenAPI and the
  generated clients type `schema.Decimal` as a string, and Postgres
  returns one, but SQLite gives a `DECIMAL` column numeric affinity, so
  every read there answered `12.5`. The Go client could not decode such
  a row at all. Every CRUD read, included relation rows and
  `GroupCountAll` groups among them, now answers `"12.5"`.
- **A multipart write stores no file for a column it cannot set.** A
  file part named after a read-only or hidden Image or File field was
  saved to storage, and then the write skipped the column, so nothing
  referenced the object; a caller could repeat it to fill the disk. The
  part is ignored now, as the write ignores a value for that column.
- **`App.Shutdown` during boot always stops the server.** A `Shutdown`
  that ran while `Start` was still booting could miss the server `Start`
  was about to adopt, and `Start` then served forever. `Start` now sees
  any earlier `Shutdown` and returns.
- **`ui.CodeBlock` with `Wrap` breaks long tokens.** A URL or hash longer
  than the line ran past the block's edge; it now wraps.

## [0.92.0] - 2026-10-19

**BREAKING** for the CLI and project files. GoFastr has one concept for
generated code, code generation, and `gofastr.yml` is the app spec it
generates from. Every generator runs as `gofastr generate <name>`:
`gofastr generate app [spec]` replaces `--from`. One
`gofastr.settings.yml` replaces `gofastr.codegen.yml`,
`gofastr.contracts.yml` and `gofastr.isolation.yml`, and an old file
makes its reader refuse and name the section to move it to.
`kiln/freeze.BlueprintYAML` is renamed `AppSpecYAML`. `gofastr upgrade`
lists each change. v0.92.0 also stamps a provenance marker on generated
files, makes `gofastr pack` name what it could not recover, and adds
`gofastr unmount` and `gofastr remount` to copy a gofastr package into
the app and back.

### Added
- **Generated files say who made them and whether you edited them.**
  The built-in generators stamp a provenance marker at the top of each
  file with a comment syntax (not JSON or binary files), in that syntax:
  `//gofastr:generated generator=app kind=scaffold version=v0.92.0 sum=…`.
  The sum covers the rest of the file, so an edit shows. A scaffold
  generator that finds files in its way says which generator and version
  wrote them and how many were edited before it refuses. `--force` lists
  only the marked files edited since they were generated, so an untouched
  file from an older template no longer counts as an edit; a file with no
  marker is still listed when it differs from its replacement. A derived generator
  (`sdk`, `styles`, the codegen config) warns before it rebuilds an
  edited file and names the source to change instead; the codegen
  manifest records a sum per file for this. `gofastr verify` counts
  marked files by generator and version and warns about edited derived
  files. The checked-in `*.gen.go` style files carry the marker now.
  An owned style sheet's marker is not style: `gofastr gen styles`
  embeds and hashes the sheet without it, and GOFASTR1814 compares the
  same bytes, so a sheet the app generator stamps stays current.
  See `gofastr docs codegen`.
- **`gofastr pack` says what it could not recover.** After packing, it
  regenerates the app from the packed spec in memory and compares each
  Go file at the app root and directly under `entities/` with the file
  on disk, ignoring CRLF line endings and a byte order mark. It lists
  hand-written files it left out of the spec, files it could not read
  (a symlink, no read permission), and every file the spec generates
  differently or that is missing from the app, with the reason the
  provenance marker gives (edited since generated, untouched but still
  different, no marker) and the part of the spec the file carries. A
  file an older gofastr release wrote is not blamed on pack alone. The
  notes and pack's errors go to stderr when the spec goes to stdout.
- **`gofastr unmount <package>` copies a gofastr package into the app;
  `gofastr remount <package>` goes back.** Unmount copies the package
  and the packages under it from the gofastr version go.mod requires
  into `unmounted/<package>` (or `--out`), rewires the module's imports
  to the copy and records it in a new `unmounted:` section of
  `gofastr.settings.yml`. It refuses a package that imports gofastr
  internals, or that a package outside the module imports (another
  gofastr package, a plugin), and names the importers. Each Go file of the copy carries the
  provenance marker with a new `source=` field, and each package gets an
  `unmounted.gen.go` whose `init()` registers the copy with the new
  `core/unmounted` package: `App.Unmounted()` and the `app_unmounted`
  MCP tool report the copies a binary was built with. Remount rewires
  the imports back, deletes the copy and its entry, and refuses a copy
  edited, added to or trimmed since unmount unless `--force`. `gofastr
  verify` warns when a copy is older than the gofastr release go.mod
  requires (with the upstream diff link), when an entry has no files and
  when a marked copy is not listed; `gofastr upgrade` and `gofastr audit
  deps` list the copies, and upgrade never changes them. GOFASTR2002
  (hand-rolled battery) stays quiet on a copy of that battery. See
  `gofastr docs unmount`.

### Changed
- **The blueprint is now the app spec.** GoFastr has one concept for
  generated code, code generation, and the `gofastr.yml` app description
  is the app spec it generates from. The CLI, errors and docs say "app
  spec"; `gofastr docs blueprints` is now `gofastr docs app-spec` and the
  tutorial is `gofastr docs tutorial-app-spec`, with no alias for the old
  names. The YAML format and flags are unchanged. Newly generated code
  behaves the same; its comments say app spec and its toast stack is
  named `app-toasts`. Apps already generated need no change.
  **BREAKING**: `kiln/freeze.BlueprintYAML` is renamed `AppSpecYAML`,
  with the same input and output.
- **BREAKING: every generator runs as `gofastr generate <name>`, and
  `gofastr generate app [spec]` replaces `--from`.** The spec defaults
  to `gofastr.yml`; `--out`, `--force`, `--add`, `--dry-run` and `--json`
  work as before. Generators come in two kinds. Scaffold generators
  (`app`, `entity`, `screen`, `package`, `cli`) write code you own, once.
  Derived generators (`sdk`, `styles`) rebuild DO NOT EDIT code on every
  run. Plain `gofastr generate`, which `gofastr build` and `--watch`
  call, runs only the codegen config, and the config lists derived
  generators and extensions: a scaffold entry there is an error naming
  the command to run instead. A `styles` entry, with optional
  `config.patterns`, now runs `gofastr generate styles` after the other
  generators. Plain `gofastr generate` refuses arguments it does not
  know, where it used to drop them, and `--watch` no longer watches an
  app spec. `gofastr generate --help` lists the generators by kind.
- **BREAKING: one settings file, `gofastr.settings.yml`, replaces
  `gofastr.codegen.yml`, `gofastr.contracts.yml` and
  `gofastr.isolation.yml`.** It sits at the project root with one
  top-level section per reader: `codegen`, `contracts`, `isolation`.
  Each reader looks for it from its start directory up to the project
  root (`go.work`, `go.mod` or `.git`) and never past it. The app spec no
  longer carries these sections: a `codegen:`, `contracts:` or
  `isolation:` key in `gofastr.yml` is an error that names the settings
  file. An old file found where a reader looks makes that reader refuse
  and name the section to move it to, so a relaxation or a disabled
  setting never drops away unnoticed. `gofastr generate --config` and
  `gofastr verify --config` take a settings file, and `gofastr init`
  writes `gofastr.settings.yml`. `gofastr upgrade` reports each old file.
  `codegen.HasCodegenSection` and `contracts.ConfigFileNames` are gone;
  `core/settings` (`settings.Find`, `File.Section`) reads the file.
  See `gofastr docs settings`.
- **`gofastr generate package <name>` records its copy as unmounted.** The
  chrome package copy carries the unmount marker and an
  `unmounted.gen.go`, and lands in the `unmounted:` section of
  `gofastr.settings.yml`, so verify, upgrade and `gofastr remount` handle
  it like any other copy. Each copy is its own entry, so two binaries
  can still carry their own copy of one package.

### Fixed
- **`gofastr validate` no longer fetches webfonts.** It rendered the app
  to catch codegen errors and fetched the theme's fonts from Google on
  the way; the render it checks now skips the fetch.
- **Affected selection sees tests that read other packages' files.** A
  test file declares what it reads with `//gofastr:test-reads <pattern>`:
  a `dir/` prefix, a base name such as `*_test.go` or `package.json`, a
  root-anchored path such as `/README.md`, a repo-relative glob where
  `**` spans directories, or `importers:<import path>`. The last matches
  a change in any package that imports the path directly, and any changed
  `.go` file that names it now or named it at the base (a nested module,
  a deleted file, a dropped import) or names it in a build-excluded file
  of a changed package. `cmd/affected` selects the package when a changed
  file matches. Only a `//` comment declares; a raw-string fixture line
  does not. A pattern that can never match, such as `./README.md`, is an
  error, as is a directive with no space before its patterns. A file a
  package's `//go:embed` patterns cover now selects that package, even
  inside a nested package and even once deleted:
  an app generator template edit under `cmd/gofastr/packages/` used to select
  neither the generator nor the gates that run it. The tests that read
  outside their package now declare those inputs, found with the new
  `scripts/test-reads-audit.sh`. The script records each file a test
  binary opens through Go's test log and fails on one whose change would
  not select its reader; reads by a subprocess or a build-tagged test
  stay outside what it sees. CI runs it weekly. Before this, a hand-booted Chrome
  added in another package never selected the launch-allowance gate, and
  one reached main that way. `core-ui/runtime`'s repo-tree gates moved to
  `core-ui/runtime/internal/treegate`, so a change to a registry caller
  selects that small package instead of the runtime's browser suite.
- **`laxenvelope` names a strict package's own types to its importers.**
  A package that strict-decodes a type it declares, the way core/mcp
  decodes its `Request`, now exports that type in its strictness fact, so
  an importer decoding the same type through `json.Unmarshal` is
  reported. Unexported types count too: an importer reaches one through
  an exported constructor. The fact left all of them out before, and it
  reached only direct importers, so a package holding the type through
  a third package's accessor went unchecked.

## [0.91.0] - 2026-10-19

**BREAKING** for generated code. The generated Go HTTP client and Go SDK
make optional `<Entity>Input` fields pointers, and the generated typed
repository writes through `<Entity>Input` and `<Entity>Patch` instead of
the model struct. Regenerating changes call sites; the compiler names
each one. A field named `input`, `patch` or `move`, or `<enum>_value`
next to an enum, now fails `gofastr generate`: its column var meets one
of the new type names. v0.91.0 also gives generated repositories typed
moves and enum values, lets the sidebar drawer and the TOC phone sheet
take a localized close label, carries page metadata through client-side
navigation, protects stored files with owner-only ACLs on Windows, and
makes affected tests replay from Go's test cache.

### Added
- **Localized close labels.** `ui.SidebarConfig.DrawerCloseLabel`
  names the sidebar drawer's dismiss button, and `MountSidebarFunc`
  resolves it per request. `ui.TOCConfig.CloseLabel` and
  `interactive.PhoneSheetConfig.CloseLabel` do the same for the TOC's
  phone sheet. Empty keeps the English default.
- **Select options carry extra attributes.** `ui.SelectOption.ExtraAttrs`
  and `headless.Option.ExtraAttrs` add per-option metadata such as `lang`
  or a short display label. The control keeps `value`, `selected` and
  `disabled`.
- **Sidebar groups nest at any depth and show a `Count`.** Every level
  uses the same disclosure and current-link contract, and the `Count`
  slot renders on a group's label as well as a link's.
- **`app.AreaSpec.Contents`** keeps a route area addressable for swaps
  without adding a box, so a sticky header inside it still composes.
- **`ui.ComponentCSS` and `ui.WithComponentCSS`** collect the CSS for the
  framework controls a document renders, for a standalone page with no
  UI host: the first returns it for an external stylesheet, the second
  inlines it.
- **`storage.LocalStorage` and `upload.LocalStorage` have `Close()`.** It
  releases the pinned directory handle, which otherwise stops Windows
  from removing the storage directory. Call it at shutdown, after
  readers from `Get` and `GetRange` are closed.
- **CI builds the module on Windows** with `CGO_ENABLED=0`.
- **The typed repository runs moves.** An entity with `States` gets one
  `<Entity>Move` constant per move and `Repo.Transition(ctx, id, move)`,
  which calls `CrudHandler.RunTransition`. System moves are included,
  since Go code is the caller they exist for.
- **Generated enum fields carry typed values.** An enum field with
  declared values gets `<Entity><Field>Value` and one constant per value
  (`PostsStatusDraft`). The model, the write shapes and the column take
  that type: the new `framework.NewEnumColumn[T]` builds a column whose
  `Eq`, `Neq` and `In` accept only `T`. A value that has no identifier
  form, or whose constant name another name in the file already uses,
  gets no constant and is listed in a comment.
- **`make bench-smoke` runs every benchmark once.** It covers the
  packages `cmd/affected` selects, with `-benchtime=1x` and Postgres
  skipped, and CI runs it after the test step, so a benchmark that
  stops working now fails the build.

### Changed
- **SQL fragments and route gates cost fewer allocations per request.**
  `core/query` returns a clean identifier or fragment as is instead of
  building a `strings.Replacer` for it each time, and `core/router`
  computes a route's gate key once at registration instead of on every
  request.
- **`gofastr verify` skips directories whose name starts with `_`**, the
  same ones `go build ./...` ignores and a directory `//go:embed` leaves
  out. Nothing there ships, so its findings were noise.
- **A disk-backed `core/static.Handler` opens its directory per request.**
  It used to hold one `os.Root` for its lifetime, which pinned the
  directory on Windows and kept serving it after a replacement. The
  handler resolves symlinks once, when it is built. Each request reopens
  that resolved path and checks it is still the directory the handler
  started with. A directory replaced at that path answers 404, so create
  a new handler to serve it. After a symlink flip, the old handler keeps
  serving the old target while it exists. The UI host's disk-backed
  static files work the same way.
- **The Timeout middleware answers 504 when a buffered handler finishes at
  or after its budget**, whichever of the handler and the timer the
  middleware sees first. A handler that finished exactly at the deadline
  got a 200 or a 504 depending on that order. One that finishes before
  the deadline keeps its response.
- **Owner-only files get a protected owner-only ACL on Windows**, where
  Unix modes mean nothing: `battery/storage`,
  `core/upload`, data export directories, harness session stores and
  bundles, the isolation data directory, desktop app state, and the
  kiln journal. ACL changes stop at the storage root, even when two
  spellings of it differ only in letter case.
- **`gofastr init` keeps the CLI's Go requirement.** The generated
  `go.mod` uses the release's minimum Go version and preferred toolchain
  even when the `go` on PATH is older, and `init .` keeps a higher
  requirement already in `go.mod`, `toolchain default` included. A
  project name must also start with a letter or digit.
- **Navigation opens every ancestor of the current link.** Active-link
  handling opens each enclosing disclosure in that navigation (not an
  enclosing site menu), updates a button-dialect group's `aria-expanded`
  and panel, compares canonical URL pathnames so a Unicode href matches
  its encoded form, and recomputes the selection when a lazy drawer
  opens. A current sidebar link wins over a saved closed state.

### Removed
- **Widget roots no longer carry `data-hydrate`.** Nothing read the
  attribute. `Widget.Render` emits `data-widget`, plus `data-behavior`
  when the component has actions.

### Fixed
- **Affected tests replay from the cache instead of re-running.** On an
  unchanged tree a second `make test` re-ran 14 packages (75 s), and CI
  replayed none of 158 packages from its restored cache.
  Tests read files the suite itself rewrites: the coverage manifests in
  the module's `.gofastr/`, the blueprint gates' scratch packages inside
  `examples/<name>/`, the `.env` the pack tests wrote into
  `examples/meridian/`, the SQLite files the blog, spa and api-tour
  examples created in their own directories when the livereload test
  booted them (they ignored `DATABASE_URL`; they read it now), and the
  `.git` directory, which framework/isolation stats and every repo
  walker's root listing carries. Any `go build` a test ran inside the
  repo stamped VCS data by running git, which moved `.git`'s mtime.
  `cmd/affected` now points `GOFASTR_AXE_COVERAGE_DIR` at a per-checkout
  directory in the user cache and adds `-buildvcs=false` to the wrapped
  command's `GOFLAGS`, the embedcheck mutation checks edit fixtures in
  the loader's overlay rather than on disk, the gates generate under a tracked
  `_scratch/blueprints/` without touching its listing, the pack tests
  read a copy, repo walkers skip `_` directories, and isolation reads
  `.git/HEAD` instead. The git hooks drop the `libexec/git-core` entry
  git puts at the front of PATH, which every test that runs `go` or
  `git` records, so a hook's sweep replays what `make test` ran. A
  second local sweep now replays all 282 packages in 7 s. CI stamps each
  file with an mtime derived from its content
  (`scripts/content-mtimes.sh`), restores the coverage manifests with the
  test cache, since a replayed package records nothing, and a second run
  of the test step must replay every package
  (`scripts/test-cache-replay.sh`). The blueprint gates that `go run` the
  generator list its in-module sources as inputs (`internal/testinputs`),
  so a generator change re-runs them instead of replaying a stale pass.
- **`gofastr verify` reads the semantic-coverage manifest from
  `GOFASTR_SEMANTIC_COVERAGE_DIR` or `GOFASTR_AXE_COVERAGE_DIR` when one
  is set.** Tests and servers already wrote it there, while verify kept
  reading the project root, found no manifest, and skipped every coverage
  rule.
- **The typed repository stores false, 0 and "".** `Create` and `Update`
  marshalled the model struct, whose `omitempty` value fields dropped
  zero values: an update could never clear a field, and a create could
  not override a non-zero default. Writes now take `<Entity>Input`
  (`Create`, `BatchCreate`, `FirstOrCreate`; every field but a required
  one with no default is a pointer) and `<Entity>Patch` (`Update`,
  `BatchUpdate`; every field a pointer), and return the stored row.
  Neither carries an `auto_generate` column, and the patch leaves out
  the owner and tenant columns, which an update never sets. A
  `cascade_write` relation keeps its children: `PostsInput.Comments`
  takes `[]CommentsInput`, and `PostsPatch.Comments` takes
  `[]PostsCommentsNested`, a `CommentsPatch` plus the `ID` of the child
  to update. Entity files generated before this keep the old repo until
  regenerated.
- **The generated Go client's create and PUT send zero values.** Its
  `<Entity>Input` had the same `omitempty` value fields, so
  `CreatePosts` could not send `published: false` over a `true` default.
  Fields are now pointers, except a required field with no default.
  Its patches also drop `auto_generate`, owner and tenant columns.
- **The JS SDK and `/openapi.json` stop requiring defaulted fields.** A
  required field with a default may be left out of a write, and the
  server takes the default. The `client.d.ts` `Input` marks it optional,
  and the create and PUT request schemas leave it out of `required` (the
  response schema keeps it). Update bodies in both, and the `d.ts`
  `Patch`, no longer offer the owner field or the `multi_tenant` tenant
  column, which an update never sets, and `Input` no longer offers an
  `auto_generate` column.
- **`CancelTask` can no longer lose to the handler it cancels.** It
  canceled the run's context before storing CANCELED, so a handler that
  completed on wake stored COMPLETED first and the client got -32002
  for a task it had asked to cancel. The cancel now halts the local
  run, which refuses the run's writes, then stores CANCELED, then wakes
  the run, so neither a handler that completes on wake nor one writing
  in a loop can beat it. A `SendMessage` waiting on the run answers
  CANCELED rather than its stale WORKING copy, and a subscriber gets
  the CANCELED event even when the handler returns before the cancel
  publishes. The cancel's store calls are bounded by the store timeout,
  so a stalled database fails the cancel instead of holding the run.
- **56 framework benchmarks ran again.** Their requests had no session,
  so generated CRUD answered 401 before the handler body ran, and the
  POST/PUT body helper ended its input with an error other than
  `io.EOF`. The fixtures now stamp a user, mount CRUD through
  `App.CrudHandler`, and switch off the test-binary-only markup scan
  and render diagnostics, so they time what a deployed app runs.
  `BenchmarkT7_FilteredList_NetHTTP` now runs the same COUNT query and
  selects the same columns as the generated list it is compared with.
- **The benchmark docs carry current numbers.** `benchmarks.md` has
  the 2026-10-18 binary sizes, build and dev RAM, and Tier 7 gaps, and
  drops the cgo note (the SQLite driver is pure Go); `perf-results.md`
  opens with a v0.90.0 snapshot ahead of the v0.26.0 one.
- **The runtime-size docs state the core's real gzip size**: 14,176 B
  at level 6 and 16,426 B at level 1, which is 2,090 B past the 14 KiB
  congestion window. They said 12.3 KB. Page metadata on navigation
  (below) accounts for 519 B of the level-6 figure.
- **Page metadata follows client-side navigation.** A navigation payload,
  and the cached copy a Back replays, carries the destination's `lang`,
  page `<meta>` and alternate links, inert until the runtime applies
  them, so a static area no longer keeps the previous page's language.
- **Theme preferences follow other tabs.** The color-scheme runtime
  listens for its storage keys, and going from an explicit scheme back to
  auto keeps following the OS setting.
- **Widget openers mirror the widget.** Every opener of a widget updates
  `aria-controls` and `aria-expanded` when it opens and closes.
- **The plugin host waits for deferred adapters.** Its broker scanned
  before parser-deferred adapter scripts registered; it now waits for
  them, and still starts at once when injected after `DOMContentLoaded`.
- **An option-only Markdown fence keeps its title.** A fence whose info
  string is only options, such as `title="notes.txt" showLineNumbers`,
  read the title as the language and dropped the filename. An info
  string whose first word is a `key=value` option or a `{...}` block is
  now all metadata, and `ui.Markdown` renders the title, numbering and
  highlights itself. A bare first word such as `showLineNumbers` still
  reads as the language.
- **A framed `CodeBlock` no longer repeats its ID.** The caller's ID stays
  on the root, and the body and copy target use `<ID>-code`.
- **`gofastr verify` and `gofastr upgrade` skip a static export with a
  custom name.** They read its exported framework CSS as authored UI and
  reported it as ownerless.
- **An entity event stream acknowledges after its listeners are
  installed.** The `: subscribed <entity>` comment went out first, so a
  write published right after it could miss the stream.
- **An RPC success toast finishes before its navigation**, including when
  the feedback module has to load first. A form submitted again during
  that wait keeps the older request from navigating.
- **Line endings no longer change results on Windows.** The CSP linter's
  comment directives, the docs compile-directive scan and the headless
  golden comparisons accept CRLF, and `.gitattributes` checks text out
  with LF.
- **A macOS bundle zipped on Windows keeps its executables runnable.**
  The zipper sets execute modes under `Contents/MacOS`, which Windows
  cannot report, and closes the output before removing a failed archive.
- **An entityui job lease can resume at its exact expiry**, and a
  released lease no longer waits for another clock tick before a retry.

## [0.90.0] - 2026-10-18

**BREAKING.** `router.Param` panics on a wildcard name its route does
not declare, so a handler that read a misspelled name and carried on
with `""` now answers 500. v0.90.0 also adds gauge, scatter and
heatmap charts, a `gofastr verify` rule for permission names nothing
registers, and a layout overlay under `gofastr dev`.

### Added
- **`style.DefaultDarkColors()` returns the canonical dark palette.**
  `framework/ui/theme.Default` reads it, and so does the contracts
  checker, which no longer links `framework/ui`.
- **`framework/testkit/cssweight` weighs the CSS a page ships.**
  `cssweight.Measure` fetches a page over HTTP and reports the raw and
  gzip size of its `<style>` elements and linked stylesheets, so an
  app's test can cap CSS per page. It resolves sheets against
  `<base href>`, and a redirect, a non-200 answer or an `@import` is an
  error rather than a number. Meridian and the docs site now fail
  their tests when a page ships more than 17 KiB of gzipped CSS, or
  less than 1 KiB, which means its stylesheets went missing; the
  heaviest pages today sit at about 15.5 KiB.
- **`ui.Gauge` draws one reading against a range.** It's a semicircle
  SVG dial on an ARIA `meter`, so a screen reader announces the label and
  `ValueText` ("CPU, 73%"). `Bands` color the arc by where the reading
  falls, for example green from 0, amber from 70, red from 90. A
  reading outside `Min`–`Max` pins the arc to the nearer end, and NaN
  or Inf shows the charts' empty state. The reading's text scales with
  `Size`. Meridian's Revenue page shows the share of payments that went
  through on one.
- **`ui.ScatterChart` plots points on two numeric axes**, one color per
  series. Both axes round out to steps of 1, 2, 2.5 or 5 × 10ⁿ, across
  zero when the data does, and tick labels keep as many decimals as the
  step needs (past 1e9, or under a step of 1e-9, as many significant
  digits: 7.5e+299, not 7.500000000000005e+299). The legend wraps at the right edge. A point with a NaN or
  Inf coordinate is skipped. Each point's `<title>` names its series,
  label and coordinates. The gallery shows seats against MRR for two
  plans.
- **`ui.Heatmap` shades a grid of values** in four strengths of one
  palette color, from the lowest value to the highest. A NaN cell draws
  empty and its title says "no data". A row label past 17 characters
  ends in an ellipsis, and a grid too wide for `Width` widens the chart
  instead of clipping. `Values` must match `Rows` × `Cols`, or it
  panics. Meridian's Revenue page counts payments by method and status
  on one.
- **A `ui.Card` with an `ID` names its heading `<ID>-title`,** so a
  chart inside the card can pass that id as `LabelledBy` and read as a
  named image. Meridian's Revenue charts and the gallery's chart demos
  are named this way.
- **`entityui.UI.CountInt`** is `Count` as an `int`, under the same read
  gate and scope, for figures a host computes with.
- **GOFASTR1904 flags a checked permission the app never registered.**
  Once an app calls `RolePolicy.Register`, `gofastr verify` checks every
  permission name in an entity `Access` block, `CrossOwnerRead`,
  `ReadScopeConfig.Unrestricted`, a transition or action `Permission`,
  and the arguments of `RequirePermission`, `Can`, `CanResource` and
  `CanResourceExact` against the names registered in the same binary,
  and suggests the nearest one. It reads literals and package-level
  constants, including another package's (`perms.PlansWrite`). A typo
  there compiled before and denied every role granted the intended name. The
  contract catalog holds 79 rules.
- **A layout layer under `gofastr dev`.** Alt+Shift+L draws the page's
  layout layers, slots, outlets, areas, owned-style scopes, islands
  and widgets as labelled outlines. It also marks every box that
  scrolls, and every element that runs past the viewport (skipping
  ones inside a scroll box, a clip or a fixed drawer), with a legend
  that says when the page scrolls sideways. It draws over open
  popovers and modal dialogs, such as a phone sheet or a confirm. The
  on/off choice survives reloads. Served at `/__devlayout.js` only in dev;
  `GOFASTR_DEV_LAYOUT=0` turns it off. See
  [dev-livereload](framework/docs/content/dev-livereload.md#layout-layer).

### Changed
- **`router.Param` panics on a name its route does not declare.**
  `Param(r, "idd")` on `/posts/{id}` returned `""`, the same as an empty
  value, and the handler carried on; now the request fails with a 500
  through the recovery middleware, naming the route and the wildcard. A
  request with no pattern, built by hand with `SetPathValue`, is not
  checked. `handler.Bind` still leaves a `path:` field the route does
  not declare at zero, so one input struct can serve `POST /users` and
  `PUT /users/{id}`.
- **The Timeout middleware reuses its response buffers.** It holds each
  response until the handler returns, and it used to grow a new buffer
  for every request: about 260 KB allocated and 36 µs spent to send a
  96 KB page, against 2 KB and 4 µs now. Once the handler has returned
  or panicked, a write or `Hijack` through its response writer fails
  with an error and a late `WriteHeader` or `Flush` does nothing, instead of
  reaching a buffer that may belong to the next request or a response
  net/http has already finished. This holds for streamed responses too.
- **GOFASTR1808 judges colour fallbacks in design-system code.** A
  `var(--color-*, …)` or `var(--tk-*, …)` fallback must be the token's
  hex for its scheme, in any case or the short form: the dark value
  inside a dark-scheme rule or `@media` block, the light value
  elsewhere. A keyword such as `currentColor` passes; `rgba()`, a named
  colour or another hex does not. Owned styles stay unjudged, since
  their colours follow the app's theme. `battery/desktop/ui` checks
  its own against its per-platform theme in its tests.

### Fixed
- **A late answer to an older form submit no longer undoes the newer
  one.** A form with no `data-cui-rpc-signal` has no abort, so two quick
  submits raced: a slow refusal landing after a newer success painted
  its errors, and a slow success cleared a newer refusal's errors or
  reset what the user had typed since. Only a form's newest request now
  touches the page, whether the form or a button inside it sent it.
- **`gofastr init` writes GoFastr's `go` and `toolchain` lines.** The
  scaffolded `go.mod` carried whatever `go mod init` wrote, the local Go
  version (`go 1.25.5` in one report), below the `go 1.27.0` the
  framework needs.
- **`gofastr init` refuses Windows device names.** `con`, `nul`, `aux`,
  `prn`, `com0`–`com9` and `lpt0`–`lpt9` passed the name check; on
  Windows `os.Stat("nul")` succeeds, so `init nul` reported "already
  exists". They are refused on every OS, so a project made on Linux
  still checks out on Windows.
- **Colour fallbacks name the current palette.** About 350 across
  `framework/ui`, `framework/uihost`, `core-ui` and the batteries still
  named the colours from before the contrast pass, such as
  `var(--color-danger, #DC2626)` where the theme declares #B91C1C, or
  a translucent `rgba(0,0,0,0.1)` border. They only render on a page
  with no theme CSS, where the section menu's #A1A1AA text failed AA on
  white, but a reader took them for the token's value.
  `battery/desktop`'s now match its macOS theme.
- **A hovered secondary `ui.BackToTop` keeps its strong border in dark
  mode.** A dark-scheme block restated the base rules' tokens, and
  because it came later it reset the hover border to `--color-border`.
  The block is gone; the theme's dark palette already flips the tokens.
- **A chart's empty state keeps its `ID`.** `ui.BarChart`,
  `ui.LineChart` and `ui.PieChart` dropped the host's `ID` when they had
  no data to draw, so a link or test selector aimed at the chart found
  nothing in exactly that state.
- **A long bottom sheet scrolls on a phone.** `touch-action: none` sat
  on the sheet root, which is its own scroller, so a finger had no pan to
  perform and the list never moved. It now sits on the drag handle only,
  where a drag down still closes the sheet.
- **A wide Markdown table scrolls sideways in its own box.** `ui.Markdown`
  wraps each table in a focusable `.fui-markdown__table` scroll box and
  caps each column at 70% of it, so a table wider than a phone no longer
  runs past the page or gets clipped by a column that hides overflow.
- **A `uihost` served on its own answers `/__livereload.js`.** Its pages
  linked the livereload client under `gofastr dev`, but only
  `framework.NewApp` registered the route, so a host served directly or
  mounted on a router built by hand got a 404 and never reloaded.
  `UIHost.Mount` now registers the dev script routes on the router it
  mounts on.

