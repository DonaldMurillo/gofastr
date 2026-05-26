# Runtime JS minification

GoFastr embeds its client-side runtime (`runtime.js` and the
on-demand modules under `core-ui/runtime/src/*.js`) directly into the
host-app binary via `//go:embed`. By default, every deployed binary
ships the **minified** form of those scripts — typically 50% smaller
raw and 50% smaller gzipped than the embedded source.

The minifier is a token-aware JavaScript pass implemented in
`core-ui/runtime/minify` (pure Go, zero deps). Tier-2 scope:
strip comments, collapse whitespace, distinguish regex literals from
division, preserve string + template-literal payloads byte-for-byte,
preserve ASI hazards (`return\nfoo`), and keep `++`/`--` together
without identifier-fusion damage. Identifier renaming and dead-code
elimination are out of scope — the output stays parseable JavaScript
a developer can still read in browser devtools, just compactly.

## When it runs

The minifier is invoked once per source on first read (`sync.Once`),
so the cost is paid at most once per process. Subsequent reads — and
every page render hits the runtime — are pure map lookups.

Whether the minifier runs at all is gated by env vars:

| Env state                                              | Result   |
|--------------------------------------------------------|----------|
| `GOFASTR_ENV` ∈ {`production`, `prod`, `live`, `staging`} | minify   |
| `GOFASTR_DEV=1` (and `GOFASTR_ENV` not a non-dev env)  | raw      |
| neither set                                            | minify   |
| `RUNTIME_NOMINIFY=1`                                   | raw      |
| `RUNTIME_MINIFY=1`                                     | minify   |

`RUNTIME_NOMINIFY` and `RUNTIME_MINIFY` are manual overrides that
trump the env detection — useful for reproducing a prod-only issue
from a dev workstation, or for debugging an end-user app that
otherwise auto-detects as production.

The defaults follow a **"production wins"** rule: an end-user who
simply `go build`s their app and runs the binary in production —
without setting any env vars — gets the minified runtime
automatically. Dev mode opts OUT via `GOFASTR_DEV=1`, the same env
flag that already enables [livereload](dev-livereload.md).

## What's served

Two HTTP routes are affected:

- `GET /__gofastr/runtime.js` — the bundled core runtime
- `GET /__gofastr/runtime/<name>.js` — each on-demand module
  (`copy`, `toasts`, `widgets`, `popover`, etc.)

Both return whichever form (raw or minified) the gating contract
selected at process startup. The minified output is still a valid
ES2020+ IIFE — no source maps are emitted, but the code remains
human-readable enough to set breakpoints in.

## Sizes

After minification (typical end-user deploy):

- `runtime.js`: ~38 KB raw / ~10 KB gz (was 92 KB raw / 28 KB gz)
- All embedded modules combined: ~131 KB raw / ~44 KB gz (was 262 KB
  / 88 KB gz)

Reverse proxies (nginx, CloudFront, etc.) typically apply
`Content-Encoding: gzip` or `br` on top, so the bytes actually on the
wire are smaller still. GoFastr itself does not set
`Content-Encoding` headers — that stays in the operator's control.

## Verifying behaviour in your app

A quick sanity check after deploying:

```bash
curl -s https://your-app.example.com/__gofastr/runtime.js | head -c 80
```

- Minified: starts with `(()=>{'use strict';try{const ua=…`
- Raw:      starts with `// GoFastr Core-UI Runtime v0.4 — ES2020+…`

If you expected one and got the other, check `GOFASTR_ENV` /
`GOFASTR_DEV` / `RUNTIME_NOMINIFY` on the running process.
