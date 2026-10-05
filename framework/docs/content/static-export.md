# Static-site export

GoFastr is SSR-first: every page is fully server-rendered on first paint,
and interactivity is server-driven RPC over islands. A **static export**
renders the whole app once, at build time, into a directory of plain HTML
+ assets you can host on any static file server (GitHub Pages, S3, Netlify,
`python3 -m http.server`). No Go server runs in production.

This replaces the old approach of crawling a running server with `wget`,
which broke client interactivity: `wget` baked the cache-bust `?v=<hash>`
query into each split runtime module's **filename**, so the static host
served a 404 for every module and silently killed the theme toggle,
command palette, copy buttons, and widgets.

## Exporting

`framework.App.ExportStatic(ctx, dir, basePath)` drives the app in-process,
with no port and no crawl: it enumerates every declared route, renders each
through the SSG-aware render path, and dumps all `/__gofastr` assets with
**query-free filenames**. `basePath` is the URL subpath for a project-site
deploy (`"/gofastr"`); pass `""` for an apex deploy.

```go
fwApp, _ := framework.NewApp(opts...)
// "" = apex; "/gofastr" for a https://<user>.github.io/gofastr/ deploy.
if err := fwApp.ExportStatic(context.Background(), "_site", ""); err != nil {
    log.Fatal(err)
}
```

The example site wires a `--export <dir>` flag so the same binary serves
live *or* exports:

```bash
go build -o site ./examples/site/
./site                 # live server
./site --export _site  # static export → ./_site
```

## What gets emitted

- One `index.html` per route (`/` → `index.html`, `/about` →
  `about/index.html`, `/products/:slug` → `products/<slug>/index.html`).
- `/__gofastr/runtime.js`: the runtime core.
- `404.html` at the export root: the not-found page the live server
  answers with, rendered through the app's root layout (see
  [Error pages](#error-pages-404html-and-non-ok-navigation) below).
  GitHub Pages, Netlify, Cloudflare Pages and S3 website hosting all
  serve it for a miss; configure nothing, it is already the name they
  look for.
- `/__gofastr/color-scheme.js`: the FOUC-prevention bootstrap loaded
  synchronously at the top of `<head>`. Without it the stored scheme
  is applied only when `headless-navigation` loads, so a reload
  flashes.
- `/__gofastr/runtime/<name>.js`: each split runtime module
  (`widgets`, `headless-feedback`, `headless-navigation`, …), one file per module.
- `/__gofastr/app.css` and `/__gofastr/comp/<name>.css`: global and
  per-component stylesheets.
- Per-route `llm.md` (unless `NoLLMMD` is set).
- With `uihost.WithSitemap` / `uihost.WithRobots`: `sitemap.xml` and
  `robots.txt`, byte-identical to what the live handlers serve (same
  route expansion via `StaticPathsProvider`, same `ExcludePaths`, same
  AI-bot rules). A gated screen the export skips is not in the sitemap
  either. Under `--export-base` every sitemap `<loc>` and the
  derived `Sitemap:` line in robots.txt include the subpath. A
  `sitemap.xml` or `robots.txt` in the app's static dir wins over the
  generated one. Note that on a subpath deploy (e.g. a GitHub Pages
  *project* site) crawlers only honor a robots.txt at the origin root;
  the file is still emitted for apex/custom-domain deploys.
- With [`uihost.WithPWA`](pwa.md): `manifest.webmanifest`,
  `service-worker.js`, `__gofastr/pwa/register.js`, and
  `__gofastr/pwa/offline/index.html`. Under `--export-base` the
  manifest's `start_url`/`scope`/`id`/icon paths, the worker's precache
  and deny lists, and the registration target are all prefixed, so the
  exported app installs and works offline from the subpath.

  A static export gets the **full-site worker**, not the live app's
  conservative one: the exported page set is closed and immutable, so
  the worker precaches the whole site at install: every page, every
  framework asset (widget chrome, `llm.md`, component stylesheets under
  their versioned `?v=` URLs), the shell. It serves navigations
  cache-first, tolerating static hosts' trailing-slash redirects. A
  visitor who lands once has the entire site cached; install the PWA
  and every page works offline, including pages never visited. User
  static-dir files are precached best-effort so one un-servable file
  cannot brick the install. The worker's cache version fingerprints the
  exported content, so a redeploy (even one that only edits page text)
  ships byte-different worker JS and the fresh export replaces the old
  cache; an unchanged rebuild reproduces identical worker bytes. Static
  caches live under their own `gofastr-pwa-static-…` prefix, so a live
  deployment on the same origin never deletes them. Denied endpoints
  (`/api`, `/auth`, framework session/SSE paths) are still never
  precached or intercepted, and a user-supplied `manifest.webmanifest`
  or `service-worker.js` in the static dir wins over the generated one.

Dynamic routes require the screen to implement `StaticPathsProvider`, the
static-export analogue of Next.js's `generateStaticParams`. Each returned
param map is substituted into the route pattern to produce one concrete
`index.html`:

```go
type StaticPathsProvider interface {
    // One param map per concrete URL to emit.
    // {"slug": "go"} → /posts/go/index.html
    StaticPaths(ctx context.Context) []map[string]string
}

// A docs catch-all emits one page per slug. The param key is the bare
// catch-all name ("path"), matching the route pattern /docs/{path...}.
func (s *DocScreen) StaticPaths(ctx context.Context) []map[string]string {
    out := make([]map[string]string, 0, len(catalog))
    for _, slug := range catalog {
        out = append(out, map[string]string{"path": slug})
    }
    return out
}
```

Routes whose screen doesn't implement `StaticPathsProvider` (or returns an
empty slice) are **skipped** at build time, but the builder now logs a
`WARN` naming the route pattern and the fix (implement `StaticPaths`) rather
than dropping it silently; the missing pages are a build-time signal, not a
silent gap. The route is still reachable via SSR if the server is running.

