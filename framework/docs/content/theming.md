# Theming

Every visual decision in GoFastr flows through one typed theme: `core-ui/style.Theme`. Components never hardcode colors, radii, or spacing—they read CSS custom properties (`var(--color-primary)`, `var(--spacing-md)`, …) emitted into `/__gofastr/app.css`. Change a token once and every component, admin screen, and widget updates together.

`app.css` is composed once per process and served immutably by content hash (`/__gofastr/app.css?v=<hash>`), so register any `style.Contribute` calls before the first render.

## Setting the theme

Pass a `style.Theme` to `site.WithTheme(t)` using one of three starting points:

- **`theme.Default(theme.Overrides{...})`** (`framework/ui/theme`): Recommended default. Ships complete, contrast-safe light and dark palettes plus flat overrides for colors, `Dark`, fonts, radii, and component options:

```go
t := theme.Default(theme.Overrides{
    Primary: "#0F766E",
    Dark:    &theme.Overrides{Primary: "#5EEAD4"},
    Components: theme.ComponentOptions{
        Density: theme.Compact,
        Button:  theme.ButtonOptions{Treatment: theme.Outline, Radius: theme.Square},
    },
})
site.WithTheme(t)
```

- **`gofastr theme init`** (`--out=theme/theme.go`): Writes the full adaptive theme as editable Go code you own.
- **`style.DefaultTheme()`**: Lower-level light-only baseline (pair with `t.DarkColors = style.DefaultDarkColors()` if you want dark mode at this level).

`WithTheme` validates the theme at boot and panics if any required token is empty or if a hex `primary`/`primary-fg` or `danger`/`danger-fg` pair falls below 4.5:1 contrast. `style.ContrastWarnings(t)` also logs boot warnings for non-hex contrast issues and form-control borders below 3:1 against `surface`.

## Token catalog

Go field paths convert to kebab-case CSS custom properties (`Colors.PrimaryFg` → `--color-primary-fg`). Required groups fail `Validate` if incomplete; `Strokes`, `Leading`, `Tracking`, `Opacities`, `Code`, and `Knobs` are optional.

| Group | CSS prefix | Examples / notes |
|---|---|---|
| `Colors` / `DarkColors` | `--color-<name>` | `--color-primary`, `--color-primary-fg`, `--color-danger`, `--color-surface`, `--color-text-muted`, `--color-border` |
| `Fonts` | `--font-<name>` | `--font-body`, `--font-heading`, `--font-mono` (system stacks by default) |
| `Spacing` | `--spacing-<name>` | `--spacing-xs` … `--spacing-3xl` |
| `Radii` | `--radii-<name>` | `--radii-sm`, `--radii-md`, `--radii-lg`, `--radii-full` (set `0` for square themes) |
| `Strokes` | `--stroke-<name>` | `--stroke-thin` (1px), `--stroke-thick` (2px), `--stroke-focus` (2px), `--stroke-focus-offset` |
| `Shadows` | `--shadow-<name>` | `--shadow-xs` (controls) … `--shadow-xl` |
| `ZIndex` | `--z-<name>` | `--z-dropdown`, `--z-modal`, `--z-toast` |
| `Durations` / `Easings` | `--duration-*`, `--easing-*` | `--duration-fast`, `--easing-ease-out`, `--easing-spring` |
| `Typography` / `FontWeights` | `--text-*`, `--font-weight-*` | `--text-sm` … `--text-2xl`, `--font-weight-normal` (400) … `bold` (700) |
| `Leading` / `Tracking` / `Opacities` | `--leading-*`, `--tracking-*`, `--opacity-*` | Line heights, letter spacing, disabled/muted opacities |
| `Breakpoints` / `Layout` | `--breakpoint-*`, `--spacing-touch-target`, `--size-*` | `--size-page-width` (66rem), `--size-page-gutter`, `--size-header-height` (56px), `--spacing-touch-target` (44px) |
| `Code` / `DarkCode` | `--tk-<name>` | Syntax-highlight colors (`--tk-kw`, `--tk-str`, `--tk-com`) |
| `Knobs` | `--ui-<name>` | Per-component overrides emitted on `:root` (e.g., `"ui-button-edge"`, `"ui-checkbox-box-size"`, `"ui-form-max"`) |

Default kit conventions: neutral zinc palette, neutral `:focus-visible` ring (`--color-text-subtle`), 16px text controls below `md` (prevents iOS Safari zoom), ≥3:1 control borders via `--ui-control-border`, `forced-colors` support, and `accent-color: var(--color-primary)`.

## App tokens (`Theme.Extend`)

### In Go

