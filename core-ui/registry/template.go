package registry

import (
	"context"
	"reflect"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core/render"
)

var templates = newTemplates()

func newTemplates() map[string]func(context.Context) render.HTML {
	return map[string]func(context.Context) render.HTML{}
}

// RegisterTemplate registers fn as the renderer of template name and returns name.
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

// Template renders the template registered under name, or reports false.
func Template(ctx context.Context, name string) (render.HTML, bool) {
	mu.Lock()
	fn, ok := templates[name]
	mu.Unlock()
	if !ok {
		return "", false
	}
	return fn(ctx), true
}
