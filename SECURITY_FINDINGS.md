# Security findings ledger

_secure-100 campaign (2 passes) — 103 verified findings, each survived an adversarial refute pass._

| # | Sev | Cat | Package | Title |
|---|---|---|---|---|
| 1 | P0 | vuln | `framework/harness/control/rest` | REST control plane ignores token session/command scope |
| 2 | P0 | vuln | `battery/auth` | Pending-2FA session can disable/reset 2FA, bypassing step-up |
| 3 | P0 | vuln | `framework/crud` | In-proc owner read/update/delete fail open (no requireOwnerContext) |
| 4 | P0 | vuln | `battery/auth` | Pending-2FA session can fetch fresh backup codes, fully bypassing 2FA |
| 5 | P1 | vuln | `core-ui/patterns/nestedlist` | Unsanitized Href enables javascript: scheme XSS |
| 6 | P1 | vuln | `framework/crud` | Upsert ON CONFLICT hijacks rows of other owners/tenants |
| 7 | P1 | vuln | `battery/email` | MIME boundary injection via body content |
| 8 | P1 | vuln | `battery/cache` | Vary: * responses are cached and replayed cross-user |
| 9 | P1 | vuln | `battery/embed` | Unbounded K in /query causes OOM allocation DoS |
| 10 | P1 | vuln | `core/schema` | Decimal accepts "NaN", bypassing all Min/Max bounds |
| 11 | P1 | vuln | `core/markdown` | javascript: URL bypass via interior tab/newline/NUL in scheme |
| 12 | P1 | vuln | `framework` | Typed Before-hook zero-value mutations silently dropped by omitempty merge-back |
| 13 | P1 | vuln | `framework/crud` | include scoped filter on owner field bypasses related-table owner scope |
| 14 | P1 | vuln | `framework/crud` | include eager-load returns soft-deleted related rows |
| 15 | P1 | vuln | `framework/ui` | LinkButton scheme guard bypassed by interior tab/newline in scheme |
| 16 | P1 | vuln | `battery/queue` | Handler panic kills worker goroutine permanently (poison-message DoS) |
| 17 | P1 | vuln | `battery/cache` | Vary:* responses are cached and replayed to all users |
| 18 | P1 | vuln | `core/featureflag` | BoolDefault fails open on store error in its second fetch |
| 19 | P1 | vuln | `core/markdown` | Fence info-string XSS via %q-escaped class attribute |
| 20 | P1 | vuln | `core/handler` | HandlerAdapter leaks raw panic value into 500 body |
| 21 | P1 | vuln | `framework/file` | DOCTYPE-prefixed SVG bypasses content sniffer (stored XSS) |
| 22 | P1 | vuln | `framework/ui` | Gallery anchor href skips safeURL, allows javascript: XSS |
| 23 | P1 | vuln | `battery/cache` | Vary: * responses cached and replayed cross-user |
| 24 | P1 | vuln | `core-ui/patterns/tree` | tree (and breadcrumbs) Node.Href bypasses safeURL → javascript: XSS |
| 25 | P1 | vuln | `framework/harness/tool/builtins` | WebFetch has no SSRF guard, reaches metadata/internal IPs |
| 26 | P1 | vuln | `framework/crud` | Include eager-load leaks related entity Hidden fields |
| 27 | P1 | vuln | `framework/uihost` | Head scrubber misses input/select/textarea/keygen autofocus XSS |
| 28 | P1 | vuln | `core/markdown` | Unbounded inline recursion (nested links/emphasis) → stack-exhaustion DoS |
| 29 | P1 | bug | `core-ui/di` | DI Inject writes singleton/resolved maps under RLock — concurrent-map-write panic |
| 30 | P1 | bug | `battery/queue` | DBQueue claimed jobs never reclaimed after crash |
| 31 | P2 | vuln | `core-ui/runtime` | _isUnsafeSignalUrl bypass via embedded tab/newline in scheme |
| 32 | P2 | vuln | `framework/file` | Content sniffer misses <img onerror>, BOM-prefixed <script> |
| 33 | P2 | vuln | `framework/file` | URL scheme check bypassed by whitespace inside scheme |
| 34 | P2 | vuln | `framework/uihost` | Unclosed dangerous head tags bypass the scrubber (XSS) |
| 35 | P2 | vuln | `framework/uihost` | Per-page SEO URLs skip the isSafeHeadURL allow-list |
| 36 | P2 | vuln | `battery/search` | Unvalidated Offset/Limit panics Memory.Search (slice OOB DoS) |
| 37 | P2 | vuln | `battery/auth` | Password reset does not revoke existing sessions |
| 38 | P2 | vuln | `core/schema` | Int float->int64 overflow saturates, accepted as valid |
| 39 | P2 | vuln | `core/upload` | MaxSize used as maxMemory; body spilled to disk before size check |
| 40 | P2 | vuln | `core-ui/component` | Render-panic fallback interpolates error into HTML unescaped (XSS) |
| 41 | P2 | vuln | `framework/crud` | Update handler resurrects/mutates soft-deleted records |
| 42 | P2 | vuln | `framework/file` | Content sniffer misses SVG/HTML when tag isn't the leading token |
| 43 | P2 | vuln | `framework/filter` | Unbounded ORDER BY via repeated ?sort= params |
| 44 | P2 | vuln | `framework/ui` | PieChart slice Color/ID injected raw into SVG enables XSS |
| 45 | P2 | vuln | `battery/email` | STARTTLS stripping: silent plaintext fallback |
| 46 | P2 | vuln | `battery/log` | Access log writes uncapped X-Forwarded-For header |
| 47 | P2 | vuln | `core-ui/runtime` | Signal URL guard misses leading C0 control chars |
| 48 | P2 | vuln | `framework/image` | int64 pixel-area overflow bypasses decompression-bomb guard |
| 49 | P2 | vuln | `framework/image` | stdimage.Decode panic on crafted input is not recovered |
| 50 | P2 | vuln | `framework/static` | SSG dynamic-route param enables path traversal of build output |
| 51 | P2 | vuln | `framework/harness/engine` | Untrusted-content tag breakout via closing tag |
| 52 | P2 | vuln | `framework/harness/session/sqlite` | Redactor misses provider sk- API keys |
| 53 | P2 | vuln | `framework/crud` | UpsertOne skips validateMediaURLs (stored XSS via Image field) |
| 54 | P2 | vuln | `framework/crud` | MCP list tool builds filter params for Hidden fields |
| 55 | P2 | vuln | `battery/auth` | Login per-account rate limiter map grows unbounded (memory DoS) |
| 56 | P2 | vuln | `core/upload` | Multipart temp files never removed (disk-exhaustion DoS) |
| 57 | P2 | vuln | `core/markdown` | Quadratic blowup on nested blockquotes (CPU DoS) |
| 58 | P2 | vuln | `core-ui/runtime` | data:image/svg+xml allowed by signal URL guard |
| 59 | P2 | vuln | `framework/harness/tool/builtins` | Bash blocklist bypassed by absolute path or prefix |
| 60 | P2 | vuln | `framework/file` | URL scheme guard misses leading C0 control bytes |
| 61 | P2 | vuln | `framework/crud` | Cursor list path leaks raw driver error text |
| 62 | P2 | vuln | `core/upload` | SanitizeFilename does unbounded work before length cap |
| 63 | P2 | bug | `core-ui/runtime` | Primary RPC dispatcher omits CSRF token |
| 64 | P2 | bug | `framework/harness` | Per-session persistLoop goroutine and context leak on Shutdown |
| 65 | P2 | bug | `battery/queue` | RedisQueue.Dequeue silently loses jobs on a single malformed message |
| 66 | P2 | bug | `battery/embed` | FixedWindow.Chunk recomputes byte offset O(N^2) |
| 67 | P2 | bug | `core/upload` | Filename length-cap splits multibyte runes -> invalid UTF-8 key |
| 68 | ~~P2~~ resolved | `core-ui/signal` | Global currentCtx data race — package removed in favor of `core-ui/interactive` |
| 69 | P2 | bug | `core/mcp` | MCP tool-handler panic is never recovered |
| 70 | P2 | bug | `core/middleware` | metrics/tracing writers drop Hijacker and Pusher |
| 71 | P2 | bug | `framework` | App.InTx leaks tx connection + row locks when fn panics |
| 72 | P2 | bug | `framework/file` | GenerateFilePath uniqueness is timestamp-only, collides |
| 73 | P2 | bug | `framework/pagination` | limit=1 always resets offset to 0, breaking offset pagination |
| 74 | P2 | bug | `battery/queue` | MemoryQueue type-filter Dequeue silently drops drained jobs |
| 75 | P2 | bug | `battery/cache` | 206 Range response poisons cache for full GETs |
| 76 | P2 | bug | `battery/log` | log access wrapper drops http.Hijacker, breaks WebSocket upgrades |
| 77 | P2 | bug | `core/schema` | Decimal Min/Max compared via float64 loses precision, bypassing bounds |
| 78 | P2 | bug | `core/query` | Backward cursor emits ASC ORDER BY, returns wrong page |
| 79 | P3 | vuln | `core/i18n` | Unbounded Accept-Language parsing enables request-amplified DoS |
| 80 | P3 | vuln | `core/middleware` | Unbounded metrics cardinality via arbitrary HTTP method |
| 81 | P3 | vuln | `framework/filter` | LIKE wildcard injection in _like filter (no ESCAPE) |
| 82 | P3 | vuln | `battery/search` | Unbounded query-term count amplifies search cost (DoS) |
| 83 | P3 | vuln | `core/schema` | Int Min/Max check via float64 loses precision, bypassing large bounds |
| 84 | P3 | vuln | `core/upload` | Unicode line separators (U+2028/U+2029/U+0085) survive sanitization |
| 85 | P3 | vuln | `battery/email` | Unescaped double-quote in attachment filename injects MIME params |
| 86 | P3 | vuln | `core-ui/runtime` | html-mode signal injects unescaped JSON.stringify of error object |
| 87 | P3 | bug | `framework/lifecycle` | Data race on lc.timeout between Shutdown and SetShutdownTimeout |
| 88 | P3 | bug | `core/schema` | String length uses byte count, not rune count |
| 89 | P3 | bug | `core/router` | Params() drops catch-all {name...} segment value |
| 90 | P3 | bug | `core/markdown` | Quadratic blowup on unmatched emphasis delimiters |
| 91 | P3 | bug | `core/middleware` | SampledLogging logs raw r.Method (log injection) |
| 92 | P3 | bug | `framework/cron` | Scheduler.Stop() deadlocks if Start() never called |
| 93 | P3 | bug | `battery/queue` | Redis visibility timeout is recorded but never enforced (lost jobs) |
| 94 | P3 | bug | `core/handler` | Bind rejects valid JSON keys from embedded struct fields |
| 95 | P3 | bug | `core/schema` | validateDecimal accepts non-decimal float literal forms |
| 96 | P3 | bug | `core/router` | Custom NotFound swallows native 405, returns 404 |
| 97 | P3 | bug | `core-ui/runtime` | SSE island name interpolated into CSS selector unescaped |
| 98 | P3 | bug | `core-ui/island` | Second SSE connection per session tears down the first on disconnect |
| 99 | P3 | bug | `battery/cache` | Cache key omits request Host: cross-host content leak |
| 100 | P3 | bug | `battery/auth` | Reset token consumed before new-password validation/hashing |
| 101 | P3 | bug | `core/migrate` | Diff emits Postgres-only DDL and catalog regardless of dialect |

_Pass 6 — fuzz harness (`go test -fuzz`) for the five parsers; FuzzRenderHTML found two distinct markdown DoS in seconds._

| # | Sev | Cat | Package | Title |
|---|---|---|---|---|
| 102 | P2 | vuln | `core/markdown` | Form-feed-prefixed `>` classifies as blockquote but never advances — infinite-loop + OOM DoS |
| 103 | P2 | vuln | `core/markdown` | Table separator row wider than header indexes align slice OOB — render panic (request DoS) |