```go
type brandTokens struct {
	BrandGlow style.Color      // --color-brand-glow
	HeroGap   style.Size       // --size-hero-gap
	Display   style.FontWeight // --font-weight-display
}

t := theme.Default().Extend(brandTokens{
	BrandGlow: style.Color{Value: "#FF7A00"},
	HeroGap:   style.Size{Value: "clamp(2rem, 6vw, 5rem)"},
	Display:   style.FontWeight{Value: 800},
})
t.DarkColors["brand-glow"] = "#FFB066"
```

`Extend` returns a copy, emits your tokens everywhere built-in tokens go (`:root`, `ui.Themed`, `ThemeHash`, `ThemeToTokens`/`ApplyTokens`), and panics if a name collides with an existing token.

### App tokens in CSS: `<name>.tokens.css`

Declare app tokens in CSS right beside your `<name>.style.css` sheets and run `gofastr gen styles` to generate typed Go bindings (`<name>_tokens.gen.go`):

```css
/* acme.tokens.css */
@property --size-hero-gap { syntax: "<length>"; inherits: true; initial-value: clamp(2rem, 6vw, 5rem); }
@property --color-highlight { syntax: "<color>"; inherits: true; initial-value: #0F766E; }

@media (--dark) {
  :root { --color-highlight: #5EEAD4; }
}
```

Then attach the generated struct to your theme:

```go
site.WithTheme(theme.Default().Extend(ui.Tokens))
```

- Only `@property --<type>-<name>` rules (matching the built-in prefixes, `inherits: true`) and one optional `@media (--dark) { :root { ... } }` block for dark colors are allowed.
- `gofastr verify` and `gofastr gen styles` enforce token hygiene across owned sheets (`GOFASTR1806` unknown `var()`, `GOFASTR1807` hardcoded literal matching a token, `GOFASTR1821` duplicate token value, `GOFASTR1822` repeated literals across sheets, `GOFASTR1825` disallowed `@import`). Custom media queries like `(--above-md)` belong in `@media`, never `@container` (native `@container style(...)` queries remain valid).

## Live theme editor: `gofastr theme edit`

```bash
gofastr theme edit                        # ephemeral loopback port, auto-open browser
gofastr theme edit --out=theme/theme.go   # target file (refuses overwrite without --force)
```

Starts a loopback-only visual configurator against `framework/gallery` with live contrast checking (4.5:1 light + dark via browser computed colors) and atomic write-back. Note that it starts from the framework default rather than reading an existing `theme.go`—pass a fresh `--out` path if you only want to experiment.

## Self-hosting web fonts

The default CSP (`default-src 'self'`) blocks third-party font CDNs. Self-host fonts instead:

1. Drop `.woff2` files in `static/fonts/` (served via `uihost.WithStaticDir("static")`).
2. Emit `@font-face` rules with `style.FontFaceCSS` (deriving slugs via `style.FontSlug`, e.g., `"Inter"` → `/fonts/inter.woff2`) and preload critical faces with `uihost.WithFontPreload`:

```go
fonts := []style.WebFont{{
    Family: "Inter",
    Fallback: style.FontFallback{ // optional metric-matched fallback to prevent layout shift
        Local: "Arial", SizeAdjust: "107%", AscentOverride: "90%",
    },
}}
uihost.WithCustomCSS(style.FontFaceCSS("", fonts...))
uihost.WithFontPreload("", fonts...)
```

3. Reference the family in `t.Fonts.Body.Value` / `t.Fonts.Heading.Value` (e.g., `"Inter", "Inter Fallback", ui-sans-serif, system-ui, sans-serif`).

## Dark mode, `ui.ColorScheme`, `ui.Themed` & page themes

### Dark mode (`DarkColors` + `data-color-scheme`)

When `Theme.DarkColors` is populated, GoFastr emits dark overrides under `:root[data-color-scheme="dark"]` plus a `prefers-color-scheme: dark` fallback when light mode isn't forced, along with `<meta name="color-scheme" content="light dark">` so native form controls and scrollbars match even with JS off.

- Never gate custom CSS on `@media (prefers-color-scheme: dark)`—it ignores `ui.ThemeToggle`. Use `DarkColors` or `:root[data-color-scheme="dark"]`.
- Lock a single section to dark or light regardless of page mode with `ui.ColorScheme(ui.SchemeDark, ...)`.

### Section overrides: `ui.Themed`

Re-skin one band of a page without affecting the rest:

```go
var DarkBand = style.RegisterThemeOverride(darkTheme)

ui.Themed(DarkBand,
    ui.Section(ui.SectionConfig{Heading: "Settings"}, ...),
)
```

`RegisterThemeOverride` clones the theme and emits a `.cui-theme-<hash>` block. Overrides without `DarkColors` intentionally stay light in dark mode; add `DarkColors` if the section should follow the page toggle.

