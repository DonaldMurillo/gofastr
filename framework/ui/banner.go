package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// bannerDismissCookiePrefix is the key the headless module's
// rememberDismissal writes (behavior.js): the dismissal is mirrored
// into a cookie under this key — the id component-encoded — so the
// server can skip rendering a dismissed banner.
const bannerDismissCookiePrefix = "gofastr.banner-dismiss."

// ─── Banner / InlineAlert ───────────────────────────────────────────
//
// A persistent in-page status strip. Different from Toast (transient,
// floating) and Notification (record-bound). Banner is for global
// status: maintenance notices, billing alerts, deprecation warnings,
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
	// records the dismissal in localStorage AND a same-name cookie; when
	// Ctx also carries the request (app.WithRequest, layouts and screens
	// get this automatically), Banner sees the cookie and renders nothing
	// at all on later requests, no flash of a dismissed banner before
	// the runtime's hide pass. (Richer server-side persistence is still
	// up to the app. Banner doesn't ship its own RPC.)
	Dismissible bool
	DismissID   string
	// Action is an optional inline call-to-action (a Link or Button
	// rendered to the right of the body).
	Action render.HTML
	// ID / Class are passed through to the outer element.
	ID    string
	Class string
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the banner's root <div>.
	// Keys the component owns are dropped: class and id (use Class /
	// ID), data-fui-*, and the severity contract (role, aria-live,
	// derived from Variant).
	ExtraAttrs html.Attrs
	// Ctx carries the per-request context used to resolve the dismiss label.
	// When nil, English fallbacks apply.
	Ctx context.Context
}

// Banner renders a persistent page-status strip.
//
// SR semantics: every tone is a polite status — role="status" — so a
// banner never interrupts; the offline banner (NetworkRetryBanner) is
// the one that alerts, because everything the reader does next fails
// until the connection is back.
func Banner(cfg BannerConfig) render.HTML {
	if cfg.Title == "" {
		panic("ui: Banner requires Title")
	}
	tone := string(cfg.Variant)
	if cfg.Variant == BannerWarn {
		tone = "warning"
	}
	switch cfg.Variant {
	case BannerInfo, BannerSuccess, BannerWarn, BannerDanger:
	default:
		panic("ui: Banner unknown Variant " + string(cfg.Variant) +
			`. Pick one of: "" (info), success, warn, danger`)
	}
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	// Flash-free dismissal: the runtime mirrors a DismissID dismissal into
	// a cookie; if this request carries it, skip the banner entirely.
	if cfg.DismissID != "" {
		if r := app.RequestFromContext(ctx); r != nil {
			if c, err := r.Cookie(bannerDismissCookiePrefix + cfg.DismissID); err == nil && c.Value == "1" {
				return render.HTML("")
			}
		}
	}

	// A banner is page-wide, so the SystemBanner primitive owns it:
	// the message is shown, the dismiss is remembered by the headless
	// module for the session, and the posture is polite whatever the
	// tone (the offline banner is the one that alerts).
	dismiss := cfg.Dismissible
	parts := headless.Parts{}
	if cfg.Class != "" {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": cfg.Class}}
	}
	return bannerStyle.WrapHTML(headless.SystemBanner(headless.SystemBannerProps{
		ID:     orDefaultStr(orDefaultStr(cfg.DismissID, cfg.ID), autoID("banner")),
		Icon:   render.HTML(bannerIcon(cfg.Variant)),
		Tone:   tone,
		Title:  cfg.Title,
		Text:   cfg.Body,
		Action: cfg.Action,
		// SystemBanner's own roles already carry the severity: the
		// offline/urgent banner alerts, the rest are polite status.
		Dismiss:      &dismiss,
		DismissLabel: i18nui.T(ctx, i18nui.KeyBannerDismiss),
		Shown:        true,
		ExtraAttrs:   headless.Safe(cfg.ExtraAttrs, "role", "aria-live"),
		Parts:        parts,
		Strings:      StringsFor(ctx),
	}, bannerClasses))
}

