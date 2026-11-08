# Fixes for C01, C09, C10, C13 and C14 — November 7, 2026

Base: `7d8cd9eea` (main), shared checkout. Uncommitted working-tree changes;
no commit, branch or PR.

| ID | Status | Production change | Regression test |
| --- | --- | --- | --- |
| C01 | Fixed | `core-ui/runtime/frag/kernel.js` (`say`) | `core-ui/runtime/say_fill_e2e_test.go` |
| C09 | Fixed | `core-ui/runtime/frag/kernel.js` (`_fallbackToast`) | `core-ui/runtime/action_message_fallback_e2e_test.go` |
| C10 | Fixed | `core-ui/runtime/frag/{signals,nav}.js`, `core-ui/runtime/src/envelope.js` | `core-ui/runtime/swap_global_binding_e2e_test.go`, `TestEnvelopeSwapShowsStoredGlobal` in `core-ui/runtime/envelope_seed_replay_e2e_test.go` |
| C13 | Fixed | `core-ui/runtime/src/ws.js` | `TestWS5xxCleanCloseReadsError` in `core-ui/runtime/ws_e2e_test.go` |
| C14 | Fixed | `core-ui/runtime/src/poll.js` | `core-ui/runtime/poll_structured_e2e_test.go` |

Every test runs in Chrome through `chromedptest`, in the `core-ui/runtime`
package's existing lane. Each one was watched failing: the fix was
reverted in place, the test run, the fix restored.

## C01: say() fills placeholders by i18n.Fill's grammar

`i18n.Fill` takes everything up to the next `}}` as the name and trims
what Go's `unicode.IsSpace` counts, and `CheckCatalog` accepts
`{{ name }}` and `{{user-name}}`. The runtime's `say()` matched `\w+`
only, so those phrases stayed literal after a client update. The kernel
regex now takes `[\s\S]*?` between `{{` and `}}` and trims JS whitespace
minus U+FEFF, plus U+0085, which is Go's `IsSpace` set. Twelve edge
inputs (spaces, hyphens, NEL, U+3000, U+FEFF, unclosed and extra braces)
were compared against `i18n.Fill` and agree.

The checker is unchanged. It still compares placeholder spellings
exactly, because the headless fills (sortablelist, multiselect,
carousel) match fixed spellings and `framework/ui/strings.go` relies on
`Placeholders` equality.

Test: `TestRuntimeSayFillsLikeGo` registers four phrases (canonical,
spaced, hyphenated, and one untranslated), checks the catalog is clean,
and compares `__gofastr.say` in the browser with `Phrase.Text`. With the
old regex the spaced, hyphenated and English phrases fail.

## C09: a string action message reaches the fallback toast

An action answer carries `message` as a string. `NS.toast` reads a string
as its title, but `_fallbackToast` read only `cfg.title`, so the message
vanished exactly when the fallback ran. The fallback now wraps a string
as `{ title }`.

Test: `TestStringActionMessageFallsBack` runs `_serverActionFor` against
an action that answers `{"message":"Order saved"}` in two arms: the
feedback module answers 503, and the module loads but the page has no
toast stack. Each asserts the `[data-cui-toast-fallback]` region reads
the message, and that the module's load state matches the arm. Without
the wrap both arms time out waiting for the region.

## C10: a swap renders kept globals into its fresh bindings

The seed merge keeps a global the reader changed, but the swapped-in
markup carries the server's value, so the store said 1 while the new
cart badge read 0. Cached Back and Forward replayed the badge as it was
when the page was left.

- A slot is marked `global` at boot (every seed key `#gofastr-signals-page`
  does not list, and every key when the host omits that list, which
  `uihost` does when the seed holds no page-scoped key) and from every
  partial seed's `g`.
- `paintGlobals(root)` (nav.js) renders each marked slot's stored value
  into the `data-cui-signal` nodes under the swapped root through
  `_renderSignal`, the per-mode writer split out of `setSignal`. It runs
  no listeners and no flash or scroll hooks, and the html mode keeps its
  string-only and untrusted rules.
- `mergeSeedFromDOM` runs it on every swap, with or without a seed
  island: partial swaps, cached partials, the boot-cache entry, fetched
  documents and intercept panes (through `_mergeSeed`).
- The envelope navigator has its own seed merge: its `g` loop marks the
  slot, its `mergeSeedFromDOM` paints, and `applyFillTargets` paints each
  filled target. This file was outside the brief's listed ownership; the
  edit is three lines.

Tests:

- `TestSwappedGlobalBindingShowsStore`, arms `page list` and
  `globals only`: Page A to a partial with no seed island and back (only
  the boot mark can mark `cart`), then to a partial whose badge reads 0
  (fresh swap), then Back to the boot entry, then Forward to the cached
  partial, changing the cart at each step. Removing both `paintGlobals`
  calls fails all three steps. Removing the boot mark fails the first.
  Restoring the earlier list-only mark fails `globals only`.
- `TestEnvelopeSwapShowsStoredGlobal`: the same through an envelope fill
  and an envelope cache replay. Removing the two envelope paint calls
  fails both. Its fixture gained a cart badge on `/` and `/a`; the other
  envelope seed tests still pass.

## C13: a clean 45xx close reads as error

`onclose` classified by `wasClean` alone, so a 4503 whose closing
handshake completed read `closed` while the module redialed. A code in
4500–4599 now reads `error` whatever `wasClean` says; 44xx stays
`refused`.

