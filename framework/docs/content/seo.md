# SEO: meta tags, Open Graph, JSON-LD, sitemap, robots, icons

Everything a crawler or social preview reads, whether meta tags, Open
Graph, JSON-LD, sitemap, or robots.txt, comes from a typed option or a small
interface on a screen component, not a hand-written `<head>` string.
Every emitted value is HTML-escaped, and URL fields are scheme-checked
(http(s)/relative only), so a user-supplied title or URL can't inject
markup.

Two declaration levels compose:

- **Sitewide defaults**: `uihost` options passed to `uihost.New`.
- **Per-screen values**: optional interfaces on the screen component.
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
| `WithCanonicalURL` | `<link rel="canonical">`, usually wrong sitewide; prefer the per-screen interface |
| `WithRobotsMeta` | `<meta name="robots">` on every page (e.g. `"noindex"` for staging); site search ignores it and honours only a screen's own directive |
| `WithSitemap(SitemapConfig{…})` | the `/sitemap.xml` endpoint (see below) |
| `WithRobots(RobotsConfig{…})` | the `/robots.txt` endpoint (see below) |
| `WithAppIcon(source)` | every icon file the app needs (see below) |
| `WithFavicon(href)` | a single `<link rel="icon">` for a hand-managed file |
| `WithThemeColor`, `WithPreconnect` | `<meta name="theme-color">`, `<link rel="preconnect">` |

## Per-screen interfaces

Implement any of these on a screen component:

```go
func (s *PostScreen) ScreenTitle(ctx context.Context) string       { return s.post.Title }
func (s *PostScreen) ScreenDescription(ctx context.Context) string { return s.post.Summary }
func (s *PostScreen) ScreenCanonical() string                       { return "https://blog.example/posts/" + s.post.Slug }
func (s *PostScreen) ScreenRobots() string                          { return "noindex" } // drafts, filtered views
func (s *PostScreen) ScreenHreflangs() []uihost.HreflangLink {
    return []uihost.HreflangLink{{Lang: "en", URL: "…"}, {Lang: "x-default", URL: "…"}}
}
func (s *PostScreen) ScreenSchema(ctx context.Context) []seo.Thing { // core-ui/seo: typed JSON-LD
    a := seo.NewArticle()
    a.Headline = s.post.Title
    return []seo.Thing{a}
}
```

Or declare everything at once with the bundle; its non-empty fields
override the per-concern interfaces:

```go
func (s *PostScreen) ScreenSEO(ctx context.Context) uihost.SEO {
    return uihost.SEO{
        Description: s.post.Summary,
        Canonical:   s.post.URL,
        Robots:      "",            // empty fields fall through
        OG:          &uihost.OG{Title: s.post.Title, Image: s.post.CoverURL},
        Schema:      []seo.Thing{article(s.post)},
    }
}
```

`core-ui/seo` ships typed Schema.org builders: `NewArticle`,
`NewBreadcrumbList`, `NewFAQPage`, `NewOrganization`, `NewPerson`,
`NewWebSite` (with SearchAction), `NewWebPage`, `NewProduct`/`Offer`.
Each is rendered as `<script type="application/ld+json">` with `</`
neutralized so content can't break out of the script block.

## Browser Reader Mode

