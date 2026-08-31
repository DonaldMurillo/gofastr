# Strict mode: launch hygiene as boot failures

`uihost.WithStrict()` makes the host refuse to serve an app whose
declared surface is missing the things a public site should never ship
without: page titles and descriptions, a site description, an icon, a
sitemap, robots directives, and, in dev, an accessibility test for
every screen. All findings are reported at once, each with its remedy,
as a boot panic at Mount time: a strict app either passes every check
or never takes traffic.

Strict mode is opt-in. Apps scaffolded by `gofastr generate` ship with
it on (and with a surface that passes every check); existing apps add
one option:

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
| Screen title | every page screen | `Screen.Title` is set: implement `ScreenTitler` or register with `WithTitle` |
| Screen description | every page screen | `Screen.Description` is set (`ScreenDescriber`), or the component implements `ScreenSEO`: a zero-value return is the documented "deliberately naked" opt-out |
| Site description | the host | `WithDescription` with a non-empty value |
| Site icon | the host | `WithAppIcon` (preferred: one source image derives the whole icon surface) or `WithFavicon` |
| Sitemap | the host | `WithSitemap` with a valid `BaseURL`: a bare http/https origin (host, no userinfo/query/fragment/path; deployment path prefixes belong in the static Builder's `BasePath`) |
| Robots | the host | `WithRobots` |
| Internal links | the site chrome (every layout's header, sidebar, footer) | every internal `href` resolves to something the app serves: a registered screen (dynamic routes included), a static file, a configured artifact endpoint, or a GET route on the framework router |
| Axe coverage | every page screen, **dev only** | the axe-coverage manifest records at least one scan that resolves to the route |

Drawers, sheets, and dialogs are exempt from the screen checks: they
render inside a page and own no `<head>`. Screens a battery registers
(the admin back-office, for example) are also outside the checks:
batteries Init at `App.Start`, after Mount, so strict mode covers
exactly the surface the app itself declared.

## The internal-link check

The chrome is where broken links are born: a footer's Legal column, a
header's nav, a sidebar's entries are all declared as data at wiring
time, and nothing stops them naming a path no screen registers — a
generated app shipped exactly that (footer links to `/terms` and
`/privacy`, neither registered) until the check existed. At Mount,
strict mode renders every layout's chrome — the default layout plus
each screen's own — with a background context, the same computation
`Layout.Wrap` performs on every request, and resolves each internal
`href` against the app's full served surface:

- registered screens, through the router itself, so a concrete link
  like `/docs/install` is covered by the `/docs/:slug` pattern and a
  trailing-slash variant resolves to its canonical screen;
- static files (`WithStaticDir` / embedded FS) and the favicon path;
- artifact endpoints Mount registers from config, gated the same way
  Mount gates them: `/sitemap.xml` with `WithSitemap`, `/robots.txt`
  with `WithRobots`, `/manifest.webmanifest` and `/service-worker.js`
  with `WithPWA`, `/llms.txt` with the agent-ready surface — a link to
  `/sitemap.xml` without `WithSitemap` is a dead link and stays a
  finding;
- any GET route the framework router registered before Mount (entity
  CRUD, custom endpoints), so a sidebar's "Export CSV" pointing at an
  endpoint is not a finding. UIHost's own `/__gofastr/` namespace is
  exempt by prefix.

Deliberately out of scope: external URLs and every scheme reference
(`mailto:`, `tel:`), protocol-relative and relative references,
fragments, query-only references, and template placeholders
(`/docs/{slug}`, `/users/:id`) — none of them names a route the app
must register. Form actions and RPC attributes target handlers, not
pages, and are not link-checked.

Two honest limits. A chrome component that needs a request to render
(a header that branches on the signed-in user) is rendered logged-out:
the links only that branch emits are not checked — a warning names any
chrome that fails to render at boot. And the outer shells of nested
screen groups whose parent and child declare *different* layouts are
not enumerated; the exportable router surface reaches the default
layout and each screen's innermost layout.

`ExemptScreens` applies one level up from usual: an entry exempts
links *pointing under it*, the same vocabulary that exempts routes.
`ExemptScreens: []string{"/machine/*"}` silences findings for chrome
links into `/machine/…` while leaving every other link checked.

## The axe-coverage check

Every successful `framework/testkit/axetest.Scan` records the page it
scanned into `.gofastr/axe-coverage.json` under the canonical coverage
root: `GOFASTR_AXE_COVERAGE_DIR` when set, else Go's own root rule
(nearest `go.work` ancestor, else nearest `go.mod`, else the working
directory: `axecov.DefaultDir`). Strict mode reads with the same
resolution, so the loop holds even when tests live in `cmd/app/` while
`gofastr dev --dir <root> --pkg ./cmd/app` serves from the root, and in
`go.work` workspaces. Known limitation: several apps sharing one
module/workspace share one manifest keyed by route path, so two strict
apps with identical routes can satisfy each other's check; set
`GOFASTR_AXE_COVERAGE_DIR` per app (for both its tests and its server)
when that matters. Under `gofastr dev`, strict mode diffs that manifest against
the app's page routes and fails boot for any route no axe test covered.
Coverage follows the router, not string equality: a recorded scan of
`/docs/install` covers the `/docs/:slug` pattern.

The demand surface mirrors the sitemap's discovery surface: a dynamic
route (`/orders/:id`) is only demanded when its screen's
`StaticPaths(ctx)` returns at least one concrete instance; then the
sitemap lists those instances and the gate scans them. A dynamic route
without `StaticPaths` (or whose `StaticPaths` returns nothing) is
invisible to a sitemap-driven gate, so strict skips it and logs a loud
warning naming the route instead of demanding the impossible. Return
concrete instances (or scan one and keep it in your gate's page list)
to bring it under coverage.

Production boots skip this check: the manifest is a local test
artifact (gitignored, wiped by `make clean`) that never ships, so a
deploy can't fail on its absence. The enforcement point for axe
coverage is the dev loop and CI, where the tests that write the
manifest actually run.

Absence and drift are treated differently, on purpose:

- **No manifest at all** (fresh clone, fresh `gofastr generate`: the
  axe suite has simply never run in this checkout) **warns loudly and
  serves.** First boot is never walled off behind a Chrome run; run
  `go test ./...` once and the manifest exists from then on.
- **A manifest that exists but misses a route fails boot.** That is
  real drift: a screen was added without extending the axe gate.
- **A manifest that exists but cannot be parsed fails boot too.**
  Corruption is not absence: relaxing enforcement exactly when the
  coverage record is untrustworthy would invert the guarantee. Delete
  the file and re-run the axe suite.
- **Deleting a screen never breaks the check.** Stale manifest entries
  that resolve to no route are ignored.

## Configuring every check

`WithStrict()` with no arguments enforces everything. Pass a
`StrictConfig` to tune each check individually: the zero value of
every field is the strictest setting, so configuration only ever
*relaxes*, and every relaxation is visible in review:

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/uihost"
-->
```go
uihost.WithStrict(uihost.StrictConfig{
    ScreenDescriptions: uihost.StrictWarn,          // log, don't fail
    Robots:             uihost.StrictOff,           // skip entirely
    AxeManifestMissing: uihost.StrictAbsenceEnforce, // unproven checkout must not serve
    ExemptScreens:      []string{"/machine/feed", "/internal/*"},
})
```

| Field | Governs | Zero value |
|---|---|---|
| `ScreenTitles` | per-screen title check | enforce |
| `ScreenDescriptions` | per-screen description check | enforce |
| `SiteDescription` | `WithDescription` present | enforce |
| `SiteIcon` | `WithAppIcon`/`WithFavicon` present | enforce |
| `Sitemap` | `WithSitemap` present | enforce |
| `Robots` | `WithRobots` present | enforce |
| `InternalLinks` | chrome `href`s resolve to a served path | enforce |
| `AxeCoverage` | per-screen axe scan recorded (dev only) | enforce |
| `AxeManifestMissing` | posture when no manifest exists at all | **warn** (`StrictAbsenceWarn`) |
| `ExemptScreens` | routes the per-screen checks skip: exact pattern or `/prefix/*` | none |

Levels are `StrictEnforce` (fail boot), `StrictWarn` (`slog.Warn`,
serve), `StrictOff` (skip). `AxeManifestMissing` uses its own trio
(`StrictAbsenceWarn`/`StrictAbsenceEnforce`/`StrictAbsenceOff`)
because its default is warn; see above.

Option composition is last-wins and total: a later bare `WithStrict()`
resets an earlier relaxed config back to all-enforced, and passing more
than one `StrictConfig` to a single call panics at construction.

Prefer the declaration-level escape hatches before reaching for
config, though; they carry more information:

- A page that should have no SEO: implement `ScreenSEO` returning the
  zero value.
- A page that shouldn't exist as a page: register it as a drawer,
  sheet, or dialog if that's what it really is.
- A whole subtree outside the bar (machine-facing endpoints, internal
  tooling): `ExemptScreens` with a `/prefix/*` entry.

## Common mistakes

- **A hand-written axe page list.** The strict check passes today and
  fails the first time someone adds a screen without extending the
  list, which is exactly the drift the check exists to catch. Derive
  the axe gate's page list from the same catalog or `app.Routes()`
  your registration uses, so a new screen is scanned (and recorded)
  automatically.
- **Expecting the axe check to guard production boots.** It runs under
  `gofastr dev` only; production has no manifest to check against. CI
  is the production-facing enforcement point: the axe suite itself
  fails there on violations, and a strict dev boot fails on gaps.
- **Silencing a description finding with filler text.** "Welcome to
  our site" on every screen satisfies the check and hurts the site.
  The honest opt-out for a page with nothing to say is a zero-value
  `ScreenSEO` return, not copy that exists to pass a gate.
