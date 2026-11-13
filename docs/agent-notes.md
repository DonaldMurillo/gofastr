# Implementation regression notes

## 2026-11-09 - Keep a fitting component subset when app.css needs a link
- Scope: framework/uihost, issue #669 first-paint CSS
- Trigger: eager offline-toast sheets moved Meridian Customers from 15,328 B of subset CSS to 20,951 B of full linked CSS.
- Approach: weigh component, app and scheme blocks separately; keep a fitting subset and bootstrap inline while linking app.css after them, under the unchanged 16 KiB inline budget.
- Evidence: TestFirstPaintSubsetLinksAppWhenCombinedHeadExceedsBudget fails with the fallback removed; Meridian CSS is 16,360 B against its unchanged 17,408 B cap. TestOfflineToastIsStyled covers linked app.css with the full bundle blocked.
- Next time: dump heads before and after a sheet-set change and compare full/subset sizes before blaming CSP; CSS weight excludes the 794 B scheme script the planner counts.

## 2026-11-09 - Put caller gates before item-schema validation
- Scope: core/mcp Gated handlers, tool gates and prompt gates
- Trigger: a refused Gated tool with required arguments disclosed its schema instead of answering as an unknown name.
- Approach: retain wrapper gates at registration and check them before validation; keep direct callable wrappers intact and weakly register their metadata.
- Pitfall: code pointers merge closures; index live metadata by reflected wrapper identity only during registration to avoid retaining closures.
- Boundary: keep Gated outermost across ordinary host decorators, wrap a supplied handler with mcp.Decorate (the endpoint twin does), or attach WithToolGate at registration; opaque functions hide inner metadata.
- Evidence: TestGatedItemAnswersAsUnknown now uses a required string schema; TestGatedValidationOrderAndIdentity pins nested gates, reuse, GC and exactly-once invocation.
- Next time: compare unknown-name code and message for missing, mistyped and extra arguments, then mutate each ordering guard with a Go overlay.
- Status: active

## 2026-11-09 - Validate cascade-filled keys inside the parent operation
- Scope: framework/crud upsert, create and batch writes
- Trigger: upsert preflight relaxed required cascade keys but never validated their final values.
- Approach: retain preflight and hidden-target checks, then validate the full parent schema after belongs-to writes inside inTx.
- Evidence: TestUpsertValidatesCascadeForeignKey covers both arms on SQLite/Postgres, child creates/edits, and owned, context-carried and bound transactions.
- Next time: compare create ordering and commit the caller transaction after a handled refusal to prove savepoint rollback.
- Status: active

## 2026-11-09 - Strip CRUD elevation at the mask callback boundary
- Scope: framework/crud field masks and battery/admin
- Trigger: a mask using CanReadScoped inherited the admin entity lift and revealed its stored value.
- Approach: remove elevation inside runMask, which covers own rows and included rows on every response surface.
- Evidence: TestReadHookParityCrud mask elevated mode and TestReadHookParityAdmin mask elevation cover permission-sensitive masks; removing WithoutElevation makes them fail.
- Next time: extend the parity matrices and check callback contexts at their common invocation boundary.
- Status: active

## 2026-11-08 - Recheck T4 claim lifetimes and settle invariants
- Scope: issue #669 T4, migrations, queue, outbox, webhook, idempotency, cron and log history
- Trigger: review of the nine GLM commits found missed settle sinks, shared AppOption state, warning gaps and weak regression premises.
- Approach: classify SQLite contention by result code, preserve the first call's context, bound retries, and exercise every claim-settle destination with a real file lock.
- Evidence: [T4 review and negative-proof ledger](reviews/2026-11-08-i669-t4.md); 19 mutation checks, scoped SQLite/Postgres packages, race tests and repository analyzers pass.
- Next time: use Go overlays for negative proofs, test two queues on the same table, and keep unreleased upgrade notes out of published release files.
- Status: active

## 2026-11-08 - Seed first-seen prototype-named globals through both navigators
- Scope: core-ui/runtime/src/envelope.js, envelope seed lifecycle
- Trigger: Cursor Bugbot found first-seen `toString` and `valueOf` globals resolving to inherited functions instead of own slots.
- Approach: create global slots using an own-property check, matching core navigation; exercise names absent from the boot seed through real envelope HTTP navigation and cached replay.
- Evidence: [runtime fix follow-up](reviews/2026-11-07-fixes-runtime.md), `TestEnvelopeSeedsPrototypeNamedGlobals`; ordinary `newGlobal` passed while both prototype names failed before the fix.
- Next time: test first-seen names as well as pre-seeded client mutations, and wait for demand modules after the navigation that loads them.
- Status: active

## 2026-11-08 - Refresh the PR base without losing review artifacts
- Scope: git integration and PR preparation
- Trigger: main advanced while correctness fixes and their review notes were uncommitted.
- Approach: retain an include-untracked safety stash, fast-forward main, reapply the changes and preserve both sides of the changelog-only conflict.
- Evidence: integration base `8b34dc070`; fresh scoped package and browser gates passed. `docs/reviews/2026-11-07-findings.md` keeps the older review/development revisions distinct.
- Next time: fetch before opening the PR, rerun current analyzers and affected tests, and never imply old-base results validate newly integrated main.
- Status: active

## 2026-11-08 - Budget disk space for the affected coverage sweep
- Scope: Go commit/push hooks and retained review probes
- Trigger: the 223-package covered hook sweep exhausted temporary disk after build, formatting, secret checks, vet and repository analyzers passed.
- Approach: clear only completed task workers' private `GOCACHE` directories with `GOCACHE=<probe cache> go clean -cache`; retain sources, binaries, fixtures and logs, and leave the shared cache alone.
- Evidence: private cache cleanup increased free space from about 7.6 GiB to 25 GiB before retrying the full hook; no test assertion caused the first failure.
- Recovery: the exhausted shared cache retained references to missing standard-library archives, so retry with a fresh task-specific `GOCACHE` rather than deleting a shared cache another build may be using.
- Next time: check `df -h /tmp` before a broad coverage build and save complete hook output, not only its final tail.
- Status: active

