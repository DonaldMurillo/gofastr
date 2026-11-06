package interactive

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestBindingsRefuseReservedNames(t *testing.T) {
	for _, name := range []string{"__proto__", "constructor", "prototype", "ready"} {
		for kind, emit := range map[string]func() render.HTML{
			"html":    func() render.HTML { return BindHTML(render.HTML("<div></div>"), name) },
			"text":    func() render.HTML { return BindText(render.HTML("<div></div>"), name) },
			"attr":    func() render.HTML { return BindAttr(render.HTML("<div></div>"), name, "title") },
			"animate": func() render.HTML { return AnimateOnSignal(render.HTML("<div></div>"), name, "active") },
		} {
			t.Run(name+"/"+kind, func(t *testing.T) {
				defer func() {
					panicked := recover() != nil
					if panicked != (name != "ready") {
						t.Errorf("reserved-name refusal = %v for %q", panicked, name)
					}
				}()
				emit()
			})
		}
	}
}
