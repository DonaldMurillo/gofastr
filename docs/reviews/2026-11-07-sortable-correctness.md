# Versioned sortable correctness review — November 7, 2026

Revision: `953ccc08256e825bdc17254c463a5695a27092b6`.
One independently reproduced conditional P3 root. No production changes.

## C20 — Versioned sortable lists retain stale token after successful move

The supported optimistic-version recipe cannot save two consecutive moves
without a full list rerender. The first POST succeeds and bumps the server
version, but later POSTs retain the initial token. A conflict refresh updates
the rows without renewing that token, so further moves keep conflicting.
No server data corruption or concurrent-request race is claimed.

### Contract and root

- `framework/docs/content/optimistic-ui.md:521` specifies a handler that
  compares the submitted version, applies a matching move, bumps the version
  and returns 2xx. Its conflict GET returns fresh `<li>` rows.
- `framework/headless/sortablelist.go:59` promises an RPC after each reorder.
  `framework/headless/sortablelist.js:77` reads the version from the root
  `data-hui-sortable-version` attribute on every POST.
- The success branch at `sortablelist.js:184` records success, invalidates
  navigation caches and announces completion; it does not renew the version.
- The conflict reconciliation at `sortablelist.js:215` replaces `innerHTML`,
  leaving the root attribute untouched. `SortableItems` explicitly emits
  rows without the root wrapper (`sortablelist.go:98`).
- `core-ui/runtime/frag/nav.js:1268` confirms `_inval` only reads the
  invalidation header and invalidates navigation caches; it does not refresh
  this component or its version token.
- The flagship follows this same recipe: `examples/site/main.go:520`
  compares the version, line 574 increments it and returns 204, and line 600
  passes the new version to the rows-only conflict renderer. The flagship
  endpoint itself was source-checked, not driven by this reproduction.

These are one missing version-renewal mechanism, not separate success and
conflict findings. The unrelated delayed-refresh race remains uncounted.

## Fresh coordinator reproduction

The coordinator authored a standalone Go HTTP server using the public
`headless.SortableList` and `SortableItems` renderers, a compare-and-bump
handler and a real `handler.WriteError` 409 envelope. Its initial order is
`a,b,c` and version is `v1`.

A separate Node VM executes the entire unmodified production
`sortablelist.js`. It parses root attributes and row keys from actual Go
HTTP responses, then dispatches delegated Space/ArrowDown/Space keyboard
events for three sequential moves, awaiting network completion between them.
POSTs and conflict GETs use real localhost HTTP, not scripted statuses.

| Scenario | POST statuses | Submitted versions | Final server order | Server version |
| --- | --- | --- | --- | --- |
| Versioned, same root | 204, 409, 409 | v1, v1, v1 | b,a,c | 2 |
| Unversioned control | 204, 204, 204 | omitted each time | c,b,a | 4 |
| Fresh versioned root before each move | 204, 204, 204 | v1, v2, v3 | c,b,a | 4 |

All observation/control assertions pass, pinning the existing defect rather
than claiming a fixed regression test passes. The final DOM order matches
server state in all three scenarios. In the failed scenario the original
root still holds `v1` even after both conflict-refresh GETs.

### Retained executables and commands

Sources, binary and output:
`/tmp/gofastr-coordinator-sortable-v2/{server.go,observe.cjs,server,observation.log}`.
The Node program starts and terminates its own test server.

```sh
GOCACHE=/tmp/scope39-probe/gocache GOPROXY=off go build -p 2 \
  -o /tmp/gofastr-coordinator-sortable-v2/server \
  /tmp/gofastr-coordinator-sortable-v2/server.go
node /tmp/gofastr-coordinator-sortable-v2/observe.cjs "$PWD"
go test -p 2 -count=1 \
  -run '^Test(SortableList(RendersTheRowsAndContract|AnnouncementsComeFromStrings|EmptyColumnStaysADropTarget|OmitsTheWiringItWasNotGiven|ContentReplacesTheLabelBody|ScrubsCarriedLabels|KeysAreData|RefusesBrokenConfiguration)|SortableItemsRendersTheRowsWithoutTheWrapper)$' \
  ./framework/headless/
```

The exact browser-free Go selection contains nine named tests and passes.
No production source or public behavior was modified.

### Adapter limits

This is a single-list, synthetic delegated-keyboard DOM model with real
HTTP and production Go markup/JavaScript. It does not measure a browser's
layout, native focus engine, pointer dragging, physical keyboard, toast
presentation or assistive technology. Origin validation and navigation
cache invalidation are seam stubs; their production behavior was
source-checked and neither renews the version token. No business-rule
rejections, request concurrency or malformed input are needed for C20.
