# SEO: meta tags, Open Graph, JSON-LD, sitemap, robots, icons

GoFastr emits `<head>` metadata, Open Graph, Twitter cards, JSON-LD, `/sitemap.xml`, `/robots.txt`, and app icons from typed Go options and screen interfaces — HTML-escaped and URL-scheme-validated automatically.

## Sitewide defaults

```go
host := uihost.New(site,
	uihost.WithDescription("Freight visibility for mid-market shippers."),
	uihost.WithOpenGraph(uihost.OG{Title: "Freightline", URL: "https://freightline.example", Type: "website"}),
	uihost.WithTwitterCard(uihost.TwitterCard{Card: "summary_large_image", Site: "@freightline"}),
	uihost.WithSitemap(uihost.SitemapConfig{BaseURL: "https://freightline.example"}),
	uihost.WithRobots(uihost.RobotsConfig{Disallow: []string{"/__gofastr/"}}),
	uihost.WithAppIcon(logoPNG), // derives 32/180/192/512px PNGs, /favicon.ico, and PWA icons
)
```

Other options: `WithRobotsMeta("noindex")` (staging), `WithFavicon`, `WithThemeColor`, `WithPreconnect`, `WithFontPreload` (see [Theming](theming.md#self-hosting-web-fonts)).

## Per-screen metadata and Reader Mode

Implement individual interfaces (`ScreenTitle`, `ScreenDescription`, `ScreenCanonical`, `ScreenRobots`, `ScreenHreflangs`, `ScreenSchema`) or the combined `ScreenSEO(ctx) uihost.SEO` bundle on any screen component. Per-screen tags precede sitewide tags (and replace the sitewide `<meta name="description">`).

- **Typed JSON-LD (`core-ui/seo`):** `NewArticle`, `NewBreadcrumbList`, `NewFAQPage`, `NewOrganization`, `NewPerson`, `NewWebSite`, `NewWebPage`, `NewProduct`/`Offer` — rendered as `<script type="application/ld+json">` with `</` neutralized. `seo.Render` skips nil `Thing` entries, including typed-nil pointers.
- **Browser Reader Mode:** register a screen with `app.AsArticle()` (or implement `ScreenArticle() app.ArticleMeta` for author, date, and cover image) to wrap content in `<article>`, emit `Article` JSON-LD, and set `og:type=article`.
- **Page titles:** `<title>` appends ` — <AppName>` automatically unless already suffixed.
- **Per-screen `llm.md`:** when `uihost.WithPublicLLMMD` is on, each route's `/llm.md` mirrors the exact same resolved SEO metadata as YAML front-matter.

## Sitemap, robots, and icons

- **`WithSitemap`:** lists every registered route (expanding dynamic routes via `StaticPathsProvider` and per-locale routes under `WithLocalePrefixes`), skips screens blocked/redirected for anonymous visitors, splits automatically into `/sitemap/<n>.xml` indexes past 50,000 URLs / 50 MiB, and writes directly to static exports (`gofastr export`).
- **`WithRobots`:** serves `/robots.txt` with `Sitemap:` derived from `SitemapConfig.BaseURL` (and AI crawler rules when `WithAgentReady` is enabled).
- **`WithAppIcon(source)`:** center-crops one PNG/JPEG (respecting JPEG EXIF orientation) into 32/180/192/512px PNGs, `/favicon.ico`, and default PWA icons. Need a quick placeholder? Generate one with `image.NewGradient(512, 512, "#4338CA", "#0E7C86")`.

Turn missing SEO metadata into boot failures with `uihost.WithStrict()` (see [Strict mode](strict-mode.md)).

## Common mistakes

- **Setting sitewide `WithCanonicalURL`:** declares the homepage canonical for every page; use per-screen `ScreenCanonical` instead.
- **Forgetting `StaticPathsProvider` on dynamic routes (`/posts/:slug`):** dynamic screens disappear from `/sitemap.xml` and static exports without it.
- **Expecting per-screen `SEO.Robots` to override sitewide `WithRobotsMeta("noindex")`:** both emit and crawlers apply the stricter directive; drop `WithRobotsMeta` in production.
- **Injecting `<script>` / JSON-LD via `WithHeadHTML`:** `WithHeadHTML` scrubs active tags (`<script>`, `<style>`, `<iframe>`, `on*=`); use `core-ui/seo` and `ScreenSchema`.
- **Declaring `PWAConfig.Icons` and expecting `WithAppIcon` to merge:** explicit manifest icons win wholesale.
