# Generative UI

GoFastr's position on AI-produced UI comes down to one rule: **models compose the design system; they never emit raw HTML or CSS.**

What "generative UI" means depends on the surface:

## 1. Agents building your app's UI (supported)

When Claude Code, Codex, or another agent builds screens in your GoFastr repo, it composes typed `framework/ui` components:
- **Enumerable catalog:** `framework/gallery` exposes self-contained component demos and snippets programmatically (`gallery.IsNoteOnly` marks note-only entries).
- **Recipes & `DESIGN.md`:** [UI composition recipes](ui-composition-recipes.md) map common layouts to component hierarchies, and `gofastr init` generates an app-owned `DESIGN.md`.
- **Automated enforcement:** [`gofastr verify`](contracts.md) catches bespoke CSS, inline styles, and hand-rolled navigation as failing diagnostics instead of review nits.

When the catalog genuinely lacks a primitive, add a component or token upstream rather than dropping raw CSS into one screen.

## 2. Live build mode: Kiln (experimental)

[Kiln](kiln.md) applies the same rule interactively during development: agents mutate a journaled world IR of entities and typed component nodes over HTTP/MCP, previewing through normal SSR, then freeze into `gofastr.yml` and owned Go.

## 3. Chat-host widgets: MCP Apps

`framework.WithMCPApp` publishes a `ui://` HTML widget and its launching tool on `/mcp` for chat hosts (Claude, ChatGPT):
- **The chat host owns the styling:** follow the host's `HostContext` theme signals rather than shipping your app's brand CSS (the one intentional exception to the app styling surface).
- **Behavior still flows through `/mcp`:** every interactive call re-enters your server through MCP tools so auth, owner scoping, and rate limits apply unchanged.

See [Agent host](agent-host.md) for the complete widget contract.

## 4. Per-request runtime generative UI (deliberately not a framework feature)

GoFastr does not call an LLM inside HTTP requests to synthesize ad-hoc views for end users — rendering is deterministic and SSR-first, and the framework takes no LLM SDK dependency.

If your product needs end-user AI-generated layouts, build it as a plugin inside the [plugin platform's](plugin-platform.md) sandboxed iframe: validate the model's output as a schema-checked tree over a statically compiled component registry, allow only explicitly exposed host actions, persist the generated view, and poll for completion with `data-cui-poll` (see [Reactivity](reactivity.md)).

## Common mistakes

- **Letting a model emit raw HTML/CSS "just once":** bypasses escaping, accessibility, dark mode, and contracts in one shot.
- **Shipping your app's brand palette inside a chat-host MCP App:** clashes with dark-mode hosts; consume `HostContext` variables instead.
- **Creating side-channel widget APIs:** route widget interactions through `/mcp` tools so auth and owner scoping stay enforced.
- **Streaming LLM tokens straight into a live SSR page DOM:** persist async generation results and update via `data-cui-poll`.
- **Judging generated UI from DOM text dumps alone:** layout bugs are visual — inspect real screenshots.
