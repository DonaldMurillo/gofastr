# WebMCP (browser-native agent tools, experimental)

> **Experimental:** WebMCP tracks Chrome's WebMCP origin trial (`navigator.modelContext`). APIs and browser flags may change as the W3C proposal evolves.

WebMCP lets browser-based AI agents invoke structured tools exposed directly by a live web page — using the visitor's already-authenticated browser session, CSRF protection, and same-origin UI state — instead of scraping DOM pixels or clicking brittle CSS selectors.

## How it works in GoFastr

When enabled on a screen or app:
- GoFastr emits declarative tool descriptors alongside the SSR HTML (matching your entities, forms, or custom screen actions).
- An on-demand runtime module registers those tools with `navigator.modelContext` when the browser supports it (and does nothing on browsers without WebMCP, costing zero bytes on pages that don't opt in).
- When a browser agent calls a tool, execution dispatches through your normal GoFastr endpoints and island RPCs — never bypassing server validation.

## Security model

Every WebMCP call runs inside the user's browser session and re-enters the GoFastr server through the normal HTTP front door:
- **Full server enforcement:** auth, CSRF, owner scoping, tenant scoping, and rate limits all apply identically to a human clicking a button.
- **Explicit allowlist:** only tools explicitly declared on the current page are exposed to `navigator.modelContext`.
- **Destructive hints & confirmation:** mark mutating or destructive tools clearly so the browser agent prompts the user before invoking them.
- **Never trust page-side tool arguments:** validate types, bounds, and permissions on the server just like any HTTP form or API request.

## WebMCP vs. server `/mcp`

- **Use server `/mcp`** ([MCP](mcp.md)) for CLI/IDE coding agents, server-to-server automation, and headless clients calling your app with bearer tokens or stdio.
- **Use WebMCP** when an in-browser assistant is acting on behalf of a logged-in user already viewing a rendered page.

## Common mistakes

- **Assuming WebMCP works in every browser today:** it is progressive enhancement behind a feature check (`navigator.modelContext`). Always keep your normal SSR forms and links working without it.
- **Skipping server-side permission checks on WebMCP-backed routes:** browser agents can be prompt-injected by untrusted page content. Enforce auth, owner scoping, and RBAC on the server.
- **Exposing admin or bulk-delete tools without destructive annotations:** always flag irreversible actions so browser hosts require user confirmation.
- **Treating WebMCP as a replacement for `/mcp`:** external agents (Claude Code, Codex, CI bots) talk to `/mcp`, not a browser tab.
