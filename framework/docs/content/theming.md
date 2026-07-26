# Theming

Every visual decision in the framework goes through one typed theme:
`core-ui/style.Theme`. Components never hardcode colors or spacing —
they read CSS custom properties (`var(--color-primary)`,
`var(--spacing-md)`, …), and the host writes the theme as a `:root`
block at `/__gofastr/app.css`. Change a token value and every
component, battery screen, and widget that reads it updates —
you never edit a component's CSS to re-skin an app.

## The token catalog

`style.Theme` is a struct made of typed token groups. Every field is
required — `WithTheme` panics at startup and names any token you left
out. Each group writes CSS variables with a fixed prefix:

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
| `Layout` | `--spacing-touch-target` | the WCAG minimum tap-target size (44px); buttons and inputs use it for sizing |
| `Code` | `--tk-<name>` | `--tk-kw`, `--tk-str`, `--tk-com` — the syntax-highlight colors code blocks read. This is the only optional group: leave a slot unset and it falls back to the built-in palette. Dark values go in `Theme.DarkCode` (a map, like `DarkColors`) |

Token names come from the Go field path, converted to kebab-case
(`Colors.PrimaryFg` → `--color-primary-fg`). Set an explicit `Name` on
a token to override that. In component CSS written with
`style.ComponentSheet` or `ui.VariantCSS`, the `{group.name}` shorthand
resolves to the variable: `{colors.primary}` → `var(--color-primary)`,
`{spacing.lg}` → `var(--spacing-lg)`.

## Setting the theme

Three entry points produce a `style.Theme` you pass to
`site.WithTheme(...)`:

- **`style.DefaultTheme()`** — the fully-populated, lower-level light
  baseline. It leaves `DarkColors` empty on purpose, for compatibility.
- **`framework/ui/theme.Default(theme.Overrides{Primary: "#0F766E", DarkColors: map[string]string{"primary": "#5EEAD4"}})`**
  — the adaptive theme fresh scaffolds start with. It ships complete,
  contrast-safe light and dark palettes, plus a flat override struct for
  the tokens hosts change most often: the light palette, explicit dark
  token values, the three font stacks, and the radius scale. Light
  overrides are not copied into dark mode automatically, because
  contrast-safe values are usually different for dark. Any field you
  don't set keeps its default.
- **`gofastr theme init`** — writes `theme/theme.go`, the full adaptive
  default as a literal you own and can edit directly. Use this for apps
  that will keep changing their theme over time; edit `Colors` and
  `DarkColors` together.

If your app needs tokens beyond the built-in set, embed `style.Theme`
in your own struct and add fields. Framework components only read the
embedded built-in tokens; your own components can read the extra
fields directly.

Check the result at `/__gofastr/app.css` — your values should show up
as `:root` custom properties.

## Self-hosting web fonts

Setting `Fonts.Body`/`Fonts.Heading` to a custom family only names the
font — the browser still needs the actual font files, and **the
default CSP blocks CDN font URLs** (`default-src 'self'`; see
[security](security.md) → "Content-Security-Policy"). A `@font-face`
rule pointing at `rsms.me`, Google Fonts, or any other outside origin
fails silently, and the browser uses the fallback font instead.
Self-host the font instead:

1. Put the font files under your static dir:
   `static/fonts/Inter-Variable.woff2` (serve it with
   `uihost.WithStaticDir("static")`).
2. Add the `@font-face` rule through site CSS —
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

Same-origin URLs pass the default CSP with no changes needed. If you
really need to load a font from a third party, you have to override
`ContentSecurityPolicy` yourself — read the warning in
[security](security.md) before you do.

## Dark mode — `DarkColors` and `data-color-scheme`

`Theme.DarkColors` is a map from color-token name to its dark value
(`"background": "#15141B"`, …). `framework/ui/theme.Default()` and
`gofastr theme init` fill in a complete map; the lower-level
`style.DefaultTheme()` leaves it empty, for compatibility.

When the map isn't empty, the generated CSS re-declares those tokens
under `:root[data-color-scheme="dark"]`, plus a
`prefers-color-scheme: dark` fallback that only applies while the user
hasn't forced light mode. The **`data-color-scheme` attribute on
`<html>` is the actual switch** — `ui.ThemeToggle` and the color-scheme
bootstrap set it (and remember the choice), and any element reading a
theme CSS variable picks up the new color the moment it flips.

Follow these rules:

