# 009 — Static File Server

**Phase:** 1 (Core Primitives) | **Tier:** 2 | **Depends on:** 003 (Router)

## Goal
Serve static files from Go `embed.FS`. Cache headers (ETag, Last-Modified, Cache-Control). Content fingerprinting via hash-based filenames. MIME detection. Directory listing off by default. SPA fallback for client-side routing.

## Deliverables
- [ ] `StaticServer` struct wrapping `embed.FS`
- [ ] `http.Handler` implementation serving files from embedded filesystem
- [ ] Cache headers:
  - `ETag` — content hash for cache validation
  - `Last-Modified` — embed timestamp or build time
  - `Cache-Control` — configurable max-age per file type
- [ ] Conditional request handling: `If-None-Match` (304 Not Modified), `If-Modified-Since`
- [ ] Content fingerprinting: generate hash-based filenames (e.g., `app.abc123.js`) with manifest map
- [ ] MIME detection via `mime.TypeByExtension` with fallback list
- [ ] Directory listing disabled by default (return 404 for directory paths)
- [ ] SPA fallback: non-file paths serve `index.html` (configurable)
- [ ] Configurable base path / mount prefix
- [ ] `static` package at `core/static/`
- [ ] Tests using `net/http/httptest` — verify headers, caching, SPA fallback, fingerprinting

## Acceptance Criteria
- Embedded files served with correct MIME types
- `ETag` header matches content hash; `If-None-Match` returns 304
- Content fingerprinting produces deterministic hash-based filenames
- Directory listing returns 404 (not a file listing)
- SPA fallback serves `index.html` for any non-file, non-asset path
- Cache-Control headers configurable (e.g., `max-age=31536000` for fingerprinted assets, `no-cache` for HTML)
- Zero dependencies outside Go stdlib
- All tests pass with `go test ./core/static/...`