Test: `TestWS5xxCleanCloseReadsError`. The server sends 4503 and reads the
client's close frame before it hangs up, so the browser reports
`wasClean`; the page wraps `WebSocket` to record it. It asserts a clean
close, class `error`, `refused` 0, and a second generation. Reverted, the
class reads `closed`.

## C14: an unchanged structured poll value is skipped

The widget poll skipped a key when `prev.value === next`. A structured
value decodes into a fresh object on every response, so it never matched,
and every tick rewrote the bound node and replayed its flash. The skip
now compares by JSON (`sameValue` in poll.js). Equal JSON reads as
unchanged; a value JSON cannot encode reads as changed.

Test: `TestPollSkipsUnchangedObject` seeds an object and a number,
polls them unchanged with `pollMs: 100`, and counts mutations on both
bound nodes after the poll starts: none in four ticks. Then the object
changes: it renders and the number stays untouched. With `===` the
object node took 11 writes in four ticks.

## Budgets

All growth is itemised at its measured size.

| Line | Before (main) | After | Line now |
| --- | --- | --- | --- |
| core, gzip level 6 | 16568 | 16771 | `coreGoalGZ` 16773 |
| core, gzip level 1 | 19160 | 19418 | `coreCongestionWindowGZ` 19420 |
| embed | 12119 | 12250 | 12288, unchanged |
| envelope module | 8191 | 8228 | 8230 (was 8201) |
| poll module | 1843 | 1937 | within its budget |
| ws module | 1265 | 1279 | within its budget |

Core items in `TestCoreBudgetAtPreLayout`: fallback toast string title
+16 (level 1 +29), `say()` grammar +27 (+21), global repaint +160 (+208).
`framework/uihost/plain_site_e2e_test.go` mirrors the core line (16773).
The cliff fixture (`TestCoreBudgetRejectsCliffOverflow`) was re-run at
the new window. An earlier combined run measured 16886 against the old
16570 line in `TestPlainSiteShipsCoreOnly`. That was an intermediate state
of these edits, before the global logic moved out of signals.js. The gate
passes at 16773.

## Gates run

All with `-p 2 -count=1`:

- `go test ./core-ui/runtime/ -run '^(TestRuntimeSayFillsLikeGo|TestStringActionMessageFallsBack|TestSwappedGlobalBindingShowsStore|TestEnvelopeSwapShowsStoredGlobal|TestWS5xxCleanCloseReadsError|TestPollSkipsUnchangedObject)$'`
  (three consecutive passes)
- `go test ./core-ui/runtime/ -run '^(TestCoreBudgetAtPreLayout|TestPlainSiteLineMatchesCoreBudget|TestCoreBudgetRejectsCliffOverflow|TestRuntimeModuleSizeBudgets|TestComposedRuntimeMatchesOnDiskFile|TestComposedRuntimeIsSymbolComplete|TestKernelMarkupMatchesGo)$'`
- `go test ./framework/uihost/ -run TestPlainSiteShipsCoreOnly`
- `go test ./core-ui/check/`: `TestLintRegistryOwnProps_RepoIsClean`
  caught `paintGlobals` reading `_signals[name]` through the prototype
  chain; it now reads through `own()`.
- `go test ./framework/docs/`
- `go test -timeout 20m ./core-ui/runtime/...`: all four packages pass;
  `core-ui/runtime` takes 730 s, so a 10-minute timeout is too short for it.
- `go test ./core-ui/runtime/hosttest/ -run 'TestRuntimeSayCallsMatchPhrases|TestStringsJSONCarriesOnlyTranslations'`
- `gofmt -l` and `go vet` on `core-ui/runtime` and `framework/uihost`: clean.

## Limitations

- A hand-built page with a seed and no `#gofastr-signals-page` marks every
  key global. Under the uihost contract a missing list means no
  page-scoped key, so this is right for generated pages. On a hand-built
  page that mixes scopes, a swap paints the stored value of a page key
  that has no `p` entry. The store already held that value.
- `sameValue` reads `{a: undefined}` and `{}` as equal (JSON drops the
  key). A response cannot carry `undefined`, so only a client-set value
  can hit this. Key order differences read as changed, which costs a
  render, never a missed update.
- The mount-time widget hydration in `widgets.js` is untouched; another
  worker owns that file.
- The coordinator corrected the stale `Placeholders` and test comments in
  `core/i18n/check.go` and `check_test.go`: headless fills still match exact
  spellings, while `Fill` and `say()` trim names. Catalog behavior did not
  change, and the full i18n package tree passed afterward.

## Coordinator validation

After the implementation finished and no mutation proof was modifying the
shared checkout, the coordinator independently passed all six runtime
regressions, both global-binding arms, the inherited-widget-name regression,
core budget derivation/mirror/cliff/module/embed gates, full `core-ui/check`,
treegate and real-browser `TestPlainSiteShipsCoreOnly`. Repository vettool
passed the runtime package tree, uihost and i18n. The focused runtime package
run took 6.957s; this does not substitute for the worker's separately
reported 730s full runtime run.

The earlier coordinator globals-only failure coincided with the worker's
in-place mutation proof according to the worker. Its restored final state
now passes independently. Next time isolate negative mutation proofs from
concurrently validated shared files; preserve complete timeout output and
allow more than ten minutes for this browser-heavy suite.
