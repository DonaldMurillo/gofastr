# Runtime JS and CSS minification

GoFastr embeds its client-side runtime (`runtime.js` and the
on-demand modules under `core-ui/runtime/src/*.js`) directly into the
host-app binary via `//go:embed`. By default, every deployed binary
ships the **minified** form of those scripts, typically 50% smaller
raw and 50% smaller gzipped than the embedded source. The stylesheets
the framework composes are minified under the same switch; see
[Stylesheets](#stylesheets).

The minifier is a token-aware JavaScript pass implemented in
`core-ui/runtime/minify` (pure Go, zero deps). Tier-2 scope:
strip comments, collapse whitespace, distinguish regex literals from
division, preserve string + template-literal payloads byte-for-byte,
preserve ASI hazards (`return\nfoo`), and keep `++`/`--` together
without identifier-fusion damage. Identifier renaming and dead-code
elimination are out of scope; the output stays parseable JavaScript
a developer can still read in browser devtools, just compactly.

## When it runs

The minifier is invoked once per source on first read (`sync.Once`),
so the cost is paid at most once per process. Subsequent reads, and
every page render hits the runtime, are pure map lookups.

Whether the minifier runs at all is gated by env vars:

| Env state                                              | Result   |
|--------------------------------------------------------|----------|
| `GOFASTR_ENV` ∈ {`production`, `prod`, `live`, `staging`} | minify   |
| `GOFASTR_DEV=1` (and `GOFASTR_ENV` not a non-dev env)  | raw      |
| neither set                                            | minify   |
| `RUNTIME_NOMINIFY=1`                                   | raw      |
| `RUNTIME_MINIFY=1`                                     | minify   |

`RUNTIME_NOMINIFY` and `RUNTIME_MINIFY` are manual overrides that
trump the env detection, useful for reproducing a prod-only issue
from a dev workstation, or for debugging an end-user app that
otherwise auto-detects as production.

The defaults follow a **"production wins"** rule: an end-user who
simply `go build`s their app and runs the binary in production,
without setting any env vars, and gets the minified runtime
automatically. Dev mode opts OUT via `GOFASTR_DEV=1`, the same env
flag that already enables [livereload](dev-livereload.md).

## What's served

Two HTTP routes are affected:

- `GET /__gofastr/runtime.js`: the bundled core runtime
- `GET /__gofastr/runtime/<name>.js`: each on-demand module
  (`copy`, `toasts`, `widgets`, `popover`, etc.), and every behaviour a
  package registered with `registry.RegisterBehavior`, which the same
  route serves under the same gate

Both return whichever form (raw or minified) the gating contract
selected at process startup. The minified output is still a valid
ES2020+ IIFE; no source maps are emitted, but the code remains
human-readable enough to set breakpoints in.

## Stylesheets

Every stylesheet the framework composes for a browser goes through
`minify.ServedCSS`, under the same env gate as the runtime:

- `/__gofastr/app.css` and its theme-variant URLs (`?t=<hash>`)
- per-component CSS (`/__gofastr/comp/<name>.css`) and the
  `/__gofastr/comp-bundle.css` bundle
- widget stylesheets, the setup wizard's sheet, the print shell's
  inlined CSS, and the `app.css` a static export writes

The CSS pass strips comments, collapses whitespace, drops the space
beside `{`, `}`, `;`, `,`, after `(` and `:`, before `)`, and the `;`
before a `}`. It keeps every space that changes meaning: before `(`
(`and (` is not the function `and(`), before `:` (`a :hover` selects
descendants), around `>`, `+` and `~`, inside `calc()`, and in an
empty custom property (`--x: ;`). Strings, escapes and unquoted
`url()` bodies pass through byte for byte. A browser test hands
Chrome every registered component sheet and `app.css`, as written and
as minified, and requires the same rule tree from both.

Two things change for code that reads the served CSS:

- **Custom properties read back in the minified spelling.**
  `getComputedStyle(el).getPropertyValue('--x')` returns the declared
  text, so `minmax(0, 1fr)` comes back as `minmax(0,1fr)`. The value
  means the same thing; compare tokens, not spacing.
- **Tests that grep served CSS** should spell the needle through
  `minify.ServedCSS("--color-primary: #4F46E5")`, which matches in
  both modes. `registry.Entry.CSSFor` and `UIHost.AppCSS` still return
  the readable source; `Entry.ServedCSSFor` returns what is sent.

On the examples/hn front page the component bundle drops from 11.4 KB
to 5.7 KB gzipped and `app.css` from 5.0 KB to 2.6 KB, so the HTML
and both stylesheets together come to about 12 KB.

## Sizes

After minification (typical end-user deploy):

- `runtime.js`: ~12.3 KB gz at level 6, ~14.2 KB at level 1, under
  the 12.5 KB goal and the 14 KB initial-congestion-window line
  `core-ui/runtime/budget_test.go` enforces
- Every on-demand module: ≤ 3 KB gz each; a page only fetches the
  modules whose markers it renders

Post-navigation cosmetics live in on-demand modules too: `activelink`
(aria-current highlighting, idle-loaded), `preload` (route prefetch,
loaded only when a route declares `app.Preload`), and `actionloader`
(per-screen compiled action scripts, loaded only when the app has
compiled actions).

Gzip these responses at a reverse proxy or CDN, or set
`AppConfig.Compression`, which adds `middleware.Compress` to the default
chain (see [security](security.md#compression)).

## Verifying behaviour in your app

A quick sanity check after deploying:

```bash
curl -s https://your-app.example.com/__gofastr/runtime.js | head -c 80
```

- Minified: starts with `(()=>{'use strict';try{const ua=…`
- Raw:      starts with `// GoFastr Core-UI Runtime v0.4 — ES2020+…`

`/__gofastr/app.css` answers the same question for CSS: minified
starts with `:root{--`, raw with `:root {` and a newline.

If you expected one and got the other, check `GOFASTR_ENV` /
`GOFASTR_DEV` / `RUNTIME_NOMINIFY` on the running process.

## Common mistakes

- **Expecting `GOFASTR_DEV=1` to win when `GOFASTR_ENV` says
  production.** The gating order checks the overrides first, then
  `GOFASTR_ENV`, then `GOFASTR_DEV`: a non-dev `GOFASTR_ENV`
  (`production`, `prod`, `live`, `staging`) beats the dev flag. To
  force raw output regardless, set `RUNTIME_NOMINIFY=1`.
- **Changing env vars on a running process.** The raw-vs-minified
  decision is made once per source on first read (`sync.Once`) and
  cached for the life of the process. Restart after changing
  `GOFASTR_ENV` / `RUNTIME_NOMINIFY` / `RUNTIME_MINIFY`.
- **Anchoring tests or scripts on comment text in the runtime.** The
  minifier strips comments and collapses whitespace, so a grep for a
  comment-only substring (or a pre-minify spacing pattern) passes in
  dev and fails in prod. Anchor on code, and accept both spacings.
  For CSS, spell the needle through `minify.ServedCSS`.
- **Hunting for source maps.** None are emitted by design; the
  minified output keeps identifiers and stays debuggable. For full
  readability while reproducing an issue, restart with
  `RUNTIME_NOMINIFY=1`.
