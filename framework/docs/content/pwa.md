# PWA — installable app + offline shell

`uihost.WithPWA` turns a UIHost app into an installable Progressive Web
App: it serves a typed web app manifest, a CSP-safe external service
worker with a versioned app-shell precache, an offline fallback screen,
and the registration script. Apps opt in with one option — no hand-wired
head tags, caching rules, or registration scripts.

```go
site := app.NewApp("Meridian")
// ... register screens ...
fwApp.Mount(uihost.New(site,
    uihost.WithStaticDir("static"),
    uihost.WithPWA(uihost.PWAConfig{
        ShortName:  "Meridian",
        ThemeColor: "#4f46e5",
        Icons: []uihost.PWAIcon{
            {Src: "/icons/icon-192.png", Sizes: "192x192", Type: "image/png"},
            {Src: "/icons/icon-512.png", Sizes: "512x512", Type: "image/png"},
            {Src: "/icons/icon-maskable.png", Sizes: "512x512", Type: "image/png", Purpose: uihost.PWAIconPurposeMaskable},
        },
    }),
))
```

Chromium's installability check needs HTTPS (or localhost), a manifest
with a name, and 192px + 512px icons — supply the icon files as static
assets. Blueprint-generated apps scaffold placeholder icons
automatically (see [Blueprints](/docs/blueprints), `app.pwa`).

## What gets mounted

| Route | Purpose | Headers |
|-------|---------|---------|
| `/manifest.webmanifest` | The web app manifest, generated from `PWAConfig` via `encoding/json` (all values escaped) | `application/manifest+json`, `no-cache` |
| `/service-worker.js` | The generated worker. Served from the root so its default scope covers the whole app | `application/javascript`, `no-cache` (browsers re-fetch it to detect updates) |
| `/__gofastr/pwa/register.js` | External registration script, injected before `</body>` on every page — CSP-safe under the default `default-src 'self'` policy (no inline JS) | `application/javascript`, `no-cache` |
| `/__gofastr/pwa/offline` | The offline fallback page, precached at install time | `text/html`, `no-cache` |

Every rendered page also gains `<link rel="manifest">` and, when
`ThemeColor` is set, a `<meta name="theme-color">` in `<head>`.

Without `WithPWA` none of these routes or tags exist — existing apps
are unchanged.

## PWAConfig

| Field | Default | Notes |
|-------|---------|-------|
| `Name` | the core-ui app title (`app.NewApp(name)`) | Manifest `name` |
| `ShortName` | omitted | Home-screen label |
| `Description` | omitted | |
| `StartURL` | `/` | |
| `Scope` | `/` | |
| `ID` | `StartURL` | Stable app identity across renames |
| `Display` | `PWADisplayStandalone` | Also `PWADisplayFullscreen`, `PWADisplayMinimalUI`, `PWADisplayBrowser` |
| `ThemeColor` / `BackgroundColor` | omitted | `ThemeColor` also emits the head meta tag |
| `Icons` | none | `PWAIcon{Src, Sizes, Type, Purpose}`; purposes: `PWAIconPurposeAny`, `PWAIconPurposeMaskable`, `PWAIconPurposeMonochrome` |
| `Precache` | none | Extra same-origin paths to keep available offline |
| `OfflineScreen` | framework default | A `component.Component` rendered at `/__gofastr/pwa/offline` |
| `DenyPaths` | none | App-specific mounts to add to the sensitive-path deny list (e.g. a CRUD API at a custom prefix); never precached, never intercepted |

## The caching model

The generated worker is deliberately conservative:

- **Documents are network-first and never cached.** Rendered HTML can
  be personalized, so navigations always hit the server; when the
  network is unavailable the precached offline screen renders instead.
- **The cache holds exactly the precache set** — the goFastr runtime
  (`runtime.js`, split modules under their content-addressed
  `?v=<hash>` URLs, `color-scheme.js`, `actions.js`, `app.css`), the
  manifest, the offline page plus the per-component stylesheets it
  links, declared icons, and your `Precache` entries. Nothing is ever
  added to Cache Storage at runtime, and precached responses are
  re-wrapped at install time so a static host's redirects can't poison
  the offline fallback.
- **Online always means fresh.** URLs are matched exactly. Only
  content-addressed assets (`?v=<hash>`) are served cache-first —
  they're immutable, and a new deployment's URLs miss the old cache.
  Every other asset is network-first, with the cache answering only
  when the network is gone — so after a deploy, new HTML never runs
  against the previous deployment's runtime or CSS.
- **Sensitive endpoints are never intercepted and can never be
  precached.** `/__gofastr/sse`, `/__gofastr/session`,
  `/__gofastr/signal/*`, `/__gofastr/action`, `/__gofastr/widgets`,
  `/api/*`, and `/auth/*` are on a deny list baked into the worker;
  `Precache` entries that point at them (or at another origin) are
  dropped. Apps that mount their API or auth elsewhere extend the list
  with `DenyPaths` (blueprint-generated apps do this automatically for
  a custom `api_prefix` / auth `base_path`).