## 2026-11-07 - Isolate negative proofs from shared validation
- Scope: parallel implementation and regression testing
- Trigger: coordinator saw the globals-only Back regression fail while the implementation worker was temporarily reverting its fix to prove the test detects it.
- Approach: run negative mutation proofs in isolated snapshots/overlays, not files other workers or coordinator tests currently consume; validate restored final state after implementation completes.
- Evidence: [runtime fix notes](reviews/2026-11-07-fixes-runtime.md); final coordinator browser regressions pass, including both Back/Forward arms and envelope replay.
- Next time: announce mutation windows if isolation is impossible; retain full timeout output rather than only a tail.
- Status: active

## 2026-11-07 - Judge canonical JSON against its double and UTF-16 contracts
- Scope: core/jcs, serialization fidelity review
- Trigger: lossless-integer or UTF-8 key-order expectations would incorrectly reject this package's explicit ECMAScript-double/UTF-16 canonicalization contract.
- Approach: compare public output byte-for-byte to a recursive sorted JSON.stringify oracle, then check idempotence/value preservation and both entry points separately.
- Evidence: coordinator regenerated scope 41's deterministic oracle and rebuilt/reran 14,145 assertions with zero failures; core/jcs suite passes. Sources/logs: /tmp/scope41-probe; [review ledger](reviews/2026-11-07-findings.md).
- Next time: distinguish deliberate double rounding and documented encoding/json replacement from byte-preserving archive expectations; keep serialization checks separate from security/signing analysis.
- Status: active, review methodology; no new finding

## 2026-11-07 - Distinguish missing flags from evaluated-off flags
- Scope: core/featureflag, framework/flags.go
- Trigger: BoolDefault fallback and allow-list checks can appear wrong when a flag exists but its Enabled/Envs gates turn it off.
- Approach: establish evaluation precedence first, compare rollout buckets to an independently implemented documented hash, and explicitly attach FlagContextMiddleware when default middleware is disabled.
- Evidence: scope 40's rebuilt coordinator probes pass 85 checks; featureflag and framework Flag selections pass. Retained sources/logs: /tmp/scope40-probe; [review ledger](reviews/2026-11-07-findings.md).
- Next time: use explicit missing/disabled/env-blocked/cohort-off controls rather than assuming a default overrides an existing flag or guessing a rollout subject's bucket.
- Status: active, review methodology; no new finding

## 2026-11-07 - Reach trigger-focus states through ordinary menu interactions
- Scope: framework/headless/menu.js, keyboard correctness review
- Trigger: an enabled menu's programmatically refocused summary is a weak reproduction; all-disabled rows naturally leave focus on the summary after Enter opens it.
- Approach: render native-summary and caller-owned-trigger variants through Menu, with enabled/all-disabled controls; execute actual menu/disclosure modules and Tab in both directions.
- Evidence: [C19 fix](reviews/2026-11-07-fixes-headless.md); coordinator passed the real-browser eight-case Tab/Shift+Tab matrix. Native-summary triggers now close the chain without blocking native Tab movement.
- Next time: establish a natural reachable focus state before counting a keyboard bug, and read TestMenuSpaceActivatesLink before misclassifying multiword type-ahead as swallowed activation.
- Status: active, fixed locally; implicit-summary focus on caller-owned triggers remains an unvalidated side observation

## 2026-11-07 - Inspect stored timestamp bytes when judging archive fidelity
- Scope: framework/export_data.go, SQLite export/import
- Trigger: the SQLite driver reformats a plain selected DATE/DATETIME, and SQLite normalizes negative zero on insert; both can look like archive corruption.
- Approach: compare stored timestamp text with SELECT +column, and compare source/destination float bits after source storage. Restore fresh databases and preserve prior artifact directories.
- Evidence: coordinator rebuilt/reran scope 37's 72 public checks plus reversed-manifest restore; all pass. Exact framework fidelity/ordering/streaming/WAL-control selection also passes; custom WAL/Postgres behavior remains unclaimed.
- Next time: separate driver presentation and database normalization from archive transformations before counting fidelity loss.
- Status: active, review methodology; no new finding

## 2026-11-07 - Model absent, empty and nested configuration separately
- Scope: core/config, core/dotenv, core/settings
- Trigger: empty source values preserve host fields and suppress defaults; absent nested pointers skip inner required checks until a source key allocates them.
- Approach: test first-hit source precedence, explicit emptiness, pointer allocation and validation as separate contracts. Compare settings-owned sections semantically while preserving surrounding user bytes.
- Evidence: coordinator rebuilt/reran scope 36's seven public probes (244 checks) and all three package suites; all pass. Windows, 32-bit and concurrent environment mutation remain unclaimed.
- Next time: distinguish an absent source from a present empty value before diagnosing a default/required mismatch, and verify documented parent-name prefixes rather than assuming nested config tags override them.
- Status: active, review methodology; no new finding

## 2026-11-07 - Test plural selection through the served catalog locale
- Scope: core/i18n, plural/catalog composition
- Trigger: direct pt-AO contexts use pt-PT rules, while header negotiation may select a pt catalog and correctly use its rules.
- Approach: compare table inheritance, operand/category evaluation and Translator/middleware composition separately against the declared CLDR snapshot.
- Evidence: coordinator reran scope 34's retained CLDR 47 differential (75,735 category cells, zero mismatches), public probes and full i18n suite; all pass. No 32-bit execution claim.
- Next time: distinguish requested language tags from the actual catalog locale before treating a regional plural difference as a bug; do not use old x/text plural data as the oracle.
- Status: active, review methodology; no new finding

