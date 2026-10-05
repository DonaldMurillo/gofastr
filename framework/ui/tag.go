package ui

import (
	"fmt"
	"strings"

	"context"
	"maps"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/render"

	"github.com/DonaldMurillo/gofastr/framework/headless"
)

// ─── Tag / Chip ─────────────────────────────────────────────────────
//
// An interactive pill, distinct from StatusBadge in that a Tag can be
// removed (a small × button) and can carry an Href to act as a filter
// link. Use for filter-chip lists, multi-select selections, applied
// search filters.

// TagConfig configures a tag/chip.
type TagConfig struct {
	// Label is the visible text. Required.
	Label string

	// Variant maps to the same StatusVariant set as StatusBadge so
	// status-coded tags compose with the rest of the system. Default
	// neutral.
	Variant StatusVariant

	// Href makes the entire tag an anchor (e.g. a filter link).
	Href string

	// Dismiss, when non-empty, is the href of the × link that removes
	// the tag. A dismissal is an in-page state change, so the Island
	// that re-renders the region is required with it: the same link is
	// the no-script destination and the island's trigger.
	Dismiss string

	// Island is where the dismissal goes with script: the endpoint
	// that renders the region again and the signal it is bound to.
	// Required when Dismiss is set.
	Island headless.Island

	// DismissMethod is the method the dismissal is sent with. Empty is
	// GET, a read whose Dismiss href the island writes to the URL after
	// the swap; a mutation (POST, PUT, PATCH, DELETE) writes no URL.
	DismissMethod string

	// DismissBody is a static JSON body the dismissal sends
	// (data-cui-rpc-body). Needs a DismissMethod other than GET.
	DismissBody string

	// Icon renders before the label, aria-hidden.
	Icon render.HTML

	// DismissLabel is the assistive-text label on the × button.
	// Defaults to "Remove <Label>".
	DismissLabel string

	// DismissAttrs forwards extra attributes (data-* test hooks,
	// analytics markers) to the × link. Keys the component owns are
	// dropped: class, id, href, aria-label and data-cui-*. The
	// dismissal's RPC wiring comes from Island, DismissMethod and
	// DismissBody, never from here.
	DismissAttrs html.Attrs

	// Ctx carries the per-request context used to resolve the
	// dismiss-label string. When nil, English fallbacks apply.
	Ctx context.Context

	ID    string
	Class string

	// ExtraAttrs forwards additional attributes (data-* test hooks,
	// analytics markers, ARIA overrides) to the tag's root element,
	// whichever shape it takes (<a> or <span>). Keys the component
	// owns are dropped: class and id (use Class / ID), data-cui-*,
	// and href (use Href; it goes through the URL sanitizer).
	ExtraAttrs html.Attrs
}

// Tag renders a small pill: optionally linked (filter chip), optionally
// removable (dismiss button). Pure server-rendered; dismiss is wired
// through standard `data-cui-rpc` semantics so the application picks
// the response side-effect.
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

	// The chip IS the headless Tag: its root (span, or anchor when
	// Href is set), its label text, its optional icon and its dismiss
	// anchor — one label in the output, whatever shape it renders.
	// The variant and interactive modifiers travel as root class
	// appends: the primitive has no tone of its own.
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
		dismissLabel = fmt.Sprintf(StringsFor(ctx).RemoveLabelled, cfg.Label)
	}
	href := ""
	if cfg.Href != "" {
		// Drop unsafe hrefs: same allow-list as ui.Link; Tag is a
		// content-level component, so a rejected href degrades to an
		// inert "#" rather than panicking.
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

// tagClasses dresses headless.Tag's parts in this package's own
// vocabulary — the names the registered ui-tag sheet matches. The
// variant and interactive modifiers travel as the root's variant
// classes, which the class map names per variant.
var tagClasses = headless.Classes{
	headless.PartRoot:         "fui-tag",
	headless.PartIcon:         "fui-tag__icon",
	headless.PartBadgeDismiss: "fui-tag__dismiss",
}

// flattenAttrs converts html.Attrs (map[string]string) into the
// map[string]string render.Tag expects (same type, different alias).
func flattenAttrs(in html.Attrs) map[string]string {
	out := make(map[string]string, len(in))
	maps.Copy(out, in)
	return out
}