- **Cache names are versioned deterministically.** The name is
  `gofastr-pwa-<app-slug>-<fingerprint>` where the fingerprint hashes
  the manifest, the precache URL list, the shell asset bodies, and the
  bytes of every precache entry served from the host's static
  storage — swapping an icon in place rotates the version. A new
  deployment installs a fresh cache; on activation the worker deletes
  only obsolete caches carrying this app's `gofastr-pwa-<app-slug>-`
  prefix. (An asset served from outside static storage — a reverse
  proxy, say — contributes only its URL; give it a new path or query
  when it changes.)

## Static exports: the full-site worker

The rules above protect a LIVE app, where rendered HTML can be
personalized. A [static export](static-export.md) has no such concern —
the page set is closed and every byte is immutable — so the exported
`service-worker.js` flips strategy: the whole site is precached at
install — every page, every framework asset (including component
stylesheets under their versioned `?v=` URLs), the shell — and
navigations are served cache-first, tolerating static hosts' trailing-
slash redirects, with the network as fallback. Land once, install the
app, and every page works offline — including pages never visited.

User static-dir files are precached best-effort: one un-servable file
does not fail the install and pin clients to a previous deployment.
The cache version fingerprints the exported content, so any redeploy
(even a text-only edit) rotates the cache through the normal
worker-update cycle, and the static worker's caches use a separate
`gofastr-pwa-static-…` prefix carrying the base path, so a live
deployment and static exports on one origin never delete each other's
caches. The deny list (`/api`, `/auth`, framework session/SSE paths,
plus your `DenyPaths`) applies unchanged. If the app's static dir
ships its own `manifest.webmanifest` or `service-worker.js`, the
export keeps the user-supplied file.


## Offline screen

The default screen is a minimal "You're offline" notice rendered
through the standard document shell (theme bootstrap, `app.css`), so it
follows your theme. Pass `OfflineScreen` to replace it:

```go
uihost.WithPWA(uihost.PWAConfig{OfflineScreen: myOfflineScreen{}})
```

The page is precached at service-worker install time, so it must not
render personalized content — for that reason it is deliberately **not**
wrapped in the app layout (a layout may embed per-user chrome).

## Update behavior

Registration runs on every page load, which doubles as the update
check. A new deployment installs its worker and cache in the
background, but the framework never calls `skipWaiting` — the new
worker activates once every tab running the old version closes, so an
open app is never yanked onto new code mid-session. To offer a "new
version available — reload" prompt, listen for the window event the
registration script dispatches when an update is installed and waiting:

```js
window.addEventListener("gofastr:pwa-update", function (e) {
  // e.detail.registration is the ServiceWorkerRegistration
  showUpdateBanner();
});
```

(Ship that listener as an external script via `WithExtraScripts` — the
default CSP blocks inline JS.)

## Static export

`App.ExportStatic` emits the full PWA surface alongside the pages:
`manifest.webmanifest`, `service-worker.js`,
`__gofastr/pwa/register.js`, and `__gofastr/pwa/offline/index.html`.
Under a non-root `BasePath` (e.g. a GitHub Pages project site) the
manifest's `start_url`/`scope`/`id`/icon paths, the worker's precache
list and deny list, and the registration target are all prefixed, so
the exported app installs and works offline from the subpath. See
[Static-site export](/docs/static-export).

## Blueprint support

`gofastr generate` scaffolds the whole surface from an `app.pwa` block —
including replaceable placeholder icons. See
[Blueprints](/docs/blueprints).

## Common mistakes

- **Declaring `Icons` that don't resolve.** Icon paths are added to the
  precache; if a file 404s, `cache.addAll` rejects and the worker never
  installs. Put the files in your static dir first, then declare them.
- **Precaching API responses.** `Precache: []string{"/api/products"}`
  is silently dropped — the shell cache is for assets, never data. If
  you need offline data, that's application logic, not the app shell.
  If your API lives at a custom prefix, list it in `DenyPaths` so the
  same guarantee covers it.
- **Expecting pages to work offline.** Only the shell and the offline
  screen are cached. Navigations are network-first by design; rendered
  pages are never stored (they can be personalized). An offline visit
  to any page shows the offline screen.
- **Forcing updates with `skipWaiting` from your own script.** The
  no-forced-reload behavior is deliberate; if you must switch
  immediately, prompt the user from the `gofastr:pwa-update` event and
  reload after they accept.
- **Rendering per-user content into a custom `OfflineScreen`.** The
  page is cached at install time under the installing user's session —
  keep it generic.
