package app

import (
	"context"
	"slices"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// LayoutLayer is one level of a screen's resolved layout chain, ordered
// outermost → innermost. The chain is DERIVED at registration/render time
// from what the app declared, the default layout, ScreenGroup nesting,
// per-screen overrides, so ScreenGroup stays the builder API and the
// chain is the single composition model every renderer (full page, SSG,
// subtree partial) and the route manifest share.
type LayoutLayer struct {
	// Layout is the shell rendered at this level. Nil for a marker-only
	// group level: a group whose layout was deduplicated because the same
	// *Layout already renders at an outer level (e.g. a SubGroup that
	// inherited its parent's layout). Marker-only levels still emit their
	// group wrapper so sibling navigation inside the group stays DOM-stable.
	Layout *Layout
	// GroupPrefix is the ScreenGroup prefix that produced this level
	// ("/settings/"). Empty for the app-default root and for a direct
	// screen's own layout.
	GroupPrefix string
}

// Key returns the stable identity the client runtime compares to decide
// the deepest layer shared between the current DOM and a navigation
// target: "g:<prefix>:<layoutName>" for group levels ("g:<prefix>" when
// marker-only), "l:<name>" for plain layouts. The layout name is part of
// a group level's key so a per-screen layout override inside a group
// compares as a DIFFERENT layer than its siblings, navigating between
// them re-renders the shell instead of silently keeping whichever one
// happened to be on screen. Empty when the level has no usable identity
// (an unnamed non-group layout), the client treats an empty key as
// never-matching, forcing a full swap at that depth.
func (l LayoutLayer) Key() string {
	if l.GroupPrefix != "" {
		if l.Layout == nil {
			return "g:" + l.GroupPrefix
		}
		return "g:" + l.GroupPrefix + ":" + l.Layout.Name
	}
	if l.Layout != nil && l.Layout.Name != "" {
		return "l:" + l.Layout.Name
	}
	return ""
}

// layoutChainFor resolves a screen's full layout chain, outermost →
// innermost. Nil for overlay screens (drawer/sheet/dialog, they skip
// layouts) and for layout-less pages.
//
// Resolution rules (each mirrors the pre-chain composition behavior):
//   - Direct screens get exactly one layer: their own layout, else the
//     router default. An explicit per-screen layout REPLACES the default;
//     only group screens nest under it.
//   - Group screens walk the group chain outermost → innermost. The
//     innermost level renders screen.Layout (group.Screen sets it to the
//     group's layout when the screen declared none, so an explicit
//     override naturally replaces the innermost group's shell while the
//     group marker is preserved).
//   - The default layout is prepended as the root iff it is set, no group
//     in the chain already uses it, and no group is Standalone.
//   - A level whose *Layout already renders at an outer level becomes a
//     marker-only level (Layout nil) instead of a duplicate shell.
//   - A group level with no layout at all contributes nothing.
func (r *Router) layoutChainFor(screen *Screen) []LayoutLayer {
	if screen == nil || screen.Type != ScreenPage {
		return nil
	}

	if screen.group == nil {
		layout := screen.Layout
		if layout == nil {
			layout = r.defaultLayout
		}
		if layout == nil {
			return nil
		}
		return []LayoutLayer{{Layout: layout}}
	}

	// Group chain, innermost → outermost.
	var groups []*ScreenGroup
	for g := screen.group; g != nil; g = g.parent {
		groups = append(groups, g)
	}

	def := r.defaultLayout
	applyDefault := def != nil &&
		!groupChainContainsLayout(screen.group, def) &&
		!groupChainIsStandalone(screen.group)

	chain := make([]LayoutLayer, 0, len(groups)+1)
	seen := make(map[*Layout]bool, len(groups)+1)
	if applyDefault {
		chain = append(chain, LayoutLayer{Layout: def})
		seen[def] = true
	}
	for i, g := range slices.Backward(groups) {

		eff := g.layout
		if i == 0 && screen.Layout != nil {
			// Innermost level: the screen's layout (set by group.Screen to
			// the group layout when the screen declared none, so this is
			// the override seam).
			eff = screen.Layout
		}
		switch {
		case eff == nil:
			// No shell and nothing to dedupe, the level contributes
			// nothing, exactly as the pre-chain composition rendered it.
		case seen[eff]:
			chain = append(chain, LayoutLayer{GroupPrefix: g.prefix})
		default:
			chain = append(chain, LayoutLayer{Layout: eff, GroupPrefix: g.prefix})
			seen[eff] = true
		}
	}
	return chain
}

// renderLayoutChain wraps content in every layer of the chain. Layer 0 is
// the outermost shell and owns the page's single <main id="main-content">.
func renderLayoutChain(ctx context.Context, chain []LayoutLayer, content render.HTML) render.HTML {
	return renderLayoutChainFrom(ctx, chain, 0, content)
}

// renderLayoutChainFrom wraps content in layers from..len(chain)-1. When
// from > 0 the outermost shared layers are already in the caller's DOM
// (subtree partials), so every rendered layer nests, none emits <main>.
func renderLayoutChainFrom(ctx context.Context, chain []LayoutLayer, from int, content render.HTML) render.HTML {
	out := content
	for i := len(chain) - 1; i >= from; i-- {
		layer := chain[i]
		key := layer.Key()
		if layer.Layout != nil {
			out = layer.Layout.wrapLayer(ctx, out, i == 0, key)
		}
		if layer.GroupPrefix != "" {
			attrs := map[string]string{"data-fui-screen-group": layer.GroupPrefix}
			if layer.Layout == nil && key != "" {
				// Marker-only level: the group wrapper is both the layer
				// element and its content cell, so sibling navigation can
				// still target the level by key (and focus it after a swap).
				attrs["data-fui-layout-key"] = key
				attrs["data-fui-layout-slot"] = key
				attrs["tabindex"] = "-1"
			}
			out = html.Div(html.DivConfig{
				Class:      "fui-screen-group",
				ExtraAttrs: attrs,
			}, out)
		}
	}
	return out
}
