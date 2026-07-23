# Accessibility — built-in guarantees, the audit command, the build gate

GoFastr treats accessibility in three layers: the component library
ships correct semantics by default, `gofastr build` enforces the static
floor the type system can see, and `gofastr audit a11y --url` runs the
full axe-core engine against the running app for everything only a real
render can catch.

## What the framework already does

`framework/ui` and `core-ui` components carry their ARIA contract
internally — labelled landmarks, `aria-expanded` on disclosures,
`aria-selected` mirroring on tabs, focus traps on modals,
`aria-live` announcement regions, keyboard navigation on menus, trees,
carousels, and sortable lists. Composing the design system instead of
hand-rolling markup is the single biggest accessibility win an app can
make (see the UI architecture doc's hard rules).

The typed HTML layer makes the remaining requirements *visible in the
config struct*: `html.Image` has an `Alt` field, `html.Button` a
`Label`, `html.Nav` a `Label`/`LabelledBy` pair, `html.FieldSet` a
`Legend`. The audit below checks that you actually set them.

## `gofastr audit a11y` — the guided static lint

```
$ gofastr audit a11y
Accessibility lint — 2 issue(s) in 1 file(s)

app/screens/home.go:42: html.Image: missing required field "Alt" in ImageConfig
    fix: every image needs Alt. Informative image → describe what it shows
    ("Team photo at launch"). Decorative image → explicit empty Alt: "" so
    screen readers skip it. Never omit the field.
```

It scans every non-test, non-generated `.go` file for core-ui/html
element configs missing their required accessibility fields, and each
finding explains the rule — the goal is that the fix teaches WCAG
name/role/value basics, not just flags a line. Exit code 1 on findings,
so it can gate CI directly.

The lint understands the ARIA escape hatch: an `ExtraAttrs` literal
carrying `aria-label` / `aria-labelledby` / `role` satisfies the
matching typed field (icon-only buttons are the canonical case). Two
deliberate scope limits: a config built in a variable
(`cfg := html.ImageConfig{…}; html.Image(cfg)`) and a non-literal
`ExtraAttrs` value can't be inspected statically, so they pass the lint
— the elements' own runtime validation (`html.Button` panics without an
accessible name) and the axe runtime scan cover those paths.

Checked elements: `Image` (Alt), `Button` (Label), `Link`/`LinkHTML`
(Href + text), `Nav`/`Section`/`Aside` (Label or LabelledBy), `Group`
(Role), `Label` (For + Text), `Input`/`Select`/`TextArea` (Name),
`FieldSet` (Legend), `Heading` (Level), `Form` (Method), `Abbr`
(Title), `Time` (Datetime), `Source` (Src + Type).

## The build gate

`gofastr build` runs the same lint between `go vet` and compilation and
**fails the build** on findings, printing the guided report. The rules
are cheap (pure static analysis, no browser) and every finding has a
concrete fix, so the default is enforcement. `--no-a11y` skips the gate
when you genuinely need a build anyway — treat it like `//nolint`, not
like a setting.

## `gofastr audit a11y --url` — the full runtime audit

```
$ gofastr audit a11y --url http://localhost:8080
Auditing 14 page(s) at http://localhost:8080 under 2 color scheme(s)…

Audited 14 of 14 discovered pages.

/pricing (dark scheme)
  [serious] color-contrast: Elements must meet minimum color contrast ratio thresholds
      guide: https://dequeuniversity.com/rules/axe/4.10/color-contrast
      at: .pricing-card__footnote
```

