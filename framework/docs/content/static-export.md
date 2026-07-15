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

`framework.App.ExportStatic(ctx, dir, basePath)` drives the app in-process
— no port, no crawl — enumerates every declared route, renders each
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
- `/__gofastr/runtime.js` — the runtime core.
- `/__gofastr/color-scheme.js` — the FOUC-prevention bootstrap loaded
  synchronously at the top of `<head>`. Without it `themeswitch.js`
  early-returns and the theme toggle is dead.
- `/__gofastr/runtime/<name>.js` — each split runtime module
  (`themeswitch`, `copy`, `widgets`, `toasts`, …), one file per module.
- `/__gofastr/app.css` and `/__gofastr/comp/<name>.css` — global and
  per-component stylesheets.
- Per-route `llm.md` (unless `NoLLMMD` is set).
- With [`uihost.WithPWA`](pwa.md): `manifest.webmanifest`,
  `service-worker.js`, `__gofastr/pwa/register.js`, and
  `__gofastr/pwa/offline/index.html`. Under `--export-base` the
  manifest's `start_url`/`scope`/`id`/icon paths, the worker's precache
  and deny lists, and the registration target are all prefixed, so the
  exported app installs and works offline from the subpath.

  A static export gets the **full-site worker**, not the live app's
  conservative one: the exported page set is closed and immutable, so
  the worker precaches the whole site at install — every page, every
  framework asset (widget chrome, `llm.md`, component stylesheets under
  their versioned `?v=` URLs), the shell — and serves navigations
  cache-first, tolerating static hosts' trailing-slash redirects. A
  visitor who lands once has the entire site cached — install the PWA
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

Dynamic routes require the screen to implement `StaticPathsProvider`:

```go
type StaticPathsProvider interface {
    // StaticPaths returns one param map per concrete URL to emit.
    // {"slug": "go"} → /posts/go/index.html
    StaticPaths(ctx context.Context) []map[string]string
}
```

Routes whose screen doesn't implement it (and have no `:param`) are
emitted once; param routes without a provider are skipped at build time.

## Static mode: what works, what's disabled

Every exported page is stamped with `<html data-fui-static>`. The runtime
reads this marker once at boot and, when present, **skips server-backed
dispatches** so a click on a dead demo doesn't fire a request that 404s
against the serverless host:

| Feature | Static export | Why |
|---|---|---|
| Theme toggle, color scheme | ✓ works | client-only (`color-scheme.js`) |
| Copy-to-clipboard | ✓ works | client-only module |
| Signal mutations (`set`/`inc`/`toggle`) | ✓ works | client-only |
| SPA navigation | ✓ works | fetches pre-rendered pages |
| `data-fui-rpc` (island round-trips) | disabled | needs the Go handler |
| `data-fui-open` (modals, ⌘K palette) | disabled | widget catalog needs the server |
| SSE islands | not emitted | the SSE `<meta>` is omitted at render time |

A dismissible "Static preview — run locally" banner (via the single shared
`framework/ui.Banner` styling surface) is injected so server-backed demos
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
    touch _site/.nojekyll   # __gofastr/ starts with _ — Jekyll would drop it
- name: Upload Pages artifact
  uses: actions/upload-pages-artifact@v3
  with:
    path: _site
```

### Subpath (`--export-base`) vs apex

An apex deploy (`https://<user>.github.io/` or a custom domain) serves the
artifact at the host root, so the framework's root-absolute `/__gofastr/…`
URLs work as-is — omit `--export-base`.

A GitHub Pages **project** site (`https://<user>.github.io/<repo>/`) serves
the artifact under a subpath. Pass `--export-base /<repo>` and the builder:

- prefixes every root-absolute `src`/`href` in the HTML (assets + nav links);
- prefixes the inline component-catalog `stylePath` JSON values the runtime
  lazy-loads;
- bakes the prefix into the emitted `runtime.js` (it constructs split-module
  URLs in JS).

External links (`https://…`), protocol-relative (`//host`), fragments (`#…`),
and relative URLs are left untouched. Code samples are safe — `core/markdown`
escapes quotes inside `<code>` to `&quot;`, so the attribute/JSON patterns
only match real markup.

## Common mistakes

- **Crawling instead of exporting.** A `wget --mirror` of a running server
  is the trap this feature replaces. The cache-bust `?v=<hash>` query
  lands in the on-disk filename (`themeswitch.js?v=…`), the static host
  strips the query, looks for bare `themeswitch.js`, and 404s — so zero
  modules load and all client interactivity silently dies. Always use
  `ExportStatic`.
- **Forgetting a `StaticPathsProvider` on a dynamic route.** A
  `/posts/:slug` route with no provider emits nothing — the build silently
  drops it. Implement `StaticPaths(ctx)` returning one param map per
  concrete URL.
- **Expecting server-backed islands to work.** RPC round-trips, the
  widget catalog, and SSE need the Go server. Static mode disables them on
  purpose (no-op, no 404). If a page's value *is* its live interactivity,
  host the binary instead of exporting.
- **Deleting the run-locally banner.** It's the honest signal that a dead
  button is disabled-by-design, not broken. Keep it (or replace it with
  your own notice) — don't ship a static export where clicks silently do
  nothing with no explanation.
- **Letting `__gofastr/` be dropped by Jekyll.** GitHub Pages runs Jekyll
  by default, which ignores `_`-prefixed directories. `touch _site/.nojekyll`
  disables that for the deploy.
