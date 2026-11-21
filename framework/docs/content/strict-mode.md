# Strict mode (`uihost.WithStrict`)

`uihost.WithStrict()` turns launch-hygiene omissions into immediate boot failures — listing every missing title, description, icon, sitemap, robots config, broken chrome link, and (in dev) missing axe accessibility test all at once so incomplete public surfaces never silently ship.

Generated apps enable strict mode by default. Opt in on any app:

```go
host := uihost.New(ui,
	uihost.WithStrict(),
	uihost.WithDescription("A tiny notes app."),
	uihost.WithAppIcon(iconPNG),
	uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://notes.example"}),
	uihost.WithRobots(uihost.RobotsConfig{}),
)
```

## What strict mode checks

| Check | Scope | Requirement |
| --- | --- | --- |
| `ScreenTitles` | Every page screen | `WithTitle` or `ScreenTitler` |
| `ScreenDescriptions` | Every page screen | `ScreenDescriber` or `ScreenSEO` (returning zero-value `SEO` explicitly opts a page out) |
| `SiteDescription` | Host | `WithDescription` or description meta in `WithHeadHTML` |
| `SiteIcon` | Host | `WithAppIcon` or `WithFavicon` |
| `Sitemap` | Host | `WithSitemap` with valid bare `http(s)` origin `BaseURL` (`uihost.InvalidSitemapBaseURL`) |
| `Robots` | Host | `WithRobots` |
| `InternalLinks` | Layout chrome (at boot) | Every internal `href` in header/sidebar/footer resolves to a registered screen, static asset, or router path once `App.Start` finishes building routes |
| `AxeCoverage` | Every page screen (**dev only**) | Covered by `.gofastr/axe-coverage.json` written by `axetest.Scan` (`GOFASTR_AXE_COVERAGE_DIR` overrides root) |

Drawers, sheets, dialogs, and battery-registered screens (like `/admin`) are exempt from screen metadata checks. In dev mode, a missing axe manifest warns loudly on fresh clones (`StrictAbsenceWarn`), while an existing manifest that misses a newly added screen fails boot.

## Tuning individual checks

Zero values in `StrictConfig` enforce everything, so overrides only relax checks visibly in code review (`StrictEnforce`, `StrictWarn`, `StrictOff`):

<!-- gofastr:compile
import "github.com/DonaldMurillo/gofastr/framework/uihost"
-->
```go
uihost.WithStrict(uihost.StrictConfig{
	ScreenDescriptions: uihost.StrictWarn,
	Robots:             uihost.StrictOff,
	AxeManifestMissing: uihost.StrictAbsenceEnforce,
	ExemptScreens:      []string{"/machine/feed", "/internal/*"},
})
```

## Common mistakes

- **Maintaining a manual page list in your axe test:** drifts as soon as a screen is added; derive the list from your screen catalog or `app.Routes()`.
- **Expecting `AxeCoverage` to run in production:** `.gofastr/axe-coverage.json` is a local dev/CI test artifact and is skipped in production boots.
- **Adding filler descriptions ("Welcome to our site") to pass strict mode:** implement `ScreenSEO` returning a zero-value `uihost.SEO{}` for pages that genuinely shouldn't emit a description, or list internal prefixes in `ExemptScreens`.
