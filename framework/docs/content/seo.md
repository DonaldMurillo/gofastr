# SEO — meta tags, Open Graph, JSON-LD, sitemap, robots, icons

Every SEO surface a crawler or social preview reads is a typed option or
a small screen interface — no hand-written `<head>` strings, and every
emitted value is HTML-escaped with URL fields scheme-checked (http(s)/
relative only), so user-supplied titles or URLs can't inject markup.

Two declaration levels compose:

- **Sitewide defaults** — `uihost` options passed to `uihost.New`.
- **Per-screen values** — optional interfaces on the screen component.
  Per-screen tags are injected *before* the sitewide ones, so
  first-match crawlers (Open Graph scrapers) see the specific value.

## Sitewide options

```go
host := uihost.New(site,
    uihost.WithDescription("Freight visibility for mid-market shippers."),
    uihost.WithOpenGraph(uihost.OG{Title: "Freightline", URL: "https://freightline.example", Type: "website"}),
    uihost.WithTwitterCard(uihost.TwitterCard{Card: "summary_large_image", Site: "@freightline"}),
    uihost.WithRobotsMeta("noindex"), // staging deploys; omit in production
    uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://freightline.example"}),
    uihost.WithRobots(uihost.RobotsConfig{Disallow: []string{"/__gofastr/"}}),
    uihost.WithAppIcon(logoPNG), // favicon + apple-touch + PWA icons from one image
)
```

| Option | Emits |
|--------|-------|
| `WithDescription` | `<meta name="description">` |
| `WithOpenGraph(OG{…})` | `og:title/description/image/url/type` metas |
| `WithTwitterCard(TwitterCard{…})` | `twitter:card/title/description/image/site` metas |
| `WithCanonicalURL` | `<link rel="canonical">` — usually wrong sitewide; prefer the per-screen interface |
| `WithRobotsMeta` | `<meta name="robots">` on every page (e.g. `"noindex"` for staging) |
| `WithSitemap(SitemapConfig{…})` | the `/sitemap.xml` endpoint (see below) |
| `WithRobots(RobotsConfig{…})` | the `/robots.txt` endpoint (see below) |
| `WithAppIcon(source)` | the full icon surface (see below) |
| `WithFavicon(href)` | a single `<link rel="icon">` for a hand-managed file |
| `WithThemeColor`, `WithPreconnect` | `<meta name="theme-color">`, `<link rel="preconnect">` |

## Per-screen interfaces

Implement any of these on a screen component:

```go
func (s *PostScreen) ScreenTitle() string       { return s.post.Title }
func (s *PostScreen) ScreenDescription() string { return s.post.Summary }
func (s *PostScreen) ScreenCanonical() string   { return "https://blog.example/posts/" + s.post.Slug }
func (s *PostScreen) ScreenRobots() string      { return "noindex" } // drafts, filtered views
func (s *PostScreen) ScreenHreflangs() []uihost.HreflangLink {
    return []uihost.HreflangLink{{Lang: "en", URL: "…"}, {Lang: "x-default", URL: "…"}}
}
func (s *PostScreen) ScreenSchema() []seo.Thing { // core-ui/seo — typed JSON-LD
    a := seo.NewArticle()
    a.Headline = s.post.Title
    return []seo.Thing{a}
}
```

Or declare everything at once with the bundle — its non-empty fields
override the per-concern interfaces:

```go
func (s *PostScreen) ScreenSEO() uihost.SEO {
    return uihost.SEO{
        Description: s.post.Summary,
        Canonical:   s.post.URL,
        Robots:      "",            // empty fields fall through
        OG:          &uihost.OG{Title: s.post.Title, Image: s.post.CoverURL},
        Schema:      []seo.Thing{article(s.post)},
    }
}
```

`core-ui/seo` ships typed Schema.org builders — `NewArticle`,
`NewBreadcrumbList`, `NewFAQPage`, `NewOrganization`, `NewPerson`,
`NewWebSite` (with SearchAction), `NewWebPage`, `NewProduct`/`Offer` —
rendered as `<script type="application/ld+json">` with `</`
neutralized so content can't break out of the script block.

## sitemap.xml and robots.txt

`WithSitemap` lists every registered route. Dynamic routes
(`/posts/:slug`) are expanded through the same `StaticPathsProvider`
interface static export uses; routes without it are skipped. Exclude
admin/internal prefixes with `ExcludePaths`.

`WithRobots` serves `/robots.txt` and derives the `Sitemap:` line from
the sitemap's `BaseURL` when `SitemapURL` is unset. With
`WithAgentReady`, AI crawler user-agents get explicit allow/deny groups
and a `Content-Signal` directive — see [Agent-ready](/docs/agent-ready).

Both endpoints are **also written as files by static export**
(`app --export`, `ExportStatic`): same bytes as the live handlers, with
`--export-base` folded into every `<loc>` and the derived `Sitemap:`
URL. See [Static-site export](/docs/static-export).

## Icons: one source image → the whole surface

`WithAppIcon(source []byte)` takes one image (ideally ≥512px; non-square
sources are center-cropped) and derives everything at startup:

- 32/180/192/512px PNGs served under `/__gofastr/icons/`,
- `/favicon.ico` (the 32px PNG — resolved by Content-Type),
- `<link rel="icon">` (32 + 192) and `<link rel="apple-touch-icon">` (180),
- the 192/512 manifest icons when `WithPWA` is on and `PWAConfig.Icons`
  is empty (explicit icons always win),
- the same files in static export output.

```go
//go:embed logo.png
var logo []byte

host := uihost.New(site, uihost.WithAppIcon(logo))
```

No logo yet? Generate a branded placeholder instead of shipping a
binary asset:

```go
img, _ := image.NewGradient(512, 512, "#4338CA", "#0E7C86") // framework/image
icon, _ := img.PNG().Bytes()
host := uihost.New(site, uihost.WithAppIcon(icon))
```

Apps that manage icon files by hand keep `WithFavicon(href)` plus
static-dir files; a host with neither serves 204 at `/favicon.ico` so
icon-less apps never 404 on every page load.

## Titles

`<title>` comes from the screen: the `Title` set at registration
(`app.NewScreen(...).WithTitle("Pricing")`) or a `ScreenTitle() string`
method (re-read after `Load`, so dynamic routes can title themselves
from data). The app name is appended: `Pricing — Freightline`.

## Common mistakes

- **Sitewide `WithCanonicalURL`.** A fixed canonical on every page
  declares the homepage canonical for the whole site — search engines
  drop everything else. Use per-screen `ScreenCanonical` and leave the
  global option out unless the app really is one page.
- **Forgetting `StaticPathsProvider` on dynamic routes.** `/posts/:slug`
  silently disappears from the sitemap (and static export) without it —
  the sitemap can't guess your slugs.
- **Setting `SEO.Robots` and expecting `WithRobotsMeta` to be replaced.**
  The per-screen tag is emitted *in addition to* the global one;
  crawlers apply the most restrictive combination. Drop the global
  option in production instead of overriding it per screen.
- **Hand-writing JSON-LD strings via `WithHeadHTML`.** Use
  `core-ui/seo` — the typed builders escape `</` so content can't
  terminate the script block, and the per-screen `ScreenSchema` hook
  places them correctly.
- **Declaring `PWAConfig.Icons` AND expecting `WithAppIcon` to fill
  gaps.** Explicit icons win wholesale — the generated 192/512 pair is
  injected only when the manifest declares none.