A screen whose policy **Redirects** or **Blocks** (the two non-`Allow`
decisions) is skipped too: there is no request or session at build time, so
a redirect to `/login` or a hard 403 has nothing to render. The builder logs
a `WARN` naming the route and the policy decision and continues; the page is
simply absent from the export, and the route stays reachable via SSR on the
live server. A screen that should ship in the export behind a gate uses a
**`RenderAlt`** policy instead: the alt component (a login prompt, a public
teaser) renders in place of the gated screen and is exported like any other
page. `Allow` and `RenderAlt` are the only decisions a static export can
materialize, so a Redirect/Block screen is never written to disk.

## Failed fills refuse the export

A tree-layout outlet fill whose `Load` or render fails can be **contained**
on a live server: the outlet degrades to its fallback (or renders empty) and
the page serves with status 200. An export must not freeze those degraded
bytes into a page every visitor would see, so the builder reads the render's
contained-failure record (`RenderResult.FillFailures`, surfaced through
`UIHost.RenderStaticPageResult`) and **fails the build**, naming the route
and each outlet address:

```text
static: render "/broken/load" refused: 1 fill(s) failed and were contained
    (a live server degrades the outlet; the export will not bake it):
    l:shell#aside (boom: ...)
```

Fix the fill, or keep the route out of the export set **on purpose** with
`Builder.ExcludeRoutes` (exact pattern or a whole path-segment prefix —
`"/broken"` excludes `/broken/load` and every other `/broken/*` page; each
skip is logged). `ExcludeRoutes` is a builder field, so
`App.ExportStatic` (which constructs the builder for you) exports every
route: drive `static.Builder` directly when a route set needs exclusions.

## Scripts the app serves itself

Every page carries the extra-script rail (`uihost.WithExtraScripts`,
`RegisterExternalScript`, and `RegisterDocumentScript` on the pages its
scope accepts). A rail script the build has not already written, such
as one the app serves from its own router
(`WithExtraScripts("/__site/reducers.js")` plus a `Router().Get` for
that path) or the plugin broker, is fetched through `Builder.Handler`,
query string included, and written into the export under its path. A
document-scoped script is fetched only when its scope accepts a route
the export rendered. A copy left in a reused output directory by an
earlier build is replaced; two srcs that differ only in their query
share one file, and the first wins. A src that is not a same-origin
path (a CDN URL, a protocol-relative or relative src) is left to the
browser and never fetched. `App.ExportStatic` sets `Handler` to the
app's router. A rail script that does not answer 200 fails the build,
since every exported page would 404 on it, and so does one whose path
is an exported page. A `static.Builder` built by hand with no `Handler`
skips these scripts.

## SPA navigation reads whole documents as envelopes

A static host serves whole HTML documents and ignores the runtime's request
headers, so every SPA navigation answer is a full page, not a partial. The
runtime handles this natively: it derives the swap boundary from the fetched
document's own layout chain (the deepest layer the live DOM shares with it),
swaps only that layer's content cell, and applies every outlet and route
area outside it as a fill — the same treatment a served envelope partial
gets. Layout state (a kept list pane and its typed filter) survives
navigation and Back exactly as on a live server.

## Error pages: 404.html and non-OK navigation

The export writes `404.html` at the root, rendered from the same
not-found page the live server serves: the app's root layout with its
header and nav, outlets at their declared defaults, and the error body
in `<main>`. Every major static host serves a file with that exact name
for a miss — GitHub Pages, Netlify, Cloudflare Pages, and S3 website
hosting — so a visitor who lands on a URL the export does not hold gets
the site's own error page, not the host's default one.

