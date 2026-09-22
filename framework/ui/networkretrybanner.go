package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── NetworkRetryBanner ─────────────────────────────────────────────
//
// Persistent banner that auto-shows when network connectivity looks
// degraded: either the SSE stream has gone silent for too long or
// the app has reported a consecutive run of RPC failures. The banner
// hides when (a) the Retry button's health-check returns 2xx, or
// (b) app code explicitly calls
// `window.__gofastr.networkStatus.reportRecovery()`. The runtime does
// NOT wrap `window.fetch`, so unrelated successful requests do not
// The banner follows the connection the framework reports: it ships
// hidden, the headless module shows it when the framework reports the
// connection lost with a retry scheduled (the offline SystemBanner
// contract), and a successful Retry pings the health endpoint and
// hides it again. The failure-count and SSE-silence triggers retired
// with the old runtime module.
//
// Place once near the top of the page chrome (above the main content,
// inside the persistent shell so SPA navigation doesn't remove it).
//
// Manual API (window.__gofastr.networkStatus): reportFailure() shows
// every mounted offline banner, reportRecovery() hides them — wire
// them into app-level connection signals when a page has no SSE
// stream to follow.

// NetworkRetryBannerConfig configures the banner.
type NetworkRetryBannerConfig struct {
	// HealthEndpoint is the URL the Retry button pings to test
	// connectivity. Must return 2xx when the server is healthy.
	// Required.
	HealthEndpoint string

	// Title is the banner heading. Default "Connection lost".
	Title string

	// Description is the body text. Default explains the recovery
	// action.
	Description string

	// RetryLabel is the retry button text. Default "Retry now".
	RetryLabel string

	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the banner's root element.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-fui-*, role, aria-live, and hidden (the runtime
	// un-hides the banner when connectivity degrades).
	ExtraAttrs html.Attrs
}

// NetworkRetryBanner renders the (initially hidden) banner.
func NetworkRetryBanner(cfg NetworkRetryBannerConfig) render.HTML {
	if cfg.HealthEndpoint == "" {
		panic("ui: NetworkRetryBanner requires HealthEndpoint")
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
	noDismiss := false

	// The offline SystemBanner owns the banner: it ships hidden, the
	// headless module shows it when the framework reports the
	// connection lost with a retry scheduled, and its ending is the
	// reconnect. The retry control is a real link to the health
	// endpoint — no script reloads through it, script re-fetches it.
	parts := headless.Parts{}
	if cfg.Class != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": cfg.Class}}
	}
	retry := render.Tag("a", map[string]string{
		"href":                   cfg.HealthEndpoint,
		"class":                  buttonClassTokens(ButtonSecondary, ButtonSizeDefault) + " fui-network-retry-banner__retry",
		"data-hui-network-retry": "",
	}, render.Text(retryLabel))
	_ = noDismiss
	return networkRetryBannerStyle.WrapHTML(headless.SystemBanner(headless.SystemBannerProps{
		ID:      orDefaultStr(cfg.ID, "network-offline"),
		Tone:    "warning",
		Title:   title,
		Text:    desc,
		Action:  retry,
		Dismiss: &noDismiss,
		Offline: true,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "role", "aria-live", "hidden",
			"data-fui-network-retry-health", "data-fui-network-retry-threshold"),
		Parts:   parts,
		Strings: StringsFor(nil),
	}, networkRetryClasses))
}

// networkRetryClasses dresses the offline SystemBanner in this
// package's own vocabulary.
var networkRetryClasses = headless.Classes{
	headless.PartRoot:           "fui-network-retry-banner",
	headless.PartVisuallyHidden: "fui-visually-hidden",
	headless.PartTitle:          "fui-network-retry-banner__title",
	headless.PartText:           "fui-network-retry-banner__desc",
	headless.PartActions:        "fui-network-retry-banner__actions",
}

var networkRetryBannerStyle = registry.RegisterStyle("ui-network-retry-banner", func(_ style.Theme) string {
	return `[data-fui-comp="ui-network-retry-banner"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}
[data-fui-comp="ui-network-retry-banner"] {
  display: grid;
  grid-template-columns: 1fr auto;
  align-items: center;
  column-gap: var(--spacing-md, 8px);
  row-gap: var(--spacing-xs, 2px);
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  border: 1px solid var(--color-warn, #B45309);
  border-radius: var(--radii-md, 8px);
  background: var(--color-warn-soft, #FEF3C7);
  color: var(--color-warn-strong, #78350F);
  position: sticky;
  inset-block-start: 0;
  z-index: 50;
}
/* SystemBanner's parts are the banner's own children: title over
   description in the first column, the retry link beside both. */
[data-fui-comp="ui-network-retry-banner"] .fui-network-retry-banner__title {
  grid-column: 1;
  grid-row: 1;
  margin: 0;
  font-weight: 700;
}
[data-fui-comp="ui-network-retry-banner"] .fui-network-retry-banner__desc {
  grid-column: 1;
  grid-row: 2;
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  opacity: 0.9;
}
[data-fui-comp="ui-network-retry-banner"] .fui-network-retry-banner__actions {
  grid-column: 2;
  grid-row: 1 / span 2;
}
[data-fui-comp="ui-network-retry-banner"] .fui-network-retry-banner__retry {
  /* Self-contained secondary button: the link carries the button
     classes, but this sheet must not depend on ui.Button's being
     loaded — a page whose only control is the banner still shows a
     button. Same variables buttonCSS reads, so with both sheets
     loaded the computed style is identical. */
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: var(--fui-density-control-h, var(--spacing-touch-target, 44px));
  padding: 10px var(--spacing-lg, 16px);
  border: 1px solid var(--color-border-strong, var(--color-border, #d0d0d8));
  border-radius: var(--fui-button-radius, var(--radii-md, 8px));
  background: var(--color-surface, #fff);
  color: var(--color-text, #18181B);
  font: inherit;
  font-weight: 600;
  text-decoration: none;
  cursor: pointer;
  white-space: nowrap;
}
[data-fui-comp="ui-network-retry-banner"][data-state="checking"] .fui-network-retry-banner__retry {
  cursor: progress;
  opacity: 0.7;
}
`
})