1. **Never gate your own light/dark styling on
   `prefers-color-scheme`.** A media query can't see the in-app toggle,
   so it disagrees with the attribute as soon as the user picks a
   scheme that doesn't match the OS. Put dark values in `DarkColors`
   (or a `:root[data-color-scheme="dark"]` scoped rule) so the toggle
   controls everything.
2. **Reference tokens, not literal colors,** in any CSS you write. A
   hardcoded hex value is invisible to the dark re-declaration and
   turns into a light-colored patch on a dark page.
3. **Don't render `ui.ThemeToggle` with a light-only custom theme.**
   Keep the scaffold's adaptive default, or supply every semantic dark
   token yourself first. The toggle also changes the browser's native
   color-scheme state, so a partial palette can end up mixing dark
   browser link/control colors with light app colors.

## Section-level overrides — `ui.Themed`

To re-skin one part of a page (a dark marketing band, a branded
callout, per-tenant accents) without touching the rest of the page,
register an override theme and wrap the section:

```go
var Dark = style.RegisterThemeOverride(darkTheme)

ui.Themed(Dark,
    ui.Section(ui.SectionConfig{Heading: "Settings"},
        ui.Button(ui.ButtonConfig{Label: "Save", Variant: ui.ButtonPrimary}),
    ),
)
```

`Themed` wraps the content in a `<div class="fui-theme-<hash>">`. The
override's token block ships in `app.css` scoped to that class, and
every component inside it reads `var(--color-…)` from that class
instead of from `:root`. Registering the same theme twice returns the
same handle, so its CSS only ships once.

## Token map — `ThemeToTokens` / `ApplyTokens`

Two surfaces need to move tokens in and out of a `style.Theme` as a flat
`map[string]string` instead of a typed struct:

- **A theme configurator** (a tweakcn-style local UI) edits token values
  and must round-trip them back to a `style.Theme`.
- **An embedded surface** lets a third-party site supply a small set of
  brand tokens. Those values are attacker-influenced and reach CSS, so
  applying them is a security boundary, not a convenience.

`style.ThemeToTokens(t)` flattens a theme to a map keyed by the CSS
custom-property identifier **without** the leading `--`:
`"color-primary"`, `"spacing-md"`, `"duration-fast"`, `"tk-kw"`. The value
is exactly what the `:root` block emits after the colon — `"#4F46E5"`,
`"8px"`, `"150ms"`. That key is chosen over a Go field-path key because it
is what the CSS emits, what a UI control edits, and stable across struct
reorganisations.

`DarkColors` / `DarkCode` (the dark-scheme maps) share their CSS var name
with the light token but live in a different selector scope, so they are
flattened under a `dark.` prefix to stay distinct:
`DarkColors["primary"]` → `"dark.color-primary"`, `DarkCode["kw"]` →
`"dark.tk-kw"`.

`style.ApplyTokens(base, tokens)` returns a copy of `base` with the
supplied tokens applied. It **fails closed on every axis**:

- An unknown key is an error — a typo in a theme file is reported, and an
  embed can't probe for which keys a host accepts.
