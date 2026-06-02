# examples/site/testdata — required test fixtures (do NOT delete)

## `axe.min.js` (~553 KB)

Vendored [axe-core](https://github.com/dequelabs/axe-core) accessibility engine.

**Do not delete or move it.** It is a deliberate, load-bearing test fixture, not
a stray build artifact:

- `examples/site/axe_test.go` embeds it via `//go:embed testdata/axe.min.js`
  and injects it into each page to run the a11y gate (`TestAxe_AllPagesAreClean`).
- Keeping it vendored makes the gate **hermetic** — CI never reaches out to a
  CDN. It moved here from `examples/website/testdata/` when that example was
  retired into `examples/site`.

It is minified JavaScript **source** (text), not a compiled binary, so it is
exempt from the "never commit binaries" rule. The repo's binary audit
(`find … -maxdepth 3`) does not reach this path (depth 4) — leave it that way.
To update: re-vendor a tagged axe-core release build and keep the embed working.
