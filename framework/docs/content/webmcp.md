# WebMCP (browser-native agent tools, experimental)

> **Experimental:** WebMCP tracks Chrome's WebMCP origin trial (`navigator.modelContext`). APIs and browser flags may change as the W3C proposal evolves.

WebMCP lets browser-based AI agents invoke structured tools exposed directly by a live web page — using the visitor's already-authenticated browser session, CSRF protection, and same-origin UI state — instead of scraping DOM pixels or clicking brittle CSS selectors.

## Wiring WebMCP (`framework/experimental/webmcp`)

Create a `webmcp.Host`, register tools with explicit HTTP middleware on `Handle`, and mount it onto your router and `UIHost`:

```go
wm := webmcp.New()
wm.Handle(fwApp.Router(), webmcp.Tool{
	Name:              "archive_ticket",
	Description:       "Archive a support ticket",
	Method:            "POST",
	Path:              "/api/tickets/archive",
	ConsequentialHint: true,
}, requireSupportMiddleware(archiveTicketHandler))

wm.Mount(fwApp.Router(), host) // host (*uihost.UIHost) registers the bridge script on every full-page render
```

- `Host.Handle` registers the tool metadata and HTTP route, but it does **not** install auth, CSRF, or RBAC middleware for you—wrap handlers explicitly (or mount them behind your app's auth/RBAC middleware).
- `Host.Mount(router, host)` serves the manifest/bridge and registers the bridge script on every full-page render through `UIHost` (doing nothing on browsers without `navigator.modelContext`).

## Security model

Every WebMCP call runs inside the user's browser session and re-enters the GoFastr server through the registered HTTP route:
- **Enforce auth & RBAC on the server:** `Host.Handle` does not add authorization for you—wrap every tool route with session auth, CSRF, owner/tenant scoping, and RBAC checks.
- **Hints are advisory only:** `ConsequentialHint`, `ReadOnlyHint`, and `UntrustedContentHint` are forwarded to the browser/agent as advisory annotations; they are **not** server-enforced confirmation gates. Always enforce permissions and safety checks server-side regardless of hints.
- **Explicit allowlist:** only tools explicitly declared on the `Host` are exposed to `navigator.modelContext`.
- **Never trust page-side tool arguments:** validate types, bounds, and permissions on the server just like any HTTP form or API request.

## WebMCP vs. server `/mcp`

- **Use server `/mcp`** ([MCP](mcp.md)) for CLI/IDE coding agents, server-to-server automation, and headless clients calling your app with bearer tokens or stdio.
- **Use WebMCP** when an in-browser assistant is acting on behalf of a logged-in user already viewing a rendered page.

## Common mistakes

- **Assuming WebMCP works in every browser today:** it is progressive enhancement behind a feature check (`navigator.modelContext`). Always keep your normal SSR forms and links working without it.
- **Skipping server-side permission checks on `Host.Handle`:** browser agents can be prompt-injected by untrusted page content, and `Host.Handle` installs no auth or RBAC automatically.
- **Treating `ConsequentialHint: true` as an enforced server gate:** hints are advisory to the browser/agent; enforce destructive permissions and server-side checks unconditionally.
- **Treating WebMCP as a replacement for `/mcp`:** external agents (Claude Code, Codex, CI bots) talk to `/mcp`, not a browser tab.
