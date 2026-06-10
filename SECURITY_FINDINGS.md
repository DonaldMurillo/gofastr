# Security findings ledger

_secure-100 campaign (2 passes) — 103 verified findings, each survived an adversarial refute pass._

## Status legend

| Status | Meaning |
|---|---|
| `fixed` | Re-verified 2026-06-10: the guard is present in current code and/or a pinning `*_security_test.go` exercises the fix, or the fixing commit is in git history. |
| `open` | Re-verified: the vulnerable pattern is still present in current code. |
| `needs-verification` | Not yet re-checked against current code. Treat as potentially open until verified — do NOT assume fixed. |

Status audit 2026-06-10: every P0/P1 row (#1–#30) was re-verified in code; P2/P3 rows were spot-checked
(#36, #92 verified fixed; #68 resolved by package removal in `67ee2d92`; #102/#103 fixed in `ba7d63e4`).
Remaining P2/P3 rows are `needs-verification` pending a per-row re-check, not presumed fixed.

### Verification evidence for `fixed` rows

- #1 — `framework/harness/control/rest/rest.go` enforces token session/command scope (lines ~100/215/259) + `rest_security_test.go`.
- #2, #4 — `battery/auth/twofa_pending_security_test.go::TestPendingTwoFA_MutationEndpointsRejected` gates pending-2FA sessions out of 2FA mutation endpoints.
- #3 — `framework/crud/crud_api.go` wires `requireOwnerContext` into every in-process read/update/delete (lines 43–314); `crud_upsert.go:49` too.
- #5, #24 — `core-ui/patterns/{nestedlist,tree}` route Href through local `safeURL`.
- #6 — `framework/crud/crud_upsert.go` `errUpsertForeignRow` cross-principal guard.
- #7 — `battery/email/smtp.go` random per-message boundary + refuses bodies containing the boundary.
- #8, #17, #23 — `battery/cache/middleware.go` `varyHasStar` makes `Vary: *` uncacheable.
- #9 — `battery/embed/embed_security_test.go::TestQueryClampsK`.
- #10 — `core/schema/validate.go:142` rejects NaN/Inf.
- #11, #28 — `core/markdown/inline.go` `stripURLControlBytes` + `maxInlineDepth = 64`.
- #12 — `framework/typed_hooks_security_test.go` pins zero-value merge-back.
- #13 — `framework/crud/include_owner_security_test.go`.
- #14, #26 — `framework/crud/eager_filtered.go` adds `deleted_at IS NULL` and strips Hidden columns on related loads.
- #15, #22 — `framework/ui/safety.go::safeURL` rejects control bytes; `gallery_security_test.go::TestGalleryDropsDangerousHref`.
- #16, #30 — `battery/queue` worker `recover()` (db.go/memory.go) + claimed-row reclaim (db.go ~309).
- #18 — `core/featureflag/flag.go` BoolDefault fail-closed contract.
- #19 — `core/markdown/parser.go:163` `escapeAttr` on fence info string.
- #20 — `core/handler/handler.go` recovers panics, logs truncated value, returns generic 500.
- #21 — `framework/file/filefield.go:315` sniffs `<!doctype` prefix.
- #25 — `framework/harness/tool/builtins/webfetch_security_test.go::TestWebFetchSSRFGuard`.
- #27 — `framework/uihost/uihost.go` autofocus/attr scrubber (~2186).
- #29 — `core-ui/di/di.go` writes under `mu.Lock()`, no RLock writes remain.
- #36 — `battery/search/memory.go` offset/limit clamp.
- #68 — package removed (`67ee2d92`).
- #92 — `framework/cron/cron.go` `started` flag gates Stop's wait.
- #102, #103 — fixed in `ba7d63e4` (commit references both numbers).

| # | Sev | Cat | Package | Title | Status |
|---|---|---|---|---|---|
| 1 | P0 | vuln | `framework/harness/control/rest` | REST control plane ignores token session/command scope | fixed |
| 2 | P0 | vuln | `battery/auth` | Pending-2FA session can disable/reset 2FA, bypassing step-up | fixed |
| 3 | P0 | vuln | `framework/crud` | In-proc owner read/update/delete fail open (no requireOwnerContext) | fixed |
| 4 | P0 | vuln | `battery/auth` | Pending-2FA session can fetch fresh backup codes, fully bypassing 2FA | fixed |
| 5 | P1 | vuln | `core-ui/patterns/nestedlist` | Unsanitized Href enables javascript: scheme XSS | fixed |
| 6 | P1 | vuln | `framework/crud` | Upsert ON CONFLICT hijacks rows of other owners/tenants | fixed |
| 7 | P1 | vuln | `battery/email` | MIME boundary injection via body content | fixed |
| 8 | P1 | vuln | `battery/cache` | Vary: * responses are cached and replayed cross-user | fixed |
| 9 | P1 | vuln | `battery/embed` | Unbounded K in /query causes OOM allocation DoS | fixed |
| 10 | P1 | vuln | `core/schema` | Decimal accepts "NaN", bypassing all Min/Max bounds | fixed |
| 11 | P1 | vuln | `core/markdown` | javascript: URL bypass via interior tab/newline/NUL in scheme | fixed |
| 12 | P1 | vuln | `framework` | Typed Before-hook zero-value mutations silently dropped by omitempty merge-back | fixed |
| 13 | P1 | vuln | `framework/crud` | include scoped filter on owner field bypasses related-table owner scope | fixed |
| 14 | P1 | vuln | `framework/crud` | include eager-load returns soft-deleted related rows | fixed |
| 15 | P1 | vuln | `framework/ui` | LinkButton scheme guard bypassed by interior tab/newline in scheme | fixed |
| 16 | P1 | vuln | `battery/queue` | Handler panic kills worker goroutine permanently (poison-message DoS) | fixed |
| 17 | P1 | vuln | `battery/cache` | Vary:* responses are cached and replayed to all users | fixed |
| 18 | P1 | vuln | `core/featureflag` | BoolDefault fails open on store error in its second fetch | fixed |
| 19 | P1 | vuln | `core/markdown` | Fence info-string XSS via %q-escaped class attribute | fixed |
| 20 | P1 | vuln | `core/handler` | HandlerAdapter leaks raw panic value into 500 body | fixed |
| 21 | P1 | vuln | `framework/file` | DOCTYPE-prefixed SVG bypasses content sniffer (stored XSS) | fixed |
| 22 | P1 | vuln | `framework/ui` | Gallery anchor href skips safeURL, allows javascript: XSS | fixed |
| 23 | P1 | vuln | `battery/cache` | Vary: * responses cached and replayed cross-user | fixed |
| 24 | P1 | vuln | `core-ui/patterns/tree` | tree (and breadcrumbs) Node.Href bypasses safeURL → javascript: XSS | fixed |
| 25 | P1 | vuln | `framework/harness/tool/builtins` | WebFetch has no SSRF guard, reaches metadata/internal IPs | fixed |
| 26 | P1 | vuln | `framework/crud` | Include eager-load leaks related entity Hidden fields | fixed |
| 27 | P1 | vuln | `framework/uihost` | Head scrubber misses input/select/textarea/keygen autofocus XSS | fixed |
| 28 | P1 | vuln | `core/markdown` | Unbounded inline recursion (nested links/emphasis) → stack-exhaustion DoS | fixed |
| 29 | P1 | bug | `core-ui/di` | DI Inject writes singleton/resolved maps under RLock — concurrent-map-write panic | fixed |
| 30 | P1 | bug | `battery/queue` | DBQueue claimed jobs never reclaimed after crash | fixed |
| 31 | P2 | vuln | `core-ui/runtime` | _isUnsafeSignalUrl bypass via embedded tab/newline in scheme | needs-verification |
| 32 | P2 | vuln | `framework/file` | Content sniffer misses <img onerror>, BOM-prefixed <script> | needs-verification |
| 33 | P2 | vuln | `framework/file` | URL scheme check bypassed by whitespace inside scheme | needs-verification |
| 34 | P2 | vuln | `framework/uihost` | Unclosed dangerous head tags bypass the scrubber (XSS) | needs-verification |
| 35 | P2 | vuln | `framework/uihost` | Per-page SEO URLs skip the isSafeHeadURL allow-list | needs-verification |
| 36 | P2 | vuln | `battery/search` | Unvalidated Offset/Limit panics Memory.Search (slice OOB DoS) | fixed |
| 37 | P2 | vuln | `battery/auth` | Password reset does not revoke existing sessions | needs-verification |
| 38 | P2 | vuln | `core/schema` | Int float->int64 overflow saturates, accepted as valid | needs-verification |
| 39 | P2 | vuln | `core/upload` | MaxSize used as maxMemory; body spilled to disk before size check | needs-verification |
| 40 | P2 | vuln | `core-ui/component` | Render-panic fallback interpolates error into HTML unescaped (XSS) | needs-verification |
| 41 | P2 | vuln | `framework/crud` | Update handler resurrects/mutates soft-deleted records | needs-verification |
| 42 | P2 | vuln | `framework/file` | Content sniffer misses SVG/HTML when tag isn't the leading token | needs-verification |
| 43 | P2 | vuln | `framework/filter` | Unbounded ORDER BY via repeated ?sort= params | needs-verification |
| 44 | P2 | vuln | `framework/ui` | PieChart slice Color/ID injected raw into SVG enables XSS | needs-verification |
| 45 | P2 | vuln | `battery/email` | STARTTLS stripping: silent plaintext fallback | needs-verification |
| 46 | P2 | vuln | `battery/log` | Access log writes uncapped X-Forwarded-For header | needs-verification |
| 47 | P2 | vuln | `core-ui/runtime` | Signal URL guard misses leading C0 control chars | needs-verification |
| 48 | P2 | vuln | `framework/image` | int64 pixel-area overflow bypasses decompression-bomb guard | needs-verification |
| 49 | P2 | vuln | `framework/image` | stdimage.Decode panic on crafted input is not recovered | needs-verification |
| 50 | P2 | vuln | `framework/static` | SSG dynamic-route param enables path traversal of build output | needs-verification |
| 51 | P2 | vuln | `framework/harness/engine` | Untrusted-content tag breakout via closing tag | needs-verification |
| 52 | P2 | vuln | `framework/harness/session/sqlite` | Redactor misses provider sk- API keys | needs-verification |
| 53 | P2 | vuln | `framework/crud` | UpsertOne skips validateMediaURLs (stored XSS via Image field) | needs-verification |
| 54 | P2 | vuln | `framework/crud` | MCP list tool builds filter params for Hidden fields | needs-verification |
| 55 | P2 | vuln | `battery/auth` | Login per-account rate limiter map grows unbounded (memory DoS) | needs-verification |
| 56 | P2 | vuln | `core/upload` | Multipart temp files never removed (disk-exhaustion DoS) | needs-verification |
| 57 | P2 | vuln | `core/markdown` | Quadratic blowup on nested blockquotes (CPU DoS) | needs-verification |
| 58 | P2 | vuln | `core-ui/runtime` | data:image/svg+xml allowed by signal URL guard | needs-verification |
| 59 | P2 | vuln | `framework/harness/tool/builtins` | Bash blocklist bypassed by absolute path or prefix | needs-verification |
| 60 | P2 | vuln | `framework/file` | URL scheme guard misses leading C0 control bytes | needs-verification |
| 61 | P2 | vuln | `framework/crud` | Cursor list path leaks raw driver error text | needs-verification |
| 62 | P2 | vuln | `core/upload` | SanitizeFilename does unbounded work before length cap | needs-verification |
| 63 | P2 | bug | `core-ui/runtime` | Primary RPC dispatcher omits CSRF token | needs-verification |
| 64 | P2 | bug | `framework/harness` | Per-session persistLoop goroutine and context leak on Shutdown | needs-verification |
| 65 | P2 | bug | `battery/queue` | RedisQueue.Dequeue silently loses jobs on a single malformed message | needs-verification |
| 66 | P2 | bug | `battery/embed` | FixedWindow.Chunk recomputes byte offset O(N^2) | needs-verification |
| 67 | P2 | bug | `core/upload` | Filename length-cap splits multibyte runes -> invalid UTF-8 key | needs-verification |
| 68 | ~~P2~~ resolved | `core-ui/signal` | Global currentCtx data race — package removed in favor of `core-ui/interactive` | fixed |
| 69 | P2 | bug | `core/mcp` | MCP tool-handler panic is never recovered | needs-verification |
| 70 | P2 | bug | `core/middleware` | metrics/tracing writers drop Hijacker and Pusher | needs-verification |
| 71 | P2 | bug | `framework` | App.InTx leaks tx connection + row locks when fn panics | needs-verification |
| 72 | P2 | bug | `framework/file` | GenerateFilePath uniqueness is timestamp-only, collides | needs-verification |
| 73 | P2 | bug | `framework/pagination` | limit=1 always resets offset to 0, breaking offset pagination | needs-verification |
| 74 | P2 | bug | `battery/queue` | MemoryQueue type-filter Dequeue silently drops drained jobs | needs-verification |
| 75 | P2 | bug | `battery/cache` | 206 Range response poisons cache for full GETs | needs-verification |
| 76 | P2 | bug | `battery/log` | log access wrapper drops http.Hijacker, breaks WebSocket upgrades | needs-verification |
| 77 | P2 | bug | `core/schema` | Decimal Min/Max compared via float64 loses precision, bypassing bounds | needs-verification |
| 78 | P2 | bug | `core/query` | Backward cursor emits ASC ORDER BY, returns wrong page | needs-verification |
| 79 | P3 | vuln | `core/i18n` | Unbounded Accept-Language parsing enables request-amplified DoS | needs-verification |
| 80 | P3 | vuln | `core/middleware` | Unbounded metrics cardinality via arbitrary HTTP method | needs-verification |
| 81 | P3 | vuln | `framework/filter` | LIKE wildcard injection in _like filter (no ESCAPE) | needs-verification |
| 82 | P3 | vuln | `battery/search` | Unbounded query-term count amplifies search cost (DoS) | needs-verification |
| 83 | P3 | vuln | `core/schema` | Int Min/Max check via float64 loses precision, bypassing large bounds | needs-verification |
| 84 | P3 | vuln | `core/upload` | Unicode line separators (U+2028/U+2029/U+0085) survive sanitization | needs-verification |
| 85 | P3 | vuln | `battery/email` | Unescaped double-quote in attachment filename injects MIME params | needs-verification |
| 86 | P3 | vuln | `core-ui/runtime` | html-mode signal injects unescaped JSON.stringify of error object | needs-verification |
| 87 | P3 | bug | `framework/lifecycle` | Data race on lc.timeout between Shutdown and SetShutdownTimeout | needs-verification |
| 88 | P3 | bug | `core/schema` | String length uses byte count, not rune count | needs-verification |
| 89 | P3 | bug | `core/router` | Params() drops catch-all {name...} segment value | needs-verification |
| 90 | P3 | bug | `core/markdown` | Quadratic blowup on unmatched emphasis delimiters | needs-verification |
| 91 | P3 | bug | `core/middleware` | SampledLogging logs raw r.Method (log injection) | needs-verification |
| 92 | P3 | bug | `framework/cron` | Scheduler.Stop() deadlocks if Start() never called | fixed |
| 93 | P3 | bug | `battery/queue` | Redis visibility timeout is recorded but never enforced (lost jobs) | needs-verification |
| 94 | P3 | bug | `core/handler` | Bind rejects valid JSON keys from embedded struct fields | needs-verification |
| 95 | P3 | bug | `core/schema` | validateDecimal accepts non-decimal float literal forms | needs-verification |
| 96 | P3 | bug | `core/router` | Custom NotFound swallows native 405, returns 404 | needs-verification |
| 97 | P3 | bug | `core-ui/runtime` | SSE island name interpolated into CSS selector unescaped | needs-verification |
| 98 | P3 | bug | `core-ui/island` | Second SSE connection per session tears down the first on disconnect | needs-verification |
| 99 | P3 | bug | `battery/cache` | Cache key omits request Host: cross-host content leak | needs-verification |
| 100 | P3 | bug | `battery/auth` | Reset token consumed before new-password validation/hashing | needs-verification |
| 101 | P3 | bug | `core/migrate` | Diff emits Postgres-only DDL and catalog regardless of dialect | needs-verification |

_Pass 6 — fuzz harness (`go test -fuzz`) for the five parsers; FuzzRenderHTML found two distinct markdown DoS in seconds._

| # | Sev | Cat | Package | Title | Status |
|---|---|---|---|---|---|
| 102 | P2 | vuln | `core/markdown` | Form-feed-prefixed `>` classifies as blockquote but never advances — infinite-loop + OOM DoS | fixed |
| 103 | P2 | vuln | `core/markdown` | Table separator row wider than header indexes align slice OOB — render panic (request DoS) | fixed |
