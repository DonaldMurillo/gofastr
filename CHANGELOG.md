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
- **`DBQueue` retries can follow a delivery policy**
  ([#680](https://github.com/DonaldMurillo/gofastr/issues/680)).
  `WithRetrySchedule(steps...)` sets each wait from a list (the last step
  repeats), and `WithRetryJitter(fraction)` spreads every backoff or
  schedule wait by up to ±fraction of itself. A handler can return
  `queue.RetryAfter(d, err)` to set the next wait itself, for a
  receiver's `Retry-After`, or wrap its error with `queue.Permanent` (or
  `%w` of `queue.ErrPermanent`) to dead-letter the job at once. Both work
  in the `MemoryQueue` worker loop too. `Job.MaxAge` stops the retries
  once the next one would fall past `CreatedAt + MaxAge`, on every
  backend. `core/backoff.Jitter` is the shared jitter.
- **`Job.SequenceKey` runs the jobs that share it one at a time, in
  order** ([#680](https://github.com/DonaldMurillo/gofastr/issues/680)).
  On `DBQueue`, a keyed job waits while any job with its key runs or an
  older one is pending, retries included; a dead-lettered job frees the
  key, and a job whose worker died is reclaimed once its lease runs out.
  A unique partial index allows one claimed job per key.
  `MemoryQueue` and `RedisQueue` refuse the job with
  `queue.ErrSequenceUnsupported`. `DBQueue` adds the `max_age_ns` and
  `sequence_key` columns and two partial indexes to an existing table on
  boot.

- **`battery/webhook` signs deliveries in a receiver's existing format**
  ([#680](https://github.com/DonaldMurillo/gofastr/issues/680)).
  `Options.Signature` takes a `SignatureFormat`: the signature and
  timestamp header names, a value prefix such as `sha256=`, and the
  signed string with `{timestamp}`, `{event}`, `{delivery_id}` and
  `{body}` placeholders. A format with no timestamp is refused, and `New`
  panics on an invalid one. `Subscribe` refuses a credential header the
  format writes. `SignatureFormat.Verifier` checks the same format on
  the receiving side. See
  [webhooks](framework/docs/content/webhooks.md#signing-in-a-receivers-existing-format).

### Changed
- **A shutdown that lands during a `DBQueue` handler no longer spends
  the attempt** ([#680](https://github.com/DonaldMurillo/gofastr/issues/680)).
  When the context passed to `Start` is cancelled mid-run and the handler
  fails, the job goes back to pending with the attempt refunded, as a job
  claimed just before shutdown already did, so a deploy no longer
  dead-letters a `MaxAttempts: 1` job it interrupted.
- **`a2a.NewServer` refuses a store whose trim methods would never run**
  ([#653](https://github.com/DonaldMurillo/gofastr/issues/653)). A store
  with `TrimTerminalTasks` or `TrimPushConfigs` that does not implement
  `RetentionTrimmer` (a custom store still on the pre-v0.96.0
  `TrimTerminalTasks(ctx, owner, keep)`) used to compile and then never
  be trimmed. The error names the wanted signatures; a store that keeps
  its own retention sets `TerminalTaskRetention` and
  `MaxPushConfigsPerTask` to -1. The v0.96.0 upgrade note's `find`
  already flags the old method.

### Security
- **`contracts_fix` and `gofastr verify --fix` write through an `os.Root`**
  ([#665](https://github.com/DonaldMurillo/gofastr/issues/665)).
  `Report.Apply` checked the symlink-resolved path against the project
  root, then read and wrote the unresolved one, so a link swapped in
  between the check and the write sent the fix outside the root. Every
  read, stat and write now goes through an `os.Root` on the report
  root. `gofastr gen styles` reads its package patterns the same way: a
  pattern directory that links outside the working directory is
  refused instead of walked. `os.Root` also refuses an absolute
  symlink, even one that points back inside the project: make such a
  link relative.
## [0.96.0] - 2026-11-06

### Added
- **`App.Run(ctx, host)` runs an App under any host, and
  `framework.ServicesHost()` runs it with no transport.** One lifecycle
  engine sits behind `Start`, `Serve` and `Run`: one set of startup
  checks, one boot order, one rollback, one `Shutdown`. A `framework.Host`
  (`Run(ctx, *Session) error` and `Stop(ctx) error`) owns only its
  transport and how long the App runs; `Session` is opaque. `ServicesHost()` constructs no
  `http.Server`, listener or HTTP client, for a native, terminal or test
  presentation that calls the App in-process. `App.ServicesReady()`
  closes once every boot phase succeeds, under any host. `OnReady` still
  means a bound HTTP listener, so `Run` under a host without one refuses
  an App with `OnReady` hooks or interactive first-run setup. When `ctx`
  ends, `Run` shuts the App down with the signal handler's deadline. To
  serve GoFastr's HTTP surface over your own transport, keep using
  `Serve` with your listener. GoFastr renders no native UI. See
  [Application hosts](framework/docs/content/application-hosts.md).
- **Navigation guards for renderer libraries.** After
  `await __gofastr.loadModule('navguard')`, `__gofastr.guardNavigation(fn)`
  registers a guard that every router-managed departure asks before it
  discards the page: a link the router takes, `navigate()`, Back and
  Forward between router pages, and the intercept stack's moves. Guards
  answer in registration order, at once or through a promise, and the
  first denial wins. The call returns a disposer. Each attempt is frozen
  (`id`, `intent`, `from`, `to`, `delta`, `signal`, `affects(node)`), and
  a blocked move fires `gofastr:navigateblocked`. URL-only writes,
  document loads and history written outside the runtime are not asked.
  A Back or Forward the guards do not allow at once is undone, then
  replayed on allow: by entry key with the Navigation API, and without
  it by the distance between the tags core now writes on every history
  entry (`history.state.__fup` beside `__fui`). With no known distance
  the module pushes nothing, and a denial gives only the landing entry
  the page's URL. Code that writes `history.state` itself spreads the
  state it replaces to keep the tags. A renderer that wrapped the
  runtime's internal `_interceptPopstate` or `_pushURL` to hold a
  departure registers a guard instead; app screens stay in Go. A page
  with no guard behaves as before. See the
  [runtime contract](framework/docs/content/runtime-contract.md).
- **`__gofastr.replaceURL(url)`** rewrites the URL of the history entry
  the browser is on, for a renderer library that records state in the
  address. It fetches nothing, asks no guard and adds no entry; the
  entry keeps its id, position and state, and `currentPath` follows
  unless an intercept stack is open (the top layer's entry takes the
  URL). It returns `false` and writes nothing for another origin, a
  path outside the app's base, another document (locale or document
  scripts), a write the browser refuses, or, with `navguard` loaded,
  while a guarded Back or Forward is unresolved. It replaces
  `_pushURL(url, {replace: true})`. See the
  [runtime contract](framework/docs/content/runtime-contract.md).
- `gofastr generate sdk` now emits a typed repository interface per entity,
  a Remote adapter over the Go client, and a Local adapter that calls the
  app in-process through the new `app.InProcessHandler()`. Both go through
  the REST access rules, scoping, validation and hooks; the SDK stays
  stdlib-only. `InProcessHandler` refuses requests carrying cookies,
  credentials or a TLS peer certificate, and battery/auth keeps the
  server-set user (and its 2FA assurance) for the calls it admits. A
  custom session or user store binds that assurance to the lookup tenant
  unless `AuthConfig.GlobalSessionLookup` says lookup ignores the tenant.
  See [sdk.md](framework/docs/content/sdk.md#typed-repositories) and
  [auth.md](framework/docs/content/auth.md#session-middleware-cookie--ctx-user).
- `router.Router.Root()` returns the router at the top of a Group chain,
  for a registrar that must serve an absolute URL outside its group's
  prefix. See [core-packages.md](framework/docs/content/core-packages.md#router).
- **`ui.CommandPaletteTrigger`, `ui.ShortcutSheetTrigger` and
  `ui.NotificationBellTrigger`** render a trigger for each request, in
  that request's locale, from the same config whose body is mounted once.
  The trigger used to be built beside the mounted body, so every reader
  got the startup locale's label, placeholder and spoken unread count.
  The admin, gallery, docs and tracker examples render their triggers
  this way.
- **`mcp.Server.SetSSEStreamLimit(n)`** bounds the SSE notification
  streams a server holds across every caller (default 1024; a connect
  past it gets `429`). The per-caller seat cap never bounded the total:
  an anonymous caller is keyed by peer host, so a client with many IPv6
  addresses got a cap for each one. `n < 0` lifts the limit. See
  `mcp.md`.
- **Windows desktop signing and auto-update** ([#651](https://github.com/DonaldMurillo/gofastr/issues/651)).
  `gofastr desktop build --sign=<thumbprint|subject>` signs the `.exe`
  with signtool, timestamped through `--timestamp-url`, and `--zip`
  writes the `windows-amd64` update archive that `gofastr desktop feed`
  now signs. The updater installs on Windows: it swaps the executable
  and `WebView2Loader.dll` as a set, checks Authenticode so an update
  never drops or changes the running build's publisher, and the
  relaunched copy waits for the old process to exit. See `desktop.md`,
  "Windows code signing" and "Auto-update".
- **`App.Serve(net.Listener)`**. Runs the same lifecycle as `Start` on a
  listener the host supplies, instead of binding a TCP address: an
  in-process listener for a mobile WebView, or one a supervisor holds.
  Isolation never remaps it, and every return closes it once. An
  in-process listener (network `memory` or `pipe`) counts as
  local-only, so `Shutdown` skips the production readiness drain. A
  unix socket, or any other network that is not TCP, keeps the drain
  and turns the dev MCP and livereload off. For a listener that is not TCP the startup banner prints
  paths, not `http://memory/...` URLs. See
  [Plugins](framework/docs/content/plugins.md#init-lifecycle).
- **`framework/localapp`.** `localapp.Open(dir)` opens a one-person app's
  data directory: its SQLite `app.db`, the session secret and the
  installation's identity, created owner-only and minted on first run.
  The local identity and its fallback owner extractor moved here from
  `internal/localuser`, so a host outside this module (a mobile shell)
  shares them with battery/desktop and battery/service instead of
  copying them. battery/service opens its `app.db` through it, so that
  file is now created owner-only too. A secret file that is not the
  32-byte base64url value it mints (empty, truncated, stray whitespace)
  is now refused instead of used, a secret other users can read is
  tightened to 0600, and a symlinked one is refused. First-boot files are published atomically, so a
  process racing the first boot never reads an empty one. See
  [Local apps](framework/docs/content/local-apps.md).
- **Host transport slot for native WebViews.** Every request the runtime
  and the kit's scripts make goes through `window.__gofastr.fetch`, which
  calls `window.__gofastr_transport` when a host installed one, so a
  phone shell whose interception drops bodies or redirects carries them
  over its own channel. A script of your own that should reach the app
  through the host calls `window.__gofastr.fetch`. Server action bodies
  no longer carry the `session` field, which was always empty. See
  [Runtime contract](framework/docs/content/runtime-contract.md#host-transport-windowgofastr_transport).
- **`access.WithReconcileInterval`.** A `GrantStore` with a fanout
  reloads every role from the database every 5 minutes by default, so a
  replica that missed a refresh signal stops honouring a revoked
  permission within one interval. A full reload also covers a role whose
  last grant was revoked (only tombstones left), which it used to skip.
  Zero or negative turns it off. See
  [access control](framework/docs/content/access-control.md#cross-replica-grant-propagation).
- **`Relation.Hidden` keeps a relation out of reads.** A has-many or
  has-one whose foreign key is Hidden on the target (users has_many notes
  through a Hidden `notes.owner_ref`) stays includable by default, since
  that is usually a caller's own rows. Setting `Hidden: true` on the
  relation (`hidden: true` in `gofastr.yml`) refuses it on `?include=`
  and nested filters the way an unknown relation is refused, and leaves
  it out of `/openapi.json` and llm.md (`entity.HidesRelation`).
- **Thirty-odd new static rules from the 2026-11 bug bashes.** One per
  bug shape that recurred across PRs #642–#646 and round 2: vet
  analyzers (`optzero`, `bareduration`, `iounderlock`, `poisonedconn`,
  `deferreset`, `recoverwrite`, `statusrange`, `doublewrite`,
  `listheader`, `textbind`, `sanitizefix`, `authpresence`,
  `multipartparse`, `ratelimitkey`, `fanoutreq`, `rsakeysize`,
  `siblingresult`, `silentskip`, `growmap`, `nullarray`, `hookedread`), new arms on
  `intwrap`, `recovercallback`, `settlectx`, `wrapwriter`, `nostore`,
  `controlbytes`, `errleak`, `discardeddecode`, `mapwriter`, `embedskip`,
  `laxcoerce`, `compositekey`, `unrechecked` and `rawtext`, contracts
  rules GOFASTR1416 `ddlconstraint`, GOFASTR1417 `settlestamp` and
  GOFASTR1826 `flexreverse` (GOFASTR1415 tightened), and the runtime JS
  lints `dateutcyear`, `keyguard` and `clipboardawait`. They run in
  `make analyze` and in an app's `gofastr verify`; a deliberate site
  carries `//gofastr:allow(<rule>) <why>`. Two new markers:
  `//gofastr:mustuse <result>` on a function's doc and
  `//gofastr:required-array` on a slice field.
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
- **Linux desktop: release archives, per-user install, URL activation,
  persistent profiles and a tray (#613, #622, #626).**
  `gofastr desktop build --format=tar.gz` writes an archive holding the
  executable, a launcher, an icon, a desktop-entry template and a
  checksum manifest; a plain ELF stays the default. `--scheme` embeds URL
  activation: a second launch on the same session bus hands its URL to
  the running app. `gofastr desktop install --bundle=<dir>` installs an
  extracted archive under the user's XDG directories with a launcher and
  a desktop entry, and `desktop uninstall --id=<id>` removes it, refusing
  changed files and keeping app data. `desktop.Config.PersistentProfile`
  keeps the WebKit profile (cookies, local storage) across launches; the
  default stays ephemeral. The tray runs under a StatusNotifierItem host,
  and hidden windows come back when the tray host goes away.
  Notifications send plain text to a daemon that does not render markup.
  See [desktop](framework/docs/content/desktop.md#per-user-linux-installation).
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
- **BREAKING: an App runs once, and `Shutdown` during boot waits for the
  running phase.**
  - A second `Start`, `Serve` or `Run` on one App returns
    `framework.ErrAlreadyStarted`; nothing refused it before. One that
    begins after `Shutdown` returns nil and starts nothing. Build a new App
    to run again.
  - `Shutdown` while the App boots cancels the lifecycle context, waits
    for the running phase (a migration, a seed, an `OnStart` hook) to
    return, starts no later phase, then tears down. It used to stop
    batteries and run `OnStop` hooks under the running phase. A boot
    callback that calls `Shutdown` synchronously now waits until its
    context's deadline, and with `context.Background()` it never returns.
    Return an error from the callback, or call `Shutdown` on another
    goroutine.
  - That teardown runs on the stopping `Shutdown`'s context, with its
    deadline and values. If the context ends before the phase returns,
    `Shutdown` returns the context's error and the teardown runs once the
    phase returns, on a five-second budget. A failure of a teardown whose
    caller stopped waiting is logged.
  - A boot error during a stop is returned unless it is only the stop's
    own cancellation: `context.Canceled`, `sql.ErrTxDone`, or SQLite's
    `SQLITE_INTERRUPT`. Before, any error `errors.Is` matched to
    `context.Canceled` was dropped, a join with an unrelated failure
    included.
  - `Start` and `Serve` join the rollback's teardown error (a failing
    `OnStop` hook, a drain that timed out) to the boot error instead of
    dropping it. `errors.Is` still matches the boot error.
  - No `OnStart` or `OnReady` hook starts once `Shutdown` has begun.
  See [Application hosts](framework/docs/content/application-hosts.md).
- **BREAKING: a top-level `AfterList` hook may not change the row
  count.** A hook that adds or drops rows now fails the list with a 500
  and a logged reason, on HTTP offset and cursor lists, `ListAll`, typed
  queries and the entityui and admin screens. Before, the dropped rows
  still counted in `Total`, `CountAll` and `Count`, so the total said how
  many rows the hook hid. Filter visibility in `BeforeList`; `AfterList`
  may still redact, project or reorder.
- **BREAKING: `CrudHandler.UpsertOne` refuses an entity with
  `BeforeUpdate` hooks.** It returned the stored row after running only
  `BeforeCreate`, so a conflict update skipped the update validators
  and field stamps. Choosing the hook from a pre-read races a
  concurrent insert, so the call now fails before any hook or SQL. Use
  `CreateOne` or `UpdateOne` for those entities. See
  `hooks-and-transactions.md`.
- **BREAKING: `headless.Repeater` names its add and remove buttons.**
  With `AddName` and `RemoveName` empty the buttons were unnamed, so a
  page without JavaScript submitted the form with no operation and the
  server could not tell add from remove. They now default to
  `Name+"_add"` (value `"1"`) and `Name+"_remove"` (the row index), the
  names `ui.FormRepeater` already used. A handler that reads every form
  field sees the two new keys; set explicit names to choose others.
- **BREAKING: CRUD field errors are keyed by the wire key.** Validation,
  state and batch refusals keyed `fields` and `field_reasons` by column
  name while the body used the wire key (camelCase by default, or a
  field's `WireName`). Every refusal now uses the wire key, the same
  key the client sent. Entity and app-spec forms name each control by
  its wire key, and the runtime marks only the control whose name is that
  exact key, so a custom form names its controls by wire key too. Ids and
  relation lookups keep the column name. Multipart inputs resolve aliases
  before the duplicate-key check, and `llm.md`, the SDK docs and
  generated READMEs match. A client that read `fields["author_name"]`
  reads `fields["authorName"]`.
- **Docs and comments must name identifiers that exist; the runtime
  contract sync reads every attribute family (2026-11-03).**
  - `TestDocsNameRealIdentifiers`, `TestRepoDocsNameRealIdentifiers`
    and `TestGoCommentsNameRealIdentifiers` in `framework/docs` fail
    when a docs page, a repo `.md` file or a Go comment names a
    `pkg.Name` or `Type.Member` the code does not declare. The first
    run found about 60 stale names, fixed here (`Screen.Load` is
    `ScreenLoader.Load`, `i18nui.KeyAgo` is `i18nui.AgoNow`). A
    sentence saying a name is gone or renamed stays quiet.
  - The `core-ui/ARCHITECTURE.md` / `runtime-contract.md` sync gate
    reads every `data-*` family, not only `data-cui-*`, matches whole
    tokens (`data-cui-compute` passed on `data-cui-computed`) and
    checks both directions. The two tables had drifted by 52 rows and
    are synced. `TestAttrRowsSpellDoubleBraceFills` refuses a `{x}` or
    `%s` fill token in an attribute row: the modules replace `{{x}}`.
  - `mapwriter`'s return arm also fires on a range over a non-empty
    map literal that returns an error built from the entry it reached:
    which entry failed first changed from run to run. Ten tree sites
    now range a slice of pairs.
  - 97 documented false negatives fixed across 51 vet analyzers:
    `rootwrite`, `rootread` (which crashed on a `filepath.Join()` with
    no arguments), `fixedtmp`, `tightenshared`,
    `worldreadable`, `layoutfunc`; `textbind`, `controlbytes`,
    `errleak`, `recoverlog`, `unboundedbody`, `reqparamlimit`,
    `authpresence`, `fanoutreq`, `listheader`, `sanitizefix`;
    `intwrap`, `divlimit`, `negdur`, `optzero`, `nonfinite`, `keytrim`,
    `nullarray`, `settlectx`, `silentskip`, `statusrange`,
    `discardeddecode` (which also crashed on one input),
    `discardederr`; `recovercallback`, `callbackunderlock`,
    `deferreset`, `doublewrite`, `growmap`, `iounderlock`,
    `nowaitdelay`, `poisonedconn`, `recoverwrite`, `unseated`,
    `wrapwriter`; `secretcompare`, `laxenvelope`, `laxcoerce`,
    `asciifold`, `compositekey`, `credfetch`, `emitident`, `nostore`,
    `rawtext`, `resolvedial`, `timestampid`, `unboundedresp`. Each
    analyzer's doc comment names the spelling it now reads.
  - `textbind` follows request text through struct fields: a field
    some literal or assignment fills with request text (directly, or
    through a constructor's parameter) is request text wherever it is
    read, in its own package and, through a fact, in every importer;
    a field another package declares is request text in the package
    that fills it. Check the value before you store it; a check on the
    read, or one after a helper already bound the value, is not
    credited. Its first tree run found the saved-view store below.
  - An allow marker can list several analyzers separated by spaces or
    tabs, `//gofastr:allow(rootwrite rootread) why`. `gofastr verify`
    already read that list; `go vet -vettool` read commas only, so the
    marker silenced nothing there. It also reads a marker inside a
    multi-line block comment, as `gofastr verify` does, and
    `TestEveryAnalyzerIsWiredOrExplained` finds an analyzer built by a
    constructor, not only one declared as a literal.
  - 22 false negatives fixed in the browser-runtime JS lints in
    `core-ui/check` (`keyguard` and the `runtimeshapes` family): string
    prose and regex literals no longer read as code, wrapped and
    parenthesised builds are followed, a negated gate earns no credit,
    and a lone backtick no longer crashes the template walk. The widened
    lints found one site, `popover.js`'s event-detail registry read.
  - 15 false negatives fixed in the `gofastr verify` contracts rules:
    GOFASTR1105, 1401, 1408, 1417, 1802, 1804, 1806, 1812, 1814, 1815,
    1816 and 1824, and the `check-csp` style lane's allow marker.
  - The other static tools no longer pass a tree they never read.
    `gofastr verify` follows a symlinked root, fails on a root it cannot
    read, counts an unreadable or dangling source file as not checked,
    and refuses a tree with no source files. An exempt glob that starts
    with `/` matches from the root, `--changed` against a ref includes
    untracked files, a git path with spaces or quotes keeps its name, a
    NaN coverage minimum is refused, and a repo vettool that fails to
    build prints the compiler's output. `check-embed` checks every
    package pattern it is given, fails on one that matches nothing, and
    blocks on an action it cannot read: options built in a variable, or
    a child built inside `Render()` that has no value at boot.
    `check-csp` honours its ignore directive only in a real comment,
    skips build output, and scans the same files for `./` as for `.`.
    `gofastr audit lint` reports a `.go` file that does not parse, keeps
    line numbers through CRLF comments, checks a bare, deferred or `go`
    `Exec`, and flags every `t.Skip` with no annotation. `gofastr audit
    deps` follows symlinked package directories, and the a11y and
    sandbox audits scan a root named `build` or `dist`. repolint catches
    an emitted `data-cui-rpc` attribute and a raw-string key,
    `nullarray` wants proof a field was set before
    `x.F = append(x.F, …)`, and the response-mount lint follows
    `.then(this.render)` to a method, a field arrow or a static. These
    walks now share one directory walker. It follows a symlinked
    directory once, walks real paths before their aliases, skips a
    dangling link, and keeps going past a directory it cannot read.
- **`gofastr init .` refuses a symlinked `.env` or settings file, even
  one inside the project.** The two files it writes a generated secret
  into, `.env` (`JWT_SECRET`, `DATABASE_URL`) and
  `gofastr.settings.yml`, must be regular files. A link that stayed in
  the project used to be followed, so in a cloned repo with
  `.env -> static/robots.txt`, `init . --force` wrote the signing key
  into a committed, statically served file (a dangling link created it
  there). Any link at either file is now refused before anything is
  written, whatever it points at; a link on the way (a linked directory
  inside the project) is still followed. Replace the link with the file
  itself. `projectfs.ReplaceOwnerOnly` holds the same rule.
- **BREAKING: every route answers errors in one envelope** ([#652](https://github.com/DonaldMurillo/gofastr/issues/652)).
  `core/handler.WriteError` nested its refusal,
  `{"error": {"code", "reason", "message", "fields"}, "success": false}`,
  while the crud routes wrote it flat, so a client parsed two shapes and
  the generated JS client read a gate's 401 as `api: 401: [object
  Object]` with no `reason`. `WriteError` now writes the crud shape:
  `{"error": "<message>", "code", "reason", "fields", "field_reasons",
  "success": false}`. That covers a declared endpoint's auth gate (401)
  and cross-site refusal (403), `RequirePermission`'s 403, the auth
  battery's session and bearer middleware, every typed handler's error
  (`handler.Fail`, `Errorf`, `ValidationError`) and its bind 400 and
  body-cap 413. The message is still the request's translation of an
  `i18n.Phrase`, with its code in `reason`. `success: false` stays: both
  old shapes sent it and clients branch on it. In `/openapi.json` the
  gate's 401 and 403 reference the shared `Error` schema. The
  runtime's form-error and error-toast readers and `ui.SortableList`'s
  versioned-409 reader read only the flat `error`; a move handler that
  hand-wrote `{"error": {"message": …}}` for its 409, as the docs used to
  say, answers through `handler.WriteError(w, r, handler.Errorf(409, …))`
  or writes `{"error": "…"}`. An unmatched path under the API prefix
  still answers an RFC 9457 problem document, and the JSON-RPC (MCP, A2A,
  ACP), desktop-bridge and service-control envelopes are protocols and
  keep their own shapes. See
  [i18n → The error contract](framework/docs/content/i18n.md#the-error-contract).
- **BREAKING: `core/yaml` resolves plain scalars by the YAML 1.2 core
  schema.** It read Go's spellings instead: a hex float (`0x1.8p1`) as
  3, any casing of `true`/`false`/`null` (`tRuE`), and `1_000.5` as a
  number; `1e999` as a string; and a local tag (`!foo bar`) as the
  string `!foo bar`. Now only `true`/`True`/`TRUE` (and the `false` and
  `null` spellings) resolve, hex floats and underscores stay strings, an
  out-of-range float is an error, and every `!` tag is refused. App
  specs, kiln, pack and codegen read through it. No file in this repo
  used the old spellings. See
  [app spec](framework/docs/content/app-spec.md#yaml-subset).
- **BREAKING: `.env` `${NAME}` reads the value that is applied.**
  `dotenv.Load` (and so `NewApp`'s auto-load) now resolves `${NAME}`
  from `os.Environ` first, then an earlier (higher-precedence) file's
  value, then the same file's earlier keys: the order `Apply` keeps
  values in. v0.95.0 expanded each file against its own keys only, so
  the docs' own `PATH_TPL="${HOME}/bin"` loaded as `/bin`, a `.env`
  `DATABASE_URL="postgres://${DB_HOST}/app"` kept the `.env` host while
  `.env.local`'s `DB_HOST` was the one applied, and with `DB_HOST=prod`
  in the environment the URL still used the dotfile's host. A value
  that relied on the empty result or on the file's spelling beating the
  environment now reads the applied value. `dotenv.Parse` alone still
  reads no environment. See
  [dotenv](framework/docs/content/dotenv.md#variable-expansion-var).
- **BREAKING: `BeforeUpdate` and `BeforeDelete` hooks no longer run for
  an id the caller cannot write.** Update, delete and their batch items
  check the target under the write's tenant, owner and soft-delete scope
  (and any `BeforeGet` clause) first; another owner's row, another
  tenant's, a trashed row or a missing id answers 404 before the hook,
  so a hook's email or file delete never fires for it. A hook that
  created the row it was asked to update must do that before the call.
  One keyed `SELECT` per write, only when such a hook is registered.
  See `hooks-and-transactions.md`.
- **BREAKING (SQLite): a `decimal` SQLite would round is refused.** A
  value with a fraction and more than 15 significant digits
  (`"999999999999999.1234"` read back `999999999999999.1`) answers 400
  `decimal_significant_digits` on SQLite instead of storing it rounded.
  Postgres keeps every digit of `DECIMAL(19,4)` and still takes it. A
  `RawType: "TEXT"` field is stored as sent. See
  `entity-declarations.md`; `schema.DecimalExactOnSQLite` is the check.
- **`core/static` redirects `/dir` to `/dir/`.** A request without the
  trailing slash for a directory that has an `IndexFile` used to get the
  index at `/dir`, so its relative links resolved against the parent.
  It now answers `301` to `./dir/` (query kept), like `net/http`'s
  `FileServer`; the relative `Location` keeps the prefix and cannot
  name another host.
- **BREAKING: `core/static`'s SPA fallback answers only client routes.**
  With `SPA: true`, a missing path whose last segment ends in a
  well-known asset extension (`.js` `.mjs` `.css` `.map`, `.png` `.jpg`
  `.jpeg` `.gif` `.svg` `.webp` `.avif` `.ico`, `.woff` `.woff2` `.ttf`,
  `.json` `.wasm` `.txt` `.xml`, any case) now gets `404` instead of the
  index: the browser ran the HTML as a script or a stylesheet, and a
  CDN could store it under the asset's URL. Every other miss is a client
  route and still gets the index, a dot in its last segment included
  (`/u/jane.doe`, `/blog/v1.2`). A client route ending in one of those
  extensions (`/feeds/latest.json`) now 404s and needs a server route.
  See [core packages](framework/docs/content/core-packages.md#static).
- **BREAKING: `/readyz` drains before shutdown.** `App.Shutdown` now
  turns `/readyz` to `503` (`{"status":"draining"}`) at once and keeps
  serving for a grace period, so a load balancer stops routing here
  before the listener closes; `/healthz` stays `200` until exit. The
  grace is 5s by default, set with `framework.WithReadinessDrain` (`0`
  skips it), and `0` in a Go test binary, under `gofastr dev`, for a
  `battery/service` run and for an app bound to loopback only (every
  desktop app). The grace counts against `Shutdown`'s context and takes
  at most half of what it has left, so the HTTP drain, batteries and
  `OnStop` hooks keep the rest: a caller that passes its own deadline
  (`app.Shutdown(ctx)` with a 5s timeout) now gives requests still in
  flight and the stop hooks half of it, not all of it. An earlier build
  of this release let the grace spend the whole deadline. A stop now
  takes up to the grace longer: the default SIGTERM handler's deadline
  is `ShutdownTimeout` plus the grace. See
  [Health and readiness](framework/docs/content/health-checks.md#draining-on-shutdown).
- **The gofastr CLI's status lines print arguments inert.** `✓`, `✗`,
  `→` and `⚠` lines (`cli.Success`, `Fail`, `Info`, `Warn`) remove
  terminal control characters from every value they print: C0 but tab
  and newline, DEL, C1, escape sequences, bidi overrides and zero-width
  characters. A cloned repo whose directory name carried an OSC 52
  clipboard write drove the terminal of whoever ran `gofastr validate`,
  `migrate generate` or `agents sync` on it; each command scrubbed, or
  forgot to, on its own. The mark keeps its colour. The generated app
  CLI's `-o table` now also drops bidi overrides and zero-width
  characters, from its headers as well as its cells.
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
  - `GOFASTR1413`: `Header().Del("Vary")`, which drops the earlier
    entries as `Set` does (the `h["Vary"] = …` map write is the
    BREAKING entry below). A map write under a non-canonical key
    (`h["vary"]`) is a second header line, not a clobber, and stays
    quiet;
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
- **BREAKING for generated clients: `/openapi.json` documents what
  `?include=` returns.** An entity's
  schema component is its read view: a relation field's property is
  `anyOf` its key, the related record or null, since an include replaces
  the key with the record, and each other relation is an optional
  property (a record or null, or an array of records). Request bodies
  still take the key. A generated client reads the field as a union, so
  code that used a relation field as a string stops compiling once the
  client is regenerated; narrow the union where you read it (see
  `includes.md`).
- **Local run registry registers local runs only (#635).** A server
  bound to every interface or a LAN address, with no `gofastr dev` and
  no `battery/service`, writes no record. Set `GOFASTR_RUNS=on` to keep
  registering it; `GOFASTR_RUNS_DIR` only says where the registry is.
- **API docs page (#635).** `/api/docs/` lists the operations, ships no
  inline CSS, and answers 404 under its mount except at the page and
  spec paths. The webhook SQL tables gain `auth`, `response_status`
  and `response_body`, added when the store opens.
- **Search docs list what each backend counts as the same word.**
  `search.md` gains a table of matches that differ by backend: SQLite
  FTS5 folds diacritics (`cafe` finds `café`) and Memory and Postgres do
  not; Postgres finds no `½`, `v1.2` or email address; doubled hyphens
  match differently on each. Nothing changed in the backends; normalize
  text yourself if every backend must agree.
- **BREAKING: `gofastr verify` reports two more spellings.** GOFASTR1601
  (write result discarded) now fires on a bare `db.Exec(...)` or
  `ExecContext(...)` statement, plain, deferred or in a goroutine, not
  only on `_, _ = db.Exec(...)`. GOFASTR1413 (Vary overwritten) now fires
  on `w.Header()["Vary"] = …`; an append to the existing entry stays
  quiet. A CI gate on `gofastr verify` can newly fail: check the error,
  or mark a deliberate site `// best-effort: <why>`. The blog, spa and
  backoffice examples' seeds now check every insert.
- **`gofastr export` checks that the app exported.** An exit 0 reported
  "Exported to <dir>" even when `main` returned before `App.Start` and
  the directory was empty or stale. The CLI now passes a run id in
  `GOFASTR_EXPORT_RUN`, `App.Start` writes it back in
  `<dir>/.gofastr-export.json` (`framework.ExportReceiptFile`) after the
  export, and the CLI checks and deletes it. A missing receipt fails the
  export for an app on v0.96.0 or later; an older pin or a `replace`
  directive gets a warning. See `static-export.md`.

- **Audit change lists ask the app's read hooks about the real record.**
  `entityui.UI.Changes`, behind the admin's audit page and dashboard
  activity, ran the entity's `AfterGet` hooks over a made-up row with no
  id, once per entry. A hook that looks the record up by id refused it,
  and the common "mask unless the caller owns the row" hook masked it, so
  the fail-closed probe hid every field and the audit log showed no
  changes. It now reads the record the entry names (stored, and through
  the caller's read hooks, as the record screen does), leaves out what
  differs, and probes only the fields empty on that row, with the real
  id. A deleted record falls back to the old probe. The new
  `entityui.WithChangeCache(ctx)` makes the hooks run once per record per
  page; the admin installs it. Masking calls `AfterGet` hooks with the
  caller's context, so they must be side-effect free: see `Changes` in
  [entityui](framework/docs/content/entityui.md).
- **A `data-cui-rpc` POST form sends one request at a time (D-09).**
  A re-submit of a form whose method is POST waits while its own POST
  is in flight. One that sends the same fields as the request in flight
  is ignored, so a double click plus Ctrl+S no longer creates three
  rows. One that sends different fields (save A, edit to B, save again)
  is held, the latest only, and goes out when the first answers; the
  first answer then changes nothing on the page, the newer one does. A
  POST that has not answered in 30 seconds stops holding the form, and
  a held save goes out then. PUT and PATCH forms, input-triggered forms,
  buttons and controls still send every dispatch, the newest winning.
- **FilterToolbar Reset follows the submit (D-37).** Without
  `ClientNavigation`, Reset carries `data-cui-nav="off"` and loads the
  whole document, as the native GET submit does, so a kept layout that
  renders query-dependent content clears too. `ui.LinkButton` and
  `headless.Button` links accept `data-cui-nav="off"`. The `layoutfunc`
  lint also flags `app.QueryFromContext` in a layout build, and a
  `Key.Get` there whose same-package resolver reads the query or the
  request, outside a `RouteArea`.
- **SDK `manifest.json` has no generation time by default (D-18).**
  `gofastr generate` and `gofastr generate sdk` rerun over an unchanged
  schema rewrite every file byte for byte. `gofastr generate sdk
  --stamp-time` writes `generatedAt` as before, and `sdkdocs` shows the
  date only then. `sdk.Manifest.GeneratedAt` is omitted when zero.
- **An app spec with auth and no login form gets a `/login` screen
  (D-35).** With `app.auth.enabled: true` and no `login_form` screen,
  the generator adds `/login` (a `login_form` in `ui.AuthCard`, no CSS),
  so gated screens, the Sign in button and a failed form login no
  longer land on a 404 (ecommerce did). The app shell now offers Sign
  in. `gofastr pack` drops the added screen.

- **BREAKING: `config.Load` promotes embedded struct fields.** In
  `type Cfg struct{ Base }`, Base's `config:"PORT"` field now reads
  `PORT`, the way `encoding/json` promotes it; it read `BASE_PORT`
  before, and that key is no longer read. When a source still holds the
  old `BASE_PORT` spelling and no field reads it, `Load` logs one
  warning per key through `slog` naming the key it reads now. A promoted
  field an outer field shadows (`Base.Port` under the outer `Port`) is
  never read from a source and takes its own `default:` tag, so a method
  on `Base` reads its default rather than zero. Pointer embeds and unexported
  embedded types promote too (an unexported one was skipped). A field on
  the outer struct wins over a promoted one with the same key; two
  embedded structs promoting one key at the same depth, or a `config:`
  tag on an embedded struct, now fail `Load`. Rename `<TYPE>_<KEY>`
  env and file keys to `<KEY>`, or name the field (`Base Base`) to keep
  the prefix. Named struct fields (`DB DBConfig` reads `DB_HOST`) are
  unchanged. See [core-packages.md](framework/docs/content/core-packages.md#config).
- **BREAKING for multi-tenant apps: feature-flag user allow lists name
  a tenant's user.** A `Flag.Users` entry `acme:42` matches user `42` of
  tenant `acme` only; a bare `42` matches only when the evaluation has
  no tenant. It used to turn the flag on for user `42` of every tenant.
  The tenant is `EvalContext.TenantID`, or the request's resolved tenant
  (`handler.GetTenantID`) when that is empty. Ids may hold `:`: the
  entry splits at its first `:` and the tenant part percent-escapes `:`
  and `%`; build entries with `featureflag.UserEntry`, read them with
  `featureflag.ParseUserEntry`, and both stores refuse malformed ones.
  A tenant's user is bucketed for `Rollout` on the pair, so partial
  rollouts in a multi-tenant app reshuffle once. `EraseUserData` removes
  the user's entries through the new
  `datexport.DataEraser.MemberSeparator`: with a tenant (the new
  `framework.WithEraseTenant`, or the request's resolved tenant) the bare
  entries and that tenant's `tenant:user` only, so another tenant's user
  with the same id keeps theirs; with no tenant, every tenant's spelling.
  An entry equal to the whole erased id (a pre-upgrade bare `google:42`)
  always goes. Rewrite bare entries as
  `tenant:user`. See [feature-flags.md](framework/docs/content/feature-flags.md#user-entries).


### Fixed

This release carries the bug-bash rounds since v0.95.0, more than
1,000 fixes across the framework, batteries, CLI and runtime. Each round is
one PR; the full per-fix list, with what changed and why, is in
[CHANGELOG.md at the v0.96.0 roll](https://github.com/DonaldMurillo/gofastr/blob/70fb7de08dfcaa19bd117d988fb569f747989bc4/CHANGELOG.md#0960---2026-11-06).

- **Bug bash 2026-10-26 and 2026-10-27** (#615, #616, #617–#624,
  #627, #630): the 2026-10-27 bash's 619 findings, from its two
  Criticals (U23, P31) and 147 Majors through the last Minors. Admin authorization,
  credential reach and races, auth and events, CRUD, A2A, the runtime,
  rtc, the CLI and kiln.
- **Bug hunt rounds 4–7** (#631, #633): CRUD reads and registration,
  auth, streams and realtime, desktop, discovery and OpenAPI, the app
  spec, email and relays.
- **The #614 deferred decisions** (#640): presence, webhook v1, role
  scopes and 14 more.
- **Core UI runtime bash** (#638): lazy loading, CSS/HTML/JS chunking,
  asset caching, navigation and pane history.
- **Multi-agent bug bash 2026-11-02** (#658) and **CLI and CLI
  generation bash 2026-11-02** (#662): the generated app CLI,
  `generate cli --from-openapi`, migrations, export, restore and
  erasure, queues, cron, webhooks and the outbox.
- **Static rules from the 2026-11 bashes** (#659, #664): the new vet
  analyzers and contracts rules, and the bugs they found on the tree.
- **Round 8** (#667, #670–#677): Majors, then Minors in groups.
  Integers past 2^53 stay exact end to end, mixed-case and
  schema-qualified Postgres tables work everywhere, erasure follows
  foreign keys, and streams re-check their caller.

Areas with the most fixes: CRUD, filters and paging; migrations on
both dialects; data export, import and erasure; queues, cron, webhooks
and the outbox; MCP, A2A and ACP; the client runtime, navigation and
headless components; desktop shells and the updater; static export,
PWA and the sitemap; semantic and site search; kiln and the harness.

#### Breaking fixes

Run `gofastr upgrade` to find the call sites; its v0.96.0 notes carry
the guidance for each.

- BREAKING: the app generator refuses names its app could not boot with.
- BREAKING: generated clients send the JSON key the server reads for every field.
- BREAKING: a magic-link sign-in of a 2FA account reaches the challenge.
- BREAKING: `client.d.ts` types nullable fields `T | null`.
- BREAKING: the JS SDK keeps integers past 2^53.
- BREAKING: a presence topic named in the URL needs `AuthorizeTopic`.
- BREAKING: auth's own screens draw with the design system.
- BREAKING: a blank write in a declared `Access` block is refused.
- BREAKING: an rtc join with no user is refused unless the app opts in.
- BREAKING: webhook signatures cover the event name and delivery id.
- BREAKING: `access.Can`, `CanResource` and `CanResourceExact` check token scopes.
- BREAKING: `auth.RequireRole` and `auth.MCPRole` need a role scope on an API token.
- BREAKING: WebSocket text writes must be valid UTF-8.
- BREAKING: an empty query or header value is treated as absent.
- BREAKING: a top-level `Group` prefix starts with `/` or names a host.
- BREAKING: `desktopui.Theme()` is removed (#607).
- BREAKING: a desktop menu `Key` needs cmd, ctrl or alt.
- BREAKING: `freeze.FreezeAndGenerate` takes a context.
- BREAKING: `ui.SparklineConfig.Color` takes a theme preset only.
- BREAKING: `TwoFAConfig.Digits` is removed, and the enrolment URL carries `TwoFAConfig.Period`.
- BREAKING: passkey `MaxPerUser` holds when registrations race.
- BREAKING: the admin reserves `/account` and `/shortcuts`, and audits a profile-name change.
- BREAKING: `handler.SSEStream` is removed.
- BREAKING: `RecordBuilder.Steps` takes the list the drawer opens from.
- BREAKING: CRUD events carry their record's owner into the outbox.
- BREAKING: configured store tables fold their case on Postgres.
- BREAKING: `WithResourceGate` hides the resource from callers it refuses.
- BREAKING: backup-code regeneration no longer writes over a concurrent 2FA change.
- BREAKING: `FileField.URL` is escaped.
- BREAKING: `WithLocaleResolver` takes an `i18n.LocaleResolver`.
- BREAKING: `ui.DateTimeFieldConfig.Step` is a `float64`.
- BREAKING: an upsert onto an existing key announces `entity.updated`.
- BREAKING: llm.md documents and server actions honor region guards.
- BREAKING: a blank optional field clears on update.
- BREAKING: generated clients and CLIs keep big integers.
- BREAKING: an MCP App widget carries its client inline.
- BREAKING: `app.ScreenLLMMD` and `ScreenLLMMDWithMeta` take a context.
- BREAKING: the auth stores share a mixed-case users table with the entity layer on Postgres.
- BREAKING: MCP tool results keep big integers exact.
- BREAKING: typed hooks keep JSON integers exact.
- BREAKING: `/api/llm.md` links the page index only when the app serves one.
- BREAKING: the magic-link, password-reset and email-verification plugins refuse their in-memory token store in production.
- BREAKING: a static export ships the app's own Content-Security-Policy.
- BREAKING: loading content speaks the reader's language.
- BREAKING: Kiln refuses hooks, routes and pages it would never run.
- BREAKING: erasure follows foreign keys.
- BREAKING: generated clients type an integer id as a number.
- BREAKING: an OpenAPI CLI takes a bare origin.
- BREAKING: `columns` is a reserved transition key.
- BREAKING: a cursor field must not hold NULL.
- BREAKING: a filter value must be of its field's type.
- BREAKING: `_like` and DSL `contains` ignore case on both dialects.
- BREAKING: `gofastr sdk` refuses an entity whose `client.d.ts` type name is already declared.
- BREAKING: a process module's artifact upgrade waits for its own migrations.
- BREAKING: pgvector replicas share one keyword leg.
- BREAKING: a pgvector table refuses a second embedding model.
- BREAKING: owned sheets refuse statement at-rules (GOFASTR1825).
- BREAKING: `__gofastr.desktop.available` is a boolean.
- BREAKING: a JSON field takes no filter.
- BREAKING: a list cursor falls back to the anchor row's key when its sort values pass the cap.
- BREAKING: `pagination.NewCursorPage` returns `(CursorPage, error)`.
- BREAKING: the SDK schema hash includes the ordered primary key.
- BREAKING: a CRUD handler bound to your own `*sql.Tx` announces events after commit only.
- BREAKING: Redis erasure needs `queue.ScriptRunner`.
- BREAKING: `registry.RegisterStyle` checks its name.
- BREAKING: `gofastr migrate` and `gofastr validate` refuse extra arguments.
- BREAKING: two indexes that share a name are refused.
- BREAKING: a misspelled `-- +migrate` directive is refused.
- BREAKING: `migrate generate` refuses a SQLite rebuild of a table another table references.
- BREAKING: `--from-openapi` CLIs treat an acronym as one word in command and flag names (`get-user-by-id`, `--http-code`).
- BREAKING: auth token management on a 2FA account needs the cookie session that passed the second factor.
- BREAKING: the 2FA challenge replaces the pending session.
- BREAKING: CSRF skips only `Authorization: Bearer` and `X-API-Key`.
- BREAKING: OAuth provider keys are 1-64 letters, digits, `-` or `_`, and an identity with no email and no link answers 400.
- BREAKING: password and OAuth sign-in of a 2FA account reach the challenge; `Init` requires `WithTwoFactorURL` and `OAuth2Config.TwoFactorURL` next to a 2FA plugin.
- BREAKING: `ui.RangeSliderConfig` `ValueLow`/`ValueHigh` are `*int`, and the password reveal's hide strings and hooks are removed.
- BREAKING: `filter.SearchConditions` returns `([]Condition, error)`.
- BREAKING: `MarkEnvelopeProcessing` on the bundled inbound webhook stores returns `(bool, error)`.
- BREAKING: an app spec's `app.auth.base_path` must be absolute with no trailing slash.
- BREAKING: `UIHost.PWAManifestJSON` takes a `context.Context` first.
- BREAKING: `widget.Mount` registers `PATCH` and panics on a method it does not know.
- BREAKING: `Harness.CreateSession` returns `(ids.SessionID, error)`.

### Security

About 140 fixes from the same rounds: read scopes on every surface
(includes, cascades, `_events`, OpenAPI, llm.md), API token scopes on
the admin, MCP, A2A and widgets, auth races across password reset,
account claim, OAuth and passkeys, the relay's header handling,
process-module isolation, harness permissions, path containment in
static, storage and export, and log and email scrubbing. The full list
is in the link above.

#### Breaking security fixes

- BREAKING: `battery/relay` relays script, style, image, font, JSON and `text/plain` only.
- BREAKING: `apiversions.ApplyToEntityConfig` panics on a projection naming an undeclared field.
- BREAKING: the MCP control tools and the log MCP tools need the `admin` role.
- BREAKING: an API token reaches the admin only with `admin.AdminScope`.
- BREAKING: the admin's platform pages refuse a tenant-scoped caller.
- BREAKING: `RevokeGrants` removes the grants it names.
- BREAKING: a process module's response headers are allow-listed.
- BREAKING: a custom CSRF cookie name gets the `__Host-` prefix on HTTPS.
- BREAKING: `gofastr harness --listen` binds loopback only.
- BREAKING: `admin.AuthPrincipal(am, member)` checks that a bulk job's creator still belongs to its tenant.
- BREAKING: the print battery refuses a root `PathPrefix`.
- BREAKING: a desktop window stays on the app's origin.
- BREAKING: one client can no longer hold every island stream.
- BREAKING: idempotency replays are keyed on the tenant and the held scopes.
- BREAKING: A2A tasks are bound to the tenant they ran in.
- BREAKING: `RequireAPIScopes` reads the resource from the route's entity.
- BREAKING: `battery/storage` S3 keys and `DefaultKeyValidator` refuse absolute, control-byte and unclean keys; presigning refuses a non-positive expiry.
- BREAKING: `core/static` refuses Windows 8.3 short names.

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
