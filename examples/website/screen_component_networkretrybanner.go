package main

import (
	"fmt"
	"net/http"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

type NetworkRetryBannerScreen struct{}

func (*NetworkRetryBannerScreen) ScreenTitle() string {
	return "NetworkRetryBanner"
}
func (*NetworkRetryBannerScreen) ScreenDescription() string {
	return "Persistent banner gated by consecutive RPC-failure threshold or SSE silence; retry button pings a health endpoint."
}
func (*NetworkRetryBannerScreen) ScreenType() app.ScreenType { return app.ScreenPage }

func (s *NetworkRetryBannerScreen) Render() render.HTML {
	// Banner instance for this demo — threshold is 3 so the test
	// button only needs 3 failure reports.
	banner := ui.NetworkRetryBanner(ui.NetworkRetryBannerConfig{
		HealthEndpoint:   "/demo/network-health",
		FailureThreshold: 3,
		Title:            "Connection lost",
		Description:      "We're having trouble reaching the server. Click Retry once the network is back.",
	})

	// Two test buttons drive the runtime API manually so the demo
	// doesn't need to fake real network failure. In production, app
	// code calls reportFailure() from RPC error handlers.
	triggerButton := render.Tag("button", map[string]string{
		"type":                                  "button",
		"class":                                 "ui-button ui-button--secondary",
		"data-fui-network-retry-demo-trigger":   "true",
	}, render.Text("Report a failure"))

	recoverButton := render.Tag("button", map[string]string{
		"type":                                  "button",
		"class":                                 "ui-button ui-button--ghost",
		"data-fui-network-retry-demo-recover":   "true",
	}, render.Text("Report recovery"))

	// The two test buttons drive __gofastr.networkStatus directly via
	// data-attrs picked up by the networkretrybanner runtime module.
	wiring := render.Tag("div", map[string]string{
		"class": "demo-stack demo-stack--md",
	},
		triggerButton,
		recoverButton,
	)

	return render.Tag("div", nil,
		render.Tag("a", map[string]string{"href": "/components/", "class": "doc-back"},
			render.Text("← Components")),
		html.Heading(html.HeadingConfig{Level: 1}, render.Text("NetworkRetryBanner")),
		render.Tag("p", map[string]string{"class": "lede"}, render.Text(
			"Persistent banner that shows when the app reports consecutive RPC failures. The Retry button pings a health endpoint; on 2xx the banner hides and the failure counter resets.")),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Banner")),
		render.Tag("p", nil, render.Text(
			"The banner is hidden by default. Click \"Report a failure\" three times below to trip the threshold. Then click \"Retry now\" in the banner to ping /demo/network-health (returns 204).")),
		banner,
		wiring,

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Multi-instance test")),
		render.Tag("p", nil, render.Text(
			"Two banners on one page — the runtime keeps per-banner state so reportFailure() affects both, not just the last-mounted one.")),
		ui.NetworkRetryBanner(ui.NetworkRetryBannerConfig{
			HealthEndpoint:   "/demo/network-health",
			FailureThreshold: 3,
			ID:               "retry-banner-secondary",
			Title:            "Secondary banner",
		}),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Concurrent-retry test")),
		render.Tag("p", nil, render.Text(
			"This banner's Retry button hits a deliberately-slow health endpoint (~400ms). Rapid clicks must NOT fan out into N parallel requests — the runtime keeps a per-banner in-flight guard so only the first click triggers a fetch.")),
		ui.NetworkRetryBanner(ui.NetworkRetryBannerConfig{
			HealthEndpoint:   "/demo/network-health-slow",
			FailureThreshold: 3,
			ID:               "retry-banner-slow",
			Title:            "Slow-retry banner",
		}),

		html.Heading(html.HeadingConfig{Level: 2}, render.Text("Runtime API")),
		render.Tag("p", nil, render.Text(
			"Wire your app's RPC error handlers to the public API:")),
		render.Tag("pre", nil, render.Tag("code", nil, render.Text(
			`window.__gofastr.networkStatus.reportFailure();
window.__gofastr.networkStatus.reportRecovery();
window.__gofastr.networkStatus.checkHealth(); // also called by the Retry button`))),
	)
}

// NetworkHealthOK returns 204 so the banner's Retry button latches
// recovery on a successful ping.
func NetworkHealthOK(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// Concurrency-test helpers for the re-entrancy guard. The slow
// endpoint sleeps 400ms before responding 204; while it's sleeping
// netRetryInflight tracks how many requests are concurrently in
// flight, and netRetryInflightMax remembers the high-water mark.
// The /demo/network-health-stats endpoint exposes the max for the
// e2e test to assert against.
var (
	netRetryInflight     atomic.Int32
	netRetryInflightMax  atomic.Int32
)

// NetworkHealthSlow simulates a slow health endpoint (~400ms). Used
// by TestE2ERetryBannerNoConcurrentHealth to verify rapid Retry
// clicks don't fan out to multiple parallel requests.
func NetworkHealthSlow(w http.ResponseWriter, _ *http.Request) {
	n := netRetryInflight.Add(1)
	defer netRetryInflight.Add(-1)
	for {
		m := netRetryInflightMax.Load()
		if n <= m || netRetryInflightMax.CompareAndSwap(m, n) {
			break
		}
	}
	time.Sleep(400 * time.Millisecond)
	w.WriteHeader(http.StatusNoContent)
}

// NetworkHealthStats returns the high-water mark of concurrent
// inflight requests seen by NetworkHealthSlow as plain text.
func NetworkHealthStats(w http.ResponseWriter, _ *http.Request) {
	max := netRetryInflightMax.Load()
	w.Write([]byte(fmt.Sprintf("%d", max)))
}

// NetworkHealthStatsReset zeroes the high-water counter. Used at the
// start of the re-entrancy test.
func NetworkHealthStatsReset(w http.ResponseWriter, _ *http.Request) {
	netRetryInflightMax.Store(0)
	w.WriteHeader(http.StatusNoContent)
}