func orDefaultStr(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

// bannerClasses dresses headless.SystemBanner's parts in this
// package's own vocabulary — the names the registered ui-banner sheet
// matches.
var bannerClasses = headless.Classes{
	headless.PartRoot:           "fui-banner",
	headless.PartVisuallyHidden: "fui-visually-hidden",
	headless.PartIcon:           "fui-banner__icon",
	headless.PartTitle:          "fui-banner__title",
	headless.PartText:           "fui-banner__body",
	headless.PartActions:        "fui-banner__action",
	headless.PartDismiss:        "fui-banner__dismiss",

	"root--info":    "fui-banner--info",
	"root--success": "fui-banner--success",
	"root--warning": "fui-banner--warn",
	"root--danger":  "fui-banner--danger",
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

func bannerCSS(_ style.Theme) string {
	return `[data-fui-comp="ui-banner"] {
  display: grid;
  grid-template-columns: auto 1fr auto auto;
  column-gap: var(--spacing-md, 8px);
  row-gap: var(--spacing-xs, 2px);
  align-items: start;
  padding: var(--spacing-md, 8px) var(--spacing-lg, 16px);
  border: 1px solid var(--ui-banner-accent, var(--color-info, #3B82F6));
  border-radius: var(--radii-md, 8px);
  background: var(--color-surface, #FFFFFF);
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-banner"] .fui-visually-hidden {
  position: absolute;
  inline-size: 1px;
  block-size: 1px;
  padding: 0;
  margin: -1px;
  overflow: hidden;
  clip: rect(0, 0, 0, 0);
  white-space: nowrap;
}
/* SystemBanner's parts are the banner's own children: the icon and
   the controls span both rows, the title sits over the body. */
[data-fui-comp="ui-banner"] .fui-banner__icon {
  grid-column: 1;
  grid-row: 1 / span 2;
  display: inline-flex;
  color: var(--color-info, #3B82F6);
  margin-top: var(--spacing-xs, 2px);
}
[data-fui-comp="ui-banner"] .fui-banner__title {
  grid-column: 2;
  grid-row: 1;
  min-width: 0;
  margin: 0;
  font-weight: 600;
  font-size: var(--text-base, 1rem);
}
[data-fui-comp="ui-banner"] .fui-banner__body {
  grid-column: 2;
  grid-row: 2;
  min-width: 0;
  margin: 0;
  color: var(--color-text-muted, #52525B);
  font-size: var(--text-sm, 0.875rem);
  line-height: 1.45;
}
[data-fui-comp="ui-banner"] .fui-banner__action {
  grid-column: 3;
  grid-row: 1 / span 2;
  display: inline-flex;
  align-items: center;
}
[data-fui-comp="ui-banner"] .fui-banner__dismiss {
  grid-column: 4;
  grid-row: 1 / span 2;
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
  font-size: var(--text-lg, 1.125rem);
  line-height: 1;
  border-radius: var(--radii-sm, 4px);
  margin: -8px -8px -8px 0;
}
[data-fui-comp="ui-banner"] .fui-banner__dismiss:hover {
  background: var(--color-surface-soft, #F4F4F5);
  color: var(--color-text, #18181B);
}
[data-fui-comp="ui-banner"] .fui-banner__dismiss:focus-visible {
  outline: 2px solid var(--color-primary, #4F46E5);
  outline-offset: 2px;
}

/* Variants — use the full outline + icon, avoiding a decorative side stripe. */
.fui-banner--success { --ui-banner-accent: var(--color-success, #16A34A); }
.fui-banner--success .fui-banner__icon { color: var(--color-success, #16A34A); }
.fui-banner--warn { --ui-banner-accent: var(--color-warning, #D97706); }
.fui-banner--warn .fui-banner__icon { color: var(--color-warning, #D97706); }
.fui-banner--danger { --ui-banner-accent: var(--color-danger, #DC2626); }
.fui-banner--danger .fui-banner__icon { color: var(--color-danger, #DC2626); }

/* Hidden state for runtime dismiss. */
[data-fui-comp="ui-banner"][hidden] { display: none; }`
}