## 2026-11-07 - Require matched references in static-export checkers
- Scope: framework/static, export validation
- Trigger: a checker matching only /__gofastr URLs reports zero missing for subpath exports even when it checks no prefixed references.
- Approach: strip the exact deployment base, require a nonzero match count, and distinguish asset files from the retry banner's live health endpoint.
- Evidence: retained apex/subpath site outputs each resolve 8,429 HTML asset references; Meridian resolves 732. Fresh static and exportcmd package controls pass, no browser coverage claimed.
- Next time: assert checker coverage counts as well as missing counts; label retained-output inspection separately from a fresh example build.
- Status: active, probe methodology; no new product finding

## 2026-11-07 - Compare A2A wire deltas with stored snapshots
- Scope: core/a2a, review methodology
- Trigger: artifact append events carry only new parts, while GetTask holds the merged artifact; applying snapshots and deltas twice would misdiagnose correct behavior.
- Approach: drive real POST/SSE transports across memory and isolated SQLite, checking event order, version-floor resubscribe and persisted task state separately.
- Evidence: coordinator reran scope 33's 14 probes on both stores (28 store subtests) and the short package suite; all pass, log at `/tmp/gfs-s33/coordinator-rerun.log`.
- Next time: capture minted artifact IDs and distinguish create-versus-append semantics; verify timestamp-offset parity empirically rather than inferring a SQL text-binding bug.
- Status: active, review methodology; no new finding

## 2026-11-07 - Separate webhook attempt state from persisted retry state
- Scope: battery/webhook, review adjudication
- Trigger: a failed send is persisted as pending with LastError when another retry is scheduled; the bridge delivers a full event envelope despite loose Data-only prose.
- Approach: follow schedule through settle and inspect existing envelope assertions before treating intermediate status or documentation wording as a functional bug.
- Evidence: coordinator reran all 18 scope 31 probes against memory/SQLite and the short package suite; all pass. Live Postgres/Redis and cross-process contention are unclaimed.
- Next time: assert persisted NextAttemptAt, attempt counts and actual delivery bodies through the whole retry/replay cycle, rather than transient local assignments.
- Status: active, review methodology; no new finding

## 2026-11-07 - Distinguish protocol requirements from tolerated inputs
- Scope: core/mcp, review adjudication
- Trigger: nil inputSchema and differing unknown-tool error codes looked suspicious, but no supported nil-schema flow or normative error-code requirement was established.
- Approach: verify actual caller contracts and distinguish spec examples from requirements; combined JSON/SSE Accept permits a JSON response.
- Evidence: scope 30 review; coordinator core/mcp and framework MCP controls pass, reviewer transport probes retained but not independently rerun.
- Next time: pin the supported input and normative requirement before counting a serialization discrepancy as a protocol bug.
- Status: active, review methodology; no new finding

## 2026-11-07 - Keep DOM and IndexedDB model checks distinct from engine coverage
- Scope: framework/headless, core-ui/localdb, framework/localentity
- Trigger: reviewer models execute real modules and retained Go markup successfully, but can hide native IDL, layout, timing or quota behavior.
- Approach: rerun model self-tests and controls; label exact model seams. Select exact browser-free Go test names rather than broad TestLocal prefixes that also match browser tests.
- Evidence: scope 24's 77 checks and scope 27's 40 localdb checks rerun cleanly; the form-function probe needed `global.fail=0` plus a final zero assertion to repair its reporting-only ReferenceError.
- Next time: distinguish production failures from probe bookkeeping and avoid treating a model's green result as a real IndexedDB/browser release gate.
- Status: active, review methodology; no new finding

## 2026-11-07 - Pin the locale oracle and distinguish standard currency digits
- Scope: core/i18n, locale formatting
- Trigger: Number keeps invariant NaN, six recognized currencies round to whole units, and CN/AE weekday defaults disagree with the declared CLDR 47 data.
- Approach: compare public API output against exact version-pinned Unicode JSON; retain source hashes and exact whitespace (Russian NaN uses U+00A0). Check standard _digits separately from _cashDigits.
- Evidence: [C16–C18 fixes](reviews/2026-11-07-fixes-locale.md); coordinator passed `go test -p 2 -count=1 ./core/i18n/...` and reran `go run -p 2 ./core/i18n/internal/cldrgen` with both output SHA-256 values unchanged. NaN, standard currency digits and week data now share the pinned generator.
- Boundary: UYW is unrecognized by the linked currency library, so its documented two-digit/no-symbol fallback is excluded from the known-currency defect. Do not use arbitrary ICU vintages as the sole oracle.
- Next time: verify NaN symbols, currency rounding data and regional week tables separately when claiming a CLDR snapshot upgrade.
- Status: active, fixed locally; nine known-currency precision cases covered, currency symbols remain x/text-sourced

## 2026-11-07 - Distinguish cron matching from DST dispatch
- Scope: framework/cron, review adjudication
- Trigger: Next can return a repeated-hour wall-clock match that FiresAt deliberately suppresses for once-only dispatch.
- Approach: read each method's own contract and the actual scheduler caller rather than imposing the dispatch policy on a matching helper.
- Evidence: scope 28 report retains independent transition-window probes; coordinator full framework/cron suite passes, custom probes not rerun. Live Postgres leadership is unclaimed.
- Next time: test the dispatch seam for duplicate firings, not merely a repeated-hour Next result.
- Status: active, rejected candidate; not counted

