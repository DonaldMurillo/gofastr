package app

import (
	"context"
	"log/slog"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// Layout is one layer of shared chrome that wraps screens: a name, a layer identity, and a build function.
type Layout struct {
	// Name identifies the layout (used in the wrapper's class and data-cui-layout attribute).
	Name string
	// Key, when set, overrides the layout's layer identity: the runtime compares "l:<Key>" (or
	// "g:<prefix>:<Key>" inside a group) to decide what to swap, instead of deriving it from Name.
	Key string

	spec    *LayoutSpec
	build   LayoutFunc
	vtSheet string
}

// WithKey sets the layout's layer key, the identity the runtime compares to decide what to swap,
// independent of the layout's name.
func (l *Layout) WithKey(key string) *Layout {
	l.Key = key
	return l
}

// WrapCtx renders the layout as the outermost shell around content.
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
		slog.Default().Error("app: WrapCtx layout build failed; rendering bare body", "err", err)
		return content
	}
	return out
}

func (l *Layout) selfKey() string {
	if id := l.identity(); id != "" {
		return "l:" + id
	}
	return ""
}

func (l *Layout) identity() string {
	if l == nil {
		return ""
	}
	if l.Key != "" {
		return l.Key
	}
	return l.Name
}
