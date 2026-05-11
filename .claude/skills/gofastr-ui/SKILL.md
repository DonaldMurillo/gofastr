---
name: gofastr-ui
description: Auto-loads when working on UI, runtime, or framework/uihost code in the GoFastr repo. Encodes the SSR-with-hydration architecture (no hard refresh, page-nav swaps content, in-page state is island RPC) and the three failure modes that have already happened. Triggers on edits to core-ui/, framework/ui/, framework/uihost/, examples/website/, or runtime.js — and on phrases like "pagination", "sort header", "tab click", "navigation", "SPA", "hydration".
---

# GoFastr UI architecture — load this before writing UI code

This skill auto-loads whenever you touch the UI surface. The model has been
misread three times in this repo. The canonical document is
`core-ui/ARCHITECTURE.md` — **read it now if you have not already.**

## The model (one paragraph)

SSR-first. Server fully renders every page on first request. `runtime.js`
hydrates the existing DOM (no re-render). Cross-page navigation
(`/a` → `/b`) is client-side via partial fetch + cache, no hard refresh.
In-page state changes (pagination, sort, filter, expand) are **islands**:
a click fires an RPC, the server returns new island HTML, the runtime
swaps just that island's content. Server-pushed updates flow through
signals + SSE for background events only — not user actions.

## The three failure modes — refuse to do these

### ❌ Treating in-page state as a route
**Symptom**: pagination/sort renders as `<a href="?p=2">` and triggers
either a full reload or an SPA route change to "the same page with
different query params".
**Correct**: pagination is an island. The buttons are `data-fui-rpc=...`
NOT `<a href>`. The server has an RPC handler that returns the new
rendered rows.

### ❌ Hard refresh on every interaction
**Symptom**: clicking a sort header reloads the whole page.
**Correct**: island RPC. The runtime swaps the island, the rest of the
page stays.

### ❌ Client-only after hydration
**Symptom**: pagination math lives in JS; the server doesn't know about
page 2.
**Correct**: server is the source of truth. Always. JS shipped to the
browser is the generic runtime (`runtime.js`) — never feature-specific
code.

## What you compose

| Need | Use |
|---|---|
| Static HTML primitive (`<button>`, `<h1>`, `<table>`) | `core-ui/html` |
| A composed UI pattern (accordion, tabs, pagination, breadcrumbs) | `core-ui/patterns/<name>` |
| A semantic component (PageHeader, FormField, DataTable) | `framework/ui/` |
| An island (server-rendered, server-state-owning, RPC-updatable) | `core-ui/widget` (builder API: `New(name).Slot(...).RPCWithSignal(...)`) |
| A theme token | `framework/ui/theme` |
| A reactive value pushed by the server | `core-ui/signal` + an SSE binding on the widget |

## Runtime data-attributes (do not invent new ones without updating the doc)

| Attribute | Effect |
|---|---|
| `data-fui-rpc="<path>"` | Click/submit fires HTTP request |
| `data-fui-rpc-signal="<name>"` | Response body becomes the value of signal `<name>` |
| `data-fui-signal="<name>"` mode=`text\|html\|attr` | Element auto-updates when the signal changes |
| `data-fui-open="<widget>"` | Opens a mounted widget |

The runtime + the data-attributes ARE the API surface for hydration.
Adding new attributes requires updating `core-ui/ARCHITECTURE.md` and
the runtime test suite — every attribute is a public contract.

## URL as source of truth

State worth bookmarking / refreshing / back-buttoning lives in the URL.
The flow:

1. Initial load: `Screen.Load(ctx)` reads `?p=2` etc. and SSR's page 2.
2. Click in-island button: RPC returns new HTML AND
   `X-Gofastr-Push-State: ?p=3` header → runtime applies pushState (no fetch).
3. Browser back: popstate → runtime fetches screen partial for the
   new URL → swap → cached for instant forward.
4. Refresh: same URL → SSR renders the right state.

## Before you ship UI code, check

- [ ] Initial render is full SSR — refresh on the URL produces the same DOM.
- [ ] No `<a href="?…">` for state changes; that's an island RPC.
- [ ] In-page state worth sharing/bookmarking is in the URL via the
      `X-Gofastr-Push-State` response header.
- [ ] No `location.href = …` in JS or in the server response.
- [ ] No JS feature code; only the generic runtime.
- [ ] Cross-page links are normal `<a>` — the runtime intercepts them
      transparently for the partial-fetch + cache flow.
- [ ] `Screen.Load(ctx)` populates state from route params + query so
      deep-links / refresh / SSG all work.

## Stop and re-read `core-ui/ARCHITECTURE.md` if

- You're about to write `data-fui-spa` or any "opt into SPA mode" attribute.
- You're about to make pagination an `<a href>`.
- You're about to add a runtime endpoint that's not part of the
  SSR / page-nav / island-RPC / SSE-push grid.
- You're about to write client-side feature logic (sorting, filtering,
  paginating, validating) in JS.

If any of those tempt you, the model is being violated. Stop, ask the
user to confirm a deviation, and document the new pattern in
`core-ui/ARCHITECTURE.md` before shipping.
