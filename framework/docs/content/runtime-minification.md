# Runtime JS and CSS minification

GoFastr embeds its browser runtime (`/__gofastr/runtime.js` and on-demand modules `/__gofastr/runtime/<name>.js`) and framework stylesheets (`/__gofastr/app.css`, `/__gofastr/comp/*.css`, `/__gofastr/comp-bundle.css`) directly into your binary and serves them minified by default (~50% smaller raw and gzipped) via a pure-Go, zero-dependency minifier (`core-ui/runtime/minify`).

## When minification runs

The decision runs once per embedded asset (`sync.Once`) on first read:

| Environment | Mode |
| --- | --- |
| `RUNTIME_NOMINIFY=1` | Raw (override) |
| `RUNTIME_MINIFY=1` | Minified (override) |
| `GOFASTR_ENV` ∈ `{production, prod, live, staging}` | Minified |
| `GOFASTR_DEV=1` (and non-production `GOFASTR_ENV`) | Raw |
| Nothing set (default binary) | Minified ("production wins") |

Minification strips comments and collapses whitespace while preserving JS identifiers (no obfuscation or source maps needed — breakpoints remain readable in DevTools) and preserving semantically significant CSS whitespace (`calc()`, media `and (`, descendant `:hover`).

## Size budgets and compression

- **Core runtime (`runtime.js`):** held under strict automated gzip gates in `core-ui/runtime/budget_test.go`; feature modules (`copy`, `toasts`, `widgets`, `popover`, `sse`, `activelink`, `preload`, `actionloader`, etc.) load on demand only when a page emits their markers.
- **HTTP compression:** enable `AppConfig.Compression` (or gzip/brotli at your reverse proxy/CDN) to compress served JS, CSS, and HTML on the wire (see [Security](security.md#compression)).
- **Tests inspecting served CSS:** use `minify.ServedCSS(needle)` instead of asserting unminified whitespace so tests pass in both dev and production modes.

## Common mistakes

- **Expecting `GOFASTR_DEV=1` to beat `GOFASTR_ENV=production`:** production always wins; set `RUNTIME_NOMINIFY=1` to force raw output anywhere.
- **Changing `RUNTIME_NOMINIFY` without restarting:** the choice is cached via `sync.Once` per process.
- **Grepping served JS or CSS for comments or exact pre-minify spaces:** comments are stripped and CSS spacing collapses (e.g. `minmax(0,1fr)`).
