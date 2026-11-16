package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

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
	SpinnerGrid SpinnerVariant = "grid" // a 3 by 3 rippling grid, for long operations
)

// SpinnerConfig configures a spinner.
type SpinnerConfig struct {
	Label   string // the announced text; defaults to "Loading…" in Ctx's language
	Size    SpinnerSize
	Variant SpinnerVariant
	Inline  bool // sits next to text instead of centering in its own row
	ID      string
	Class   string
	// ExtraAttrs reach the root; class, id, style, role and data-cui-* are dropped.
	ExtraAttrs html.Attrs
	// Ctx is the request context the default label is said in.
	Ctx context.Context
}

var spinnerClasses = headless.Classes{
	headless.PartRoot:              "fui-spinner",
	headless.PartSpinnerRing:       "fui-spinner__ring",
	headless.PartSpinnerDots:       "fui-spinner__dots",
	headless.PartSpinnerDot:        "fui-spinner__dot",
	headless.PartSpinnerGrid:       "fui-spinner__grid",
	headless.PartSpinnerCell:       "fui-spinner__cell",
	headless.PartVisuallyHidden:    "fui-visually-hidden",
	headless.Part("root--size-sm"): "fui-spinner--sm",
	headless.Part("root--size-lg"): "fui-spinner--lg",
}

// Spinner renders a loading indicator that announces its label as a status.
func Spinner(cfg SpinnerConfig) render.HTML {
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	label := cfg.Label
	if label == "" {
		label = i18nui.Loading.Text(ctx)
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