- A value must validate for its token's type. Colors accept a bounded
  grammar (hex, `rgb()`/`rgba()`, `hsl()`/`hsla()`, `oklch()`/`oklab()`,
  `color-mix()`, `var(--…)`, and the CSS named colors) and reject
  everything else. Integer/duration tokens must match their numeric
  format. Every free-form string (Font, Shadow, Easing, FontSize,
  CodeColor) is rejected if it contains a declaration-breaking sequence
  (`;`, `}`, `{`, `/*`, `*/`, `<`, `>`, `\`, a newline, or `url(`).

A value like `red; --x:}body{display:none}` escapes its CSS declaration;
CSS alone can then exfiltrate via attribute selectors and
`background-image` URLs. `ApplyTokens` rejects it rather than sanitising —
never strip, always reject.

Round-trip is exact over `ThemeHash`:
`ApplyTokens(t, ThemeToTokens(t))` produces a theme with the same hash as
`t` (identical emitted CSS), so a configurator's save/load cycle changes
no pixel:

```go
tokens := style.ThemeToTokens(myTheme)
// ...edit tokens["color-primary"], tokens["dark.color-primary"], ...
reApplied, err := style.ApplyTokens(myTheme, tokens)
if err != nil {
    return err
}
// style.ThemeHash(reApplied) == style.ThemeHash(myTheme)
```

For an embedded surface, apply the caller's brand tokens, then register
the result as a theme variant so the surface is served by content hash,
not by caller-supplied values:

```go
brand, err := style.ApplyTokens(app.Theme, map[string]string{
    "color-primary":      customerPrimary,
    "dark.color-primary": customerPrimaryDark,
})
if err != nil {
    return fmt.Errorf("invalid brand tokens: %w", err)
}
hash := host.RegisterThemeVariant(brand) // framework/uihost.UIHost
// hand `hash` to the themed surface's app.css URL
```

### Common mistakes

- **Applying caller-supplied theme values without `ApplyTokens`.** A raw
  `Colors.Primary.Value = req.BrandColor` puts attacker-controlled text
  directly into CSS. Route every external value through `ApplyTokens` so
  the bounded grammar and declaration-breaker rejection run.
- **Expecting `ApplyTokens` to enforce semantic constraints.** It validates
  type and safety (parseable, not injecting); it does not run
  `Theme.Validate()`. A spacing of `0px` parses fine but is a layout bug —
  call `Theme.Validate()` on the result for the name/value sanity checks
  `WithTheme` runs at boot.
- **Keying the map by Go field path.** `Colors.Primary` is not a valid key;
  `color-primary` is. Round-trip through `ThemeToTokens` once to see the
  exact key set a theme exposes.

## Per-component knobs — the `--ui-*` variables

Some components expose dimensions or accents that aren't global
tokens — a container's max width, a doc layout's rail width, a code
block's scroll max height. These are exposed as
`--ui-<component>-<knob>` variables with built-in fallbacks, so a host
can override them from its own stylesheet without forking the
component:

```css
/* app.css or a style.Contribute block */
:root { --ui-container-wide: 1240px; }
```

You can also scope them — set one inside a `ui.Themed` section, or on
a specific wrapper class, to change a single instance. Each
component's source lists its knobs next to the CSS that reads them
(for example `ui.Container`: `--ui-container-default/narrow/wide`;
`ui.DocLayout`: `--ui-doc-layout-rail/gap/max-width`) — grep
`framework/ui` for `--ui-` to see the full list.

## Why you can't just override component CSS

Component stylesheets and your site CSS don't always load in the same
order. At first paint, the host writes the page's component-CSS bundle
first and `/__gofastr/app.css` after it. But if a component's CSS
loads lazily, after hydration — because it first shows up in an island
response, a widget, or an SPA navigation — its `<link>` gets appended
to the end of `<head>`, after `app.css`. So a site rule with the same
specificity as a component's internal rule (`.ui-button { background:
… }`) wins on one page and silently loses on another, depending on how
that component's stylesheet arrived. Reaching for `!important` or a
higher-specificity selector "fixes" it today and breaks again the next
time the component changes.

Don't restyle component internals directly. Use one of these instead —
they work the same way no matter what order things loaded in:

- **Token values** (this doc) for anything the palette or scale
  controls.
- **`--ui-*` variables** for per-component knobs.
- **Registered variants** (`ui.RegisterButtonVariant`,
  `RegisterCardVariant`, `RegisterStatusVariant`, …) for a new named
  look — the variant CSS ships inside the component's own stylesheet
  and goes through the same render-time validation. See
  [ui-getting-started](ui-getting-started.md) § "Custom variants on
  framework components".

If none of those can do what you need, the component is missing a
config option or variant. Add it there, upstream, instead of patching
its internals from the outside.

## Common mistakes

- **Gating dark mode on `prefers-color-scheme` alone.** The in-app
  toggle sets `data-color-scheme` on `<html>`; a bare media query
  ignores it and fights the user's choice. Put dark values in
  `Theme.DarkColors` and let the generated CSS handle both signals.
- **Overriding a component's internals from site CSS.** The order of
  `app.css` and a component's stylesheet differs between first paint
  (component CSS loads first) and a lazy load after hydration
  (component CSS loads last), so an equal-specificity override works
  on some pages and silently fails on others. Use tokens, `--ui-*`
  knobs, or a registered variant instead.
- **Hardcoding a hex value where a token belongs.** It looks fine in
  light mode and turns into a wrong-colored patch the first time dark
  mode or a `ui.Themed` section wraps it. Write `{colors.primary}` /
  `var(--color-primary)` instead.
- **Starting the app with a half-filled-in theme.** Every token is
  required; `WithTheme` panics at startup and names the missing field
  path. Start from `style.DefaultTheme()` or `gofastr theme init` and
  edit values from there.
- **Editing `--color-*` variables on one component instead of
  theming.** Re-declaring a global token on one component's selector
  "works," but dark mode and every other consumer of that token never
  see it. For a one-section reskin, use `ui.Themed` plus a registered
  override theme instead.
