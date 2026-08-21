// This is a nested Go module. It is the ONLY part of the print feature that
// imports chromedp, so a host that wires battery/print without PDF output
// never pulls headless-Chromium tooling into its build.
//
// The import path is unchanged from when this was part of the root module
// (github.com/DonaldMurillo/gofastr/battery/print/chromepdf): a nested module
// keeps the path, it just gains its own go.mod so the dependency surface is
// isolated. The parent module is referenced via a local replace.
module github.com/DonaldMurillo/gofastr/battery/print/chromepdf

go 1.27.0

require (
	github.com/DonaldMurillo/gofastr v0.0.0
	github.com/chromedp/cdproto v0.0.0-20260321001828-e3e3800016bc
	github.com/chromedp/chromedp v0.15.1
)

require (
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/chromedp/sysutil v1.1.0 // indirect
	github.com/go-json-experiment/json v0.0.0-20260214004413-d219187c3433 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/gobwas/httphead v0.1.0 // indirect
	github.com/gobwas/pool v0.2.1 // indirect
	github.com/gobwas/ws v1.4.0 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel v1.44.0 // indirect
	go.opentelemetry.io/otel/metric v1.44.0 // indirect
	go.opentelemetry.io/otel/trace v1.44.0 // indirect
	golang.org/x/image v0.45.0 // indirect
	golang.org/x/sys v0.47.0 // indirect
)

// Local replace: the parent module is the repo root. Standard nested-module
// pattern (see cmd/check-embed/embedcheck/testdata/go.mod). Published as part
// of the same repo; the replace is dropped only if this module is ever tagged
// independently, which it is not today.
replace github.com/DonaldMurillo/gofastr => ../../..