## 2026-11-07 - Validate sortable conflict races against version semantics
- Scope: framework/headless/sortablelist.js, review adjudication
- Trigger: a held conflict refresh replaces a newer accepted order in a fresh full-module keyboard-event adapter.
- Approach: retain no-newer and refresh-before-newer controls; verify a supported server could accept the newer commit before counting the stale-response symptom. Strict unchanged-token optimistic locking is an explicit objection.
- Evidence: `/tmp/gofastr-coordinator-sortable/observation.cjs`; fake DOM/fetch, no real-server or browser claim. Scope 29 recheck pending; browser-free headless package selection passes.
- Next time: exercise conflict GET completion interleaved with newer commits, and distinguish a reproducible adapter outcome from a supported end-to-end contract violation.
- Status: pending validation, not counted or fixed

## 2026-11-07 - Renew sortable tokens with the accepted server state
- Scope: framework/headless/sortablelist.js, ui.RenewSortableVersion
- Trigger: a versioned list saves once then repeatedly sends its original token; rows-only conflict refresh cannot renew it.
- Approach: return the percent-encoded version header on accepted writes and conflict refreshes; renew only group peers holding the submitted token, and only the refreshed list on reconciliation.
- Evidence: [C20 fix](reviews/2026-11-07-fixes-headless.md); coordinator passed real-browser three-move/group-conflict tests and flagship persistence/header checks. Sortable gzip grew 113 bytes with explicit budget accounting.
- Next time: test consecutive accepted moves and stale-peer recovery, not only one successful move; keep the separate delayed-refresh race unclaimed.
- Status: active, fixed locally

## 2026-11-07 - Separate token grammar from incorrect examples
- Scope: core-ui/style, core-ui/ownstyle, review adjudication
- Trigger: the package-doc example `{fonts.weight.bold}` stays literal, while ResolveAll specifies single-dot `{category.name}` references.
- Approach: compare examples with the resolver's explicit grammar before counting functional drift; do not equate emitted CSS text or successful compilation with browser layout coverage.
- Evidence: `core-ui/style/doc.go:49`, `core-ui/style/tokens.go:65`; fresh style/ownstyle package suites pass. Reviewer CLI/theme round trips were not independently rerun.
- Next time: use supported single-dot references and verify CSS compilation separately from browser behavior.
- Status: active, documentation observation only; not counted or fixed

## 2026-11-07 - Compare widget registry membership with server admission
- Scope: core-ui/widget, runtime/widgets
- Trigger: Mount accepts toString/valueOf and serves their assets, but the client mistakes inherited Object.prototype members for mounted widgets.
- Approach: feed real Go catalog/chrome fixtures into the actual widgets.js, alongside an ordinary-name mount/close control; assert inserted roots and lifecycle events, not fetch success alone.
- Evidence: [C15 independent reproduction](reviews/2026-11-07-widget-correctness.md); deterministic DOM adapter, not browser layout coverage.
- Next time: inspect own-property membership at every registry read before concluding a registered name is openable end to end.
- Status: active, review only; not fixed

## 2026-11-07 - Separate WebSocket close phase budgets
- Scope: core/stream, review adjudication
- Trigger: a stalled peer takes drain time plus a close-frame write plus reciprocal wait, rather than a single WriteTimeout total.
- Approach: distinguish the drain bound in Close from per-frame WriteTimeout and the post-send CloseTimeout; the close-drain test explicitly budgets all three.
- Evidence: `core/stream/websocket.go:150`, `core/stream/websocket.go:186`, `core/stream/websocket.go:659`, `core/stream/websocket_close_drain_test.go:159`; reviewer timing not independently rerun.
- Next time: establish a whole-operation deadline before treating composed phase budgets as a defect.
- Status: active, rejected candidate; not a counted bug

## 2026-11-07 - Preserve the caller's finally step in loading probes
- Scope: core-ui/runtime/src/envelope.js, core-ui/runtime/src/loading.js
- Trigger: a partial harness falsely suggested a delayed skeleton parks freshly applied content after success.
- Approach: include envelope.js:1169 load.finish in the full schedule/wait/apply/sweep/finally sequence; _restoreLoading clears pending timers before checking shown state.
- Evidence: [scope 20 adjudication](reviews/2026-11-07-findings.md); retained reviewer refutation and omission control at `/tmp/gofastr-s20-953ccc0/f1-loading-after-timer.cjs`.
- Next time: trace every success/error/finally caller cleanup before promoting an isolated helper-timer observation to a finding.
- Status: active

## 2026-11-07 - Observe update hooks and WebSocket reason classes directly
- Scope: core-ui/runtime/src/ws.js, core-ui/runtime/src/poll.js
- Trigger: clean server-5xx closes retry correctly with the wrong reason class; identical structured polls replay flash/scroll hooks.
- Approach: isolate each close case in its own VM and assert class plus retry; execute actual poll.js and production setSignal against fresh JSON parses, counting structured and scalar binding writes.
- Evidence: [C13/C14 fixes](reviews/2026-11-07-fixes-runtime.md); coordinator passed real-browser clean-4503 close/retry and unchanged-structured-poll mutation regressions. Reason class and skipped writes are checked independently of reconnect/value correctness.
- Next time: assert exposed status fields as well as recovery, and compare structured JSON values rather than only object identity when promising unchanged-value DOM suppression.
- Status: active

