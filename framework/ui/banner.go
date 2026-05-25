package ui

import (
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ─── Banner / InlineAlert ───────────────────────────────────────────
//
// A persistent in-page status strip. Different from Toast (transient,
// floating) and Notification (record-bound). Banner is for global
// status — maintenance notices, billing alerts, deprecation warnings,
// "you're impersonating" admin reminders.

// BannerVariant picks the color / icon family.
type BannerVariant string

const (
	BannerInfo    BannerVariant = ""
	BannerSuccess BannerVariant = "success"
	BannerWarn    BannerVariant = "warn"
	BannerDanger  BannerVariant = "danger"
)

// BannerConfig configures a Banner.
type BannerConfig struct {
	// Title is the bold lead-in (required).
	Title string
	// Body is the supporting text (optional).
	Body string
	// Variant picks color + role. Defaults to BannerInfo.
	Variant BannerVariant
	// Dismissible adds an X button. When DismissID is set the runtime
	// records the dismissal in localStorage so the same banner doesn't
	// re-appear on the next page load. (Server-side persistence is up
	// to the app — Banner doesn't ship its own RPC.)
	Dismissible bool
	DismissID   string
	// Action is an optional inline call-to-action (a Link or Button
	// rendered to the right of the body).
	Action render.HTML
	// ID / Class / Attrs are passed through to the outer element.
	ID    string
	Class string
	ExtraAttrs html.Attrs
}

// Banner renders a persistent page-status strip.
//
// SR semantics: BannerWarn/Danger emit role="alert" so an injected
// banner is announced; BannerInfo/Success use role="status" so the
// announcement is polite (doesn't interrupt screen-reader reading).
func Banner(cfg BannerConfig) render.HTML {
	if cfg.Title == "" {
		panic("ui: Banner requires Title")
	}
	switch cfg.Variant {
	case BannerInfo, BannerSuccess, BannerWarn, BannerDanger:
		// recognized
	default:
		panic("ui: Banner unknown Variant " + string(cfg.Variant) +
			` — pick one of: "" (info), success, warn, danger`)
	}

	cls := "ui-banner"
	if cfg.Variant != BannerInfo {
		cls += " ui-banner--" + string(cfg.Variant)
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	attrs := html.Attrs{"class": cls}
	if cfg.ID != "" {
		attrs["id"] = cfg.ID
	}
	// role + aria-live tailored to severity. Danger/Warn = alert
	// (assertive, interrupts SR reading); Info/Success = status
	// (polite, queued).
	switch cfg.Variant {
	case BannerWarn, BannerDanger:
		attrs["role"] = "alert"
	default:
		attrs["role"] = "status"
		attrs["aria-live"] = "polite"
	}
	for k, v := range cfg.ExtraAttrs {
		attrs[k] = v
	}

	children := []render.HTML{
		render.Tag("div", map[string]string{"class": "ui-banner__icon", "aria-hidden": "true"},
			render.HTML(bannerIcon(cfg.Variant))),
	}

	bodyChildren := []render.HTML{
		html.Paragraph(html.TextConfig{Class: "ui-banner__title"}, render.Text(cfg.Title)),
	}
	if cfg.Body != "" {
		bodyChildren = append(bodyChildren,
			html.Paragraph(html.TextConfig{Class: "ui-banner__body"}, render.Text(cfg.Body)))
	}
	children = append(children,
		render.Tag("div", map[string]string{"class": "ui-banner__content"}, bodyChildren...))

	if cfg.Action != "" {
		children = append(children,
			render.Tag("div", map[string]string{"class": "ui-banner__action"}, cfg.Action))
	}

	if cfg.Dismissible {
		dismissAttrs := map[string]string{
			"type":         "button",
			"class":        "ui-banner__dismiss",
			"aria-label":   "Dismiss",
			"data-fui-banner-dismiss": "true",
		}
		if cfg.DismissID != "" {
			dismissAttrs["data-fui-banner-dismiss-id"] = cfg.DismissID
		}
		children = append(children,
			render.Tag("button", dismissAttrs, render.HTML(bannerCloseIcon())))
	}

	return bannerStyle.WrapHTML(render.Tag("div", attrs, children...))
}

var bannerStyle = registry.RegisterStyle("ui-banner", bannerCSS)

func bannerIcon(v BannerVariant) string {
	switch v {
	case BannerSuccess:
		return `<svg width="20" height="20" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M16.667 5l-7.5 7.5L5 8.333" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`
	case BannerWarn:
		return `<svg width="20" height="20" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M10 7v4m0 3h.01M2.5 16.667h15a1.667 1.667 0 001.443-2.5l-7.5-12.5a1.667 1.667 0 00-2.886 0l-7.5 12.5a1.667 1.667 0 001.443 2.5z" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/></svg>`
	case BannerDanger:
		return `<svg width="20" height="20" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg"><circle cx="10" cy="10" r="8.333" stroke="currentColor" stroke-width="2"/><path d="M10 6.667v3.333M10 13.333h.008" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>`
	default: // info
		return `<svg width="20" height="20" viewBox="0 0 20 20" fill="none" xmlns="http://www.w3.org/2000/svg"><circle cx="10" cy="10" r="8.333" stroke="currentColor" stroke-width="2"/><path d="M10 13.333V10M10 6.667h.008" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>`
	}
}

func bannerCloseIcon() string {
	return `<svg width="14" height="14" viewBox="0 0 14 14" fill="none" xmlns="http://www.w3.org/2000/svg"><path d="M11 3L3 11M3 3l8 8" stroke="currentColor" stroke-width="2" stroke-linecap="round"/></svg>`
}

func bannerCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-banner"] {
  display: flex;
  gap: var(--spacing-md, 12px);
  align-items: flex-start;
  padding: var(--spacing-md, 12px) var(--spacing-lg, 16px);
  border: 1px solid var(--color-border, #E4E4E7);
  border-left: 4px solid var(--color-info, #3B82F6);
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-banner"] .ui-banner__icon {
  flex: 0 0 auto;
  display: inline-flex;
  color: var(--color-info, #3B82F6);
  margin-top: 2px;
}
[data-fui-comp="ui-banner"] .ui-banner__content {
  flex: 1 1 auto;
  min-width: 0;
  display: grid;
  gap: 2px;
}
[data-fui-comp="ui-banner"] .ui-banner__title {
  margin: 0;
  font-weight: 600;
  font-size: 0.95rem;
}
[data-fui-comp="ui-banner"] .ui-banner__body {
  margin: 0;
  color: var(--color-text-muted, #52525B);
  font-size: 0.9rem;
  line-height: 1.45;
}
[data-fui-comp="ui-banner"] .ui-banner__action {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
}
[data-fui-comp="ui-banner"] .ui-banner__dismiss {
  flex: 0 0 auto;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  /* WCAG 2.5.5 — even the dismiss X needs the 44px tap floor. */
  min-block-size: var(--spacing-touch-target, 44px);
  min-inline-size: var(--spacing-touch-target, 44px);
  background: transparent;
  border: 0;
  color: var(--color-text-muted, #52525B);
  cursor: pointer;
  border-radius: var(--radii-sm, 4px);
  margin: -8px -8px -8px 0;
}
[data-fui-comp="ui-banner"] .ui-banner__dismiss:hover {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-banner"] .ui-banner__dismiss:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}

/* Variants — recolor border-left + icon. */
.ui-banner--success { border-left-color: var(--color-success, #16A34A); }
.ui-banner--success .ui-banner__icon { color: var(--color-success, #16A34A); }
.ui-banner--warn { border-left-color: var(--color-warning, #D97706); }
.ui-banner--warn .ui-banner__icon { color: var(--color-warning, #D97706); }
.ui-banner--danger { border-left-color: var(--color-danger, #DC2626); }
.ui-banner--danger .ui-banner__icon { color: var(--color-danger, #DC2626); }

/* Hidden state for runtime dismiss. */
[data-fui-comp="ui-banner"][hidden] { display: none; }`
}
