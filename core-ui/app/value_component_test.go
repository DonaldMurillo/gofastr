package app

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// statelessValue is the documented value-component form: no state, no
// injection, registered without a pointer (newInstance returns it
// as-is).
type statelessValue struct{}

func (statelessValue) Render() render.HTML { return render.HTML("<p>value ok</p>") }

// wantsInjectionValue is the wiring mistake: a value component whose
// struct asks for DI, which can never be filled without a pointer.
type wantsInjectionValue struct {
	Svc *struct{} `inject:""`
}

func (wantsInjectionValue) Render() render.HTML { return render.HTML("x") }

// Pins #259: a stateless value component is a supported form and must
// render, not die with "di: target must be a non-nil pointer".
func TestRenderValueComponentStateless(t *testing.T) {
	a := NewApp("t")
	a.Register("/", statelessValue{}, nil)

	res, err := a.RenderPageResult(context.Background(), "/")
	if err != nil {
		t.Fatalf("stateless value component must render: %v", err)
	}
	if !strings.Contains(string(res.HTML), "value ok") {
		t.Fatalf("body missing: %s", res.HTML)
	}
}

// The partial (SPA navigation) path shares the same contract: a value
// component that renders as a full page must also render as a partial
// (PR #274 review finding — renderPartial had its own unconditional
// Inject call).
func TestRenderPartialValueComponentStateless(t *testing.T) {
	a := NewApp("t")
	a.Register("/", statelessValue{}, nil)

	res, err := a.RenderPartialResult(context.Background(), "/")
	if err != nil {
		t.Fatalf("stateless value component must render as a partial: %v", err)
	}
	if !strings.Contains(string(res.HTML), "value ok") {
		t.Fatalf("partial body missing: %s", res.HTML)
	}
}

// The other half of #259: a value struct that DECLARES inject tags
// would silently render with nil services if DI were merely skipped;
// it must fail naming the type and the pointer remedy.
func TestRenderValueComponentWithInjectTagsFails(t *testing.T) {
	a := NewApp("t")
	a.Register("/", wantsInjectionValue{}, nil)

	_, err := a.RenderPageResult(context.Background(), "/")
	if err == nil {
		t.Fatal("value component with inject tags must error")
	}
	for _, want := range []string{"wantsInjectionValue", "pointer", "inject-tagged"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error should mention %q: %v", want, err)
		}
	}
}