## 2026-11-07 - Check Decimal guards against SQLite storage, not digit proxies
- Scope: core/schema/decimal.go, framework/crud/decimal_sqlite.go
- Trigger: a numeric-affinity write rejects exact signed-int64 values but accepts nonzero 1e-400 as zero.
- Approach: compare real POST/GET results and SQLite typeof/CAST storage with direct bound-string inserts; include NUMERIC, REAL/FLOAT/DOUBLE, and TEXT controls plus affinity-precedence names (`FLOATING POINT`, `REAL TEXT`).
- Evidence: [C12 fix and PR #688 follow-up](reviews/2026-11-07-fixes-data.md); plain int64 digits pass only for INTEGER/NUMERIC affinity. Independent review caught REAL columns silently rounding; schema and CRUD regressions failed before the correction.
- Boundary: the `1e-307` magnitude floor is conservative and also refuses some normal doubles SQLite could retain; TEXT-backed Decimal and Postgres behavior are unchanged.
- Next time: verify SQLite affinity precedence before admitting int64 literals; probe integer, floating-point, and underflow boundaries independently of significant-digit counts.
- Status: active

## 2026-11-07 - Distinguish seed ledgers from per-boot hooks
- Scope: framework/factory, framework/seed.go
- Trigger: correctness review found no confirmed defect in factory or seed lifecycle contracts.
- Approach: compare entity seeds (ledger-gated) with WithSeed hooks (each boot); observe factory events on the asynchronous lane and distinguish CreateMany partial results from atomic batches.
- Evidence: [scope 19 review status](reviews/2026-11-07-findings.md); reviewer probes retained under `/tmp/scope19-probe`, not independently rerun by the coordinator.
- Next time: assert persisted rows and ledger state across two real App boots; do not interpret immediate async-event absence as a bug.
- Status: active

## 2026-11-07 - Budget runtime browser suites explicitly
- Scope: core-ui/runtime, test tooling
- Trigger: `go test -short` still launches Chrome and the broad runtime package hit Go's ten-minute timeout.
- Evidence: [validation limitation](reviews/2026-11-07-runtime-correctness.md); the active test was only one second old at the package-wide alarm, so an individual hang is not established.
- Fix-run evidence: runtime worker reports the full package tree passed with `-timeout 20m`, main package 730s; coordinator independently reran focused browser, composition, lint and budget gates, not the full tree.
- Approach: quick composition checks select named browser-free tests; full browser validation needs an intentional timeout budget and `-json` progress capture.
- Next time: inspect browser-test selection before assuming `-short` excludes browsers; never report a timed-out package as green or count elapsed time as a functional bug.
- Status: active

## 2026-11-07 - Validate serialized pack output, not only its in-memory spec
- Scope: cmd/gofastr/internal/packcmd
- Trigger: a supported data table packs successfully but its rows become strings and cannot validate/regenerate.
- Approach: build a fresh CLI under /tmp and exercise validate → generate → pack → validate → generate, with a no-row control.
- Evidence: [C11 fix](reviews/2026-11-07-fixes-codegen.md); nested lists now use empty/scalar-flow/complex-block forms. Coordinator passed full packcmd and public generate→pack→load→regenerate regressions, including quoted cells and nested maps.
- Next time: parse and validate the exact serialized YAML during round-trip checks, not just the pre-serialization spec.
- Status: active

## 2026-11-07 - Separate signal-store retention from post-swap DOM rendering
- Scope: core-ui/runtime
- Trigger: cart state survives navigation in memory while the swapped badge displays its server snapshot; string action messages also disappear in fallback.
- Approach: compare store and bound node after partial merge; include a Back navigation from a page with no signal seed, not only pages with page-scoped declarations. Test fallback string and object configs under both module failure and missing-stack conditions.
- Evidence: [C09/C10 reproduction](reviews/2026-11-07-runtime-correctness.md); merge root is nav.js, not the reviewer's signals.js path. Coordinator method-seam probes and reviewer browser evidence are explicitly distinct.
- Final checkpoint: coordinator browser run passes fallback messages, both page-list/globals-only Back/Forward arms and envelope fills/replays. The earlier globals-only failure is superseded; the worker says its in-place mutation proof was active at that time. [Fix evidence](reviews/2026-11-07-fixes-runtime.md).
- Next time: assert visible bound DOM as well as stored state, and normalize accepted toast shapes before selecting a renderer.
- Status: active

## 2026-11-07 - Probe metrics across plugin initialization boundaries
- Scope: battery/log, plugin lifecycle
- Trigger: Metrics promises any-time safety but its handler is allocated only during Init.
- Approach: call direct Metrics and a retained MetricsHandler before Init, then initialize the same plugin with an in-memory sink and repeat both calls.
- Evidence: [C08 fix](reviews/2026-11-07-fixes-services.md) publishes the fanout handler atomically and returns zero metrics before Init; coordinator full log suite and the direct/HTTP lifecycle regression pass.
- Boundary: one narrow lifecycle defect; handler panic is not inherently process termination. The fix is local and uncommitted.
- Next time: test explicitly documented pre-Init access independently of the normal register-initialize-serve flow.
- Status: active

## 2026-11-07 - Retain cache variants and clamp image dimensions before fit
- Scope: battery/cache, framework/image
- Trigger: normal Vary traffic repeatedly missed and wide banners produced square placeholders.
- Approach: use a two-variant sequence with spare capacity/long TTL, and decode actual Placeholder JPEGs beside Resize controls for boundary and ordinary aspect ratios.
- Evidence: [C06/C07 fixes](reviews/2026-11-07-fixes-services.md); coordinator full cache/image suites pass with generation-indexed variants and no second fit of a derived image box.
- Boundary: cache invalidation is not atomic with an in-flight GET store, and unreachable variants occupy slots until TTL; Redis/S3 were not executed.
- Next time: test variant retention, eviction and expiry separately, and preserve the requested image axis when deriving the other side.
- Status: active

## 2026-11-07 - Compare SDK declarations with actual entity response fields
- Scope: cmd/gofastr/internal/appgen, framework/openapi
- Trigger: SDK-only declaration walks missed injected timestamps and typed an integer-key relation as a string.
- Approach: generate both SDK targets, create real records through the HTTP handler, compare OpenAPI/Go AST/TS declarations, and feed the captured response through the actual generated JS client.
- Evidence: [C04/C05 fixes](reviews/2026-11-07-fixes-codegen.md); coordinator passed full appgen, `TestSDKShapesMatchCRUDResponse` including Node, and Meridian's generated-tree gate. Injected timestamps appear only in response/field shapes; integer-key relation types now match `number | string` target keys.
- Boundary: read-only timestamps stay out of writable inputs. Unselected/composite/many relation targets retain string typing; no tsc diagnostic or Postgres execution is claimed.
- Next time: derive response fields from resolved entity schema and relation types from resolved target keys, with explicit controls for input/read-only distinctions.
- Status: active

## 2026-11-07 - Alternate metadata sources do not imply always-strip semantics
- Scope: battery/email, review adjudication
- Trigger: an OMP candidate inferred that every leading `Subject: ` line must disappear even after another source supplies the subject.
- Approach: compare the loader's fallback/alternative-source contract against fresh `LoadFromFS` and `Execute` probes before counting a body-formatting discrepancy.
- Evidence: [retained probe and rejection](reviews/2026-11-07-email-correctness.md); observations reproduce, but unconditional stripping is not promised. This candidate has no finding ID.
- Next time: distinguish subject selection from body normalization; single-source controls do not establish duplicate-source semantics.
- Status: active

## 2026-11-07 - Match schema enum cardinality to component requirements
- Scope: framework/entityui, framework/ui
- Trigger: a valid required single-value enum replaced both record and create forms with a generic error.
- Approach: independently render both screens with and without a default, then compare optional-single and required-two-option controls.
- Evidence: [C03 and retained overlay reproduction](reviews/2026-11-07-entityui-correctness.md) confirms four failed form renders and four successful controls; `typedInput` selects a segmented control below its two-option minimum.
- Next time: test schema-to-control boundaries at minimum supported cardinality, and assert unrelated form fields survive instead of treating panic containment as success.
- Status: active

## 2026-11-07 - Count independently validated root causes, not review claims
- Scope: correctness review coordination
- Trigger: expand the OMP GLM review toward twenty distinct findings without relaxing the evidence bar.
- Approach: keep the numbered [findings ledger](reviews/2026-11-07-findings.md); require fresh public-boundary reproduction, retained evidence and deduplication before assigning an ID.
- Evidence: the ledger reached 20 distinct reproduced roots; C20 uses real HTTP and sequential versioned moves, while the delayed-refresh race and ACP parse-error reserve remain uncounted.
- Next time: resume from the ledger, preserve conditional compatibility caveats, and stop allocating new reviews when the requested target is reached.
- Status: active

## 2026-11-07 - Respect component cardinality and registry ownership
- Scope: framework/entityui, core-ui/runtime widgets
- Trigger: valid one-option enums panicked during form rendering; inherited Object members looked like mounted widgets.
- Approach: select segmented controls only for their supported cardinality, and reuse the own-property registry reader for duplicate-mount detection.
- Evidence: [C03/C15 fixes](reviews/2026-11-07-fixes-forms-widgets.md); coordinator render and real-browser lifecycle regressions pass, including ordinary-name and genuine-duplicate controls.
- Next time: cover minimum component cardinality and inherited-member names through public rendering and lifecycle boundaries.
- Status: active

## 2026-11-07 - Reconcile agent activity with final reports
- Scope: review orchestration
- Trigger: stale running labels contradicted final reports already present in four review transcripts.
- Approach: inspect the final transcript before claiming a reviewer remains active; distinguish report completion from cancellation acknowledgement.
- Evidence: [review closure](reviews/2026-11-07-findings.md); recovered scopes 29/32/38/42 were finished, while cancellation returned only `cancel_requested`.
- Next time: report observed work and uncertainty explicitly; do not launch replacements for already completed reviews.
- Status: active

## 2026-11-07 - Exercise versioned components beyond the first save
- Scope: framework/headless, optimistic state
- Trigger: a sortable list saved once, then conflicted forever because the root retained its initial version.
- Approach: test at least three sequential moves through the compare-and-bump handler, including rows-only conflict reconciliation and fresh-render/unversioned controls.
- Evidence: [C20 reproduction](reviews/2026-11-07-sortable-correctness.md); statuses 204/409/409 versus 204/204/204 in both controls.
- Next time: verify renewal of concurrency tokens after both successful writes and authoritative fragment refreshes.
- Status: active

## 2026-11-07 - Cursor comparisons must match stored SQLite timestamps
- Scope: framework/filter, framework/crud, framework/pagination
- Trigger: OMP reported missing/repeated cursor rows for timestamps populated outside canonical CRUD writes.
- Approach: independently exercise the real HTTP list handler over `Unmanaged` in-memory SQLite tables, testing both `TEXT` and `DATETIME` columns plus a canonical-format control.
- Evidence: [C02 fix](reviews/2026-11-07-fixes-data.md); coordinator passed full CRUD and verbose timestamp regressions. Unmanaged SQLite keysets bind stored text; base CRUD comparisons refuse noncanonical columns with 500 `timestamp_layout` instead of returning wrong rows.
- Boundary: the refusal scans per request and does not guard nested-relation/DSL/typed/read-scope comparisons or direct SQL into managed tables; normalize those sources. Coordinator Postgres test skipped without a DSN; worker container results are separate evidence.
- Next time: test cursor encode/rebind symmetry against the actual stored layout and settle unmanaged-table compatibility before changing timestamp coercion.
- Status: active

## 2026-11-07 - Validate translation placeholders across runtimes
- Scope: core/i18n, core-ui/runtime
- Trigger: OMP reported placeholder grammar drift and blank translations during a correctness review.
- Approach: exercise `Define`, `CheckCatalog`, `RegisterStrings` and `StringsJSON`, then execute the actual `say` function extracted from `core-ui/runtime/frag/kernel.js` with Node.
- Evidence: [C01 fix](reviews/2026-11-07-fixes-runtime.md); coordinator passed real-browser Go/say grammar comparisons, including Unicode whitespace controls, and full i18n tests. Catalog placeholder spellings remain exact for headless fills; stale comments were corrected without changing that check.
- Boundary: reproduced blank-text/blank-plural output remains a contract question, not a confirmed fallback bug; existing guarantees concern absent entries/forms.
- Next time: use a canonical control and the exported translation table to validate both runtimes; settle blank-value semantics before changing fallback behavior.
- Status: active

## 2026-11-07 - Queue correctness review evidence
- Scope: battery/queue, review tooling
- Trigger: independently validate an OMP GLM-5.3 correctness review of scheduling, retries and shutdown at `953ccc082`.
- Approach: run explicit package checks; an affected-only run can omit an unchanged package under review.
- Evidence: `go test ./battery/queue/ -count=1`, `go test ./battery/queue/ -race -count=1 -run 'DST|Dst|Cron|Interval|Scheduler|Backoff|Lease|Shutdown|Close|Reclaim|Nack|CatchUp|Fire'`, and `go vet ./battery/queue/` all passed independently; no functional bug was confirmed in this scope.
- Limits: the race run is a subset, service-backed tests retain their existing skip conditions, and the reviewer's deleted temporary probes were not independently rerun. Passing checks do not establish release readiness.
- Next time: rerun the explicit commands at the release revision and retain executable reproducers for any candidate finding.
- Status: active

## 2026-11-04 - Local 2FA assurance is not a session credential
- Scope: battery/auth, generated Local SDK
- Trigger: cookie-only `RequireTwoFA` refused Local CRUD, while inheriting a session token could outlive an inner tenant's principal clearing and authorize account mutations.
- Approach: bind the verification fact to manager, user and lookup tenant by default, including an empty tenant. Only exact `*EntityUserStore` plus `*MemorySessionStore` or `*EntitySessionStore` types are automatically global; wrappers stay custom. `AuthConfig.GlobalSessionLookup` explicitly opts out when both lookups ignore the tenant, making assurance about the user alone; it does not add filtering.
- Recheck: retain bounded candidate tokens and the original partitioned lookup tenant in the identity closure. Re-resolve the same user without rejecting a downstream tenant change; `tenant.ResolveMiddleware` runs afterward. A revoked first cookie can fall back only to another session of that user.
- Boundary: every built-in auth route uses `browserAccountOnly`; the five session-account gates also call the shared refusal directly. Local account and credential operations return 403, never inherit browser credentials, and never re-check session revocation from the assurance snapshot.
- Evidence: `go test ./battery/auth -run '^TestInProcess' -count=1` covers real outer cookies, the full Local middleware stack, both cookie orders, pending/expired/revoked/other-user candidates, nested clearing, store failures, forged network assurance, and all account routes/gates.
- Mutation evidence: treating every store as global or skipping the gate's tenant comparison fails `TestInProcessTwoFAAssuranceCustomStoreDefaultTenantBinding`; treating built-ins as custom fails `TestSessionRecheckAfterTenantResolution` and `TestInProcessTwoFAAssuranceAfterTenantResolution`; ignoring the opt-out fails `TestInProcessTwoFAAssuranceCustomStoreGlobalSessionLookup`. Using the authorization tenant for lookup fails `TestSessionRecheckPreservesLookupTenant`.
- Next time: test real built-in memory and entity sessions in the documented auth-then-tenant order with no knob; do not mistake a test fake or wrapper for a built-in. Test omitted-knob custom lookup and the explicit global opt-out with a downstream tenant resolver. Keep manager/user-switch, original lookup-tenant and full-revocation refusals pinned.
- Status: active

## 2026-11-04 - Local repository admission and middleware parity
- Scope: framework, auth, generated SDK
- Trigger: socket-free CRUD lost context identity and failed CSRF or used the wrong API prefix.
- Approach: pass `App.InProcessHandler()`; reject ambient credentials before setting the private marker, and preserve ordinary anonymous clearing and CSRF.
- Evidence: `go test ./cmd/gofastr/internal/appgen -run 'TestSDKRepoParity|TestSDKAppPrefixDefaults' -count=1` exercises generated Local/Remote repositories with default middleware, real session auth and CSRF, owner/tenant rules, and integer/composite keys.
- Next time: test the real middleware stack and mutate credential rejection, session clearing, and CSRF skip independently; preserve explicit empty prefix overrides.
- Status: active

### Admission guard mutation evidence
- Removing Cookie/Authorization refusal fails `TestInProcessHandlerRefusesCredentials` (204 dispatched instead of 403).
- Keeping users without the private marker fails both `TestSessionMiddleware_ClearsUserOnAnonymousFallthrough` and `TestSessionMiddleware_ClearsUserOnStoreError`.
- Skipping CSRF without the marker fails `TestCSRFChecksBrowserAttachedAuthorization` and `TestInProcessSessionAndCSRF/Network`.
- Removing either Local exception fails `TestSDKRepoParity` with 401 (session) or 403 (CSRF); removing app-prefix inference fails with 404. Removing the post-dispatch cancellation check fails `TestLocalCanceledDuringDispatch` with EOF.

## 2026-11-04 - Global toast catalog versus per-router assets
- Scope: framework/uihost, core-ui/widget
- Trigger: a second host registered a default beside the first host's custom stack and left its style missing on the second router.
- Approach: reuse all registered stack definitions and attach only missing CSS routes at their published `StyleURL`; toast stacks have no per-stack script.
- Evidence: `go test ./framework/uihost -run 'Test.*Toast' -count=1` covers simultaneous custom hosts, prefixed assets, repeated boot, unrelated widgets, and the original two-default-host case; mutations of stack-kind, missing-route, and default-registration guards each fail.
- Next time: boot every host before rendering the first, assert the region count, and fetch every linked stylesheet on each router.
- Status: active

## 2026-11-08 - Obstruct contract application without permission bits
- Scope: contract tooling tests, upload, migrate
- Trigger: chmod-based write failures still succeed as root; macOS immutable flags do not fix Linux.
- Approach: `internal/contractstest.ObstructApply` links readable fixture source outside the project root, so discovery sees it and Apply's `os.Root` refuses it after the first file. Windows holds a read-sharing handle to refuse a write without symlink privileges. Upload uses a missing leaf and a regular-file parent; migrate omits the snapshot directory.
- Evidence: all six issue #669 obstruction tests pass as uid 0 in the `golang:1.27` container; removing ObstructApply makes all four CLI/MCP partial-application tests fail.
- Next time: choose a kernel path or confinement failure that holds for every uid, and assert the prior file changed before accepting a partial-application result.
- Status: active

## 2026-11-08 - Check Builder control flow and PostgreSQL identifier boundaries
- Scope: textbind, battery/semantic
- Trigger: Builder resets in sibling branches cleared taint; PostgreSQL truncated both index suffixes of a 62-byte table to the same name.
- Approach: require a reset to dominate the read; test byte, rune and formatted writes. Keep distinct suffixes when shortening index names and inspect the actual pg_indexes catalog.
- Evidence: new Builder fixtures fail on the GLM analyzer; TestPgVectorLongTableHasBothIndexes finds one index with its original naming helper and two with the fix.
- Next time: test both branches and identifier lengths 61, 62 and 63; pin EXPLAIN settings to one transaction rather than a pooled connection.
- Status: active

## 2026-11-08 - Measure analyzer guards with explicit mutation operators
- Scope: internal/analyzers, issue #669
- Trigger: historical survivor counts did not describe the current sources or mutation operators.
- Approach: run each package's analysistest fixtures, negate every nonliteral if condition and replace each reporter selector (including a.pass) with a compiling no-op separately, with a bounded test timeout; restore source after each run.
- Evidence: fixedtmp 79/79, timestampid 86/86, recoverlog 66/66, nowaitdelay 47/47 and keytrim 32/32 mutants killed. Added fixture cases kill missing guards; two verdict-invariant branches were removed.
- Next time: reject mutation build failures as invalid; the draft reporter rewrite produced pass.mutateNopReportf, so it hid an untested exec.Cmd.Path reporter. Record the operator set and keep bad and quiet fixtures.
- Status: active

## 2026-11-08 - Encode JSON before committing an HTTP status
- Scope: JSON response helpers, discardeddecode
- Trigger: a NaN or invalid RawMessage produced an empty 200 because the encoder failed after success headers were written.
- Approach: marshal first and answer a generic 500, propagate broker/queue serialization errors, and log output failures. Preserve encoder options and avoid fabricated cursor placeholders.
- Evidence: HTTP regressions in framework/crud, framework, battery/auth and cmd/kiln fail on their original writers; the widget failure-writer test pins logging.
- Next time: inject unencodable values at the real response boundary and assert status, body and sensitive-response headers before widening the analyzer.
- Status: active

## 2026-11-13 - Screenshot the viewport, not a full-page stitch, when chrome is pinned
- Scope: any page with a sticky sidebar, toolbar or header (the admin shell, `ui.ContentRow{Sticky: true}`)
- Trigger: a full-page capture of the admin Logs page showed the list running on below a sidebar that ended one viewport down. It read as a broken layout, was noticed, and was sent to the user anyway as "an artifact" without a check.
- Approach: a full-page capture draws viewport-height pinned chrome once, at the top, so it cannot show what a reader sees after scrolling. Capture the viewport at the top and again scrolled to the bottom, and measure the pinned element's rect at both (`getBoundingClientRect` top and bottom against `innerHeight`).
- Evidence: scrolled to the bottom of the Logs and Audit pages at 1360x900, the sidebar's rect stayed 0 to 900 with `position: sticky`; the viewport screenshot shows it beside the last row.
- Next time: anything in a screenshot that looks wrong is checked before the screenshot is shown to anyone, and a capture that misrepresents the page is retaken, not explained.
- Status: active

## 2026-11-13 - Telemetry's goal: most of the value, a tenth of the weight
- Scope: battery/telemetry and everything built on its files (screens, CLI, MCP tools, what comes next)
- Trigger: the owner's statement of the goal: about 80% of what Datadog, Sentry and groundcover give a small team, at about 10% of the code and of the machine.
- Approach: before adding a feature, name the question an operator asks that it answers, and whether something already built answers it. Prefer one more filter on an existing screen to a new screen, the standard library to a dependency, a scan with a budget to an index, and a setting with a good default to a mode. Measure the result against the 1 vCPU, 1 GB target, not against a laptop.
- Evidence: at this note the feature is about 3,900 lines of non-test Go with no new module dependency, runs inside the app's process, and holds 13 days at 20 requests a second in 5 GiB.
- Next time: a proposal that needs a second process, a database, or a new dependency has to say what it buys that the files cannot.
- Status: active
