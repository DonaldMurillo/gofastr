package component

import (
	"strings"
	"testing"

	"github.com/gofastr/gofastr/core/render"
)

// ---------------------------------------------------------------------------
// Test helpers — lightweight component implementations used across tests
// ---------------------------------------------------------------------------

// staticComp is the simplest possible Component — it just returns fixed HTML.
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
		// handler body intentionally empty — we just need it registered
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
// 1. TestComponentInterface
// ---------------------------------------------------------------------------

func TestComponentInterface(t *testing.T) {
	want := render.Raw("<p>hello</p>")
	c := &staticComp{html: want}
	got := c.Render()
	if got != want {
		t.Fatalf("Render() = %q, want %q", got, want)
	}
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
// 4. TestComponentBase
// ---------------------------------------------------------------------------

func TestComponentBase(t *testing.T) {
	base := ComponentBase{ID: "my-id", Class: "my-class"}
	if base.ID != "my-id" {
		t.Errorf("ID = %q, want %q", base.ID, "my-id")
	}
	if base.Class != "my-class" {
		t.Errorf("Class = %q, want %q", base.Class, "my-class")
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

	// -- Param --
	if got := ctx.Param("x"); got != "42" {
		t.Errorf("Param(x) = %q, want %q", got, "42")
	}
	if got := ctx.Param("missing"); got != "" {
		t.Errorf("Param(missing) = %q, want empty", got)
	}

	// -- ParamInt --
	n, err := ctx.ParamInt("x")
	if err != nil || n != 42 {
		t.Errorf("ParamInt(x) = %d, %v; want 42, nil", n, err)
	}
	if _, err := ctx.ParamInt("y"); err == nil {
		t.Error("ParamInt(y) should error for non-integer value")
	}

	// -- GetState / SetState --
	state := map[string]any{}
	ctx.StateGetter = func(key string) any { return state[key] }
	ctx.StateSetter = func(key string, value any) { state[key] = value }

	ctx.SetState("count", 7)
	if got := ctx.GetState("count"); got != 7 {
		t.Errorf("GetState(count) = %v, want 7", got)
	}
	if got := ctx.GetState("missing"); got != nil {
		t.Errorf("GetState(missing) = %v, want nil", got)
	}

	// -- nil-safe getters --
	emptyCtx := &ComponentContext{}
	if got := emptyCtx.GetState("anything"); got != nil {
		t.Errorf("GetState on nil getter = %v, want nil", got)
	}
	emptyCtx.SetState("k", "v") // should not panic
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
	reg.Register(ActionDef{Event: "submit", Handler: handler, Server: true})

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
// 12. TestServerCall
// ---------------------------------------------------------------------------

func TestServerCall(t *testing.T) {
	call := Server("save", "key1", "val1", "key2", "val2")
	if call.Action != "save" {
		t.Errorf("ServerCall.Action = %q, want %q", call.Action, "save")
	}
	if len(call.Params) != 2 {
		t.Fatalf("len(Params) = %d, want 2", len(call.Params))
	}
	if call.Params["key1"] != "val1" {
		t.Errorf("Params[\"key1\"] = %v, want %q", call.Params["key1"], "val1")
	}
	if call.Params["key2"] != "val2" {
		t.Errorf("Params[\"key2\"] = %v, want %q", call.Params["key2"], "val2")
	}
}

// ---------------------------------------------------------------------------
// 13. TestLifecycle
// ---------------------------------------------------------------------------

func TestLifecycle(t *testing.T) {
	lc := NewLifecycle()
	var order []string

	lc.OnMount(func() { order = append(order, "mount") })
	lc.OnUpdate(func() { order = append(order, "update") })
	lc.OnUnmount(func() { order = append(order, "unmount") })

	if !lc.HasMountHooks() {
		t.Error("HasMountHooks should be true after OnMount")
	}

	lc.TriggerMount()
	lc.TriggerUpdate()
	lc.TriggerUnmount()

	want := []string{"mount", "update", "unmount"}
	if len(order) != len(want) {
		t.Fatalf("lifecycle order = %v, want %v", order, want)
	}
	for i, v := range want {
		if order[i] != v {
			t.Errorf("order[%d] = %q, want %q", i, order[i], v)
		}
	}
}

// ---------------------------------------------------------------------------
// 14. TestLifecycleTriggerOrder
// ---------------------------------------------------------------------------

func TestLifecycleTriggerOrder(t *testing.T) {
	lc := NewLifecycle()
	var order []int

	lc.OnMount(func() { order = append(order, 1) })
	lc.OnMount(func() { order = append(order, 2) })
	lc.OnMount(func() { order = append(order, 3) })

	lc.TriggerMount()

	if len(order) != 3 {
		t.Fatalf("expected 3 mount hooks, got %d", len(order))
	}
	for i, want := range []int{1, 2, 3} {
		if order[i] != want {
			t.Errorf("hook %d fired value %d, want %d", i, order[i], want)
		}
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
	if !strings.Contains(s, `data-hydrate=`) {
		t.Errorf("expected data-hydrate attribute, got %s", s)
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
