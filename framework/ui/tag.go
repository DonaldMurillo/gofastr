package ui

import (
	"strings"

	"context"
	"maps"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"

	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// TagConfig configures a tag/chip.
type TagConfig struct {
	Label   string        // required visible text
	Variant StatusVariant // defaults to neutral
	Href    string        // renders the tag as a link; an unsafe Href becomes "#"
	// Dismiss is the href of the × link that removes the tag; it needs Island.
	Dismiss string
	// Island is the endpoint and signal the dismissal re-renders the region through.
	Island headless.Island
	// DismissMethod defaults to GET, whose Dismiss href the island writes
	// to the URL; a mutation writes none.
	DismissMethod string
	// DismissBody is a static JSON body; it needs a DismissMethod other than GET.
	DismissBody  string
	Icon         render.HTML // drawn before the label, aria-hidden
	DismissLabel string      // the × link's name; defaults to "Remove <Label>"
	// DismissAttrs reach the × link; class, id, href, aria-label and data-cui-* are dropped.
	DismissAttrs html.Attrs
	// Ctx is the request context the dismiss label is said in.
	Ctx   context.Context
	ID    string
	Class string
	// ExtraAttrs reach the root <a> or <span>; class, id, href and data-cui-* are dropped.
	ExtraAttrs html.Attrs
}

// Tag renders an interactive pill: optionally a filter link, optionally
// removable through its island.
func Tag(cfg TagConfig) render.HTML {
	if cfg.Label == "" {
		panic("ui: Tag requires Label")
	}
	v := cfg.Variant
	if v == "" {
		v = StatusNeutral
	}
	checkStatusVariant("Tag", v)
	ctx := cfg.Ctx
	if ctx == nil {
		ctx = context.Background()
	}

	var mods []string
	mods = append(mods, "fui-tag--"+string(v))
	if cfg.Href != "" {
		mods = append(mods, "fui-tag--interactive")
	}
	if cfg.Class != "" {
		mods = append(mods, cfg.Class)
	}
	parts := headless.Parts{Attrs: headless.PartAttrs{}}
	if len(mods) > 0 {
		parts.Attrs[headless.PartRoot] = html.Attrs{"class": strings.Join(mods, " ")}
	}
	if dismissAttrs := headless.Safe(cfg.DismissAttrs, "href", "aria-label"); len(dismissAttrs) > 0 {
		parts.Attrs[headless.PartBadgeDismiss] = dismissAttrs
	}
	dismissLabel := cfg.DismissLabel
	if dismissLabel == "" && cfg.Dismiss != "" {
		dismissLabel = i18n.FillContext(ctx, StringsFor(ctx).RemoveLabelled, map[string]any{"label": cfg.Label})
	}
	href := ""
	if cfg.Href != "" {
		href = urlsafe.CleanAnchor(cfg.Href)
		if href == "" {
			href = "#"
		}
	}
	return tagStyle.WrapHTML(headless.Tag(headless.TagProps{
		Label:            cfg.Label,
		Icon:             cfg.Icon,
		DismissHref:      cfg.Dismiss,
		DismissAriaLabel: dismissLabel,
		Href:             href,
		Island:           cfg.Island,
		DismissMethod:    cfg.DismissMethod,
		DismissBody:      cfg.DismissBody,
		ID:               cfg.ID,
		ExtraAttrs:       headless.Safe(cfg.ExtraAttrs, "class", "id", "href"),
		Parts:            parts,
		Strings:          StringsFor(ctx),
	}, tagClasses))
}

var tagClasses = headless.Classes{
	headless.PartRoot:         "fui-tag",
	headless.PartIcon:         "fui-tag__icon",
	headless.PartLabel:        "fui-tag__label",
	headless.PartBadgeDismiss: "fui-tag__dismiss",
}

func flattenAttrs(in html.Attrs) map[string]string {
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
