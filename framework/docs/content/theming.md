# Theming

Every visual decision in the framework routes through one typed theme:
`core-ui/style.Theme`. Components never hardcode colors or spacing —
they reference CSS custom properties (`var(--color-primary)`,
`var(--spacing-md)`, …), and the host emits the theme as a `:root`
block at `/__gofastr/app.css`. Change a token value and every
component, battery screen, and widget that references it updates —
no component CSS is ever edited to re-skin an app.

## The token catalog

`style.Theme` is a struct of typed token groups. Every field is
required (`WithTheme` panics at startup naming any missing token), and
each group emits CSS variables with a fixed prefix:

| Theme group | Emits | Examples |
|---|---|---|
| `Colors` | `--color-<name>` | `--color-primary`, `--color-surface`, `--color-text-muted`, `--color-danger`, `--color-code-surface` |
| `Fonts` | `--font-<name>` | `--font-body`, `--font-heading`, `--font-mono` |
| `Spacing` | `--spacing-<name>` | `--spacing-xs` … `--spacing-3xl` (px) |
| `Radii` | `--radii-<name>` | `--radii-sm`, `--radii-md`, `--radii-full` |
| `Shadows` | `--shadow-<name>` | `--shadow-sm` … `--shadow-xl` |
| `ZIndex` | `--z-<name>` | `--z-dropdown`, `--z-modal`, `--z-toast` |
| `Durations` | `--duration-<name>` | `--duration-fast`, `--duration-overlay-enter` |
| `Easings` | `--easing-<name>` | `--easing-ease-out`, `--easing-spring` |
| `Typography` | `--text-<name>` | `--text-sm`, `--text-base`, `--text-2xl` |
| `Breakpoints` | `--breakpoint-<name>` | `--breakpoint-md` (informational — media queries can't read vars) |
| `Layout` | `--spacing-touch-target` | the WCAG minimum tap target (44px) buttons and inputs size against |
| `Code` | `--tk-<name>` | `--tk-kw`, `--tk-str`, `--tk-com` — the syntax-highlight palette code blocks read. The one *optional* group: unset slots fall back to the built-in palette. Dark values go in `Theme.DarkCode` (map, like `DarkColors`) |

Token names auto-derive from the Go field path in kebab-case
(`Colors.PrimaryFg` → `--color-primary-fg`); an explicit `Name` on a
token overrides that. In component CSS written with
`style.ComponentSheet` or `ui.VariantCSS`, the `{group.name}` shorthand
resolves to the variable: `{colors.primary}` → `var(--color-primary)`,
`{spacing.lg}` → `var(--spacing-lg)`.

## Setting the theme

Three equivalent entry points, all producing a `style.Theme` you pass
to `site.WithTheme(...)`:

- **`style.DefaultTheme()`** — the fully-populated baseline. Mutate
  token values on the copy (this is what generated apps do; see
  `examples/meridian/app.go` → `appTheme()`).
- **`framework/ui/theme.Default(theme.Overrides{Primary: "#14B8A6"})`**
  — the framework's canonical theme (same shape, slightly adjusted
  defaults) with a flat override struct for the tokens hosts most
  often swap: the palette, the three font stacks, and the radius
  scale. Unset fields keep their defaults.
- **`gofastr theme init`** — writes `theme/theme.go`, the complete
  default as a literal you own and edit. Best for apps that will keep
  diverging.

Apps that need tokens beyond the canonical set embed `style.Theme` in
their own struct and add fields; framework components only read the
embedded canonical tokens, app components read the extras directly.

Verify the result at `/__gofastr/app.css` — your values should appear
as `:root` custom properties.

## Self-hosting web fonts

Setting `Fonts.Body`/`Fonts.Heading` to a custom family only names the
font — the browser still needs the files, and **CDN font URLs are
blocked by the default CSP** (`default-src 'self'`; see
[security](security.md) → "Content-Security-Policy"). A
`@font-face` pointing at `rsms.me`, Google Fonts, or any other origin
silently fails and the stack falls through to its fallback. Self-host
instead:

1. Put the font files under your static dir:
   `static/fonts/Inter-Variable.woff2` (serve it with
   `uihost.WithStaticDir("static")`).
2. Register the `@font-face` through the styling surface — site CSS via
   `uihost.WithCustomCSS` / `ReadCustomCSSFile("static/app.css")`:

   ```css
   @font-face {
     font-family: "Inter";
     font-style: normal;
     font-weight: 100 900;
     font-display: swap;
     src: url("/fonts/Inter-Variable.woff2") format("woff2");
   }
   ```

3. Name the family in the theme tokens:

   ```go
   t.Fonts.Body.Value = `"Inter", ui-sans-serif, system-ui, sans-serif`
   t.Fonts.Heading.Value = t.Fonts.Body.Value
   ```

Same-origin URLs pass the default CSP untouched. If you genuinely must
load a third-party font, you have to override
`ContentSecurityPolicy` explicitly — see the warning in
[security](security.md) before you do.

## Dark mode — `DarkColors` and `data-color-scheme`

