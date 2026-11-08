# Fixes for C06, C07 and C08 — November 7, 2026

Base: `7d8cd9eea` (main), shared checkout. Uncommitted working-tree changes;
no commit, branch or PR.

| ID | Status | Production change | Regression tests |
| --- | --- | --- | --- |
| C06 | Fixed | `battery/cache/middleware.go` | `battery/cache/vary_variants_test.go`, `battery/cache/vary_index_test.go` |
| C07 | Fixed | `framework/image/geometry.go` | `framework/image/derived_side_fit_test.go` |
| C08 | Fixed | `battery/log/log.go` | `battery/log/metrics_before_init_test.go` |

Docs: `framework/docs/content/cache.md` (Vary bullet),
`framework/docs/content/image.md` (fit modes), `framework/docs/content/log.md`
(metrics before Init). `battery/log/console_init_test.go` reads the handler
through its new atomic field.

## C06: Vary variants get their own entries

A response with Vary used to be stored under the URL's key with the
request's variant headers inside the entry, so each new variant overwrote
the last. Now:

- A response with no Vary is stored under the URL key, as before.
- A varying response is stored under
  `<url key>|vary:<generation>:<sha256>`. The digest covers the
  length-prefixed `Name:value` fields that `captureVariant` produces, so
  request header values never appear in a key.
- The URL key holds a `varyIndex` (`Vary`: bare header names,
  `Generation`: `rand.Text()`). Lookup reads the index, builds the
  variant key from the request's headers and reads that entry. It still
  checks `variantMatches`, the 304 refusal and the 2xx/3xx range on the
  entry it serves.
- The index keeps its generation while it lives and is rewritten after
  each variant with the same TTL. An unsafe request deletes the URL keys,
  index included. The next store then mints a new generation, so variants
  stored before the DELETE are never served again.
- An index decodes as a `cachedResponse` with status 0 and bare Vary
  names. `variantMatches` refuses bare names, so a process running the
  old code on a shared Redis misses on the index instead of replaying it
  (WriteHeader(0) would panic).

Tests, real HTTP through `httptest.NewServer` unless noted:

- `TestVaryVariantsKeepOwnEntries`: light, light, dark, light, dark gives
  MISS HIT MISS HIT HIT; the origin runs twice; bodies are checked.
- `TestNoVaryResponseSharedAcrossHeaders`: control. Without Vary, one
  entry serves every header value.
- `TestUnsafeRequestEvictsEveryVariant`: after a DELETE, both variants
  miss and come back at v2. Re-storing light must not revive dark's v1.
- `TestVaryVariantsExpireWithTTL`: `testing/synctest` with a fake clock
  and `ServeHTTP`. HIT HIT inside the one-minute TTL, MISS MISS past it.
- `TestVariantKeysHideHeaderValues` (internal): no Set key contains the
  header value; the index sits under `getKey` with no headers, body or
  status; bare names fail `variantMatches`; the variant keeps the full
  response.

Mutations run: storing every response under the URL key failed the first
four tests with the bug sequence MISS HIT MISS MISS MISS. A fixed
generation failed the DELETE test with `theme=dark v1 HIT`. Both
reverted.

Limits:

- A variant stored before an eviction keeps its slot until its TTL. It
  is unreachable but still counts toward `WithMaxEntries`. The index also
  takes one entry.
- A GET whose store races a DELETE can rewrite the index with the old
  generation and revive older variants. The same race already let such a
  GET store a pre-change body; `cache.md` documents it.
- Two concurrent first misses can mint two generations; the loser's
  variant is orphaned until its TTL.
- Redis was not exercised; the change uses only `Get`/`Set`/`Delete`.

## C07: a derived side skips the fit

`resizeBox` truncated the derived side and then ran FitInside/FitOutside
on that box. A derived height of 0 made FitInside's scale 0, and the clamp
returned 1×1. Truncation also lost width in milder cases: 1000×333 at
width 16 gave 15×5. A box with a derived side already has the source's
aspect, so the fit now runs only when both sides were given. The derived
side still rounds down and clamps to 1, and the MaxPixels area check and
`WithoutEnlargement` still apply.

Tests (`framework/image/derived_side_fit_test.go`, public API):

- `TestDerivedSideKeepsGivenWidth`: Fill, Inside and Outside each give
  1920×36→16×1, 2000×3→16×1, 1000×333→16×5, 2000×125→16×1 and
  640×160→16×4 at width 16. Height-given: 36×1920→1×16 and
  1920×36→213×4. Upscale: 100×50→400×200.
- `TestGivenBoxStillFits`: control. 640×160 into 100×100 gives Inside
  100×25, Outside 400×100, Fill 100×100. `WithoutEnlargement` keeps a
  100×50 source unchanged.
- `TestPlaceholderKeepsWidthOnBanner`: decodes the JPEG data URL. 1920×36
  and 2000×3 give 16×1; 640×160 gives 16×4.

Mutation: dropping the derived guard brought back 1×1, 15×5 and 213×3.
Reverted.

## C08: Metrics before Init

`Plugin.handler` is now `atomic.Pointer[fanoutHandler]`. Init stores it
before building the logger, and the stop hooks close the local handler.
`Metrics` returns the zero `Metrics` while the pointer is nil, so a
direct call and `MetricsHandler` both answer before Init. The atomic also
makes a scrape that overlaps Init race-free. Behavior after Init is
unchanged.

`TestMetricsBeforeInitIsZero` makes the plugin with `log.New`, calls
`Metrics()` (zero), then scrapes `MetricsHandler` over
`httptest.NewServer` (200 with four `_total 0` lines). It then registers
the same plugin, runs `InitPlugins` and `Shutdown`, logs once more, and
checks `PostStopDrops` is 1 both directly and through the handler built
before Init.

Mutation: disabling the nil guard brought back the nil-pointer panic.
Reverted.

## Commands run

Coordinator follow-up: inspected the production diffs and independently
passed the full `battery/cache`, `framework/image` and `battery/log` suites
on the combined working tree. Qualified the public cache documentation:
an in-flight GET store can republish an older generation after eviction,
so invalidation is not an atomic consistency guarantee. Redis was not run.
The runtime gzip-budget failure was forwarded to the runtime fix agent;
the combined validation gate remains pending, not waived.
Coordinator also independently passed race-enabled `battery/log`,
`core/tracing` and `battery/print`, followed by `framework/docs` and all
three focused image regressions after the documentation cleanup.

- `go test -p 2 -count=1 ./battery/cache/ ./framework/image/ ./battery/log/ ./framework/docs/`
- `go test -p 2 -count=1 -race ./battery/log/ ./core/tracing/ ./battery/print/`
- `go test -p 2 -count=1 ./framework/file/ ./battery/storage/ ./framework/imagefield/ ./cmd/gofastr/internal/agentscmd/`
- `go vet -p 2 -vettool=<scratch>/vettool ./battery/cache/ ./battery/log/ ./framework/image/`:
  no findings in these packages. An earlier draft tripped `statusrange`
  on zero-status `cachedResponse` literals, which is why the index has
  its own type.
- `go test -p 2 -count=1 ./framework/uihost/` fails in
  `TestPlainSiteShipsCoreOnly`: the runtime.js gzip size is 16886, over
  the 16570 budget. That comes from another session's uncommitted
  `core-ui/runtime` edits; none of these fixes touch the runtime.
