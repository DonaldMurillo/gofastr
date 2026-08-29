# Generative UI

Where GoFastr stands on UI that AI produces. The short version: **models
compose the design system; they never emit markup or CSS.** Every
generative surface the framework supports applies that one rule. What
varies between surfaces is who renders the composition and whose design
language wins.

"Generative UI" means at least four different things in practice, and
they get different answers here.

## 1. Agents building your app's UI — the supported path

An agent writing a GoFastr app composes `framework/ui` and
`core-ui/patterns` components. It does not write CSS and does not
hand-roll structural markup. The framework backs that constraint with
tooling rather than advice:

- **A catalog it can enumerate.** `framework/gallery` ships the
  component catalog as self-contained demos renderable against any
  theme, with code snippets per entry (a handful of entries needing
  backend wiring are note-only — `gallery.IsNoteOnly`). Agents (and the theme tools) iterate it
  programmatically.
- **Recipes instead of blank pages.**
  [UI composition recipes](ui-composition-recipes.md) map product jobs
  (command center, master/detail, marketing page) to component
  hierarchies; `gofastr init` creates an app-owned `DESIGN.md` and
  points the generated agent guidance at both.
- **Enforcement.** [`gofastr verify`](contracts.md) reports bespoke
  CSS, inline styles, and hand-built navigation as diagnostics with
  IDs and fixes. Drift is a failing check, not a review comment.

This is a deliberate trade: the model can only build what the catalog
expresses. When a design genuinely needs something the catalog lacks,
the answer is a new component or token upstream in the design system,
never a one-off style. Bounded composition is what keeps generated UI
reviewable, consistent, accessible, and theme-correct without anyone
inspecting generated CSS, because there is none.

## 2. Build mode — Kiln

[Kiln](kiln.md) (experimental) is the same rule at runtime during
development: agents mutate a typed world model over HTTP and pages
render through the normal SSR pipeline. The agent edits declarations,
not markup. Freezing produces a blueprint and then owned Go code.

## 3. Widgets inside chat hosts — MCP Apps

`framework.WithMCPApp` registers an MCP App on the app's `/mcp` server:
a `ui://` HTML resource plus the tool that launches it, per the MCP
Apps extension. A spec-compliant host (Claude, ChatGPT) renders the
widget in a sandboxed iframe inside the conversation.

Two positions govern this surface, and the first one is an exception
worth reading twice:

- **The chat host owns the design language.** A widget renders inside
  someone's conversation, not inside your app. Follow the host's theme
  signals and conventions; do not ship your app's brand, tokens, or
  design-system CSS into the widget. The one-styling-surface rule
  governs *app* surfaces. A widget is the host's surface, so it is the
  one place where the framework's styling doctrine deliberately does
  not reach.
- **Behavior still flows through the app.** Everything interactive in a
  widget is an MCP tool call that re-enters the app through `/mcp`, so
  auth, owner scoping, and rate limits apply unchanged. A widget gets
  no side-channel API and holds no server-side state of its own.

## 4. Runtime generative UI for end users — deliberately not a feature

The framework does not call a model during a request to produce a
bespoke view for a user, and will not grow that feature. The reasons
are structural, not fashion:

- Serving is SSR-first with an instant-swap interaction model; model
  calls take seconds and would put that budget inside the request path.
- Rendering is deterministic by design (deterministic SSR bytes are an
  enforced invariant); per-request generation is not.
- The framework ships no LLM client and takes no provider dependency.

The compatible way to build it is a **plugin** behind the
[plugin platform's](plugin-platform.md) iframe boundary: the plugin
brings its own renderer and a *statically compiled* component registry,
the model composes registry entries (ids, props, layout) as a
schema-validated tree, generated views may wire only actions the host
app explicitly exposed, and generations persist as data with a
placeholder-then-poll UX (`data-fui-poll`, per the
[reactivity ladder](reactivity.md)). The composition rule holds even
in exile: a bounded registry the model arranges, never markup it
invents.

## Common mistakes

- **Letting the model emit raw HTML or CSS "just this once."** The
  escape hatch is a new component or token upstream, same as for human
  authors. One pass-through of generated markup forfeits escaping,
  accessibility, and theming in a single move.
- **Styling a chat-host widget with the app's brand.** Inside the
  conversation the host's design language wins; a widget that ignores
  the host's theme signals reads as a foreign object, and dark-mode
  hosts will render your light-mode assumptions unreadable.
- **Giving a widget its own API.** If a widget needs data or actions,
  it needs an MCP tool, gated like any other. A bespoke endpoint for
  "just the widget" bypasses the gating story that makes widgets safe.
- **Streaming model output into the DOM.** Generation is async:
  placeholder first, poll for the persisted result. Token-streaming
  into a live page trades a working interaction model for a demo
  effect.
- **Claiming generated UI works from a DOM dump.** Composition bugs
  are visual. Screenshot the rendered result; it is the only probe
  that sees layout.