Safari Reader, Firefox Reader View, and Edge Immersive Reader each show a
reader button only when they're confident a page *is* an article. That
confidence comes from three signals the page emits: a semantic `<article>`
element wrapping the content (the dominant signal: Safari Reader and
Firefox's Readability engine scan the DOM for it), an `Article` JSON-LD
block (which Safari Reader reads to populate its title/byline/date), and
`og:type=article`.

### Turn any screen into an article: `app.AsArticle()`

The zero-config path. Add one option to a screen's registration and the
framework emits all three signals, deriving the headline and description
from the screen's own `ScreenTitle` / `ScreenDescription`; no
article-specific data anywhere:

```go
// Any normal screen + one option = reader-ready.
site.Register("/posts/:slug", &PostScreen{}, layout, app.AsArticle())

// PostScreen is an ordinary screen: title + prose, nothing more.
type PostScreen struct{ post Post }
func (s *PostScreen) ScreenTitle(ctx context.Context) string { return s.post.Title }
func (s *PostScreen) Render() render.HTML { /* the post body */ }
```

The framework wraps the rendered content in `<article>` (inside the
layout's `<main>`), synthesizes an `Article` JSON-LD item whose headline
and description come from the screen's title/description, and sets
`og:type=article`. `Render()` should return a single `<h1>` matching the
title, then the prose; keep nav and chrome in the layout (outside the
`<article>`) so the browser extracts exactly the prose.

### Richer reader views: the `ScreenArticle` interface

`AsArticle()` carries no byline or date. To give the browser structured
metadata, implement `ScreenArticle` on the component. Every reader view
then shows a consistent byline, date, and cover image whatever the prose. Its
fields fill what the screen's title can't, and it also marks the screen as
an article (the option isn't needed when the interface is implemented):
```go
func (s *PostScreen) ScreenArticle() app.ArticleMeta {
    return app.ArticleMeta{
        Author:        s.post.Author,           // → JSON-LD author (Person)
        DatePublished: s.post.PublishedAt.Format(time.RFC3339),
        Image:         s.post.CoverURL,
    }
}
```

In both forms only gaps are filled: an explicit `ScreenSchema` Article or
`ScreenSEO` OG value wins over the derived/synthesized one, so richer
declarations aren't duplicated.

## Per-screen `llm.md` inherits the same metadata

When `uihost.WithPublicLLMMD` is on, each screen's `/llm.md` document
opens with a YAML front-matter block mirroring the resolved values above:
`title`, `description`, `canonical`, `robots`, the OG / Twitter fields,
`hreflang` (a list), and `schema_types` (the JSON-LD `@type` names). The
front-matter is rendered from the same resolved `SEO` value as the HTML
`<head>`, so an agent reading the markdown and a crawler reading the
page see one consistent metadata set per route. Screens with no SEO
declarations get no front-matter. See [Agent-readiness](/docs/agent-ready).

## sitemap.xml and robots.txt

`WithSitemap` lists every registered route. Dynamic routes
(`/posts/:slug`) are expanded through the same `StaticPathsProvider`
interface static export uses; routes without it are skipped. A screen
whose policy chain answers an anonymous visitor with a redirect or a block
(a `/app/*` screen behind sign-in) is left out too: the sitemap evaluates
each page's policy as a signed-out GET, whoever fetches it, which is the
same rule static export uses to skip gated screens. It runs the policy
through `(*App).PagePolicy(ctx, path)`, which does what a request does:
it installs the route match and resolver state, runs the eager
resolvers and the policy chain, and applies region-guard redirects. A
policy that reads resolver state decides the same way here, and a
resolver error leaves the page out, including one a region guard reads.
Under `WithLocalePrefixes` each locale is judged in the context its
prefixed request carries, so a policy that reads the path locale
decides per variant; only readable variants are listed, each naming
only readable alternates, and `x-default` follows the default locale.
A dynamic route's `StaticPaths` runs once per locale, under that
locale's context, so a locale lists its own slugs (`/es/p/acerca`) and
names a variant as an alternate only where that locale's expansion
lists it.
A screen whose policy
renders an alternative (a login prompt) stays listed. Exclude other
admin/internal prefixes with `ExcludePaths`.

One sitemap file holds at most 50,000 URLs and 50 MiB, the
sitemaps.org limits; a search engine rejects a larger file whole. Past
either, `/sitemap.xml` is a sitemap index naming `/sitemap/1.xml`,
`/sitemap/2.xml`, and so on, each a URL set within the limits.
`SitemapXML(basePath)` returns the `/sitemap.xml` body, and
`SitemapFiles(basePath, keep)` returns every file with its path; a
non-nil `keep` is asked about each page by its routing path, locale
prefix included, and a page it refuses leaves the sitemap as an entry
and as an alternate. The live server keeps one render for a minute,
so the policy and `StaticPaths` runs are not repeated on every GET; a
page added or gated in that minute shows on the next render. Under
`gofastr dev` every request renders afresh.

`WithRobots` serves `/robots.txt` and derives the `Sitemap:` line from
the sitemap's `BaseURL` when `SitemapURL` is unset. With
`WithAgentReady`, AI crawler user-agents get explicit allow/deny groups
and a `Content-Signal` directive; see [Agent-ready](/docs/agent-ready).

Both endpoints are **also written as files by static export**
(`app --export`, `ExportStatic`): built by the live handlers' code,
listing only the pages the export wrote (and the index's parts as
`sitemap/<n>.xml` files), with `--export-base` folded into every
`<loc>` and the derived `Sitemap:` URL. A `BaseURL` that already ends in the base path
(`https://user.github.io/docs` beside `--export-base /docs`) gets it
once, not twice. See [Static-site export](/docs/static-export).

## Icons: one source image → every icon file

`WithAppIcon(source []byte)` takes one image (ideally ≥512px; non-square
sources are center-cropped) and derives everything at startup:

- 32/180/192/512px PNGs served under `/__gofastr/icons/`,
- `/favicon.ico` (the 32px PNG, resolved by Content-Type),
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
(`app.NewScreen(...).WithTitle("Pricing")`) or a `ScreenTitle(ctx context.Context) string`
method (re-read after `Load`, so dynamic routes can title themselves
from data). The app name is appended: `Pricing — Freightline`. If the
title already ends with the exact suffix ` — Freightline`, it is left
unchanged. Full pages, recovery screens, not-found outlet pages and
client-navigation title headers use this rule.

## Enforcing all of this

None of the surfaces above error when missing: SEO emission is
silently skipped for anything you didn't declare. `uihost.WithStrict()`
flips that for apps that want the launch bar enforced: boot fails
listing every page screen without a title/description and any missing
site-level surface (description, icon, sitemap, robots). See
`gofastr docs strict-mode`.

## Common mistakes

- **Sitewide `WithCanonicalURL`.** A fixed canonical on every page
  declares the homepage canonical for the whole site; search engines
  drop everything else. Use per-screen `ScreenCanonical` and leave the
  global option out unless the app really is one page.
- **Forgetting `StaticPathsProvider` on dynamic routes.** `/posts/:slug`
  silently disappears from the sitemap (and static export) without it;
  the sitemap can't guess your slugs.
- **Setting `SEO.Robots` and expecting `WithRobotsMeta` to be replaced.**
  The per-screen tag is emitted *in addition to* the global one;
  crawlers apply the most restrictive combination. Drop the global
  option in production instead of overriding it per screen.
- **Hand-writing JSON-LD strings via `WithHeadHTML`.** Use
  `core-ui/seo`; the typed builders escape `</` so content can't
  terminate the script block, and the per-screen `ScreenSchema` hook
  places them correctly.
  `WithHeadHTML` and `SEOScreen.HeadHTML` are scrubbed anyway: every
  `<script>` goes, with the other active tag families (`<style>`,
  `<iframe>`, `<svg>`, media, forms, `<base>`, `<img>`), meta-refresh,
  `on*=` handlers, `autofocus` and preload/script `<link>`s. The scrub
  runs until its output stops changing, so `<scr<script></script>ipt>`
  cannot reassemble into `<script>`; head HTML still changing after the
  pass cap is dropped whole and the host logs one warning.
- **Declaring `PWAConfig.Icons` AND expecting `WithAppIcon` to fill
  gaps.** Explicit icons win wholesale; the generated 192/512 pair is
  injected only when the manifest declares none.