### Page themes: `ui.ThemePicker`

Let visitors switch whole-page themes in place (stored in `localStorage["gofastr.theme"]`, restored before first paint with no flash; works alongside `ui.ThemeToggle`):

```go
var (
    Brutal     = style.RegisterThemeOverride(theme.Brutal())
    brutalName = i18n.Define("myapp.themes.brutal", "Brutal")
)

ui.ThemePicker(ui.ThemePickerConfig{
    Ctx:    ctx,
    Themes: []ui.ThemeChoice{{Label: brutalName, Theme: Brutal}},
})
```

Build overrides from your app theme (not bare `theme.Default()`) so custom fonts/spacing don't revert, give every offered theme a full `DarkColors` palette, and self-host any fonts they reference. Note that the server does not see browser-stored picker choices (emails and SSR-only exports use the app theme).

## Component options & per-component knobs

**Component options** (`theme.Overrides.Components`) change how component families draw across the whole theme (or inside a `ui.Themed` scope):

- `Density`: `theme.Comfortable` (44px controls via `--spacing-touch-target`) or `theme.Compact` (36px). Explicit component `Size` always wins over density.
- `Button.Treatment`: `theme.Filled`, `theme.Outline`, or `theme.Soft` — controls *how* semantic variants (`ButtonPrimary`, `ButtonDanger`) draw without changing what they mean.
- `Button.Radius`: `theme.Round`, `theme.Square`, `theme.Pill`.
- `Field.Layout`: `theme.Stacked` or `theme.Inline` (stacks automatically on narrow widths); `Field.Radius`: `theme.FieldRound` or `theme.FieldSquare`.

**Per-component knobs (`--ui-*`):** For fine-grained dimensions or accents (`--ui-button-edge`, `--ui-form-max`, `--ui-markdown-measure`, `--ui-press-hover-translate`, `--ui-layout-gap-*`, `--ui-control-padding-y`), set them in `Theme.Knobs` (keyed without `--`, e.g., `"ui-button-edge": "var(--color-border-strong)"`).

## Runtime token map: `ThemeToTokens` / `ApplyTokens`

To accept user/tenant brand tokens safely (configurators, embedded surfaces), flatten and apply through `style.ApplyTokens`—it validates every key and type and **rejects** unknown keys or CSS declaration breakers (`;`, `{`, `}`, `/*`, `url(`, `<`, `>`, newlines):

```go
brand, err := style.ApplyTokens(app.Theme, map[string]string{
    "color-primary":      customerPrimary,
    "dark.color-primary": customerPrimaryDark,
    "knob.ui-button-edge": "var(--color-border-strong)",
})
if err != nil {
    return err
}
hash := host.RegisterThemeVariant(brand) // serve by content hash
```

Round-tripping `ApplyTokens(t, ThemeToTokens(t))` preserves `ThemeHash(t)` identically.

## Why you shouldn't override component CSS directly

At first paint, component CSS loads before `app.css`; on lazy island/widget/SPA arrival, component CSS appends *after* `app.css`. Equal-specificity overrides like `.fui-button { ... }` therefore win on some pages and silently lose on others. Customize through:
1. Theme tokens (`style.Theme`) and component options (`theme.Overrides.Components`).
2. `--ui-*` knobs (`Theme.Knobs`).
3. Registered variants (`ui.RegisterButtonVariant`, `ui.RegisterCardVariant`, `ui.RegisterStatusVariant`, …) — see [ui-getting-started.md](ui-getting-started.md).

## Common mistakes

- **Applying external brand colors without `style.ApplyTokens`.** Assigning request strings directly onto `Theme` fields creates CSS injection; always validate through `ApplyTokens` (and call `Theme.Validate()` for semantic checks).
- **Gating dark mode on `prefers-color-scheme` alone.** It ignores `ui.ThemeToggle`; put dark values in `DarkColors` or `:root[data-color-scheme="dark"]`.
- **Hardcoding hex colors in CSS.** They break in dark mode and `ui.Themed` scopes—reference `var(--color-*)` tokens.
- **Overriding `.fui-*` internals in site CSS or writing `.fui-*` on custom markup.** `fui-` is reserved for `framework/ui`, and lazy stylesheet ordering makes selector overrides unreliable.
- **Expecting a `ui.Themed` scope without `DarkColors` to turn dark.** Scopes without a dark palette intentionally stay light.
- **Confusing `Variant` with `Treatment`.** Variant (`ButtonPrimary`, `ButtonDanger`) is semantic meaning; Treatment (`Filled`, `Outline`, `Soft`) is visual style across all variants.