The runtime handles the 404 status on purpose. A navigation answer
whose Content-Type is `text/html` — a static host's `404.html`, or a
live server's error page — flows through the same apply paths a 200
takes: the swap lands at the deepest layer the live DOM shares with the
error document (the root shell), so the header stays and `<main>` shows
the error page. The URL keeps the target (Back returns to where the
visitor was), the title comes from the response, and the error page is
never written to the screen cache.

An error body that is NOT HTML (a plain-text `404 page not found` from
a host that serves nothing custom, a JSON API miss) cannot be applied;
the runtime then shows a toast naming the status — `Could not load /x
(HTTP 404)` — and stays on the current page. Only a failed fetch
(offline, DNS) keeps the older "check your connection" wording. There
is never a full-page-reload fallback.

Routes whose fills fail on purpose (see
[Failed fills refuse the export](#failed-fills-refuse-the-export)) are
absent from the export; clicking their links on the static host lands
on this 404 page, which is exactly the honest answer for a static copy.

## Static mode: what works, what's disabled

Every exported page is stamped with `<html data-cui-static>`. The runtime
reads this marker once at boot and, when present, **skips server-backed
dispatches** so a click on a dead demo doesn't fire a request that 404s
against the serverless host:

| Feature | Static export | Why |
|---|---|---|
| Theme toggle, color scheme | ✓ works | client-only (`color-scheme.js`) |
| Copy-to-clipboard | ✓ works | client-only module |
| Signal mutations (`set`/`inc`/`toggle`) | ✓ works | client-only |
| SPA navigation | ✓ works | fetches pre-rendered pages |
| `data-cui-rpc` (island round-trips) | disabled | needs the Go handler |
| `data-cui-open` (modals, ⌘K palette) | disabled | widget catalog needs the server |
| SSE islands | not emitted | the SSE `<meta>` is omitted at render time |

A dismissible "Static preview — run locally" banner (using the shared
`framework/ui.Banner` component) is injected so server-backed demos
read as intentionally inactive rather than broken. The dismissal persists
in `localStorage`.

Live pages never carry the marker, so every static-mode guard is a no-op
in the normal server-backed app.

## Deploying to GitHub Pages

```yaml
- name: Export static site
  run: |
    # --export-base /gofastr: this repo is a Pages *project* site served
    # from https://<user>.github.io/gofastr/, so assets/nav/runtime-constructed
    # URLs must resolve under the /gofastr mount path. Omit for an apex deploy.
    ./site --export _site --export-base /gofastr
    touch _site/.nojekyll   # __gofastr/ starts with _; Jekyll would drop it
- name: Upload Pages artifact
  uses: actions/upload-pages-artifact@v3
  with:
    path: _site
```

### Subpath (`--export-base`) vs apex

An apex deploy (`https://<user>.github.io/` or a custom domain) serves the
artifact at the host root, so the framework's root-absolute `/__gofastr/…`
URLs work as-is; omit `--export-base`.

A GitHub Pages **project** site (`https://<user>.github.io/<repo>/`) serves
the artifact under a subpath. Pass `--export-base /<repo>` and the builder:

- prefixes every root-absolute `src`/`href` in the HTML (assets + nav links);
- prefixes the inline component-catalog `stylePath` JSON values the runtime
  lazy-loads;
- bakes the prefix into the emitted `runtime.js` (it constructs split-module
  URLs in JS).

External links (`https://…`), protocol-relative (`//host`), fragments (`#…`),
and relative URLs are left untouched. Code samples are safe: `core/markdown`
escapes quotes inside `<code>` to `&quot;`, so the attribute/JSON patterns
only match real markup.

## Common mistakes

- **Crawling instead of exporting.** A `wget --mirror` of a running server
  is the trap this feature replaces. The cache-bust `?v=<hash>` query
  lands in the on-disk filename (`headless-feedback.js?v=…`), the
  static host strips the query, looks for bare `headless-feedback.js`,
  and 404s, so zero
  modules load and all client interactivity silently dies. Always use
  `ExportStatic`.
- **Forgetting a `StaticPathsProvider` on a dynamic route.** A
  `/posts/:slug` route with no provider emits nothing; the build logs a
  `WARN` and skips it. Implement `StaticPaths(ctx)` returning one param map
  per concrete URL.
- **Expecting server-backed islands to work.** RPC round-trips, the
  widget catalog, and SSE need the Go server. Static mode disables them on
  purpose (no-op, no 404). If a page's value *is* its live interactivity,
  host the binary instead of exporting.
- **Deleting the run-locally banner.** It's the honest signal that a dead
  button is disabled-by-design, not broken. Keep it (or replace it with
  your own notice); don't ship a static export where clicks silently do
  nothing with no explanation.
- **Letting `__gofastr/` be dropped by Jekyll.** GitHub Pages runs Jekyll
  by default, which ignores `_`-prefixed directories. `touch _site/.nojekyll`
  disables that for the deploy.
