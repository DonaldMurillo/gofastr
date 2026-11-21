# PWA: installable app & offline shell

`uihost.WithPWA` makes any GoFastr UI app an installable Progressive Web App in one option: it mounts `/manifest.webmanifest`, a versioned `/service-worker.js`, a CSP-safe registration script (`/__gofastr/pwa/register.js`), and an offline fallback screen (`/__gofastr/pwa/offline`).

## Quickstart

```go
fwApp.Mount(uihost.New(site,
	uihost.WithAppIcon(logoPNG), // automatically derives 192px + 512px PWA icons when Icons is empty
	uihost.WithPWA(uihost.PWAConfig{
		ShortName:  "Meridian",
		ThemeColor: "#4f46e5",
	}),
))
```

Key `PWAConfig` fields: `Name`, `ShortName`, `Description`, `StartURL`, `Scope`, `ID`, `Display` (default `PWADisplayStandalone`), `ThemeColor`, `BackgroundColor`, `Icons`, `Precache`, `OfflineScreen`, and `DenyPaths`.

## Caching and security model

- **Live apps (conservative):** navigation requests are **network-first and never cached at runtime** (so personalized or authenticated pages aren't written to cache during browsing); offline navigations render the precached `/__gofastr/pwa/offline` page. However, `Precache` fetches any listed same-origin path outside the deny list at install time and writes successful responses into Cache Storage without inspecting `Content-Type` or `Cache-Control` — **list only public static assets in `Precache`**, never authenticated or personalized routes, and add any custom sensitive prefixes to `DenyPaths`. Only content-addressed assets (`?v=<hash>`) serve cache-first; everything else in the precache set serves network-first with offline fallback.
- **Hard deny list:** `/api/*`, `/auth/*`, `/__gofastr/sse`, `/__gofastr/session`, `/__gofastr/action`, `/__gofastr/widgets`, and `/core-ui/widget/*` (plus your `DenyPaths`) are never intercepted or precached.
- **Static exports (full-site offline):** `gofastr export` emits a full-site service worker under a separate `gofastr-pwa-static-…` cache prefix that precaches every exported page and asset at install time so the whole site works offline after one visit (and rewrites paths for `--base` and `WithLocalePrefixes`).
- **Graceful updates:** new workers install in the background without calling `skipWaiting`, activating cleanly when old tabs close. Listen for `window.addEventListener("gofastr:pwa-update", ...)` in an external script (`WithExtraScripts`) if you want to show a "Reload for update" banner.

## Common mistakes

- **Declaring `Icons` paths that 404:** `cache.addAll` rejects and the service worker fails to install. Use `uihost.WithAppIcon` or verify icon files exist in your static dir.
- **Adding `/api/...` to `Precache`:** stripped automatically by the deny list; add custom API prefixes to `DenyPaths` too.
- **Putting per-user content into a custom `OfflineScreen`:** precached at install time; keep it generic and layout-free.
- **Calling `skipWaiting()` unconditionally:** yanks open tabs onto a new asset graph mid-session; prompt the user via `gofastr:pwa-update` instead.
