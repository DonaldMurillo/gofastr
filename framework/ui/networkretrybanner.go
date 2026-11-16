package ui

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// NetworkRetryBannerConfig configures the banner.
type NetworkRetryBannerConfig struct {
	// Ctx resolves the default title, text and retry label; nil uses English.
	Ctx context.Context

	// HealthEndpoint is the required relative or http(s) URL Retry fetches; a 2xx means recovery.
	HealthEndpoint string

	// Title is the heading. Default "Connection lost".
	Title string

	// Description is the body text. Default explains the recovery.
	Description string

	// RetryLabel is the retry link's text. Default "Retry now".
	RetryLabel string

	ID    string
	Class string

	// ExtraAttrs forwards attributes to the root; class, id, data-cui-*, role, aria-live and hidden are dropped.
	ExtraAttrs html.Attrs
}

// NetworkRetryBanner renders the offline banner, hidden until the runtime
// reports the connection lost. Mount it once, in the persistent shell.
func NetworkRetryBanner(cfg NetworkRetryBannerConfig) render.HTML {
	if cfg.HealthEndpoint == "" {
		panic("ui: NetworkRetryBanner requires HealthEndpoint")
	}
	if urlsafe.CleanAnchor(cfg.HealthEndpoint) == "" {
		panic(fmt.Sprintf("ui: NetworkRetryBanner HealthEndpoint %q is not a safe link: use a relative path or an http(s) URL", cfg.HealthEndpoint))
	}
	title := cfg.Title
	if title == "" {
		title = i18nui.NetworkRetryTitle.Text(cfg.Ctx)
	}
	desc := cfg.Description
	if desc == "" {
		desc = i18nui.NetworkRetryText.Text(cfg.Ctx)
	}
	retryLabel := cfg.RetryLabel
	if retryLabel == "" {
		retryLabel = i18nui.NetworkRetryNowText.Text(cfg.Ctx)
	}
	noDismiss := false
	parts := headless.Parts{}
	if cfg.Class != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": cfg.Class}}
	}
	retry := render.Tag("a", map[string]string{
		"href":                   cfg.HealthEndpoint,
		"class":                  buttonClassTokens(ButtonSecondary, ButtonSizeDefault) + " fui-network-retry-banner__retry",
		"data-hui-network-retry": "",
	}, render.Text(retryLabel))
	return networkRetryBannerStyle.WrapHTML(headless.SystemBanner(headless.SystemBannerProps{
		ID:      orDefaultStr(cfg.ID, "network-offline"),
		Tone:    "warning",
		Title:   title,
		Text:    desc,
		Action:  headless.Own(retry),
		Dismiss: &noDismiss,
		Offline: true,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "role", "aria-live", "hidden",
			"data-fui-network-retry-health", "data-fui-network-retry-threshold"),
		Parts:   parts,
		Strings: StringsFor(cfg.Ctx),
	}, networkRetryClasses))
}

var networkRetryClasses = headless.Classes{
	headless.PartRoot:           "fui-network-retry-banner",
	headless.PartVisuallyHidden: "fui-visually-hidden",
	headless.PartTitle:          "fui-network-retry-banner__title",
	headless.PartText:           "fui-network-retry-banner__desc",
	headless.PartActions:        "fui-network-retry-banner__actions",
}

var networkRetryBannerStyle = registry.RegisterStyle("ui-network-retry-banner", func(_ style.Theme) string {
	return `[data-cui-comp="ui-network-retry-banner"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}
[data-cui-comp="ui-network-retry-banner"] {
  display: grid;
  grid-template-columns: 1fr auto;
  align-items: center;
  column-gap: var(--spacing-md, 8px);
  row-gap: var(--spacing-xs, 2px);
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  border: var(--stroke-thin, 1px) solid var(--color-warning, #854D0E);
  border-radius: var(--radii-md, 8px);
  background: color-mix(in srgb, var(--color-warning) 15%, transparent);
  color: color-mix(in srgb, var(--color-warning) 80%, var(--color-text));
  position: sticky;
  inset-block-start: var(--ui-sticky-top, 0px);
  z-index: var(--z-sticky, 200);
}
/* SystemBanner's parts are the banner's own children: title over
   description in the first column, the retry link beside both. */
[data-cui-comp="ui-network-retry-banner"] .fui-network-retry-banner__title {
  grid-column: 1;
  grid-row: 1;
  margin: 0;
  font-weight: var(--font-weight-bold);
}
[data-cui-comp="ui-network-retry-banner"] .fui-network-retry-banner__desc {
  grid-column: 1;
  grid-row: 2;
  margin: 0;
  font-size: var(--text-sm, 0.875rem);
  opacity: var(--ui-network-retry-banner-desc-opacity, 0.9);
}
[data-cui-comp="ui-network-retry-banner"] .fui-network-retry-banner__actions {
  grid-column: 2;
  grid-row: 1 / span 2;
}
[data-cui-comp="ui-network-retry-banner"] .fui-network-retry-banner__retry {
  /* Self-contained secondary button: the link carries the button
     classes, but this sheet must not depend on ui.Button's being
     loaded — a page whose only control is the banner still shows a
     button. Same variables buttonCSS reads, so with both sheets
     loaded the computed style is identical. */
  display: inline-flex;
  align-items: center;
  justify-content: center;
  min-height: var(--fui-density-control-h, var(--spacing-touch-target, 44px));
  padding: calc(var(--spacing-sm, 4px) * 2.5) var(--spacing-lg, 16px);
  border: var(--stroke-thin, 1px) solid var(--color-border-strong, var(--color-border, #E4E4E7));
  border-radius: var(--fui-button-radius, var(--radii-md, 8px));
  background: var(--color-surface, #fff);
  color: var(--color-text, #09090B);
  font: inherit;
  font-weight: var(--font-weight-semibold);
  text-decoration: none;
  cursor: pointer;
  white-space: nowrap;
}
[data-cui-comp="ui-network-retry-banner"][data-state="checking"] .fui-network-retry-banner__retry {
  cursor: progress;
  opacity: var(--ui-network-retry-banner-retry-checking-opacity, 0.7);
}
`
})
