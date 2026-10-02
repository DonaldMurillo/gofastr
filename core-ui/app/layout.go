package app

import (
	"context"
	"log/slog"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Layout is one layer of shared chrome that wraps screens: a name, a
// layer identity, and a build function. NewLayout(name, spec, build)
// creates one; the build composes the layer's body per render and places
// the primary slot, the spec's outlets, and its route areas through the
// *LayoutTree it receives (see layout_tree.go). The layout keeps the
// name, layer-key, and wrapper-marker duty: every layer renders a
// wrapper carrying data-fui-layout (the name, the CSS/debug contract)
// and data-fui-layout-key (the identity the runtime compares to decide
// what a navigation swaps).
type Layout struct {
	// Name identifies the layout (used in the wrapper's class and
	// data-fui-layout attribute).
	Name string
	// Key, when set, overrides the layout's layer identity: the runtime
	// compares "l:<Key>" (or "g:<prefix>:<Key>" inside a group) to decide
	// what to swap, instead of deriving it from Name. Use it when one
	// layout shape must re-render per context, the #408 case being a
	// shell that varies by language: key layer 0 per language and the
	// runtime swaps it like any other layer. Name keeps driving
	// data-fui-layout and the wrapper class, so the CSS contract stays
	// stable while the identity varies. Empty derives from Name.
	Key string

	// spec and build carry the layout's declaration and build function
	// (NewLayout sets both; see layout_tree.go).
	spec  *LayoutSpec
	build LayoutFunc
}

// WithKey sets the layout's layer key, the identity the runtime compares
// to decide what to swap, independent of the layout's name. A multilingual
// site gives each language's shell the same Name (one CSS contract) and a
// per-language Key, so navigating between languages re-renders the shell
// instead of keeping whichever loaded first.
func (l *Layout) WithKey(key string) *Layout {
	l.Key = key
	return l
}

// WrapCtx renders the layout as the OUTERMOST shell around an arbitrary
// body: the primary slot is filled with content, every outlet renders
// its Default or nothing, and route areas run with ctx's match when one
// is installed. The uihost error documents (the 404 and 405 pages,
// RenderScreen's full arm) and the embed route use it to finish a body
// through the app's own shell; a nil layout returns the content
// unchanged.
func (l *Layout) WrapCtx(ctx context.Context, content render.HTML) render.HTML {
	if l == nil || l.build == nil {
		return content
	}
	key := l.selfKey()
	var fills fillSet
	if l.spec != nil {
		for _, o := range l.spec.Outlets {
			if o.Default == nil {
				continue
			}
			if html, err := component.SafeRenderCtx(ctx, newComponentInstance(o.Default)); err == nil {
				fills.set(key+"#"+o.Name(), treeFill{html: html})
			} else {
				slog.Default().Error("app: WrapCtx outlet default failed; outlet empty",
					"addr", key+"#"+o.Name(),
					"err", err)
			}
		}
	}
	out, err := l.wrapTreeLayer(ctx, []LayoutLayer{{Layout: l}}, 0, 0, content, &fills, nil)
	if err != nil {
		// A broken build fails the app's own pages loudly; an error
		// document degrades to the bare body instead of compounding
		// the failure.
		slog.Default().Error("app: WrapCtx layout build failed; rendering bare body", "err", err)
		return content
	}
	return out
}

// selfKey is the layer key a layout carries when it is wrapped directly
// (WrapCtx) rather than through a resolved chain, the plain-layer form
// of LayoutLayer.Key: Key when declared, else Name.
func (l *Layout) selfKey() string {
	if id := l.identity(); id != "" {
		return "l:" + id
	}
	return ""
}

// identity is the part of the layer key that distinguishes two layouts
// at the same depth: Key when declared, else Name.
func (l *Layout) identity() string {
	if l == nil {
		return ""
	}
	if l.Key != "" {
		return l.Key
	}
	return l.Name
}
