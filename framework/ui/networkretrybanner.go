package ui

import (
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── NetworkRetryBanner ─────────────────────────────────────────────
//
// Persistent banner that auto-shows when network connectivity looks
// degraded — either the SSE stream has gone silent for too long or
// the app has reported a consecutive run of RPC failures. The banner
// hides when (a) the Retry button's health-check returns 2xx, or
// (b) app code explicitly calls
// `window.__gofastr.networkStatus.reportRecovery()`. The runtime does
// NOT wrap `window.fetch`, so unrelated successful requests do not
// auto-dismiss the banner — apps wire reportFailure / reportRecovery
// into their RPC error handlers themselves.
//
// Place once near the top of the page chrome (above the main content,
// inside the persistent shell so SPA navigation doesn't remove it).
// The banner is hidden by default — the runtime un-hides it when
// thresholds trip.
//
// Public runtime API (window.__gofastr.networkStatus):
//
//	reportFailure()  — call from any RPC failure handler. After
//	                    FailureThreshold consecutive calls without an
//	                    intervening reportRecovery(), the banner shows.
//	reportRecovery() — call on any successful RPC. Resets the counter
//	                    and hides the banner.
//	checkHealth()    — manually fire the health-check ping (the same
//	                    one the Retry button triggers).

// NetworkRetryBannerConfig configures the banner.
type NetworkRetryBannerConfig struct {
	// HealthEndpoint is the URL the Retry button pings to test
	// connectivity. Must return 2xx when the server is healthy.
	// Required.
	HealthEndpoint string

	// FailureThreshold is the number of consecutive RPC failures that
	// trip the banner. Default 3. Zero disables the failure-count
	// trigger (banner only shows on explicit reportFailure threshold
	// hits never reached).
	FailureThreshold int

	// SSESilenceMs triggers the banner if no SSE event arrives for
	// this many milliseconds. Default 0 (disabled — opt-in). When set,
	// the runtime polls window.__gofastr.sseStatus.lastEventAt (kept
	// current by the SSE module on every frame) and shows the banner
	// after this much silence; on SSE reconnect a gofastr:sse-status
	// event re-probes the health endpoint so the banner can dismiss.
	SSESilenceMs int

	// Title is the banner heading. Default "Connection lost".
	Title string

	// Description is the body text. Default explains the recovery
	// action.
	Description string

	// RetryLabel is the retry button text. Default "Retry now".
	RetryLabel string

	ID    string
	Class string
}

// NetworkRetryBanner renders the (initially hidden) banner.
func NetworkRetryBanner(cfg NetworkRetryBannerConfig) render.HTML {
	if cfg.HealthEndpoint == "" {
		panic("ui: NetworkRetryBanner requires HealthEndpoint")
	}
	threshold := cfg.FailureThreshold
	if threshold == 0 {
		threshold = 3
	}
	title := cfg.Title
	if title == "" {
		title = "Connection lost"
	}
	desc := cfg.Description
	if desc == "" {
		desc = "We're having trouble reaching the server. Your changes are paused until the connection comes back."
	}
	retryLabel := cfg.RetryLabel
	if retryLabel == "" {
		retryLabel = "Retry now"
	}

	cls := "ui-network-retry-banner"
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}
	attrs := map[string]string{
		"class":                              cls,
		"role":                               "alert",
		"aria-live":                          "assertive",
		"data-fui-network-retry-health":      cfg.HealthEndpoint,
		"data-fui-network-retry-threshold":   fmt.Sprintf("%d", threshold),
		"data-fui-network-retry-sse-silence": fmt.Sprintf("%d", cfg.SSESilenceMs),
		"hidden":                             "",
	}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}

	return networkRetryBannerStyle.WrapHTML(render.Tag("div", attrs,
		render.Tag("div", map[string]string{"class": "ui-network-retry-banner__body"},
			render.Tag("strong", map[string]string{"class": "ui-network-retry-banner__title"},
				render.Text(title)),
			render.Tag("span", map[string]string{"class": "ui-network-retry-banner__desc"},
				render.Text(desc)),
		),
		render.Tag("button", map[string]string{
			"type":                          "button",
			"class":                         "ui-button ui-button--secondary ui-network-retry-banner__retry",
			"data-fui-network-retry-button": "",
		}, render.Text(retryLabel)),
	))
}

var networkRetryBannerStyle = registry.RegisterStyle("ui-network-retry-banner", func(_ style.Theme) string {
	return `[data-fui-comp="ui-network-retry-banner"] {
  display: flex;
  align-items: center;
  gap: var(--spacing-md, 12px);
  padding: var(--spacing-md, 12px) var(--spacing-lg, 16px);
  border: 1px solid var(--color-warn, #B45309);
  border-radius: var(--radii-md, 8px);
  background: var(--color-warn-soft, #FEF3C7);
  color: var(--color-warn-strong, #78350F);
  position: sticky;
  inset-block-start: 0;
  z-index: 50;
}
[data-fui-comp="ui-network-retry-banner"] .ui-network-retry-banner__body {
  display: grid;
  gap: var(--spacing-xs, 2px);
  flex: 1 1 auto;
}
[data-fui-comp="ui-network-retry-banner"] .ui-network-retry-banner__title {
  font-weight: 700;
}
[data-fui-comp="ui-network-retry-banner"] .ui-network-retry-banner__desc {
  font-size: var(--text-sm, 0.9rem);
  opacity: 0.9;
}
[data-fui-comp="ui-network-retry-banner"][data-state="checking"] .ui-network-retry-banner__retry {
  cursor: progress;
  opacity: 0.7;
}
`
})
