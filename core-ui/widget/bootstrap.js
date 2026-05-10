// Per-widget loader. The framework runtime (window.__gofastr.mountWidget)
// owns all DOM/SSE/RPC plumbing. This file is just an init that calls it
// once with the widget's config + chrome HTML — both are substituted by
// server.go before the response is written:
//
//   __FUI_CONFIG__  → JSON object: { name, position, backdrop, sse[], … }
//   __FUI_CHROME__  → JSON-encoded HTML string for the widget chrome
//
// The runtime is prepended by server.go (so a single response carries
// everything), so by the time this code runs window.__gofastr is live.
window.__gofastr.mountWidget(__FUI_CONFIG__, __FUI_CHROME__);
