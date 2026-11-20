# Accessibility

GoFastr enforces accessibility in three layers: accessible semantics built into `framework/ui`, a static lint gate built into `gofastr dev` and `gofastr build`, and a headless-Chrome axe-core runtime audit (`gofastr audit a11y --url`) plus Go test harness (`framework/testkit/axetest`).

## Built-in component guarantees

- **Semantic defaults:** `framework/ui` components handle landmarks, `aria-expanded`, `aria-selected`, native `<dialog>` modals, live regions, and keyboard navigation out of the box.
- **Form validation & focus:** fields show errors only after blur or submit (`:user-invalid` mirrored to `aria-invalid` by `headless-validity`), refused submits announce once through a polite live region (`#cui-form-errors`) and move focus to the first invalid field, and hints sit between label and input.
- **Document language & text scaling:** every page emits `<meta name="text-scale" content="scale">` and defaults `<html lang="en">` (override with `app.WithLang(...)` or `uihost.WithLang(...)`).
- **Typed HTML requirements:** `core-ui/html` surfaces required accessibility fields directly on config structs (`html.Image` `Alt`, `html.Button` `Label`/`AriaLabel`, `html.Nav` `Label`/`LabelledBy`, `html.FieldSet` `Legend`).

## Static lint and build gate (`gofastr audit a11y`)

```bash
gofastr audit a11y
```

Scans non-test, non-generated `.go` files for missing required accessibility fields on `core-ui/html` and `framework/ui` configs (`Image`, `Button`, `Link`, `Nav`, `Section`, `Aside`, `Group`, `Label`, `Input`, `Select`, `TextArea`, `FieldSet`, `Heading`, `Form`, `Abbr`, `Time`, `Source`), printing concrete fix hints and exiting `1` on findings.

Both `gofastr dev` and `gofastr build` run this lint automatically and block builds on findings (`--no-a11y` is an emergency escape hatch, not a project setting).

## Runtime audit (`gofastr audit a11y --url`)

```bash
gofastr audit a11y --url http://localhost:8080

# Authenticated pages (submits the /login form first):
gofastr audit a11y --url http://localhost:8080 \
  --email admin@example.com --password "$ADMIN_PASSWORD" \
  --pages /admin,/admin/entities/products
```

Drives headless Chromium with vendored axe-core (zero CDN calls) across **both light and dark color schemes**, discovering routes from `/sitemap.xml` (or `--pages`). Unreachable/redirected pages fail the run so a silent `/login` redirect never reports a false pass.

## Pinning axe in `go test` (`framework/testkit/axetest`)

```go
import "github.com/DonaldMurillo/gofastr/framework/testkit/axetest"

func TestAxeAllPagesClean(t *testing.T) {
	browser := axetest.NewBrowser(t)
	for _, page := range pages {
		tab, done := axetest.NewTab(t, browser)
		if err := chromedp.Run(tab, chromedp.Navigate(base+page), axetest.WaitReady()); err != nil {
			t.Fatalf("load %s: %v", page, err)
		}
		violations, err := axetest.ScanSchemes(tab, axetest.Schemes, allowlist)
		// fail on err or violations
		done()
	}
}
```

Use `axetest.ScanSchemes` to scan both schemes on one loaded page. Every successful `Scan` records the final resolved path in `.gofastr/axe-coverage.json` (`framework/axecov`), which `uihost.WithStrict()` checks in dev mode so new screens cannot skip axe coverage (see [Strict mode](strict-mode.md)).

## Common mistakes

- **Omitting `Alt` instead of setting `Alt: ""` on decorative images:** explicit `""` tells screen readers to skip the image; omitting `Alt` fails the static lint.
- **Using `--no-a11y` as a permanent flag:** fix the underlying primitive instead (for example use `Div` when a container is not a landmark `Section`).
- **Auditing only one color scheme:** contrast regressions hide in dark mode; always run both schemes.
- **Auditing protected routes without `--email` / `--password`:** redirects to `/login` fail the run as unreachable pages.
- **Patching contrast findings with ad-hoc CSS:** fix the theme token (`--color-text-subtle`, `--color-surface`) so every component inherits the fix.
