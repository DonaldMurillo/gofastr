# Agent host

The agent-host surface is what a chat host (Claude, ChatGPT) touches when
it renders your app's widget: the `/mcp` endpoint it talks to, the
`ui://` resources it reads, and the widget client script the widget
fetches. This page is the contract for that surface — the decisions that
settle how far the framework reaches into it, what a widget author
writes, and where the boundary deliberately sits.

The word "widget" here means a chat-host widget (an MCP App). The
[widget builder](widgets.md) pages describe island widgets inside your
own app's screens — a different surface with its own rules.

## The three decisions

Everything on this surface follows from three settled decisions:

1. **A role of the same binary, never a second codebase.** The agent
   host mounts in-process (as `/mcp` does today) or runs standalone via
   the existing role machinery. Entities, tools, the agent card, and
   widgets come from the same declarations.
2. **No styling doctrine.** The chat host owns the design language.
   There is no design-system-to-embed compiler, and the
   one-styling-surface rule is unchanged for the web host — it simply
   does not claim this surface.
3. **Behavior doctrine does apply.** Every widget behavior goes through
   MCP tool calls re-entering the app. No side-channel APIs, no
   server-side widget state. Auth, owner scoping, and rate limits apply
   because the calls come through the front door.

## What `RoleAgent` serves

`GOFASTR_ROLE=agent` (or `framework.WithRole(framework.RoleAgent)`)
serves the agent surface only: `/mcp` (POST JSON-RPC + GET SSE, plus its
spec-reserved subpaths), `/healthz` and `/readyz`, and the widget client
script at `mcp.WidgetClientScriptURL`. Entity CRUD routes, OpenAPI,
docs, admin, and well-known discovery are not served; worker consumers
(cron, queues, the outbox relay) do not start.

`/mcp` requests forward to the app router, so the middleware chain —
session/bearer auth, owner scoping context, recovery — behaves exactly
as on a full serve process. A role-split deployment gives agent traffic
a dedicated, narrow listener (a tunnel, an allow-listed load balancer)
without exposing the browser surface on it. The widget client script
rides the same listener because a widget document fetches it from the
same public origin that serves `/mcp`.

See [horizontal scaling](scaling.md) for the full role split and
[MCP](mcp.md) for the `/mcp` transport itself.

## Authoring a widget

A widget is a single HTML document served as a `ui://` resource, plus
the tool that launches it. `mcp.WidgetDocument` assembles the document
so the boilerplate is right by construction — and above all so the
`<script src>` that loads the widget client comes from the same constant
the server mounts the route at. A one-character typo in a hand-written
script URL is a widget that renders and silently never receives
anything: no error, no console message, nothing to grep for.
<!-- gofastr:compile
import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/mcp"
)

var studioTool = func(context.Context, map[string]any) (any, error) { return "ok", nil }
-->
```go
import (
	"log"

	"github.com/DonaldMurillo/gofastr/framework"
)

doc := mcp.WidgetDocument{
	Title:  "Studio",                    // <title>, text-escaped
	Body:   `<p id="status">idle</p>`,  // your markup, verbatim
	Script: `var app = window.__gofastrMcpApp; app.connect({});`,
}
html, err := doc.HTML() // the full document, or a validation error
if err != nil {
	log.Fatal(err)
}
opt := framework.WithMCPApp(mcp.AppConfig{
	Name:        "studio",
	Description: "Open the studio widget.",
	InputSchema: map[string]any{"type": "object"},
	Handler:     studioTool,
	ResourceURI: "ui://myapp/studio.html",
	HTML:        html,
	CSP:         mcp.AppCSP{ConnectDomains: []string{"https://api.example.com"}},
})
_ = opt
```

The emitted document is exactly: doctype, `<html lang>`, charset and
viewport, `<title>`, one root `<div id="app">` wrapping `Body`, the
widget client `<script src="/__gofastr/mcp/app/widgetclient.js">`, and
`Script` in a classic inline `<script>` after it — so
`window.__gofastrMcpApp` exists when your code runs. No CSS, no design
tokens, no default styling of any kind: the builder emits structure
only, because the palette belongs to the host (decision 2).

