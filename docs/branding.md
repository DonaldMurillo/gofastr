# GoFastr branding

## Approved identity

Approved for current use on October 31, 2026: lowercase **go** with a
rightward arrow cut through both letters, and italic **fastr beneath the o**.
The arrow is transparent negative space, not a separately colored shape.
The symbol originated in Antigravity's selected concept 05; Codex composed
the under-o label and redrew the approved silhouette as smooth vector paths.
The `fastr` lettering is outlined from Arial Bold Italic. No font is needed
to display any delivered asset.

The canonical master is [`assets/brand/source/gofastr.svg`](../assets/brand/source/gofastr.svg).
All other logo files are exports of that master. Rejected explorations and
the retired amber/lambda artwork have been removed from the working tree.

## Versions

| Use | Assets in `assets/brand/` |
| --- | --- |
| Inline, theme-adaptive full logo | `gofastr-logo.svg` (`currentColor`) |
| Full logo on light surfaces | `gofastr-logo-light.svg`, `gofastr-logo-light.png` |
| Full logo on dark surfaces | `gofastr-logo-dark.svg`, `gofastr-logo-dark.png` |
| Default transparent PNG | `gofastr-logo.png` (same as light-surface version) |
| Symbol without `fastr` | `gofastr-symbol.svg`, `gofastr-symbol-light.svg/.png`, `gofastr-symbol-dark.svg/.png` |
| App/social icon | `gofastr-icon.svg`, `gofastr-icon.png` (512 × 512) |
| Standalone favicon | `gofastr-favicon.ico` (16, 32, 48 pixels) |
| Light/dark comparison | `gofastr-preview.png`, `gofastr-preview-light.png`, `gofastr-preview-dark.png` |

All logo and symbol PNGs are 1585 × 1065 with transparent backgrounds.
The SVGs contain only outlined paths: no embedded bitmaps, remote resources,
or live text. The silhouette is already a one-color/monochrome mark.
The icon uses the symbol on a charcoal tile so it remains visible against
either browser theme; use it instead of squeezing `fastr` into a favicon.

## Color and placement

- **Ink:** `#202725`; **warm white:** `#F5F2EB`.
- `-light` names the destination surface, not the logo's color: dark ink on
  light surfaces, warm white on dark surfaces. Neither export adds a background.
- Keep the transparent arrow cutout, original proportions, and the built-in
  16-unit clear space. Do not stretch, outline, add shadows, or split the colors.
- Use the full lockup at 120px wide or larger for general placements. The
  product header has a tested compact 76px treatment; use the symbol alone
  when the small label would otherwise become unreadable.
- External SVG images do not inherit the surrounding page's `currentColor`.
  Use the fixed-color variants for `<img>`/Markdown, or a `<picture>` with
  light/dark sources as in the README. Reserve the adaptive SVG for inline use.
- Supply the accessible name on the host link or image (`GoFastr`). The inline
  SVG is decorative and hidden from the accessibility tree.

## Application wiring

`assets/brand` embeds the canonical adaptive SVG and 512px icon. `brand.SVG()`
returns trusted static markup for inline rendering; `brand.IconPNG()` returns
an independent byte copy for `uihost.WithAppIcon`. These are brand assets,
not a runtime font or image-generation dependency.

The site header inherits its theme's text color, including explicit theme
selection. Its version capsule remains intact. UIHost derives the site's
favicon, touch icon, and 192/512px icons from the approved tile. The README
selects fixed-color light/dark logos with `<picture>`.

## Regeneration and checks

Requires Python 3 with Pillow and Chrome/Chromium. No new production dependency
is required. Set `CHROME_BIN` if Chrome is not in a standard location.

```sh
python3 assets/brand/generate.py
python3 assets/brand/generate.py --check
go run ./cmd/gofastr gen styles ./examples/site/siteheader
go test ./assets/brand ./examples/site/siteheader
go test ./examples/site -run TestSiteServesApprovedBrandIcon
```

Edit the canonical master, then regenerate; never independently redraw the
light and dark variants. The exporter validates path-only SVGs, identical
light/dark alpha masks, clear margins, icon dimensions, and ICO sizes.
`renders.sha256` records each raster's hash beside its source's, so
`--check` fails on a PNG or ICO left behind by an interrupted run.
Browser regression coverage checks mobile/desktop layouts, theme inheritance,
contrast, and the accessible home link. The site HTTP test verifies all icon
sizes and compares the served 512px icon to the approved source.
The icon pipeline passes through premultiplied RGBA; its pixel comparison
allows two 8-bit rounding steps at transparent edges, not a different image.

## Maintenance note

Scope: branding. Earlier attempts repeatedly restarted symbol exploration;
the user has now selected the go-arrow/under-o composition. Preserve that
identity and use the canonical master above. Next time, refine/export this
source rather than reviving rejected lambda/GF studies or upscaling the old
raster mockup. Keep all usage references pointed at `assets/brand/`.

## Name screen

Screened 2026-07-16. This is a practical collision check, not legal
trademark clearance.

- `gofastr` has prior software-package use: CRAN's archive holds three
  releases of an R package with that exact lowercase name from 2017.
  Nearby `Fastr` brands also operate in developer tooling and web software.
- Broad indexed searches did not surface an exact `GOFASTR` US trademark
  record. That is not a database clearance. Before commercial adoption,
  search the USPTO database for the exact wording, expanded wording,
  phonetic variants, and the relevant software and service classes, then
  repeat in other intended markets.
- The `GF` monogram category is crowded with stock interlocking marks, often
  orange on black. Avoid that treatment in a final mark.

References:

- [USPTO federal trademark search guidance](https://www.uspto.gov/trademarks/search/federal-trademark-searching)
- [USPTO trademark search](https://www.uspto.gov/trademarks/search)
- [CRAN archive for `gofastr`](https://cran.r-project.org/src/contrib/Archive/gofastr/)
