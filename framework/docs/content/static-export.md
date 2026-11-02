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

Disk-backed `core/static.Handler` instances release their directory handles
between requests, including on Windows. Each request checks the original
directory identity; replacing that directory makes the handler return 404.
Create a new handler to serve the replacement directory. The handler
resolves symlinks once, when it is built, so after a symlink flip it keeps
serving the old target while that directory exists.

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

`gofastr export <dir>` does this for any app without code: it runs the
app's main package with `GOFASTR_EXPORT=<dir>` (and
`GOFASTR_EXPORT_BASE` from `--base`), and `App.Start` exports after
migrations, seeds and battery init, then returns instead of serving.
Pages that read the database render the seeded rows. The app's `main`
has to return once `Start` does, which the generated `main.go` does.

```bash
gofastr export --base /gofastr _site
```

A registered battery that implements `static.Contributor` sees every
rendered page and adds files of its own; [site search](site-search.md)
writes its index this way. A contributor file at a page's path, in any
case, fails the build, and so does a contributor file, rail script,
static-dir or `ExtraDirs` file whose path holds a backslash (a separator
on Windows). A build that writes two files differing only in case (a
contributor's `/_HEADERS` beside the generated `_headers`, a rail script
`/REDUCERS.js` beside a static `reducers.js`, a static `ABOUT/logo.png`
beside the page `about/index.html`) fails and names both: a
case-insensitive filesystem keeps one spelling, and a case-sensitive
host then answers 404 for the other. A static file that replaces a
generated one (`manifest.webmanifest`, `robots.txt`) must match its
spelling exactly. A rebuild into the same directory renames a file or
directory an earlier build left in another case to this build's
spelling, so an export built on macOS deploys to a case-sensitive host
unchanged; only an entry that is the build's own file under the other
name moves, and nothing past a symlink does: an entry behind a symlink
in another spelling fails the build and is named, as does a symlink
whose target names a directory in a spelling the disk no longer has. A contributor may not
write `_headers` or `_redirects`, which the build writes itself; ship
extra rules in the static dir, where they are appended. Exports are
reproducible: two runs over the same content write the same bytes (a
page that shows live data aside), the PWA worker included, in a fresh
directory or over the last build.

The example site also wires a `--export <dir>` flag so the same binary
serves live *or* exports:

```bash
go build -o site ./examples/site/
./site                 # live server
./site --export _site  # static export → ./_site
```

## What gets emitted

- One `index.html` per route (`/` → `index.html`, `/about` →
  `about/index.html`, `/products/:slug` → `products/<slug>/index.html`).
  Static hosts serve a directory index at the trailing-slash URL and
  answer `/about` with a 301 to `/about/`, so every `href`, `action` and
  `data-cui-push-state` that names an exported page is written as
  `/about/` (query and fragment kept). A click, client navigation
  included, then costs one request instead of two. The same rewrite
  covers each hidden widget's dumped chrome, the PWA offline page and
  the sitemap's `<loc>` URLs. Links to anything else (assets, routes the export skipped,
  absolute URLs) are left as written.
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
- Per-route `llm.md` (unless `NoLLMMD` is set). The exported `llm.md`
  index links only the docs the export wrote: a dynamic route lists its
  pattern followed by a link to each exported page's doc, and a page
  whose doc was not written appears without a link. The builder hands
  the written docs to the index through `app.WithExportedDocs(ctx, docs)`.
  Each page's doc renders under the same synthetic request as its HTML,
  so a screen that reads the request works in both.
- `.gofastr-pages`: the list of page and `llm.md` files this build
  wrote. The next build into the same `OutDir` removes any listed file
  it no longer writes, such as a page that became gated or a route that
  was dropped, so the static host stops serving it with a 200. A file
  the builder never listed, and anything this build wrote or copied,
  stays; a first build with no list removes nothing. A page renamed by
  case only (`/About` to `/about`) is the same file on a
  case-insensitive filesystem, so its old spelling is not removed
  there.
