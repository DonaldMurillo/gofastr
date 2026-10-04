package registry

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func rowA(context.Context) render.HTML { return render.HTML("<p>a</p>") }
func rowB(context.Context) render.HTML { return render.HTML("<p>b</p>") }

func TestTemplateRegisterAndLookup(t *testing.T) {
	reset()
	if got := RegisterTemplate("row", rowA); got != "row" {
		t.Fatalf("RegisterTemplate returned %q, want the name back", got)
	}
	html, ok := Template(context.Background(), "row")
	if !ok || string(html) != "<p>a</p>" {
		t.Fatalf("Template = %q, %v; want the registered render", html, ok)
	}
	if _, ok := Template(context.Background(), "missing"); ok {
		t.Fatal("an unregistered name reported a template")
	}
}

func TestTemplateSameRendererTwiceIsANoop(t *testing.T) {
	reset()
	RegisterTemplate("row", rowA)
	RegisterTemplate("row", rowA)
	if html, _ := Template(context.Background(), "row"); string(html) != "<p>a</p>" {
		t.Fatalf("re-registration changed the template: %q", html)
	}
}

func TestTemplateDifferentRendererPanics(t *testing.T) {
	reset()
	RegisterTemplate("row", rowA)
	defer func() {
		r := recover()
		if r == nil || !strings.Contains(r.(string), "registered twice") {
			t.Fatalf("a second renderer for the same name did not panic: %v", r)
		}
	}()
	RegisterTemplate("row", rowB)
}

func TestTemplateEmptyNameAndNilRendererPanic(t *testing.T) {
	reset()
	for name, fn := range map[string]func(context.Context) render.HTML{"": rowA, "row": nil} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("RegisterTemplate(%q, nil=%v) did not panic", name, fn == nil)
				}
			}()
			RegisterTemplate(name, fn)
		}()
	}
}

func TestIsolateForTestHidesAndRestoresTemplates(t *testing.T) {
	reset()
	RegisterTemplate("row", rowA)
	t.Run("isolated", func(t *testing.T) {
		IsolateForTest(t)
		if _, ok := Template(context.Background(), "row"); ok {
			t.Fatal("the isolated registry still sees the outer template")
		}
		RegisterTemplate("row", rowB)
	})
	html, ok := Template(context.Background(), "row")
	if !ok || string(html) != "<p>a</p>" {
		t.Fatalf("after isolation the outer template is %q, %v; want the original", html, ok)
	}
}
