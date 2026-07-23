# Strict mode — launch hygiene as boot failures

`uihost.WithStrict()` makes the host refuse to serve an app whose
declared surface is missing the things a public site should never ship
without: page titles and descriptions, a site description, an icon, a
sitemap, robots directives, and — in dev — an accessibility test for
every screen. All findings are reported at once, each with its remedy,
as a boot panic at Mount time: a strict app either passes every check
or never takes traffic.

Strict mode is opt-in — one option:

```go
host := uihost.New(ui,
    uihost.WithStrict(),
    uihost.WithDescription("A tiny notes app."),
    uihost.WithAppIcon(iconPNG),
    uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://notes.example"}),
    uihost.WithRobots(uihost.RobotsConfig{}),
)
```

## The checks

| Check | Applies to | Passes when |
|---|---|---|
| Screen title | every page screen | `Screen.Title` is set — implement `ScreenTitler` or register with `WithTitle` |
| Screen description | every page screen | `Screen.Description` is set (`ScreenDescriber`), or the component implements `ScreenSEO` — a zero-value return is the documented "deliberately naked" opt-out |
| Site description | the host | `WithDescription` with a non-empty value |
| Site icon | the host | `WithAppIcon` (preferred — one source image derives the whole icon surface) or `WithFavicon` |
| Sitemap | the host | `WithSitemap` |
| Robots | the host | `WithRobots` |
| Axe coverage | every page screen, **dev only** | the axe-coverage manifest records at least one scan that resolves to the route |

Drawers, sheets, and dialogs are exempt from the screen checks — they
render inside a page and own no `<head>`.

## The axe-coverage check

Every successful `framework/testkit/axetest.Scan` records the page it
scanned into `.gofastr/axe-coverage.json` (see `framework/axecov` and
the accessibility doc). Under `gofastr dev`, strict mode diffs that
manifest against the app's page routes and fails boot for any route no
axe test covered. Coverage follows the router, not string equality: a
recorded scan of `/docs/install` covers the `/docs/:slug` pattern.

Production boots skip this check — the manifest is a local test
artifact (gitignored, wiped by `make clean`) that never ships, so a
deploy can't fail on its absence. The enforcement point for axe
coverage is the dev loop and CI, where the tests that write the
manifest actually run.

Two consequences worth knowing:

- **A fresh clone fails strict dev boot until the axe suite has run
  once.** That is by design — run `go test ./...` (or just the axe
  gate) and the manifest regenerates.
- **Deleting a screen never breaks the check.** Stale manifest entries
  that resolve to no route are ignored.

## Relaxing a finding

There is no per-check severity config and no warn level. The intended
escape hatches are the same declarations normal apps use:

- A page that should have no SEO: implement `ScreenSEO` returning the
  zero value.
- A page that shouldn't exist as a page: register it as a drawer,
  sheet, or dialog if that's what it really is.
- An app that isn't ready for the bar: remove `WithStrict()`. It is a
  single option, not a mode with degrees.

## Common mistakes

- **A hand-written axe page list.** The strict check passes today and
  fails the first time someone adds a screen without extending the
  list — which is exactly the drift the check exists to catch. Derive
  the axe gate's page list from the same catalog or `app.Routes()`
  your registration uses, so a new screen is scanned (and recorded)
  automatically.
- **Expecting the axe check to guard production boots.** It runs under
  `gofastr dev` only; production has no manifest to check against. CI
  is the production-facing enforcement point — the axe suite itself
  fails there on violations, and a strict dev boot fails on gaps.
- **Silencing a description finding with filler text.** "Welcome to
  our site" on every screen satisfies the check and hurts the site.
  The honest opt-out for a page with nothing to say is a zero-value
  `ScreenSEO` return, not copy that exists to pass a gate.
