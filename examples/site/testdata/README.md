# examples/site/testdata

The vendored axe-core engine that previously lived here has moved to the shared
harness package: **[`internal/axetest/testdata/axe.min.js`](../../../internal/axetest/testdata)**.

It was extracted so every example app's axe gate (the site catalog + Meridian's
full app/auth/admin sweep) reuses one hermetic copy via `//go:embed` in
`internal/axetest`. See `axe_test.go` in this package and
`internal/axetest/axetest.go` for the harness API.
