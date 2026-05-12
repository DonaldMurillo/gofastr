# Plan: typed theme + variant system

Captures the design we worked out in conversation. Implementation
follows this contract. Lives in the worktree so it ships with the PR.

## Context

Current state — after the per-component CSS landing:
- `style.Theme` is map-backed (`Colors map[string]string`, `Spacing
  map[string]int`, …). String-keyed, no type safety, no autocomplete.
- Components reference theme either via `{tokens.text}` (build-time
  resolved to literals) or `var(--color-text, #fallback)` (runtime
  CSS variable, but with duplicated fallback). Inconsistent.
- `theme.css` and `styles.css` ship as separate `<link>` requests.
- Variant patterns are inconsistent: `DangerButton` is its own
  function, `StatusBadge`/`Callout` take `Variant: StatusVariant`.

Why change:
- Single source of truth for tokens (one place to declare a value).
- Compile-time catches for typos / renames.
- Section-level theme overrides via CSS cascade — requires every
  component CSS reference to be `var(--*)`, never a build-time
  literal.
- A predictable design system with typed variants and theme-driven
  customization (option B from the design conversation).

---

## Decisions locked

1. **Token values are typed structs.** Each carries Name + Value:
   ```go
   type Color   struct { Name, Value string }    // name = "primary", value = "#4F46E5"
   type Spacing struct { Name string; Value int } // value in pixels
   type Radius  struct { Name string; Value int }
   type Shadow  struct { Name, Value string }    // value is full CSS shadow expression
   type Font    struct { Name, Value string }
   type Z       struct { Name string; Value int } // z-index
   type Dur     struct { Name string; Value time.Duration }
   type FontSize struct { Name, Value string }   // typography scale ("base" → "1rem")
   type Breakpoint struct { Name string; Value int }
   ```
   Each has a `.CSS() string` method that returns
   `var(--<category>-<name>)` — *always* a CSS variable reference,
   never the literal value. Components use `.CSS()` exclusively.
   `.Value` is exposed for non-CSS contexts (charts, JSON, image
   generation).

2. **`style.Theme` becomes a canonical struct.** Fixed field set —
   the framework knows exactly which fields exist at compile time:
   ```go
   type Theme struct {
       Colors      Colors
       Spacing     Spacing
       Radii       Radii
       Fonts       Fonts
       Breakpoints Breakpoints
       Shadows     Shadows
       ZIndex      ZIndex
       Durations   Durations
       Typography  Typography
   }
   type Colors struct {
       Primary, Secondary, Text, TextMuted, Background, Surface,
       SurfaceSoft, Border, BorderStrong, Danger, Success, Warning,
       Info Color
   }
   // … each category's struct definition
   ```
   Token vocabulary expanded vs today: + Shadows, + ZIndex (named
   layers: dropdown/sticky/modal/popover/toast), + Durations
   (fast/normal/slow), + Typography (xs/sm/base/lg/xl/2xl/3xl).

3. **Required everything for primitives.** Apps must declare every
   field. Scaffold ships full defaults as the starting point so this
   isn't punishing.

4. **App-specific token extensions via embedding:**
   ```go
   type AppTheme struct {
       style.Theme            // canonical
       Brand struct { Logo, Glow style.Color }
   }
   var App = AppTheme{Theme: style.Theme{...}, Brand: ...}
   ```
   Framework code only sees the embedded `style.Theme`. App-local
   components can reference `theme.App.Brand.Logo` directly.

5. **Components always emit `var(--*)`.** Build-time `{token}`
   resolution is removed from `ComponentSheet.Set` — passing a
   typed token resolves to `var(--…)`. The string-token path is
   retired (deprecation period, then removed).

6. **`theme.css` + `styles.css` merged into one asset.** One fewer
   HTTP request. theme.css is generated from `Theme` (via reflection
   over canonical fields + any app-embedded extensions), styles.css
   is appended. Served at `/__gofastr/app.css` (new), with the old
   two endpoints kept as 410 GONE.

7. **`<style>` blocks remain forbidden** — CSP stance unchanged.
   theme + styles are external because inline `<style>` is blocked
   by `default-src 'self'`. (We considered loosening style-src to
   `'unsafe-inline'`; decided against — keeps the strict-CSP posture
   consistent.)

8. **Section-level theme overrides via class cascade.** Framework
   exposes:
   ```go
   ui.Themed(themeOverride, ...children) render.HTML
   ```
   which wraps children in a `<div class="fui-theme-<id>">`. The
   framework also emits a `.fui-theme-<id> { --color-…: …; }` block
   in app.css that re-declares the overridden variables. Children
   pick up the cascade via `var()`.

