package component

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// ---------------------------------------------------------------------------
// Test helpers, lightweight component implementations used across tests
// ---------------------------------------------------------------------------

// staticComp is the simplest possible Component, it just returns fixed HTML.
type staticComp struct {
	html render.HTML
}

func (s *staticComp) Render() render.HTML { return s.html }

// interactiveComp implements InteractiveComponent via Render + Actions.
type interactiveComp struct {
	component ComponentBase
	clicks    int
}

func (ic *interactiveComp) Render() render.HTML {
	return render.Tag("button", nil, render.Text("click me"))
}

func (ic *interactiveComp) Actions() {
	On("click", func(ctx *ComponentContext) {
		// handler body intentionally empty, we just need it registered
	})
	On("hover", func(ctx *ComponentContext) {})
}

// childComp is a small component used in composition tests.
type childComp struct {
	label string
}

func (c *childComp) Render() render.HTML {
	return render.Tag("span", nil, render.Text(c.label))
}

// parentComp renders a child component inside its own output.
type parentComp struct {
	child *childComp
}

func (p *parentComp) Render() render.HTML {
	inner := p.child.Render()
	return render.Tag("div", nil, inner)
}

// ---------------------------------------------------------------------------
// 2. TestInteractiveComponent
// ---------------------------------------------------------------------------

func TestInteractiveComponent(t *testing.T) {
	var c Component = &interactiveComp{}
	ic, ok := c.(InteractiveComponent)
	if !ok {
		t.Fatal("interactiveComp should satisfy InteractiveComponent")
	}
	// Calling Actions should not panic.
	ic.Actions()
}

// ---------------------------------------------------------------------------
// 3. TestIsInteractive
// ---------------------------------------------------------------------------

func TestIsInteractive(t *testing.T) {
	if IsInteractive(&staticComp{}) {
		t.Error("staticComp should NOT be interactive")
	}
	if !IsInteractive(&interactiveComp{}) {
		t.Error("interactiveComp SHOULD be interactive")
	}
}

// ---------------------------------------------------------------------------
// 5. TestComponentList
// ---------------------------------------------------------------------------

