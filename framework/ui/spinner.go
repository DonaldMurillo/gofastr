package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── Spinner ────────────────────────────────────────────────────────
//
// headless.Spinner carries the contract: role=status, a hidden visual
// and a label that says what is being waited for. This adapter adds
// this package's visual vocabulary — the three shapes, the sizes, the
// inline posture — through the class map.

// SpinnerSize selects a named size.
type SpinnerSize string

const (
	SpinnerSm SpinnerSize = "sm"
	SpinnerMd SpinnerSize = "" // default
	SpinnerLg SpinnerSize = "lg"
)

// SpinnerVariant selects the visual style.
type SpinnerVariant string

const (
	SpinnerRing SpinnerVariant = "" // default: bordered ring
	SpinnerDots SpinnerVariant = "dots"
	// SpinnerGrid renders a 3×3 grid of small squares animated in a
	// staggered ripple. Distinct enough from ring/dots to be the
	// "loading…heavy" indicator on long-running operations.
	SpinnerGrid SpinnerVariant = "grid"
)

// SpinnerConfig configures a spinner.
type SpinnerConfig struct {
	// Label is the assistive-text announced by screen readers.
	// Defaults to "Loading…" when empty.
	Label string

	// Size selects a named size (sm | md (default) | lg).
	Size SpinnerSize

	// Variant selects the visual treatment.
	Variant SpinnerVariant

	// Inline true renders inline-flex (sits next to text); false
	// renders block (centered in its own row).
	Inline bool

	ID    string
	Class string
	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers) to the spinner's root element. Keys the
	// component owns are dropped: class and id (use Class / ID),
	// style, data-fui-* and role — the live-region contract is the
	// primitive's.
	ExtraAttrs html.Attrs
	// Ctx carries the per-request context used to resolve the loading label.
	// When nil, English fallbacks apply.
	Ctx context.Context
}

// spinnerClasses dresses headless.Spinner's parts.
var spinnerClasses = headless.Classes{
	headless.PartRoot:              "fui-spinner",
	headless.PartSpinnerRing:       "fui-spinner__ring",
	headless.PartSpinnerDots:       "fui-spinner__dots",
	headless.PartSpinnerDot:        "fui-spinner__dot",
	headless.PartSpinnerGrid:       "fui-spinner__grid",
	headless.PartSpinnerCell:       "fui-spinner__cell",
	headless.PartVisuallyHidden:    "ui-visually-hidden",
	headless.Part("root--size-sm"): "fui-spinner--sm",
	headless.Part("root--size-lg"): "fui-spinner--lg",
}

// Spinner renders a loading indicator on headless.Spinner.
//
// Pair with data-fui-rpc lifecycle to surface pending state on
// island-side updates: the runtime adds `aria-busy="true"` to the
// containing form / button while the RPC is in flight, so a CSS
// rule can switch a sibling Spinner from `visibility:hidden` to
// visible without any per-component wiring.
func Spinner(cfg SpinnerConfig) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	label := cfg.Label
	if label == "" {
		label = i18nui.T(ctx, i18nui.KeyLoading)
	}
	cls := cfg.Class
	if cfg.Inline {
		cls = joinNonEmpty("fui-spinner--inline", cls)
	}
	return spinnerStyle.WrapHTML(headless.Spinner(headless.SpinnerProps{
		Label:      label,
		Announce:   true,
		Size:       string(cfg.Size),
		Variant:    string(cfg.Variant),
		ID:         cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "role"),
		Parts:      rootClassParts(cls),
	}, spinnerClasses))
}