9. **Component variant mapping owned by theme (option B).** Each
   variant-bearing component has a corresponding optional section
   on `Theme`:
   ```go
   type Theme struct {
       // ... primitives ...
       Buttons       ButtonsTheme       // optional override
       Badges        BadgesTheme        // optional override
       // ... etc.
   }
   type ButtonsTheme struct {
       Primary, Secondary, Danger, Ghost ButtonVariantStyle
   }
   type ButtonVariantStyle struct {
       Background, Foreground, Border style.Color
       HoverBackground style.Color
   }
   ```
   `framework/ui` components ship internal defaults for each
   variant (mapped from theme primitives). Apps override
   selectively. If `theme.Buttons.Primary == zero`, the component
   uses its built-in default. Only primitives are required.

10. **No per-variant tree-shaking.** Each component's CSS file
    includes all variants. Per-component tree-shaking (already
    implemented) is the only level. Dead-CSS weight from unused
    variants is small (<300 bytes per component) and only on first
    paint.

11. **String-typed variant enums.** Consistent across all
    components:
    ```go
    type ButtonVariant string
    const (
        ButtonPrimary   ButtonVariant = "primary"
        ButtonSecondary ButtonVariant = "secondary"
        ButtonDanger    ButtonVariant = "danger"
        ButtonGhost     ButtonVariant = "ghost"
    )
    ui.Button(ui.ButtonConfig{Label: "Save", Variant: ui.ButtonPrimary})
    ```
    `DangerButton(...)` becomes a deprecated alias for
    `Button{Variant: ButtonDanger}`. Other components follow the
    same shape.

12. **Component theme parameter pattern.**
    `StyleFn func(theme style.Theme) string` — unchanged from
    today, just the theme is now typed. Components write:
    ```go
    func buttonCSS(t style.Theme) string {
        return style.NewComponentSheet("ui-button", t).
            Rule("&").Set("background", t.Colors.Primary.CSS()).End().
            Rule("&[data-variant=danger]").Set("background", t.Colors.Danger.CSS()).End().
            MustBuild()
    }
    ```

13. **`gofastr theme init` scaffold command.** Writes a starter
    `theme.go` to the user's project (or to `./theme/theme.go`)
    containing the full `DefaultTheme()` declaration. User owns the
    file forever; no regeneration. One-shot scaffold, not codegen.

14. **`app.WithTheme(theme)` binding stays.** No new boot
    mechanism — the existing pattern threads the typed theme
    through `app.App` to the registry to the StyleFn calls.

15. **All in PR #1.** Same branch (`worktree-core-ui-css`).

---

## Migration sequence

Each step is independently committable + testable. Approximate
order to minimize churn:

### Step 1 — typed token values
- `core-ui/style/tokens_typed.go` (new): `Color`, `Spacing`,
  `Radius`, `Shadow`, `Font`, `Z`, `Dur`, `FontSize`, `Breakpoint`
  structs, each with `.CSS()` method.
- Tests for each `.CSS()` shape.

### Step 2 — typed Theme struct
- `core-ui/style/theme.go`: replace map-backed Theme with the
  typed struct. Keep `DefaultTheme() Theme` returning a fully-
  populated value. Add reflection-based `CSSCustomProperties()` —
  walks every typed field (recursing through embedded structs),
  emits `--<category>-<name>: <value>;` lines. Sorted output for
  determinism.
- Migrate existing token references in `framework/ui/theme/*` to
  the new types.
- Tests: determinism + every default field present.

### Step 3 — component-theme sections
- Per-component variant-style structs (`ButtonsTheme`,
  `BadgesTheme`, etc.) added to `Theme`.
- Each variant-bearing component declares its default mapping
  internally (Go function returning the appropriate
  `<Component>VariantStyle` from theme primitives) and overrides
  via `theme.<Component>.<Variant>` if non-zero.

### Step 4 — ComponentSheet only emits var()
- Drop `{tokens.text}` build-time resolution. `Set("color",
  t.Colors.Text)` (typed value) becomes the supported path. Old
  string path stays during migration but logs a deprecation
  warning in dev mode.

### Step 5 — Migrate every per-component CSS builder
- `framework/ui/styles_*.go` rewritten to take typed theme and
  call `t.Colors.X.CSS()` (var-only).