func TestComponentList(t *testing.T) {
	a := &staticComp{html: render.Raw("<p>a</p>")}
	b := &staticComp{html: render.Raw("<p>b</p>")}
	got := ComponentList(a, b)
	want := render.HTML("<p>a</p><p>b</p>")
	if got != want {
		t.Fatalf("ComponentList = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// 6. TestRenderComponent
// ---------------------------------------------------------------------------

func TestRenderComponent(t *testing.T) {
	c := &staticComp{html: render.Raw("<span>hi</span>")}
	got := RenderComponent(c)
	if got != c.Render() {
		t.Fatalf("RenderComponent = %q, want %q", got, c.Render())
	}
}

// ---------------------------------------------------------------------------
// 7. TestComponentContext
// ---------------------------------------------------------------------------

func TestComponentContext(t *testing.T) {
	// -- NewComponentContext --
	ctx := NewComponentContext("click", "btn-1", map[string]string{
		"x": "42",
		"y": "hello",
	})
	if ctx.EventName != "click" {
		t.Errorf("EventName = %q, want %q", ctx.EventName, "click")
	}
	if ctx.TargetID != "btn-1" {
		t.Errorf("TargetID = %q, want %q", ctx.TargetID, "btn-1")
	}
}

// ---------------------------------------------------------------------------
// 8. TestActionRegistry
// ---------------------------------------------------------------------------

func TestActionRegistry(t *testing.T) {
	reg := NewActionRegistry()

	if reg.HasActions() {
		t.Error("new registry should have no actions")
	}

	handler := func(ctx *ComponentContext) {}
	reg.Register(ActionDef{Event: "click", Handler: handler})
	reg.Register(ActionDef{Event: "submit", Handler: handler})

	if !reg.HasActions() {
		t.Error("registry should have actions after Register")
	}

	click, ok := reg.Get("click")
	if !ok {
		t.Fatal("Get(click) should return true")
	}
	if click.Event != "click" {
		t.Errorf("click.Event = %q, want %q", click.Event, "click")
	}

	if _, ok := reg.Get("hover"); ok {
		t.Error("Get(hover) should return false for unregistered event")
	}

	all := reg.All()
	if len(all) != 2 {
		t.Fatalf("All() returned %d actions, want 2", len(all))
	}
}

// ---------------------------------------------------------------------------
// 9. TestOn
// ---------------------------------------------------------------------------

func TestOn(t *testing.T) {
	// Manually exercise On with a temporary registry.
	reg := NewActionRegistry()
	prev := currentRegistry
	currentRegistry = reg
	defer func() { currentRegistry = prev }()

	handler := func(ctx *ComponentContext) {}
	def := On("keydown", handler)

	if def.Event != "keydown" {
		t.Errorf("On returned ActionDef.Event = %q, want %q", def.Event, "keydown")
	}
	if !reg.HasActions() {
		t.Error("On should register into the current registry")
	}
	got, ok := reg.Get("keydown")
	if !ok || got.Handler == nil {
		t.Error("On should register a handler")
	}
}

// ---------------------------------------------------------------------------
// 10. TestExtractActions
// ---------------------------------------------------------------------------

func TestExtractActions(t *testing.T) {
	ic := &interactiveComp{}
	reg := ExtractActions(ic)

	if !reg.HasActions() {
		t.Fatal("ExtractActions should find actions on InteractiveComponent")
	}

	click, ok := reg.Get("click")
	if !ok {
		t.Fatal("ExtractActions should register 'click'")
	}
	if click.Handler == nil {
		t.Error("click handler should not be nil")
	}

	if _, ok := reg.Get("hover"); !ok {
		t.Fatal("ExtractActions should register 'hover'")
	}

	all := reg.All()
	if len(all) != 2 {
		t.Fatalf("ExtractActions returned %d actions, want 2", len(all))
	}
}

// ---------------------------------------------------------------------------
// 11. TestExtractActionsNonInteractive
// ---------------------------------------------------------------------------

func TestExtractActionsNonInteractive(t *testing.T) {
	sc := &staticComp{html: render.Raw("<p>hi</p>")}
	reg := ExtractActions(sc)

	if reg.HasActions() {
		t.Error("ExtractActions on a non-interactive component should yield empty registry")
	}
}

// ---------------------------------------------------------------------------
// Widget test helpers
// ---------------------------------------------------------------------------

// greetComponent is a simple non-interactive component for widget tests.
type greetComponent struct {
	name string
}

func (g *greetComponent) Render() render.HTML {
	return render.Tag("span", nil, render.Text("Hello "+g.name))
}

// counterComponent is an interactive component for widget tests.
type counterComponent struct{}

func (c *counterComponent) Render() render.HTML {
	return render.Tag("button", nil, render.Text("count"))
}

func (c *counterComponent) Actions() {
	On("click", func(ctx *ComponentContext) {})
}

// ---------------------------------------------------------------------------
// 15. TestComponentComposition
// ---------------------------------------------------------------------------

func TestComponentComposition(t *testing.T) {
	child := &childComp{label: "inner"}
	parent := &parentComp{child: child}

	got := parent.Render()
	want := render.HTML("<div><span>inner</span></div>")
	if got != want {
		t.Fatalf("composition Render() = %q, want %q", got, want)
	}
}

// ---------------------------------------------------------------------------
// 16. TestNewWidget
// ---------------------------------------------------------------------------

func TestNewWidget(t *testing.T) {
	comp := &greetComponent{name: "World"}
	w := NewWidget("greet", comp)
	if w.ID != "greet" {
		t.Errorf("expected ID greet, got %s", w.ID)
	}
}

// ---------------------------------------------------------------------------
// 17. TestWidgetRender
// ---------------------------------------------------------------------------

func TestWidgetRender(t *testing.T) {
	comp := &greetComponent{name: "World"}
	w := NewWidget("greet", comp)
	html := w.Render()
	s := string(html)
	if !strings.Contains(s, `data-widget="greet"`) {
		t.Errorf("expected data-widget attribute, got %s", s)
	}
	// No runtime path reads data-hydrate, so the widget root must not
	// carry it.
	if strings.Contains(s, `data-hydrate=`) {
		t.Errorf("unexpected data-hydrate attribute, got %s", s)
	}
	if !strings.Contains(s, "Hello World") {
		t.Errorf("expected component content, got %s", s)
	}
}

// ---------------------------------------------------------------------------
// 18. TestSafeRenderPanickingComponent
// ---------------------------------------------------------------------------

type panickingComponent struct{}

func (p *panickingComponent) Render() render.HTML { panic("oh no") }

func TestSafeRenderPanickingComponent(t *testing.T) {
	comp := &panickingComponent{}
	html, err := SafeRender(comp)
	if err == nil {
		t.Error("expected error from panicking component")
	}
	if !strings.Contains(string(html), "Error:") {
		t.Errorf("expected error UI, got: %s", html)
	}
}

// ---------------------------------------------------------------------------
// 19. TestSafeRenderErrorBoundary
// ---------------------------------------------------------------------------

type customErrorBoundary struct{}

func (c *customErrorBoundary) Render() render.HTML { panic("oh no") }
func (c *customErrorBoundary) RenderError(err error) render.HTML {
	return render.HTML("<div>Custom error: " + err.Error() + "</div>")
}

func TestSafeRenderErrorBoundary(t *testing.T) {
	comp := &customErrorBoundary{}
	html, err := SafeRender(comp)
	if err == nil {
		t.Error("expected error from panicking component")
	}
	if !strings.Contains(string(html), "Custom error") {
		t.Errorf("expected custom error UI, got: %s", html)
	}
}

// ---------------------------------------------------------------------------
// 20. TestSafeRenderNormalComponent
// ---------------------------------------------------------------------------

func TestSafeRenderNormalComponent(t *testing.T) {
	comp := &staticComp{html: render.Raw("<p>fine</p>")}
	html, err := SafeRender(comp)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if html != render.HTML("<p>fine</p>") {
		t.Errorf("expected <p>fine</p>, got: %s", html)
	}
}

// ---------------------------------------------------------------------------
// 21. TestWidgetInteractive
// ---------------------------------------------------------------------------

func TestWidgetInteractive(t *testing.T) {
	// Non-interactive component
	comp := &greetComponent{name: "World"}
	w := NewWidget("greet", comp)
	if w.IsInteractive() {
		t.Error("non-interactive component should not be interactive")
	}

	// Interactive component
	interactive := &counterComponent{}
	wi := NewWidget("counter", interactive)
	if !wi.IsInteractive() {
		t.Error("interactive component should be interactive")
	}
}