Field trust matches `AppConfig.HTML` itself. `Title`, `Lang`, and
`RootID` are data and are escaped for their contexts by `html/template`
(HTML text, attribute value). `Body` and `Script` are author-authored
document content and pass through verbatim, exactly as a hand-written
HTML string would. One guard `HTML()` enforces: `Script` must not
contain `</script` (case-insensitive) or `<!--` — either can make the
HTML parser close (or stop closing) the script element early. The builder
rejects the document with an error naming the sequence. Inside the
JavaScript string literals in `Script`, write `"<\/script>"`: `\/` is a
valid JavaScript escape for `/`. Keep `Script` in a Go raw string
literal (backticks) so the backslash reaches JavaScript as written. In
an interpreted string literal, double it (`"<\\/script>"`); a lone `\/`
is not a legal Go escape, so the code will not compile.

`framework.WithMCPApp` registers the resource and the linking tool in
one call and serves the widget client script at
`mcp.WidgetClientScriptURL` automatically. The client's full method
surface — the `ui/initialize` handshake, `callTool`, the notification
registrars, teardown — is in [MCP](mcp.md).

## Host theme, not your palette

Widgets consume host-provided theme signals and do not invent palettes.
The MCP Apps spec delivers the signals in `HostContext`:

| Signal | Spec field | What it is |
|---|---|---|
| Color theme | `hostContext.theme` | `"light"` or `"dark"` |
| CSS variables | `hostContext.styles.variables` | Custom properties with standardized names (`--color-text-primary`, `--font-sans`, …); hosts should write `light-dark()` values |
| Font CSS | `hostContext.styles.css.fonts` | `@font-face` / `@import` CSS for the view to inject |

The widget client applies them for you. On `connect()` and on every
`ui/notifications/host-context-changed` (partial updates are merged into
the running state, as the spec requires), it writes to the document
root:

- `theme` → `<html data-theme="dark">` plus `color-scheme: dark` — the
  pair that makes the host's `light-dark()` variable values resolve on
  the correct side;
- `styles.variables` → inline custom properties on the root, inherited
  by all author markup; a property the update no longer lists is
  removed;
- `styles.css.fonts` → one `<style data-mcpapp-fonts>` element, replaced
  when the host sends new font CSS, removed when an update sends none.

Application happens before your registered `onHostContextChanged` handler
runs (and even when you register none), so the root is never stale when
author code reads it. `app.hostContext()` returns a copy of the merged
state — theme, locale, display mode, and the rest — for code that wants
more than styling.

Author CSS then references the signals. Declare fallbacks for hosts that
omit `styles` (the spec tells views to), and let the host's values win:

```css
:root {
  /* fallbacks: used when the host sends no variables */
  --color-background-primary: light-dark(#ffffff, #171717);
  --color-text-primary: light-dark(#171717, #fafafa);
}
.panel {
  background: var(--color-background-primary);
  color: var(--color-text-primary);
}
```

The widget ships no palette of its own: not your app's brand, not your
theme tokens, not the design system's CSS. Inside someone's conversation,
the host's design language wins.

## What GoFastr deliberately does not do

- **It is not a chat host.** GoFastr serves the widget side of MCP Apps:
  the `ui://` resource, the linking tool, and the script the widget runs.
  Nothing in the framework renders another server's widget or speaks the
  host half of the widget protocol.
- **It does not compile your design system into widgets.** There is no
  design-system-to-embed compiler. The host owns the design language;
  the builder emits structure and the client applies the host's signals.
- **It does not generate UI at runtime.** Runtime generative UI for end
  users is plugin territory, not a framework feature; see
  [generative UI](generative-ui.md).

## Common mistakes

- **Hand-writing the client `<script src>`.** The URL must be exactly
  `mcp.WidgetClientScriptURL` (`/__gofastr/mcp/app/widgetclient.js`). A
  one-character typo renders a widget that silently never receives
  anything. Use `mcp.WidgetDocument`, which bakes in the constant.
- **Shipping your app's brand into the widget.** App tokens, theme CSS,
  design-system styles: none of it belongs in a chat-host widget. The
  host's design language wins, and a dark-mode host will render light
  assumptions unreadable.
- **Omitting fallback variable values.** Hosts may pass no `styles` at
  all; the spec tells views to declare their own fallbacks for every
  variable they use.
- **Fighting `data-theme` / `color-scheme`.** The client writes both on
  the root so `light-dark()` resolves. Key your own adjustments off
  `:root[data-theme="…"]` instead of resetting them.
- **Giving the widget a side-channel API.** Data and actions flow through
  MCP tool calls re-entering the app through `/mcp`, where auth, owner
  scoping, and rate limits apply. A bespoke "widget-only" endpoint
  bypasses the gating story that makes widgets safe.
- **Building widget state on the server.** A widget holds no server-side
  state of its own; each `tools/call` carries what it needs, like any
  stateless replica-serving surface.
