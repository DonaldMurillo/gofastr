# Pattern-CSS unification

**Status:** shipped 2026-05-19.

## Problem

GoFastr had two CSS contracts:

| Layer | Mechanism | Auto-wired? |
|---|---|---|
| `framework/ui/*` | `registry.RegisterStyle("<name>", styleFn)` + `Style.WrapHTML(...)` emits a `data-fui-comp="<name>"` marker; the runtime scans rendered HTML and `<link>`s only what's used | yes |
| `core-ui/patterns/*` | exported `BaseCSS() string`; host app had to import the package AND concatenate it into `WithCustomCSS` in their theme.go | no |

Every new pattern was one missed concatenation away from shipping
invisible. The failure mode triggered on 2026-05-19: the nestedlist
pattern landed with DOM-passing e2e tests but rendered with browser
default red underlined links on the live site because the host's
theme.go was never updated to add `nestedlist.BaseCSS()`.

## Decision

Unify on `registry.RegisterStyle`. Pattern packages now wrap their
top-level rendered element with `Style.WrapHTML(...)` so the
`data-fui-comp` marker rides on every rendered instance, and the
existing runtime auto-loader picks them up the same way it picks up
`framework/ui/*` components.

Class selectors stay class-based (`.accordion`, `.tabs`, `.nested-list`)
— the marker only signals to the auto-loader "fetch this stylesheet."
Patterns don't need to scope-wrap their selectors with the marker
attribute.

`BaseCSS()` exports are forbidden. A build-time lint
(`core-ui/check.LintNoPatternBaseCSS`) fails the test suite if any
new `core-ui/patterns/*` package re-introduces the legacy export.

## Migration shape (per pattern)

```go
// Before
package nestedlist
func Render(cfg Config) render.HTML { return render.Tag("div", ...) }
func BaseCSS() string                { return `.nested-list { ... }` }

// After
package nestedlist

var Style = registry.RegisterStyle("nestedlist", styleFn)
func styleFn(_ style.Theme) string { return baseCSS }
func Render(cfg Config) render.HTML {
    return Style.WrapHTML(render.Tag("div", ...))
}
const baseCSS = `.nested-list { ... }`
```

For patterns that need dynamic CSS (e.g. tabs's `:has()` rule
generation for N tab slots), the function form stays — `styleFn`
just calls a builder:

```go
var Style = registry.RegisterStyle("tabs", styleFn)
func styleFn(_ style.Theme) string { return buildCSS() }
func buildCSS() string { /* generate :has() rules */ }
```

## What also had to change

- **Test asserts on exact `<nav aria-label="X">` strings** had to
  relax to `aria-label="X"` — the wrapper now also carries
  `data-fui-comp="<name>"`, so the literal substring no longer
  matched. Touched: breadcrumbs, pagination, framework/ui datatable,
  examples/website smoke test.
- **Skeleton-preset line widths** had to move from `Width:"50%"`
  (inline `style="inline-size:50%"`, blocked under strict CSP)
  into the registered preset CSS as classes like
  `.ui-skeleton-card__title { inline-size: 50%; }`.
- **Demo prose mentioning `style="…"`** as documentation text was
  matched by the no-inline-style drift regex and had to be reworded.

## Outcome

- `examples/website/theme.go` lost 6 pattern imports + 6 `.BaseCSS()`
  concatenations. Host apps now get pattern CSS auto-wired with no
  setup — same contract as `framework/ui/*` components.
- 33 packages under `core-ui/...` + `framework/...` green.
- 7 of the existing `core-ui/patterns/*` packages now register
  (combobox / disclosure / infinitescroll / multiselect / sortablelist
  / tree were already on the contract; accordion / breadcrumbs /
  nestedlist / pagination / progress / skeleton / tabs migrated in
  this round).
- The lint guard catches the next regression at build time.

## Where the contract is documented

- `core-ui/ARCHITECTURE.md` — "Patterns use the same contract"
  subsection under Component CSS.
- `.claude/skills/component-build/SKILL.md` — "CSS contract: one
  registry, no manual wiring" plus an explicit anti-pattern entry
  forbidding `BaseCSS()` exports.
- This proposal — design rationale + migration shape.
- `core-ui/check/nopatternbasecss.go` — the lint rule + the test
  (`TestNoPatternBaseCSS_RepoIsClean`) that enforces it on every
  `go test ./...`.

## Related session memory

- `feedback_verify_visually.md` — the 2026-05-19 nestedlist incident
  that prompted this. DOM-passing e2e tests missed the bug because
  the classes were on the markup but the CSS wasn't loaded.
