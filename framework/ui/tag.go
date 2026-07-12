package ui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"

	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// ─── Tag / Chip ─────────────────────────────────────────────────────
//
// An interactive pill — distinct from StatusBadge in that a Tag can be
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

	// Dismiss, when non-empty, renders a × button that fires an RPC
	// to this path on click. Pair with data-fui-rpc-signal in DismissAttrs
	// or simply rely on the runtime's default RPC behavior.
	Dismiss string

	// DismissLabel is the assistive-text label on the × button.
	// Defaults to "Remove <Label>".
	DismissLabel string

	// DismissAttrs lets callers attach extra data-fui-* attributes to
	// the × button (e.g. data-fui-rpc-signal).
	DismissAttrs html.Attrs

	// Ctx carries the per-request context used to resolve the
	// dismiss-label string. When nil, English fallbacks apply.
	Ctx context.Context

	ID    string
	Class string
}

// Tag renders a small pill — optionally linked (filter chip), optionally
// removable (dismiss button). Pure server-rendered; dismiss is wired
// through standard `data-fui-rpc` semantics so the application picks
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

	cls := "ui-tag ui-tag--" + string(v)
	if cfg.Href != "" {
		cls += " ui-tag--interactive"
	}
	if cfg.Class != "" {
		cls += " " + cfg.Class
	}

	labelSpan := html.Span(html.TextConfig{Class: "ui-tag__label"}, render.Text(cfg.Label))

	body := []render.HTML{labelSpan}
	if cfg.Dismiss != "" {
		dismissLabel := cfg.DismissLabel
		if dismissLabel == "" {
			dismissLabel = i18nui.TVars(ctx, i18nui.KeyTagRemove, map[string]string{"label": cfg.Label})
		}
		attrs := html.Attrs{
			"data-fui-rpc":        cfg.Dismiss,
			"data-fui-rpc-method": "POST",
			"aria-label":          dismissLabel,
			"type":                "button",
			"class":               "ui-tag__dismiss",
		}
		for k, val := range cfg.DismissAttrs {
			attrs[k] = val
		}
		// SVG × icon — kept aria-hidden so the aria-label is the
		// single announced name.
		body = append(body, render.Tag("button", flattenAttrs(attrs),
			html.Span(html.TextConfig{ExtraAttrs: html.Attrs{"aria-hidden": "true"}},
				render.HTML("&times;"))))
	}

	if cfg.Href != "" {
		// Drop unsafe hrefs (javascript:, data:, control bytes, …) —
		// same allow-list as ui.Link; see framework/ui/safety.go. Tag
		// is a content-level component, so a rejected href degrades to
		// an inert "#" rather than panicking.
		href := safeURL(cfg.Href)
		if href == "" {
			href = "#"
		}
		return tagStyle.WrapHTML(html.LinkHTML(html.LinkHTMLConfig{
			Href:    href,
			Class:   cls,
			ID:      cfg.ID,
			Content: render.Join(body...),
		}))
	}
	return tagStyle.WrapHTML(html.Span(html.TextConfig{
		Class: cls, ID: cfg.ID,
	}, body...))
}

// flattenAttrs converts html.Attrs (map[string]string) into the
// map[string]string render.Tag expects (same type, different alias).
func flattenAttrs(in html.Attrs) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