- Drop hardcoded `#hex` fallbacks from `var(--color-x, #hex)`.
- Drop the `&::backdrop`-rejecting tail check from
  `ComponentSheet` (no longer relevant once all tokens are
  typed).

### Step 6 — Merge theme.css + styles.css
- New `/__gofastr/app.css` endpoint serving theme custom
  properties + customCSS payload concatenated.
- Inject one `<link rel="stylesheet" href="/__gofastr/app.css">`
  per page instead of the existing two.
- Old `/__gofastr/theme.css` and `/__gofastr/styles.css`
  endpoints become 410 GONE.
- SSG emits `app.css` instead of the two separate files.

### Step 7 — `ui.Themed` wrapper + section overrides
- Framework collects every distinct theme override per request
  and emits `.fui-theme-<id> { ... }` blocks in app.css.
- `ui.Themed(theme, ...children)` wraps children in the matching
  class.
- chromedp E2E: nested theme override visibly changes color in
  a subtree.

### Step 8 — Variant standardization across components
- `framework/ui.Button(...)` lands (currently absent).
- `DangerButton` becomes deprecated alias.
- `StatusBadge`, `Callout`, etc. unchanged — they already use the
  config-field pattern.

### Step 9 — `gofastr theme init` scaffold
- `cmd/gofastr/theme_init.go`: writes `./theme/theme.go` with the
  full `DefaultTheme()` inlined as a Go file the user can edit.
- Refuses to overwrite if file exists.

### Step 10 — Migrate existing apps
- `examples/website/theme.go` rewritten to the new shape.
- `examples/website` E2E suite still green.

### Step 11 — Docs + drift
- `core-ui/ARCHITECTURE.md`: section on typed theme + variant
  pattern. data-fui-variant attribute (if added) goes in
  primitives table. (Note: per our discussion, no
  data-fui-variant — variants stay class-based since we skip
  per-variant tree-shaking.)
- `gofastr-ui` skill teaches the pattern.
- Drift test extended.

---

## Files

**New:**
- `core-ui/style/tokens_typed.go` — token value types with `.CSS()`.
- `core-ui/style/component_variants.go` — `<Component>VariantStyle`
  types + per-component theme sections.
- `cmd/gofastr/theme_init.go` (or under `cmd/gofastr/cmds/`) — the
  scaffold command.

**Modify:**
- `core-ui/style/theme.go` — typed Theme struct, reflection-based
  CSSCustomProperties.
- `core-ui/style/component_sheet.go` — drop string-token path;
  accept typed values in `Set`. Drop `{tokens}` resolver.
- `core-ui/style/classes.go` — adjust ComponentStyles map / drop if
  not needed.
- `framework/uihost/uihost.go` — `/__gofastr/app.css` endpoint;
  collect Themed overrides; emit one `<link>` instead of two.
- `framework/uihost/uihost_test.go`, `registry_css_test.go` —
  asserts the merged asset, the overrides emission.
- `framework/static/builder.go` — write `app.css` instead of
  `theme.css` + `styles.css`.
- `framework/ui/styles_*.go` — every per-component CSS builder
  rewritten to use typed theme and `var()`-only emission.
- `framework/ui/components.go` — `Button` lands; `DangerButton`
  deprecated alias.
- `examples/website/theme.go` — rewritten to typed shape.
- `core-ui/ARCHITECTURE.md` — typed-theme section.
- `.claude/skills/gofastr-ui/SKILL.md` — recipe.
- `examples/website/drift_test.go` — updated whitelist if needed.

---

## Verification

- `go test ./...` — full green sweep.
- `make build` — clean (csp-check + go build).
- Chromedp E2E exercising:
  - Section-level theme override visibly changes color in
    subtree (`/framework-ui/theme` demo updated to use
    `ui.Themed(...)`).
  - Page references exactly one `<link href="/__gofastr/app.css">`
    instead of theme.css + styles.css.
  - Components rendered with different variants paint correctly.
- Manual: `./scripts/dev-watch.sh` + browse the site,
  DevTools Network tab confirms one merged CSS asset + per-
  component sheets dedup as before.

---

## Out of scope (separate efforts)

- Per-variant tree-shaking — explicitly skipped per discussion.
- Multi-tenant runtime theme swapping (different themes per
  request based on tenant ID) — possible but a separate plumbing
  effort.
- Replacing the `ClientJS`-based action system with `data-fui-*`
  primitives — out of scope; that's its own architectural move.
