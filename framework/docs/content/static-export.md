# Static-site export

`gofastr export` (and `App.ExportStatic`) renders a GoFastr UI app into a self-contained directory of static HTML, versioned CSS, minimal runtime JS, icons, sitemap, robots.txt, PWA files, and optional client-side site search — ready for GitHub Pages, Cloudflare Pages, Netlify, or S3.

```bash
gofastr export _site
gofastr export --base /docs _site    # deploy under a subpath (e.g. GitHub Pages)
```

## What gets exported

- **Every registered static screen** (for example `/`, `/pricing`, `/about`), rendered through the exact same SSR pipeline and layouts as the live server.
- **Dynamic screens** (`/docs/:slug`, `/posts/:slug`) whose component implements `StaticPathsProvider` (`StaticPaths(ctx context.Context) []map[string]string`). Dynamic routes without `StaticPathsProvider` are skipped with a warning.
- **Locale-prefixed trees** when `framework.WithLocalePrefixes` is enabled (`/es/...` alongside the default locale).
- **Assets & metadata:** `/__gofastr/app.css`, per-component CSS, runtime JS, icons from `WithAppIcon`, `/sitemap.xml`, `/robots.txt`, `/llm-pages.md` and per-page `llm.md`, static files from `WithStaticDir`, PWA files (`WithPWA`), and `battery/sitesearch` shards.
- **Custom contributors:** any battery implementing `static.Contributor` (`ExportStart`, `ExportPage`, `ExportFiles`) can write additional files alongside exported pages.

## What gets skipped

- **Auth-gated or redirected screens:** every page is evaluated as an anonymous `GET` (`PagePolicy`). Screens that redirect to `/login`, return non-200 (such as `app.SetStatus(ctx, 404)`), or fail access checks are skipped automatically so private chrome never leaks into a static bundle.
- **Server-only features:** API CRUD routes, SSE (`/__gofastr/sse`), server-action RPCs, and live form POSTs do not run on a static host. Static exports support client-side navigation, theme switching, headless disclosure/tabs/modals, `core-ui/localdb` / `localentity`, and `battery/sitesearch`.

## Subpath hosting (`--base`)

Pass `--base /my-project` (or pass `"/my-project"` as the third argument to `app.ExportStatic(ctx, dir, "/my-project")`) when hosting below a domain root:
- Rewrites internal links, asset URLs, sitemap `<loc>` URLs, robots `Sitemap:`, PWA manifest `start_url`/`scope`/icons, and site-search shard paths cleanly without double-prefixing.
- Preserves trailing-slash compatibility for static hosts (`dir/index.html`).

## PWA and offline behavior

When `WithPWA` is enabled, a static export emits a full-site service worker that precaches every exported page and asset at install time so the entire site works offline after one visit, using a separate `gofastr-pwa-static-…` cache prefix (see [PWA](pwa.md)).

## Common mistakes

- **Forgetting `StaticPathsProvider` on dynamic routes:** `/docs/:slug` produces zero files unless the screen tells the exporter which slugs exist.
- To publish `/llms.txt` on a static site, add an `llms.txt` file to the static source configured with `WithStaticDir`. `WithLLMsTxt` configures the live server's discovery document.
- **Exporting an interactive server-RPC island and expecting it to work on GitHub Pages:** static hosts have no Go backend. Use `core-ui/localdb` / `localentity` for browser-local persistence, or deploy the Go binary when you need server mutations.
- **Hardcoding `/assets/...` without honoring `--base`:** use framework helpers and `WithStaticDir` so subpath deploys rewrite paths automatically.
- **Using `app.SetStatus(ctx, 404)` and wondering why the page is missing from `_site`:** static export skips non-200 responses by design.
