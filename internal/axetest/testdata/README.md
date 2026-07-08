# internal/axetest/testdata — required test fixture (do not delete)

## `axe.min.js` (~553 KB)

Vendored [axe-core](https://github.com/dequelabs/axe-core) accessibility engine.

**Do not delete or move it.** It is a deliberate, load-bearing test fixture, not
a stray build artifact:

- `internal/axetest` embeds it via `//go:embed testdata/axe.min.js` and injects
  it into each page to run the a11y gate. `axetest.Scan` is the shared entry
  point used by every example app's axe test (the site catalog gate in
  `examples/site/axe_test.go`, the Meridian app gate in
  `examples/meridian/axe_test.go`).
- Keeping it vendored makes the gates **hermetic** — CI never reaches out to a
  CDN. It moved here from `examples/site/testdata/` when the harness was
  extracted into the shared `internal/axetest` package.

It is minified JavaScript **source** (text), not a compiled binary, so it is
exempt from the "never commit binaries" rule. To update: re-vendor a tagged
axe-core release build and keep the embed working.