This drives headless Chrome with the vendored axe-core engine (the same
harness the framework's own example gates use — hermetic, no CDN fetch)
and audits **both color schemes**: contrast, focus order, landmark
structure, ARIA validity — the classes of failure only a rendered page
exposes. Pages are discovered from the app's `/sitemap.xml`
(`uihost.WithSitemap`), so a sitemap-configured app gets full-site
coverage with zero flags; use `--pages /a,/b` to scope. Exit code 1 on
violations.

For an authenticated app, pass the same credentials a user enters on the
auth battery's `/login` page:

```
gofastr audit a11y --url http://localhost:8080 \
  --email admin@example.com --password "$ADMIN_PASSWORD" \
  --pages /admin,/admin/e/products
```

The auditor fills `input[name=email]` and `input[name=password]`, then clicks
the login form's submit button so the app's own submit event, session cookie,
and redirect all run normally. With no explicit page list, sitemap discovery
runs after login in that authenticated browser session.

Every run reports `Audited N of M discovered pages`. Redirected pages are
listed under `Could not reach` and make the command exit 1 even when axe found
no violations on the reachable pages. A run whose only page is `/login` also
exits 1 with a coverage warning; it is not evidence that authenticated pages
are clean.

Requires a Chrome/Chromium install (headless). Run it against `gofastr
dev`'s server during development, or against a staging deploy in CI.

## The axe test harness — pin the gate as a test

`framework/testkit/axetest` is the same harness the audit command and
the framework's own example gates use, exported so a host app can pin
the runtime audit into its own suite:

```go
import "github.com/DonaldMurillo/gofastr/framework/testkit/axetest"

func TestAxeAllPagesClean(t *testing.T) {
    browser := axetest.NewBrowser(t)
    for _, page := range pages { // derive from your screen catalog
        for _, scheme := range axetest.Schemes {
            tab, done := axetest.NewTab(t, browser)
            // navigate tab to the page, then:
            chromedp.Run(tab, axetest.Prepare(scheme))
            violations, err := axetest.Scan(tab, scheme, allowlist)
            // fail on violations…
            done()
        }
    }
}
```

Derive the page list from the same source your screens register from
(a catalog, `app.Routes()`) so a new screen is scanned automatically —
a hand-maintained list drifts. `examples/site/axe_test.go` is the full
reference pattern (per-page allowlists with justifications, mobile
target-size pass).

Every successful `Scan` also records the scanned path into
`.gofastr/axe-coverage.json` (the axe-coverage manifest,
`framework/axecov`) — a per-project record of which pages the axe suite
actually exercised. The manifest is a local build artifact — gitignored,
wiped by `make clean`, never shipped. `GOFASTR_AXE_COVERAGE=0` disables
recording. Apps that opt into `uihost.WithStrict()` fail dev boot for
any page route the manifest doesn't cover — every screen must have an
axe test (`gofastr docs strict-mode`).

## Recommended loop

1. Compose `framework/ui` components; reach for `core-ui/html` only for
   genuinely bespoke fragments.
2. Let `gofastr build` keep the static floor green (it's on by default).
3. Before shipping UI changes: `gofastr audit a11y --url
   http://localhost:8082` and fix what axe reports in both schemes.
4. For apps with their own test suites, pin the runtime gate as a test —
   see `examples/site/axe_test.go` for the reference pattern (per-page
   allowlists with justifications, mobile target-size pass).

## Common mistakes

- **`Alt: ""` versus no `Alt` at all.** The empty string is a deliberate
  "decorative, skip me" signal; omitting the field entirely is the bug
  the lint flags. Decide which one the image is — don't silence the
  finding with filler text like "image".
- **Silencing the build gate permanently.** `--no-a11y` is an escape
  hatch for a blocked build, not a project setting. If a rule seems
  wrong for a real case, the element probably isn't the right primitive
  (a `Section` that needs no label may just be a `Div`).
- **Auditing only one color scheme.** Contrast regressions hide in the
  scheme your machine doesn't use — the runtime audit forces dark AND
  light on every page for exactly this reason. Don't scope it back down.
- **Running the runtime audit without a sitemap or page list.** Without
  `uihost.WithSitemap` the scan falls back to `/`. The coverage line makes
  that narrow run visible, but it still cannot discover routes that were
  never advertised. Configure the sitemap or pass `--pages` explicitly.
- **Auditing protected routes without credentials.** A redirect to `/login`
  is reported as unreachable and fails the run. Pass `--email` and
  `--password`; both are required together.
- **Fixing the symptom in CSS.** An axe `color-contrast` finding on a
  component means the *token* is wrong (`--color-text-subtle` on
  `--color-surface`), not that one selector needs an override — fix the
  theme so every component inherits the correction.
