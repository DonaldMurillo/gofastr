package ui

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// BackToTopPosition selects which corner the button anchors to.
type BackToTopPosition string

const (
	BackToTopBottomRight BackToTopPosition = "br"
	BackToTopBottomLeft  BackToTopPosition = "bl"
	BackToTopTopRight    BackToTopPosition = "tr"
	BackToTopTopLeft     BackToTopPosition = "tl"
)

// BackToTopSize controls the button diameter.
type BackToTopSize string

const (
	BackToTopSM BackToTopSize = "sm"
	BackToTopMD BackToTopSize = ""
	BackToTopLG BackToTopSize = "lg"
)

// BackToTopVariant selects the color variant.
type BackToTopVariant string

const (
	BackToTopPrimary   BackToTopVariant = ""
	BackToTopSecondary BackToTopVariant = "secondary"
	BackToTopGhost     BackToTopVariant = "ghost"
)

// BackToTopOffset is the distance from the viewport edge.
type BackToTopOffset string

const (
	BackToTopOffsetNone BackToTopOffset = "none"
	BackToTopOffsetSM   BackToTopOffset = "sm"
	BackToTopOffsetMD   BackToTopOffset = ""
	BackToTopOffsetLG   BackToTopOffset = "lg"
	BackToTopOffsetXL   BackToTopOffset = "xl"
)

// BackToTopScrollBehavior controls the scroll animation.
type BackToTopScrollBehavior string

const (
	BackToTopSmooth  BackToTopScrollBehavior = ""
	BackToTopInstant BackToTopScrollBehavior = "instant"
)

// BackToTopConfig configures the back-to-top button.
type BackToTopConfig struct {
	// Ctx is the language of the accessible name; nil is English.
	Ctx context.Context
	// Position is the corner; empty is BackToTopBottomRight.
	Position BackToTopPosition
	// Icon replaces the chevron.
	Icon render.HTML
	// ThresholdPx is the scroll distance that shows the button; 0 is 400.
	ThresholdPx int
	// Smooth is the scroll animation; empty is smooth.
	Smooth BackToTopScrollBehavior
	// Size is the button diameter; empty is 2.75rem.
	Size BackToTopSize
	// Variant is the colour scheme; empty is solid primary.
	Variant BackToTopVariant
	// Offset is the distance from the viewport edge.
	Offset BackToTopOffset
	// Label is the accessible name. Defaults to "Back to top".
	Label string
	// ScrollTarget is the element id to scroll to, "#" optional; empty is the top.
	ScrollTarget string
	// ID is the root's id.
	ID string
	// Class is an extra class on the root.
	Class string
	// ExtraAttrs land on the root; class, id, href, aria-label and the wiring are dropped.
	ExtraAttrs html.Attrs
}

// BackToTop renders a fixed button that appears past ThresholdPx and scrolls back up.
func BackToTop(cfg BackToTopConfig) render.HTML {
	threshold := cfg.ThresholdPx
	if threshold == 0 {
		threshold = 400
	}
	target := strings.TrimPrefix(cfg.ScrollTarget, "#")

	position := cfg.Position
	if position == "" {
		position = BackToTopBottomRight
	}
	mods := []string{"fui-back-to-top--" + string(position)}
	if cfg.Size != "" {
		mods = append(mods, "fui-back-to-top--"+string(cfg.Size))
	}
	if cfg.Variant != "" {
		mods = append(mods, "fui-back-to-top--"+string(cfg.Variant))
	}
	if cfg.Offset != "" && cfg.Offset != BackToTopOffsetMD {
		mods = append(mods, "fui-back-to-top--offset-"+string(cfg.Offset))
	}
	if cfg.Class != "" {
		mods = append(mods, cfg.Class)
	}
	parts := headless.Parts{}
	if len(mods) > 0 {
		parts.Attrs = headless.PartAttrs{headless.PartRoot: {"class": strings.Join(mods, " ")}}
	}

	icon := cfg.Icon
	if icon == "" {
		icon = headless.Own(Icon("chevron-up", IconConfig{Size: "20"}))
	}

	return backToTopStyle.WrapHTML(headless.BackToTop(headless.BackToTopProps{
		Href:      "#top",
		Target:    target,
		Label:     cfg.Label,
		Threshold: threshold,
		Icon:      icon,
		Smooth:    cfg.Smooth != BackToTopInstant,
		ID:        cfg.ID,
		ExtraAttrs: headless.Safe(cfg.ExtraAttrs, "class", "id", "href",
			"aria-label", "data-hui-back-to-top", "data-hui-back-to-top-target"),
		Parts:   parts,
		Strings: StringsFor(cfg.Ctx),
	}, backToTopClasses))
}

var backToTopClasses = headless.Classes{
	headless.PartRoot:  "fui-back-to-top",
	headless.PartIcon:  "fui-back-to-top__icon",
	headless.PartLabel: "fui-visually-hidden",
}
