# examples/site/testdata

The vendored axe-core engine that previously lived here has moved to the shared
harness package: **[`framework/testkit/axetest/testdata/axe.min.js`](../../../framework/testkit/axetest/testdata)**.

It was extracted so every example app's axe gate (the site catalog + Meridian's
full app/auth/admin sweep) reuses one hermetic copy via `//go:embed` in
`framework/testkit/axetest`. See `axe_test.go` in `examples/site` and
`framework/testkit/axetest/axetest.go` for the harness API.
