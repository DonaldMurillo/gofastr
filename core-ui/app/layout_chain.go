package app

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// LayoutLayer is one level of a screen's resolved layout chain, ordered outermost → innermost.
type LayoutLayer struct {
	// Layout is the shell rendered at this level.
	Layout *Layout
	// GroupPrefix is the ScreenGroup prefix that produced this level ("/settings/").
	GroupPrefix string
}

// Key returns the layer identity the runtime compares to find the deepest shared layer.
func (l LayoutLayer) Key() string {
	if l.GroupPrefix != "" {
		if l.Layout == nil {
			return "g:" + l.GroupPrefix
		}
		return "g:" + l.GroupPrefix + ":" + l.Layout.identity()
	}
	if l.Layout != nil {
		return l.Layout.selfKey()
	}
	return ""
}

func resolveChainKeys(chain []LayoutLayer, params map[string]string) []LayoutLayer {
	if len(chain) == 0 || len(params) == 0 {
		return chain
	}
	out := make([]LayoutLayer, len(chain))
	changed := false
	for i, l := range chain {
		if l.GroupPrefix != "" && strings.Contains(l.GroupPrefix, "{") {
			p := l.GroupPrefix
			for _, name := range slices.Sorted(maps.Keys(params)) {
				p = strings.ReplaceAll(p, "{"+name+"}", params[name])
			}
			if p != l.GroupPrefix {
				changed = true
				l.GroupPrefix = p
			}
		}
		out[i] = l
	}
	if !changed {
		return chain
	}
	return out
}

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
			eff = screen.Layout
		}
		switch {
		case eff == nil:
		case seen[eff]:
			chain = append(chain, LayoutLayer{GroupPrefix: g.prefix})
		default:
			chain = append(chain, LayoutLayer{Layout: eff, GroupPrefix: g.prefix})
			seen[eff] = true
		}
	}
	return chain
}

func renderLayoutChain(ctx context.Context, chain []LayoutLayer, content render.HTML, fills *fillSet, guards regionGuards) (render.HTML, error) {
	return renderLayoutChainFrom(ctx, chain, 0, content, fills, guards)
}

func renderLayoutChainFrom(ctx context.Context, chain []LayoutLayer, from int, content render.HTML, fills *fillSet, guards regionGuards) (render.HTML, error) {
	out := content
	for i := len(chain) - 1; i >= from; i-- {
		layer := chain[i]
		key := layer.Key()
		if layer.Layout != nil {
			wrapped, err := layer.Layout.wrapTreeLayer(ctx, chain, i, from, out, fills, guards)
			if err != nil {
				return "", err
			}
			out = wrapped
		}
		if layer.GroupPrefix != "" {
			attrs := map[string]string{"data-cui-screen-group": layer.GroupPrefix}
			if layer.Layout == nil && key != "" {
				attrs["data-cui-layout-key"] = key
				attrs["data-cui-layout-slot"] = key
				attrs["tabindex"] = "-1"
			}
			if i == from && layer.Layout == nil {
				for k, v := range docShellAttrs(ctx) {
					attrs[k] = v
				}
			}
			out = html.Div(html.DivConfig{
				Class:      "cui-screen-group",
				ExtraAttrs: attrs,
			}, out)
		}
	}
	for i := min(from, len(chain)) - 1; i >= 0; i-- {
		if chain[i].Layout.isTree() {
			chain[i].Layout.collectTreeLayer(ctx, chain, i, fills, guards)
		}
	}
	return out, nil
}
