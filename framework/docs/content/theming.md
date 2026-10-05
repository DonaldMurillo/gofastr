# Theming

Every visual decision in the framework goes through one typed theme:
`core-ui/style.Theme`. Components never hardcode colors or spacing;
they read CSS custom properties (`var(--color-primary)`,
`var(--spacing-md)`, …), and the host writes the theme as a `:root`
block at `/__gofastr/app.css`. Change a token value and every
component, battery screen, and widget that reads it updates;
you never edit a component's CSS to re-skin an app.

The sheet is composed once per process and content-addressed: pages
link `/__gofastr/app.css?v=<fingerprint>` and the response is
immutable for the life of the deploy. That means `style.Contribute`
calls must land before the first page render (package init or before
`Mount`); a later contribution can't ship, and the host logs a
warning when one arrives too late.

## The token catalog

`style.Theme` is a struct made of typed token groups. Every field is
required except `Strokes`, `Leading`, `Tracking`, `Opacities`, `Code` and
`Knobs`; `WithTheme` panics at startup and
names any token you left out. Each group writes CSS variables with a
fixed prefix:

| Theme group | Emits | Examples |
|---|---|---|
| `Colors` | `--color-<name>` | `--color-primary`, `--color-primary-fg`, `--color-danger`, `--color-danger-fg`, `--color-text-muted`, `--color-code-surface` |
| `Fonts` | `--font-<name>` | `--font-body`, `--font-heading`, `--font-mono` |
| `Spacing` | `--spacing-<name>` | `--spacing-xs` … `--spacing-3xl` (px) |
| `Radii` | `--radii-<name>` | `--radii-sm`, `--radii-md`, `--radii-full`. Any step can be `0`: a square theme sets them all to it |
| `Strokes` | `--stroke-<name>` | `--stroke-thin` (1px: every control and card border, every divider, the inset ring on a shadow), `--stroke-thick` (2px: an emphasised border such as a selected card or an active tab's rule), `--stroke-focus` (2px: the focus outline) and `--stroke-focus-offset` (2px: its gap from the element). A value is `"0"` or a non-negative px/rem/em length. The group is optional: a stroke you leave unset is not emitted and the kit draws its default width, so a `theme.go` written before strokes existed keeps its borders |
| `Shadows` | `--shadow-<name>` | `--shadow-xs` … `--shadow-xl`: `xs` is the hairline lift under a resting control (button, input, select), `sm` sits under a card, `md` under a popover or menu, `lg` under a dialog |
| `ZIndex` | `--z-<name>` | `--z-dropdown`, `--z-modal`, `--z-toast` |
| `Durations` | `--duration-<name>` | `--duration-fast`, `--duration-overlay-enter` |
| `Easings` | `--easing-<name>` | `--easing-ease-out`, `--easing-spring` |
| `Typography` | `--text-<name>` | `--text-sm`, `--text-base`, `--text-2xl` |
| `Leading` | `--leading-<name>` | `--leading-tight` (1.2: headings), `--leading-snug` (1.4: labels, captions, compact rows), `--leading-normal` (1.5: controls and body text), `--leading-relaxed` (1.6: reading text). A value is a unitless number or a px/rem/em length. Optional like `Strokes`: an unset step is not emitted and the kit draws its default |
| `Tracking` | `--tracking-<name>` | `--tracking-tighter` (-0.03em), `--tracking-tight` (-0.02em: display headings), `--tracking-snug` (-0.01em: titles, brand marks), `--tracking-wide` (0.04em), `--tracking-wider` (0.08em: eyebrows and small caps). A value is `0` or a px/rem/em length. Optional like `Strokes` |
| `Opacities` | `--opacity-<name>` | `--opacity-faint` (0.2), `--opacity-disabled` (0.5: a disabled control), `--opacity-muted` (0.6: secondary glyphs and labels). A value is a number from 0 to 1. Optional like `Strokes` |
| `FontWeights` | `--font-weight-<name>` | `--font-weight-normal` (400), `--font-weight-medium` (500), `--font-weight-semibold` (600), `--font-weight-bold` (700) |
| `Breakpoints` | `--breakpoint-<name>` | `--breakpoint-md` (informational; media queries can't read vars) |
| `Layout` | `--spacing-touch-target`, `--size-<name>` | `--spacing-touch-target` is the WCAG 2.5.5 minimum tap-target size (44px default); comfortable-density controls reach it through `--fui-density-control-h` (see component options), and pagination, inputs and the mobile hamburger summary read it directly. The `style.Size` fields are the dimensions a page is built around: `--size-page-width` (66rem, the column a site's header, main and footer share; `ui.Container`'s page width), `--size-page-gutter` (clamp(20px, 5vw, 32px), the side space outside it), `--size-header-height` (56px, which `ui.ContentRow`'s viewport mode subtracts), and `ui.Container`'s caps `--size-narrow-width` (640px), `--size-content-width` (1080px) and `--size-wide-width` (1280px) |
| `Code` | `--tk-<name>` | `--tk-kw`, `--tk-str`, `--tk-com`, the syntax-highlight colors code blocks read. Optional like `Strokes`: leave a slot unset and it falls back to the built-in palette. Dark values go in `Theme.DarkCode` (a map, like `DarkColors`) |
| `Knobs` | `--ui-<name>` | a map of per-component knob values (`"ui-button-edge": "var(--color-border-strong)"`, `"ui-checkbox-box-size": "20px"`), emitted in the theme's own `:root` block. Keys are `ui-` plus lower-case words joined by single dashes; values pass the same check as any free-form CSS value. See [Per-component knobs](#per-component-knobs-the---ui--variables) |

Token names come from the Go field path, converted to kebab-case
(`Colors.PrimaryFg` → `--color-primary-fg`). Set an explicit `Name` on
a token to override that. In component CSS written with
`style.ComponentSheet` or `ui.VariantCSS`, the `{group.name}` shorthand
resolves to the variable: `{colors.primary}` → `var(--color-primary)`,
`{spacing.lg}` → `var(--spacing-lg)`.

A few components also read a `--ui-*` custom property with a built-in
default. These are not theme tokens: a layout or a wrapping component
sets them for its own subtree.

| Property | Default | Read by |
|---|---|---|
| `--ui-control-padding-y` | `10px` | the block padding of buttons, text fields, selects, textareas, and the search, password, color, and grouped inputs |
| `--ui-detail-list-label-track` | `minmax(7rem, 13rem)` | `ui.DetailList`'s label column; a narrow panel caps it so values keep their line |
| `--ui-grid-min` | set by `GridConfig.Min` | `ui.Grid`'s minimum column width |

The desktop layouts in `battery/desktop/ui` set
`--ui-control-padding-y` to `4px` and the desktop theme lowers
`Layout.TouchTarget` to 24 (the comfortable density's control height),
which is how the same framework controls come out near native size in
a desktop window.

## The default look

`style.DefaultTheme()` and `theme.Default()` share one neutral zinc
palette: a near-black primary (`#18181B`) on a white page, one hairline
border (`#E4E4E7`), and one soft surface (`#F4F4F5`) for hover states,
secondary buttons and filled chips. The dark palette in
`theme.Default()` inverts it: a near-white primary on `#09090B`. Brand
colour is the host's call (`theme.Overrides.Primary`); every component
reads the tokens, so setting it re-colours primary buttons, links and
selected states together. Fonts are the platform's system stack, so the
default theme ships no font files.

A few rules hold across every component, so a page built only from kit
parts reads as one system:

- **One radius scale.** Controls use `--radii-md` (8px), cards and
  panels `--radii-lg` or `--radii-xl` (10px, 14px), small chips
  `--radii-sm` (6px).
- **One focus ring.** Every focusable part draws
  `outline: var(--stroke-focus) solid var(--color-text-subtle);
  outline-offset: var(--stroke-focus-offset)` on `:focus-visible`, in
  neutral grey rather than the brand colour. Re-skin it by changing
  `TextSubtle` and the two focus strokes, not per component.
- **One line weight.** Borders, dividers and inset rings read
  `--stroke-thin` and emphasised borders `--stroke-thick`; pill shapes
  read `--radii-full`, transitions `--duration-*` and stacking layers
  `--z-*`. A theme that sets `Strokes.Thin` to 3px, every radius to 0
  and the shadows to hard offsets restyles the whole kit with no
  component CSS. `gofastr verify` holds the kit to this: a bare width,
  radius, short duration or layer in kit CSS is GOFASTR1823
  ([contracts](contracts.md)).
- **Soft tones for status.** Badges, tags, chips and the pricing
  badge are soft fills: the tone tints the background and colours the
  text, never a solid saturated block.
- **Text controls are 16px on phones.** Inputs draw at `--text-sm` on
  desktop. Below the md breakpoint (767.98px) every text control
  (text field, textarea, select, combobox, tag, grouped, password,
  number and time inputs, search) goes back to `--text-base`, because
  iOS Safari zooms the page into a focused control whose text is under
  16px.

Two gate tests in `framework/ui` hold the second rule and the token
spellings: `TestFocusRingIsNeutral` fails on any registered sheet that
draws its ring in `--color-primary`, and `TestSheetVarsNameDeclaredTokens`
fails on a `var(--x)` that no theme or sheet declares (a misspelt token
falls back to its literal and ignores every re-theme). Component knobs
(`--ui-*`, `--fui-*`, `--cui-*`, `--hui-*`) are exempt, since a host
sets them. `TestTextControlsAreBaseSizeOnPhones` holds the phone rule.

## Setting the theme

Three entry points produce a `style.Theme` you pass to
`site.WithTheme(...)`:

- **`style.DefaultTheme()`**: the fully-populated, lower-level light
  baseline. It leaves `DarkColors` empty on purpose, for compatibility.
- **`framework/ui/theme.Default(theme.Overrides{Primary: "#0F766E", Dark: &theme.Overrides{Primary: "#5EEAD4"}})`**:
  the adaptive theme fresh scaffolds start with. It ships complete,
  contrast-safe light and dark palettes, plus a flat override struct for
  the tokens hosts change most often: the light palette, the dark
  palette (`Dark`, the same typed colour fields as the light), the
  three font stacks, and the radius scale. Light overrides are not
  copied into dark mode automatically, because contrast-safe values
  are usually different for dark; a light colour with no dark twin
  logs a warning naming it. Any field you don't set keeps its default.
- **`gofastr theme init`**: writes `theme/theme.go`, the full adaptive
  default as a literal you own and can edit directly. Use this for apps
  that will keep changing their theme over time; edit `Colors` and
  `DarkColors` together.

Check the result at `/__gofastr/app.css`; your values should show up
as `:root` custom properties.

## App tokens: `Theme.Extend`

When your app needs a value the built-in set doesn't have, a brand
accent, a hero spacing, a display weight, declare it as a token
rather than a literal in a stylesheet. Group your tokens in a struct
of typed fields and pass it to `Extend`:

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
site.WithTheme(t)
```

The field's type picks the prefix and its name the rest, the same rule
the built-in groups follow, so `--size-hero-gap` sits beside
`--size-page-width` and reads as one vocabulary. An explicit `Name`
overrides the field name. Nested structs work the way the built-in
groups do.

An app token goes everywhere a built-in token goes:

- the `:root` block, and every `ui.Themed` scope;
- the dark blocks, when `DarkColors` names an app colour (an app colour
  without a dark value shows up in the dark-palette boot warning, like
  a built-in one);
- `ThemeToTokens` and `ApplyTokens`, validated by its type (a `Size`
  takes a CSS length or a `calc()`/`clamp()`/`min()`/`max()` over
  lengths, never a bare word);
- `ThemeHash`, so the stylesheet URL changes when an app token does;
- `Validate`, which names the field when a value is missing
  (`main.brandTokens.HeroGap: Size.Value (Name="hero-gap"): value is empty`).

`Extend` copies what you pass it and leaves its receiver alone. It
panics when a token would emit a key the theme already emits, naming
both fields: a `Primary style.Color` in your struct would declare
`--color-primary` a second time, and whichever declaration the cascade
reached last would paint the page. `Validate` makes the same check, so
a hand-built `Theme.Extensions` slice can't slip a duplicate past it.

Framework components read only the built-in tokens.

## App tokens in CSS: `<name>.tokens.css`

Writing the struct by hand works, but the stylesheets that read your
tokens are CSS, and the tokens belong next to them. Declare them in a
`<name>.tokens.css` file instead and let `gofastr gen styles` write the
Go:

```css
/* acme.tokens.css */

/* Space above and below the home hero. */
@property --size-hero-gap { syntax: "<length>"; inherits: true; initial-value: clamp(2rem, 6vw, 5rem); }
@property --color-highlight { syntax: "<color>"; inherits: true; initial-value: #0F766E; }
@property --font-weight-display { syntax: "<number>"; inherits: true; initial-value: 800; }
@property --duration-unroll { syntax: "<time>"; inherits: true; initial-value: 260ms; }

@media (--dark) {
  :root { --color-highlight: #5EEAD4; }
}
```

A tokens file holds `@property` rules and, at most, one
`@media (--dark) { :root { … } }` block of dark colour values. Nothing
else: a class rule, another media query, or a dark value for a
non-colour token is an error. Each `@property`:

- is named `--<type>-<name>`, where the prefix is a token type
  (`color`, `font`, `spacing`, `radii`, `stroke`, `shadow`, `z`,
  `duration`, `easing`, `text`, `leading`, `tracking`, `opacity`,
  `font-weight`, `size`) and the name is
  lowercase
  kebab-case,
- declares the syntax that matches its type,
- says `inherits: true`, since a theme token must reach every element,
- has an `initial-value`, validated exactly as `ApplyTokens` validates
  that type.

| Prefix | Go type | `syntax` |
|---|---|---|
| `--color-` | `style.Color` | `"<color>"` |
| `--size-` | `style.Size` | `"<length>"` or `"<length-percentage>"` |
| `--text-` | `style.FontSize` | `"<length>"` or `"<length-percentage>"` |
| `--spacing-`, `--radii-`, `--stroke-` | `style.Spacing`, `style.Radius`, `style.Stroke` | `"<length>"` |
| `--leading-` | `style.LineHeight` | `"<number>"` or `"<length>"` |
| `--tracking-` | `style.LetterSpacing` | `"<length>"` |
| `--opacity-` | `style.Opacity` | `"<number>"` |
| `--font-weight-` | `style.FontWeight` | `"<number>"` or `"<integer>"` |
| `--z-` | `style.ZIndexValue` | `"<integer>"` |
| `--duration-` | `style.Duration` | `"<time>"` |
| `--font-`, `--shadow-`, `--easing-` | `style.Font`, `style.Shadow`, `style.Easing` | `"*"` |

Breakpoints and `--tk-*` code colours can't be app tokens; media
queries read the custom-media names, and code colours belong to the
built-in palette.

The generator writes `acme_tokens.gen.go` beside the file, in the
directory's package. The tokens are grouped the way `style.Theme`
groups its own, and a CSS comment above an `@property` becomes the
field's doc comment:

```go
// Code generated by "gofastr gen styles" from acme.tokens.css. DO NOT EDIT.
var Tokens = acmeTokens{
	Colors:      acmeColors{Highlight: style.Color{Name: "highlight", Value: "#0F766E"}},
	Durations:   acmeDurations{Unroll: style.Duration{Name: "unroll", Value: 260 * time.Millisecond}},
	FontWeights: acmeFontWeights{Display: style.FontWeight{Name: "display", Value: 800}},
	Sizes:       acmeSizes{HeroGap: style.Size{Name: "hero-gap", Value: "clamp(2rem, 6vw, 5rem)"}},
}

func (acmeTokens) DarkTokens() map[string]string {
	return map[string]string{"highlight": "#5EEAD4"}
}
```

A package with more than one tokens file names each var after its file
(`AcmeTokens`, `BrandTokens`). Add the set to the theme with `Extend`:

```go
site.WithTheme(theme.Default().Extend(ui.Tokens))
```

`Extend` reads `DarkTokens` from any value that has the method and
merges it into `DarkColors`, so the dark block above reaches
`data-color-scheme="dark"` with no extra line. It panics when a dark
key isn't one of the set's own colours, or when the value isn't a
colour. A theme with an empty `DarkColors` (`style.DefaultTheme()`)
has no dark mode, so the dark values are dropped with it.

Go code reads a token through its field: `ui.Tokens.Sizes.HeroGap.CSS()`
is `var(--size-hero-gap)`. CSS reads it as `var(--size-hero-gap)`.

`style.ParseToken(key, value)` is the parser underneath: it returns the
typed slot for a `--<type>-<name>` key and a CSS value, or the
validation error.

### What the checks enforce

`gofastr gen styles` and `gofastr verify` check owned sheets against
the built-in tokens plus every app token in the program, so a sheet in
one package can read a token declared in another.

- **GOFASTR1806**: in a `*.style.css`, `var(--name)` must name a token
  the theme or a tokens file declares. A fallback,
  `var(--brand-glow, #FF7A00)`, does not waive it: the fallback hides
  the missing declaration, and the value it carries is an untyped
  literal. Declare the token, or waive the line with a reason.
- **GOFASTR1807**: a literal equal to a token's value must read the
  token. This covers app tokens too, and font weights
  (`font-weight: 600` is `var(--font-weight-semibold)`) and sizes
  (`width`, `height`, `inline-size`, `block-size`, their `min-`/`max-`
  forms, and `flex-basis`). Padding, margin and gap compare against
  spacing only; `border-width` (and its side and logical forms),
  `outline-width`, `outline-offset` and `column-rule-width` compare
  against strokes (`outline-offset: 2px` is
  `var(--stroke-focus-offset)`). A `border` shorthand is judged whole,
  so `1px solid …` passes this rule; write `var(--stroke-thin) solid …`
  anyway, so a theme's line weight reaches it.
- **GOFASTR1821**: an app token whose value is already another token's
  value of the same type, built-in or app (`--color-brand: #18181B`
  where `--color-primary` is `#18181B`). Read the other token, or give
  the new one its own value.
- **GOFASTR1822** (warning): the same literal written in two or more
  owned sheets of one program for the same token type. Declare it once as a token and
  read `var()` in each. Values that are not a design choice pass: a zero
  in any unit (`margin: 0`, `padding: 0 0`), `100%`, and `z-index: -1`.

A tokens file may not reuse a built-in name (`--color-primary`), and a
token is declared in one file only.

The checks that compare files (duplicate style names, GOFASTR1821 and
GOFASTR1822, a token declared twice) judge one program at a time: a
`main` package plus every package its imports resolve to. Build
constraints are evaluated for each shipped platform (darwin and linux
on amd64 and arm64, windows on amd64), and a package under a nested
`go.mod` imports through that module's path. Two binaries in one module
may each carry their own `siteheader` copy, so
`generate package siteheader --out=alpha/siteheader` beside
`--out=beta/siteheader` generates both. Packages no `main` imports are
checked together as one group. A sheet two programs share is judged
against each program's tokens, not their union, because a binary
carries only its own. `gofastr gen styles` runs the same grouping as
`gofastr verify`, so the two cannot disagree. One gap remains: a build
tag that names no platform (a project's own `extra`) is treated as
set, so the imports of a `//go:build !extra` file are never followed.

When a line is deliberately off-token, waive the rule in place:

```css
.legacy { color: var(--vendor-ink); } /* gofastr:allow(GOFASTR1806) vendor widget sets this */
```

The marker starts the comment, names one rule and carries a reason. It
covers its own line when code comes before it, otherwise the next line
with code. A marker with no reason waives nothing.

`gofastr theme edit` edits the built-in tokens; app tokens stay in
their tokens file.

## Editing live: `gofastr theme edit`

`gofastr theme edit` boots a local theme configurator: a controls pane
(generated from `ThemeToTokens`) on the left and a live component preview
on the right. Changing a token value re-applies it through `ApplyTokens`,
registers the result as a theme variant, and swaps the preview's
`app.css?t=<key>`, the same path an embedded surface uses. No page
reload: the browser re-resolves every `var(--*)` reference against the new
`:root` values the moment the stylesheet link swaps.

```sh
gofastr theme edit                        # ephemeral loopback port, auto-open
gofastr theme edit --out=theme/theme.go   # write-back target (default)
gofastr theme edit --addr=127.0.0.1:8090 --no-open
```

**It is loopback-only.** The page carries its own bearer token and the
write-back endpoint rewrites a Go file on disk, so a non-loopback bind is
refused: a Host pin stops a browser from rebinding DNS onto the port, but not a
direct TCP client, which chooses its own Host and can simply read the token out
of the page. A bare `--addr=:8090` is read as `127.0.0.1:8090`.

**It starts from the framework default, not from your `theme.go`.** The tool
does not read an existing theme, so it is for arriving at a palette, not for
iterating on one you have already hand-edited: writing back over a file you
edited by hand replaces it with the defaults plus whatever you changed in that
session. That is why it refuses to overwrite an existing `--out` without
`--force`. To try a change against an existing theme, point `--out` at a new
path and copy across what you want.

The preview renders the `framework/gallery` catalog, every design-system
component against your theme, so you see the effect of a token change
across buttons, badges, cards, inputs, and the status tones at once.

The controls pane groups tokens — Colors first, then **Component
options**, then the rest. Component options (density, the button
treatment and radius, the field layout and radius) render as selects
whose options are exactly the members the option vocabulary accepts,
with the current value preselected: the list comes from
`theme.Options()` in `framework/ui/theme`, so a new option cannot
appear without its control following. Picking one applies through the
same `ApplyTokens` path as a typed token, and the API behind it refuses
a non-member value or an unknown `component.*` key with a 4xx, leaving
the working theme untouched.

**Contrast checking runs in the browser**, not in Go. `getComputedStyle`
resolves every colour space (`oklch()`, `color-mix()`, `var()`) natively.
The checker reads RGBA values through a canvas, composites text over the
measured probe background, then composites any transparent probe background
over the page background. A transparent page canvas falls back to white.
Pairs below 4.5:1 are flagged for both light and dark schemes. The pairs
checked are the ones `core-ui/style/theme.go` documents: text tiers on
`surface`, `primary-fg` on `primary`, `danger-fg` on `danger`, and each
status tone both as a white-text fill and as label text on its own 15%
tint. `Theme.Validate` additionally refuses, at boot, a hex
`primary` × `primary-fg` or `danger` × `danger-fg` pair below 4.5:1 —
in the light palette and, key by key with the light token as the
fallback for an absent key, in a non-empty `DarkColors` map.

**Write-back** emits `%q` string literals, then writes a temporary file in the
target directory, calls `fsync`, and renames it over the destination. Each
click re-checks the destination and refuses to replace a **hand-edited** file
unless the editor started with `--force`. A file this session already wrote is
its own to rewrite, so iterating on a palette is one Write per change rather
than one Write per process. If the directory already contains a Go file,
the generated file uses that package name; otherwise it uses the sanitized
directory name. The editor regenerates the file whole, so confirm before
replacing hand-edited values.

The tool binds loopback, pins the Host header (DNS-rebinding defence), and
mints a per-process bearer token delivered in a `<meta>` tag, the same
posture `gofastr harness --web` uses.

## Self-hosting web fonts

Setting `Fonts.Body`/`Fonts.Heading` to a custom family only names the
font; the browser still needs the actual font files, and **the
default CSP blocks CDN font URLs** (`default-src 'self'`; see
[security](security.md) → "Content-Security-Policy"). A `@font-face`
rule pointing at `rsms.me`, Google Fonts, or any other outside origin
fails silently, and the browser uses the fallback font instead.
Self-host the font instead:

1. Put the font files under your static dir:
   `static/fonts/inter.woff2` (serve it with
   `uihost.WithStaticDir("static")`).
2. Generate the `@font-face` rule with `style.FontFaceCSS` and pass it
   through `uihost.WithCustomCSS`:

   ```go
   css := style.FontFaceCSS("", style.WebFont{Family: "Inter"})
   // → @font-face { font-family: 'Inter'; font-style: normal;
   //     font-weight: 400 700; font-display: swap;
   //     src: url('/fonts/inter.woff2') format('woff2'); }
   ```

   The file name is derived from the family by `style.FontSlug`
   (`"Inter"` → `inter`, `"IBM Plex Mono"` → `ibm-plex-mono`), so the
   URL in the rule and the file you drop on disk cannot disagree. Set
   `File` to override it, and `Weight` / `Style` / `Display` to override
   the defaults above. Pass a first argument to serve fonts from
   somewhere other than `/fonts`.

   Writing the `@font-face` string by hand works too, but it is a second
   styling surface: `gofastr verify` reports it as `GOFASTR1801`, for
   the same reason it reports any other app-authored CSS.

3. Name the family in the theme tokens:

   ```go
   t.Fonts.Body.Value = `"Inter", ui-sans-serif, system-ui, sans-serif`
   t.Fonts.Heading.Value = t.Fonts.Body.Value
   ```

Same-origin URLs pass the default CSP with no changes needed. If you
really need to load a font from a third party, you have to override
`ContentSecurityPolicy` yourself; read the warning in
[security](security.md) before you do.

## Dark mode: `DarkColors` and `data-color-scheme`

`Theme.DarkColors` is a map from color-token name to its dark value
(`"background": "#15141B"`, …). `framework/ui/theme.Default()` and
`gofastr theme init` fill in a complete map; the lower-level
`style.DefaultTheme()` leaves it empty, for compatibility.

When the map isn't empty, the generated CSS re-declares those tokens
under `:root[data-color-scheme="dark"]`, plus a
`prefers-color-scheme: dark` fallback that only applies while the user
hasn't forced light mode. The **`data-color-scheme` attribute on
`<html>` is the actual switch**: `ui.ThemeToggle` and the color-scheme
bootstrap set it (and remember the choice), and any element reading a
theme CSS variable picks up the new color the moment it flips.

Which map is read when: `Colors` paints the light scheme, and
`DarkColors` re-declares the same custom properties under a dark
preference. Under a dark preference, editing `Colors` alone changes
nothing on screen; the dark value wins wherever one exists.

A partial `DarkColors` is not an error: tokens you leave out keep
their light values in dark mode, which is usually a contrast bug. The
UI host warns once at boot listing exactly which tokens those are
(`style.DarkPaletteGaps` computes the list), so the omission is
visible instead of silent.

The framework theme (`framework/ui/theme.Default()`) ships a complete
dark palette; `style.DefaultTheme()` is light-only by design.

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

## Section-level overrides: `ui.Themed`

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

`Themed` wraps the content in a `<div class="cui-theme-<hash>">`. The
override's token block ships in `app.css` scoped to that class, and
every component inside it reads `var(--color-…)` from that class
instead of from `:root`. Registering the same theme twice returns the
same handle, so its CSS only ships once.

### Scoped themes and dark mode

A registered override with a dark palette (`DarkColors` or `DarkCode`)
follows the document's scheme, not the wrapper's: the same two
selectors that flip the root theme flip the scope —
`[data-color-scheme="dark"] .cui-theme-<hash>` for the explicit toggle
and a `prefers-color-scheme` fallback that stops applying once the
user forced light. Flip `ui.ThemeToggle` (or set
`data-color-scheme` on `<html>`) and every scoped theme with a dark
palette recolors with the page.

A scope with **no** dark palette stays **light** in dark mode. Its
light declarations block inheritance, on purpose: a dark section on a
light page is a theme with a dark palette, not an accident of
inheritance. If you want a section to follow the page's scheme, give
its override the dark values too.

## Component options

Tokens retune the palette and the scales; **component options** decide
how a component family draws itself. They live in the theme, beside
the tokens, and travel the same roads (`ThemeToTokens`,
`ApplyTokens` under the `component.` prefix, the theme-edit writeback,
`ThemeHash`). Today the theme stores them and the compiler emits their
variables; no component stylesheet reads those variables yet, so an
option changes nothing on screen until the first rebuilt component
(Button, the next PR) consumes them:

```go
t := theme.Default(theme.Overrides{
    Components: theme.ComponentOptions{
        Density: theme.Compact,
        Button:  theme.ButtonOptions{Treatment: theme.Outline, Radius: theme.Square},
    },
})
```

The flattened form on `style.Theme` is a map —
`Components{"density": "compact", "button.treatment": "outline",
"button.radius": "square"}` — with a fixed grammar (lowercase
dot-separated keys, one lowercase word per value) that
`Theme.Validate` enforces at boot. The grammar is checked at boot; the
vocabulary — is `"cozy"` a density? — at boot when the styled layer is
linked (`framework/ui`'s compiler runs inside `Validate`), otherwise
at first render, where the compiler lives.

Two axes, and keeping them apart is the point:

- **Variant** (`ui.ButtonPrimary`, `ui.ButtonDanger`, ghost) is what a
  button *means*. A danger button says danger whatever the theme.
- **Treatment** (`theme.Filled`, `theme.Outline`, `theme.Soft`) is how
  a theme *draws* that meaning: where the ink goes. Primary and danger
  supply the semantic colour; the treatment decides fill against
  border. Changing the treatment restyles every variant at once — that
  is what it is for.

**Density versus explicit size:** `Density` (`theme.Comfortable`,
`theme.Compact`) retunes control heights and gaps theme-wide. The
comfortable height rides the `--spacing-touch-target` token (44px by
default — raise `Layout.TouchTarget` and comfortable controls grow
with it); compact is a deliberate 36px squeeze below that floor, and
the md/sm spacing step separates controls in each. An explicit `Size`
on one component always wins over density — density is the default
rhythm, not a ceiling. Reach for density when a whole screen should
tighten; reach for a size when one control must.

Zero values mean *unspecified* while overrides merge, and only then:
`theme.Default()` flattens a complete set (Comfortable, Filled,
Round), and an explicit `Comfortable`, `Filled` or `Round` in a later
override **resets** an earlier one rather than being ignored. Every
theme the framework builds therefore declares the full option set.

### How options reach CSS (and why they nest)

`core-ui/style` cannot draw the options — it does not know what a
density is. The one function that can is the **component-options
compiler** `framework/ui` registers from its `init`
(`style.RegisterComponentOptionsCompiler`, one per process; a late
registration panics because the host freezes `app.css` at first
render). It turns the flattened options into `--fui-*` custom
properties:

A binary that never imports `framework/ui` — a host built on
`framework/uihost` alone — registers no compiler: it stores options it
cannot draw, emits none of the `--fui-*` variables, and still hashes
option-different themes apart (`ThemeHash` fingerprints the flattened
options directly, not only the compiled output), so adding the styled
layer later cannot silently alias two themes that were distinct all
along.

Registration carries the framework's complete default set, and that
set is the **:root floor**: a theme with no `Components` of its own (a
bare `style.DefaultTheme()`, the `gofastr theme init` scaffold, a host
with no `App.Theme`) emits the defaults at `:root`, so the component
rules consuming `--fui-*` variables resolve on every host. A theme
that sets only some options gets them merged over the floor key by
key: `Components{"density": "compact"}` emits the compact density and
keeps the default filled, round buttons and stacked fields, so declare
only the options you change. Scoped themes get the same floor: a
`ui.Themed` override with no `Components` (a palette-only band built
from `style.DefaultTheme()`) still declares the full option set inside
its `.cui-theme-<hash>` blocks, so its primary button draws the
scope's `--color-primary`, not the page's. A key a scope leaves out
takes the framework default, not the enclosing scope's value; to
carry an option into a nested scope, set it on that scope too. The
floor does not touch a theme's identity: an optionless theme hashes
as optionless in every binary. There are still no in-CSS fallbacks
(`var(--x, fallback)`): the floor lives at the theme boundaries,
where one declaration covers every rule beneath it.

| Option | Emits |
|---|---|
| `density: comfortable` | `--fui-density-control-h: var(--spacing-touch-target)`, `--fui-density-gap: var(--spacing-md)` |
| `button.radius: round` / `square` / `pill` | `--fui-button-radius: var(--radii-md)` / `0` / `var(--radii-full)` |
| `button.treatment: filled` | `--fui-button-primary-bg: var(--color-primary)`, `--fui-button-primary-fg: var(--color-primary-fg)`, `--fui-button-primary-border: transparent`, and the same `-danger` trio from `--color-danger` / `--color-danger-fg` |
| `button.treatment: outline` | `--fui-button-primary-bg: transparent`, `--fui-button-primary-fg: var(--color-primary)`, `--fui-button-primary-border: var(--color-primary)`, and the `-danger` trio from `--color-danger` |
| `button.treatment: soft` | `--fui-button-primary-bg: color-mix(in srgb, var(--color-primary) 15%, transparent)`, `--fui-button-primary-fg: var(--color-primary)`, `--fui-button-primary-border: transparent`, and the `-danger` trio likewise |
| `field.layout: stacked` | `--fui-field-columns: minmax(0, 1fr)`, `--fui-field-message-column: 1 / -1` |
| `field.layout: inline` | `--fui-field-columns: minmax(8rem, 1fr) minmax(0, 3fr)`, `--fui-field-message-column: 2` |
| `field.radius: round` / `square` | `--fui-field-radius: var(--radii-md)` / `0` |

The `.fui-button--primary` and `.fui-button--danger` rules in the
`ui-button` sheet consume those trios (`background:
var(--fui-button-primary-bg)` and friends); secondary and ghost draw
themselves and read no treatment.

The field family's rules in the `ui-form-field` sheet consume
`--fui-field-columns` (the label/control track split),
`--fui-field-message-column` (which track the hint and error sit in)
and `--fui-field-radius` (what the field's inputs, selects and
summaries draw). Inline is a preference, not a promise: below a stated
width the sheet stacks the row whatever the theme asked for, the
control track's minimum is zero so a long value can never force
overflow, and a long label wraps rather than widening its track.
Choice rows (checkbox, radio, switch and their groups) keep their own
wrapping-label structure and deliberately ignore the columns
variables.

The cascade rule: **theme boundaries declare the option variables,
component rules consume them.** A component stylesheet writes
`border-radius: var(--fui-button-radius)` and never redeclares the
variable; a treatment that changes fill, text and border together is
three variables, never a descendant rule (`.cui-theme-a .fui-button`
would outrank the component's own variant and state selectors, and
could not nest). Because every theme declares the complete set, an
inner `ui.Themed` scope redeclares all of it and wins by proximity:
nesting A → B → A ends on A's values.

The declarations are re-emitted at every boundary — root and scope,
light and dark — because a custom property's `var()` references
compute where the declaration sits: `--fui-button-primary-bg:
var(--color-primary)` declared only at `:root` would carry the root's
resolved primary into a scope with its own palette.

**See it:** the product site renders the whole contract on one page under
each of five boot-registered themes, `/examples/headless/{theme}/landing`:
`default` (comfortable · filled · round), `dense` (compact · outline ·
square), `soft` (soft · pill, violet), `editorial` (filled · square, a
serif face) and `contrast` (outline · pill, 7:1 pairs), each with its own
dark palette. The page carries a theme switcher, the same palette under
two option sets, an A → B → A nest, and the browser proofs that read the
computed values (`examples/site/e2e_headless_landing_test.go`,
`examples/site/e2e_headless_themes_test.go`).

The `fui-` prefix is reserved for `framework/ui`'s class names and
option variables. Writing `fui-button` on your own markup gets the
framework's styling whenever that sheet is on the page. Headless has
its own prefix: the `data-hui-*` hooks belong to `framework/headless`.

## Token map: `ThemeToTokens` / `ApplyTokens`

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
is exactly what the `:root` block emits after the colon: `"#18181B"`,
`"8px"`, `"150ms"`. That key is chosen over a Go field-path key because it
is what the CSS emits, what a UI control edits, and stable across struct
reorganisations.

`DarkColors` / `DarkCode` (the dark-scheme maps) share their CSS var name
with the light token but live in a different selector scope, so they are
flattened under a `dark.` prefix to stay distinct:
`DarkColors["primary"]` → `"dark.color-primary"`, `DarkCode["kw"]` →
`"dark.tk-kw"`.
`Knobs` entries are flattened under a `knob.` prefix:
`Knobs["ui-button-edge"]` → `"knob.ui-button-edge"`. A `knob.` key
whose name is not `ui-` plus lower-case words, or whose value fails the
free-form check below, is refused like any other bad token.

`style.ApplyTokens(base, tokens)` returns a copy of `base` with the
supplied tokens applied. It **fails closed on every axis**:

- An unknown key is an error: a typo in a theme file is reported, and an
  embed can't probe for which keys a host accepts.
- A value must validate for its token's type. Colors accept a bounded
  grammar (hex, `rgb()`/`rgba()`, `hsl()`/`hsla()`, `oklch()`/`oklab()`,
  `color-mix()`, `var(--…)`, and the CSS named colors) and reject
  everything else. Integer/duration tokens must match their numeric
  format. Every free-form string (Font, Shadow, Easing, FontSize,
  CodeColor, knob values) is rejected if it contains a declaration-breaking sequence
  (`;`, `}`, `{`, `/*`, `*/`, `<`, `>`, `\`, a newline, or `url(`).

A value like `red; --x:}body{display:none}` escapes its CSS declaration;
CSS alone can then exfiltrate via attribute selectors and
`background-image` URLs. `ApplyTokens` rejects it rather than sanitising:
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
  `Theme.Validate()`. A spacing of `0px` parses fine but is a layout bug;
  call `Theme.Validate()` on the result for the name/value sanity checks
  `WithTheme` runs at boot.
- **Keying the map by Go field path.** `Colors.Primary` is not a valid key;
  `color-primary` is. Round-trip through `ThemeToTokens` once to see the
  exact key set a theme exposes.

## Per-component knobs: the `--ui-*` variables

Some components expose dimensions or accents that aren't global
tokens: a gallery's column count, a markdown block's reading measure.
These are exposed as `--ui-<component>-<knob>` variables with
built-in fallbacks, so a host can override them from its own
stylesheet without forking the component:

```css
/* app.css or a style.Contribute block */
:root { --ui-markdown-measure: 68ch; }
```

A dimension every page shares is a theme token instead: the page
column, its gutter, the header height and `ui.Container`'s caps live
in `Theme.Layout` (`t.Layout.WideWidth.Value = "1240px"`).

A theme sets them for the whole app through `Theme.Knobs`, which
emits them in the same `:root` block as the tokens (and in a scoped
theme's block, so a `ui.Themed` section can carry its own):

```go
t := theme.Default(theme.Overrides{})
t.Knobs = map[string]string{
    "ui-button-edge":       "var(--color-border-strong)",
    "ui-checkbox-box-size": "20px",
}
```

Knob values reach `ThemeToTokens` and `ApplyTokens` under a `knob.`
prefix (`"knob.ui-button-edge"`), so a theme editor round-trips them,
and `gofastr theme edit` writes them back to `theme.go`.

Some of the knobs:

| Knob | Default | What it sets |
|---|---|---|
| `--ui-rating-color` | `#D97706` (amber) | the filled glyph colour of `ui.Rating` and `ui.RatingInput`; heart and fire shapes default to `--color-danger`, thumb to `--color-primary`, diamond to `--color-info`; a value set on the rating or any ancestor overrides every shape |
| `--ui-form-max` | `42rem` | `ui.Form`'s maximum width, so a wide pane does not stretch every input across it; set `none` to fill |
| `--ui-copy-btn-size`, `--ui-copy-btn-bg`, `--ui-copy-btn-border`, `--ui-copy-btn-color`, `--ui-copy-btn-shadow`, `--ui-copy-btn-hover-bg`, `--ui-copy-btn-hover-color` | the outline button look | `ui.CopyButton`'s size and colours; the framed `ui.CodeBlock` head sets them for a quiet button on its dark chrome |
| `--ui-status-pill-font` | `inherit` | `ui.StatusPill`'s font family (set `var(--font-mono)` for a terminal-style pill) |
| `--ui-pricing-card-badge-fg` | `var(--color-text)` | the text colour of `ui.PricingCard`'s Recommended badge |

You can also scope them: set one inside a `ui.Themed` section, or on
a specific wrapper class, to change a single instance. Each
component's source lists its knobs next to the CSS that reads them
(for example `ui.Container`: `--ui-container-pad-start/end`); grep `framework/ui`
and `core-ui/app` for `--ui-` to see the full list.

## Why you can't just override component CSS

Component stylesheets and your site CSS don't always load in the same
order. At first paint, the host writes the page's component-CSS bundle
first and `/__gofastr/app.css` after it. But a component's CSS can
load lazily, after hydration, when it first shows up in an island
response, a widget, or an SPA navigation; then its `<link>` gets appended
to the end of `<head>`, after `app.css`. So a site rule with the same
specificity as a component's internal rule (`.fui-button { background:
… }`) wins on one page and silently loses on another, depending on how
that component's stylesheet arrived. Reaching for `!important` or a
higher-specificity selector "fixes" it today and breaks again the next
time the component changes.

Don't restyle component internals directly. Use one of these instead;
they work the same way no matter what order things loaded in:

- **Token values** (this doc) for anything the palette or scale
  controls.
- **`--ui-*` variables** for per-component knobs.
- **Registered variants** (`ui.RegisterButtonVariant`,
  `RegisterCardVariant`, `RegisterStatusVariant`, …) for a new named
  look; the variant CSS ships inside the component's own stylesheet
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
- **Writing `fui-` classes on your own markup.** The prefix belongs
  to `framework/ui`; a hand-written `fui-button` picks up the
  framework's styling whenever that stylesheet is loaded, today or
  after any release. Style your own markup with your own classes.
- **Redeclaring an option variable on a component.** A rule like
  `.my-button { --fui-button-radius: 0; }` blocks inheritance, so the
  component stops following the enclosing `ui.Themed` scope. Options
  are declared at theme boundaries (`theme.Overrides.Components`) and
  consumed by component rules; that is the whole contract.
- **Expecting a scope without a dark palette to follow dark mode.** It
  stays light, on purpose: its light declarations block inheritance.
  Give the override `DarkColors` if the section should flip with the
  page.
- **Confusing Variant with Treatment.** `ui.ButtonPrimary` is what the
  button means; `theme.Outline` is how the theme draws it. A variant
  is a per-component prop; a treatment is a theme-wide option.
