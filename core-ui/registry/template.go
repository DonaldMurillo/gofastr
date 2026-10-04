package registry

import (
	"context"
	"reflect"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// A template is a markup fragment a layer above the kernel registers
// under a name, for a renderer below it to emit at render time. It is
// how a package that may not import the kit (core-ui/widget/preset,
// framework/uihost) still ships the kit's markup: the kit registers the
// fragment with its own classes and words, the lower layer looks it up
// by name and knows nothing of what is inside. The first template is
// the toast stack's row (preset.ToastTemplate): preset's slot renders
// the stack container and, inside it, the <template> the
// headless-feedback module clones a runtime toast from.
//
// Unlike styles, templates are not part of the frozen catalog: a
// lookup happens per render, so registration after Freeze is fine.
var templates = newTemplates()

func newTemplates() map[string]func(context.Context) render.HTML {
	return map[string]func(context.Context) render.HTML{}
}

// RegisterTemplate registers fn under name and returns the name, so a
// package-level `var _ = registry.RegisterTemplate(...)` reads as the
// registration it is. The renderer takes the request context so a kit
// can render the request's language (framework/ui.StringsFor).
//
// Registering the same function twice is a no-op (test binaries link
// a package more than once); a second, different renderer for a name
// panics, because the second registration would silently replace the
// first and the page would wear whichever package initialised last.
func RegisterTemplate(name string, fn func(context.Context) render.HTML) string {
	if name == "" {
		panic("registry: RegisterTemplate with an empty name")
	}
	if fn == nil {
		panic("registry: RegisterTemplate(" + strconv.Quote(name) + ") with a nil renderer")
	}
	mu.Lock()
	defer mu.Unlock()
	if prev, ok := templates[name]; ok {
		if reflect.ValueOf(prev).Pointer() == reflect.ValueOf(fn).Pointer() {
			return name
		}
		panic("registry: template " + strconv.Quote(name) + " registered twice with different renderers")
	}
	templates[name] = fn
	return name
}

// Template renders the template registered under name, or reports
// false when none is: the caller renders without it, the way preset's
// toast slot renders a bare stack when no kit registered a row.
func Template(ctx context.Context, name string) (render.HTML, bool) {
	mu.Lock()
	fn, ok := templates[name]
	mu.Unlock()
	if !ok {
		return "", false
	}
	return fn(ctx), true
}