- With `uihost.WithSitemap` / `uihost.WithRobots`: `sitemap.xml` and
  `robots.txt`, built by the live handlers' code (same route expansion
  via `StaticPathsProvider`, same `ExcludePaths`, same AI-bot rules).
  The sitemap lists only the pages the export wrote: a page it skipped
  (a gated screen, an `ExcludeRoutes` route, a 404-outlet route, a page
  that answers a status other than 200, a path a redirect takes) has no
  file on the static host, so its `<url>` entry is dropped, in every
  locale, and so is every hreflang alternate naming it. The host asks
  about each page by the path the export wrote it under, so a
  `StaticPaths` that answers differently from one call to the next can
  drop an entry but never list a page the export did not write. Past
  50,000 URLs the sitemap is an index with its parts under
  `sitemap/<n>.xml`. Under `--export-base` every sitemap `<loc>` and the
  derived `Sitemap:` line in robots.txt include the subpath, once
  even when the sitemap's `BaseURL` already ends in it. A
  `sitemap.xml` or `robots.txt` in the app's static dir wins over the
  generated one (a user's `sitemap.xml` replaces the generated parts
  too). Note that on a subpath deploy (e.g. a GitHub Pages
  *project* site) crawlers only honor a robots.txt at the origin root;
  the file is still emitted for apex/custom-domain deploys.