`Theme.DarkColors` is a map of color-token name → dark value
(`"background": "#15141B"`, …). Empty by default: an app opts into
dark mode by supplying it, and a light-only app is never surprised
into dark by an OS preference.

When non-empty, the emitted CSS re-declares those tokens under
`:root[data-color-scheme="dark"]`, plus a `prefers-color-scheme: dark`
fallback that applies only while the user hasn't forced light. The
**`data-color-scheme` attribute on `<html>` is the switch** —
`ui.ThemeToggle` and the color-scheme bootstrap set it (and persist
the choice), and every surface that emits theme CSS recolors through
the variable cascade when it flips.

Two rules follow:

1. **Never gate your own light/dark styling on
   `prefers-color-scheme`.** A media query can't see the user's
   in-app toggle, so it disagrees with the attribute the moment the
   user picks the non-OS scheme. Dark values belong in `DarkColors`
   (or a `:root[data-color-scheme="dark"]` scoped rule) so the toggle
   drives everything.
2. **Reference tokens, not literal colors,** in any CSS you write —
   a hardcoded hex is invisible to the dark re-declaration and
   becomes a light-colored island on a dark page.

## Section-level overrides — `ui.Themed`

To re-skin one subtree (a dark marketing band, a branded callout,
per-tenant accents) without touching the rest of the page, register an
override theme and wrap the section:

```go
var Dark = style.RegisterThemeOverride(darkTheme)

ui.Themed(Dark,
    ui.Section(ui.SectionConfig{Heading: "Settings"},
        ui.Button(ui.ButtonConfig{Label: "Save", Variant: ui.ButtonPrimary}),
    ),
)
```

`Themed` emits a `<div class="fui-theme-<hash>">`; the override's
token block ships in `app.css` scoped to that class, and every
descendant component dereferences `var(--color-…)` against it instead
of `:root`. Registering the same theme twice returns the same handle,
so the CSS ships once.

## Per-component knobs — the `--ui-*` variables

Some components expose dimensions or accents that aren't global
tokens — a container's width cap, a doc layout's rail width, a code
block's scroll max. These are published as `--ui-<component>-<knob>`
variables with built-in fallbacks, so a host overrides them from its
own stylesheet without forking the component:

```css
/* app.css or a style.Contribute block */
:root { --ui-container-wide: 1240px; }
```

They also work scoped — set one inside a `ui.Themed` section or on a
specific wrapper class to retune a single instance. The knobs are
named in each component's source next to the CSS that reads them
(e.g. `ui.Container`: `--ui-container-default/narrow/wide`;
`ui.DocLayout`: `--ui-doc-layout-rail/gap/max-width`) — grep
`framework/ui` for `--ui-` to see the full set.

## Why you can't just override component CSS

Component stylesheets and your site CSS load in an order you don't
control. At SSR, the host emits the page's component-CSS bundle first
and `/__gofastr/app.css` after it — but any component whose CSS loads
lazily *after* hydration (it first appears in an island response, a
widget, or an SPA navigation) gets its `<link>` appended to the end of
`<head>`, after `app.css`. So an equal-specificity site rule against a
component's internals (`.ui-button { background: … }`) wins on one
page and silently loses on another, depending on how the component's
sheet arrived; escalating with `!important` or higher-specificity
selectors wins today and breaks on the next component change.

Component internals are not the theming surface. Restyle through the
supported channels instead — they behave identically regardless of
load order:

- **Token values** (this doc) for anything the palette/scale controls.
- **`--ui-*` variables** for per-component knobs.
- **Registered variants** (`ui.RegisterButtonVariant`,
  `RegisterCardVariant`, `RegisterStatusVariant`, …) for a new named
  look — the variant CSS ships inside the component's own sheet and
  joins render-time validation. See
  [ui-getting-started](ui-getting-started.md) § "Custom variants on
  framework components".

If none of those can express what you need, the component is missing
a config option or variant — extend it upstream rather than patching
its internals from outside.

## Common mistakes

- **Gating dark mode on `prefers-color-scheme` alone.** The in-app
  toggle sets `data-color-scheme` on `<html>`; a bare media query
  ignores it and fights the user's choice. Put dark values in
  `Theme.DarkColors` and let the emitted CSS handle both signals.
- **Overriding a component's internals from site CSS.** The relative
  order of `app.css` and a component's sheet differs between first
  paint (component CSS first) and lazy load after hydration (component
  CSS last), so an equal-specificity override works on some pages and
  silently fails on others. Use tokens, `--ui-*` knobs, or a
  registered variant.
- **Hardcoding a hex where a token belongs.** It renders fine in
  light mode and turns into a wrong-colored island the first time
  dark mode or a `ui.Themed` section wraps it. Write
  `{colors.primary}` / `var(--color-primary)`.
- **Booting with a half-populated theme.** Every token is required;
  `WithTheme` panics at startup naming the missing field path. Start
  from `style.DefaultTheme()` / `gofastr theme init` and edit values.
- **Editing `--color-*` variables per component instead of theming.**
  Re-declaring global tokens on one component's selector "works" but
  is invisible to dark mode and other consumers of the token. For a
  one-section reskin that's what `ui.Themed` + a registered override
  theme is for.