- With [`uihost.WithPWA`](pwa.md): `manifest.webmanifest`,
  `service-worker.js`, `__gofastr/pwa/register.js`, and
  `__gofastr/pwa/offline/index.html` (the offline page carries the
  export's CSP meta tag, as every page does). Under `--export-base` the
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

- `_headers` (Netlify, Cloudflare Pages) with the
  Content-Security-Policy, `X-Content-Type-Options` and
  `Referrer-Policy`, and the same policy as a `<meta>` in every page;
  see [Content-Security-Policy](#content-security-policy) below.
- For each `app.Redirect`, a page at the old path and a `_redirects`
  rule; see [Redirects](#redirects) below.
- Every file in the app's static dir (or embedded static FS), copied
  to the same path, `index.html` files included. Two kinds of file are
  refused: one the UI host would not serve (a path with a dot segment
  such as `.env` or `.git/config`, a server config file name such
  as `web.config`, or a symlink whose target leaves the static dir or
  is not a regular file), and one at the path of a page the build rendered,
  in any case (`about/index.html` or `About/index.html` beside an
  `/about` route). The build log names
  each. Files under a leading `.well-known/`, unless they are dotfiles
  themselves, and the root `.nojekyll` GitHub Pages reads are copied.
  A static-dir
  `_headers` or `_redirects` is appended to the generated one, so its
  rules apply and the generated ones (the security headers and the
  `app.Redirect` rules) stay first. One spelled in another case
  (`_HEADERS`) is refused: no host reads it, and on a case-insensitive
  filesystem it would replace the generated file.
- `Builder.ExtraDirs` files under their mount path, minus the dotfiles
  and config files the static dir also skips. A page or asset
  the build wrote, in any case (the generated `_headers` and `_redirects` included),
  or a file the static source provides, wins over an extra file at the
  same path. An extra file spelled in another case than an asset the
  build wrote fails the build. Which file wins is decided by this
  build and the static source, never by what an earlier build left in
  the output directory, so `Builder.Watch` (which rebuilds into the
  same directory) picks up a changed extra file.

In an app that routes by locale prefix (`framework.WithLocalePrefixes`)
each prefixed locale gets its own page tree (`/es/about/index.html`)
and its page links are prefixed (`/es/about/`). The static dir and
`ExtraDirs` are copied once, to the export root, so a link from a
Spanish page to `/files/report.pdf` keeps its root path; so does a link
to any path that no route or redirect takes. A link is matched in its
decoded form, so an escaped `/caf%C3%A9` to a `/café` route is a page
link and takes the prefix. A copied file wins over a route that also
matches its path, as it does on the live server.

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

A value fills one URL segment, escaped the way the sitemap links it: the
key `a/b` on `/orders/{id}` is the page `/orders/a%2Fb`. A static host
decodes that URL to `orders/a/b`, so the export writes the page to
`orders/a/b/index.html`. The build refuses a value whose file would land
outside its route's directory: an empty, `.` or `..` path component on
disk, a NUL, or a backslash (a Windows separator). It also refuses two
pages that land on one file, such as that key beside a route
`/orders/a/b`: the error names both paths, where the later page would
otherwise replace the earlier one without a word. Two files that differ
only in case (`/orders/A` beside `/orders/a`) count as one, since a
case-insensitive filesystem (macOS, Windows) stores them as one, and so
do two that differ only in Unicode normalization (`café` precomposed or
with a combining accent), which APFS stores as one. A
redirect's page is checked the same way, against the pages and the
other redirects.

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

A page that answers a status other than 200 is skipped the same way:
a component called `app.SetStatus` while it drew (entityui's Record
for a missing row), or the screen declares `ScreenStatusCode`. A
static host serves every file with a 200, so writing the page would
tell every visitor it exists. The builder logs a `WARN` naming the
route and the status (`uihost.StatusPageError`), and writes no
`llm.md` for that page either.

## Content-Security-Policy

A live app sends its Content-Security-Policy as a response header
(`core/middleware.SecurityHeaders`). A static host sets no headers of
its own, so the export carries the policy two ways:

- a `<meta http-equiv="Content-Security-Policy">` right after `<head>`
  in every page, which every browser enforces on any host;
- a `_headers` file at the export root, which Netlify and Cloudflare
  Pages turn into a real response header.

The policy is the one the app serves live:
`AppConfig.SecurityHeaders.ContentSecurityPolicy` (set with
`framework.WithSecurityHeaders`), or the built-in default
(`default-src 'self'; img-src 'self' data:; object-src 'none';
form-action 'self'; frame-ancestors 'none'; base-uri 'self'`) when it
is unset. `App.ExportStatic` passes it as
`static.Builder.ContentSecurityPolicy`; a builder built by hand sets
that field. An app that allows `font-src https://fonts.gstatic.com`
live gets the same font host on the static site.

`_headers` carries the value as written. The `<meta>` differs in two
ways, because a policy delivered in a `<meta>` behaves differently
(CSP Level 3, section 3.1):

- `frame-ancestors`, `report-uri` and `sandbox` are left out of the
  meta: a browser ignores them there and logs a console warning for
  each. The clickjacking guard (`frame-ancestors`) therefore applies
  only on hosts that read `_headers`.
- A comma-separated list of policies becomes one `<meta>` per policy.
  A single meta parses one policy, so a list written into one would
  fold the second policy's sources into the first's last directive
  and allow more than either.

The build fails on a policy it cannot write safely:

- a `'nonce-…'` source. A server mints a nonce per response; written
  into files, every visitor gets the same value, and an injected
  script can carry it. Exported pages link every script, so no page
  needs one. Drop the nonce source (hashes are fine).
- a control byte (a line break would add a header line of the
  policy's choosing to `_headers`), or a `"`, `<` or `>`, which no CSP
  source uses and which would end the meta's attribute.

## Redirects

`app.Redirect("/old", "/new")` answers `/old` with a 308 on the live
server. A static host has no route for it, so the export writes
`old/index.html`: a `<meta http-equiv="refresh">` to the target and a
plain link to it as the fallback, `noindex`, with the export's CSP and
no script or style. The target is the end of the redirect chain, in
the form the export links pages (`/new/`), under `--export-base` when
one is set. In an app that routes by locale prefix each locale tree
gets its own page, and its target follows the rule page links follow:
a page target takes the tree's prefix, while a static file, a path no
route takes, or a target that already names a locale (`/es/about`)
keeps its path.

The export also writes `_redirects` with one `/old /new/ 308` rule per
redirect, which Netlify and Cloudflare Pages (the hosts that read
`_headers`) answer with a real 308. A static-dir `_redirects` is
appended after these rules, and both hosts apply the first rule that
matches, so an `app.Redirect` wins over a static rule for the same
path. An app with no redirects writes none, removes one an earlier
build left in the output directory, and copies the static dir's
`_redirects` as it is.

An exact redirect may take one concrete path of a dynamic screen
(`/products/alpha` beside `/products/:slug`). The live router answers
that path with the redirect, so the export writes the redirect page
there, not the screen's page, and leaves the path out of
`Result.Pages`, `llm.md` and the sitemap.

`app.RedirectPattern` matches URLs no single file can stand for. The
export skips it with a `WARN` naming the pattern; its URLs answer the
export's `404.html`, and the live server keeps redirecting them. A
redirect chain that never ends (the live router refuses it too) is
skipped the same way.

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
| `data-cui-open` (modals, ⌘K palette) | ✓ works | the export writes the widget catalog (`widgets.json`) and each widget's chrome and style sheet; an unregistered widget shows a "Needs the Go server." toast. A widget with `RequireSession` or `RequireAuthenticated` gets its style sheet and no chrome file, since the live `/chrome` endpoint refuses an anonymous caller and every exported file is public |
| Palette search (`Search: true`) | ✓ works | reads the [site search](site-search.md) index files |
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

- prefixes every root-absolute `src`/`href`/`action`/`data-cui-push-state` in
  the HTML (assets, nav links, form targets, palette destinations);
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
