package app

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

// ---------------------------------------------------------------------------
// Tests for issue #6: Screen contract cleanup
//
// A. Default ScreenType — screens implementing ScreenTitler (ScreenTitle())
//    but NOT ScreenTyper (ScreenType()) should default to ScreenPage.
//
// B. Nested <main> detection — ValidateScreenOutput flags screens whose
//    component output contains <main> (since the framework already wraps
//    ScreenPage in <main>).
// ---------------------------------------------------------------------------

// --- A. Default ScreenType ---

// minimalScreenSpec implements ONLY ScreenTitle — no ScreenType(), no
// ScreenDescription(). After the fix, Register() should still detect
// ScreenTitle via the ScreenTitler interface and default to ScreenPage.
type minimalScreenSpec struct {
	title string
}

func (m *minimalScreenSpec) Render() render.HTML {
	return render.Text("minimal spec content")
}

func (m *minimalScreenSpec) ScreenTitle() string { return m.title }

// TestDefaultScreenTypeViaRegister verifies that when a component implements
// only ScreenTitle (via ScreenTitler), the app still registers it as
// ScreenPage without requiring ScreenType().
func TestDefaultScreenTypeViaRegister(t *testing.T) {
	a := NewApp("DefaultTypeApp")
	comp := &minimalScreenSpec{title: "TestPage"}
	a.Register("/test", comp, nil)

	screen, _, ok := a.Router.Resolve("/test")
	if !ok {
		t.Fatal("expected to resolve /test")
	}
	if screen.Type != ScreenPage {
		t.Errorf("expected ScreenPage for component without ScreenType(), got %v", screen.Type)
	}
	if screen.Title != "TestPage" {
		t.Errorf("expected title 'TestPage', got %q", screen.Title)
	}
}

// TestDefaultScreenTypeRenderPage verifies the full render pipeline works
// with a component that only implements ScreenTitle.
func TestDefaultScreenTypeRenderPage(t *testing.T) {
	a := NewApp("DefaultTypeApp")
	comp := &minimalScreenSpec{title: "Minimal"}
	a.Register("/minimal", comp, nil)

	html, err := a.RenderPage(context.Background(), "/minimal")
	if err != nil {
		t.Fatalf("RenderPage failed: %v", err)
	}
	s := string(html)

	if !strings.Contains(s, "<main") {
		t.Errorf("expected <main> wrapper in output")
	}
	if !strings.Contains(s, "minimal spec content") {
		t.Errorf("expected content in rendered output")
	}
}

// TestBareComponentDefaults verifies that a component implementing none
// of the screen metadata interfaces still works — defaults to ScreenPage
// with empty title and description.
func TestBareComponentDefaults(t *testing.T) {
	// stubComponent has no ScreenTitler, ScreenDescriber, or ScreenTyper
	a := NewApp("DescApp")
	a.Register("/desc", &stubComponent{html: render.Raw("<p>desc</p>")}, nil)

	screen, _, ok := a.Router.Resolve("/desc")
	if !ok {
		t.Fatal("expected to resolve /desc")
	}
	// Bare component: no ScreenTitler → empty title, default ScreenPage
	if screen.Type != ScreenPage {
		t.Errorf("expected ScreenPage, got %v", screen.Type)
	}
}

// --- B. Nested <main> detection ---

// nestedMainScreen returns HTML containing a <main> element — a common
// mistake since the framework already wraps ScreenPage output in <main>.
type nestedMainScreen struct{}

func (n *nestedMainScreen) Render() render.HTML {
	return render.Raw(`<main class="content"><p>Nested!</p></main>`)
}

func (n *nestedMainScreen) ScreenTitle() string       { return "Nested" }
func (n *nestedMainScreen) ScreenDescription() string  { return "Test nested main" }
func (n *nestedMainScreen) ScreenType() ScreenType     { return ScreenPage }

// TestNestedMainWarning verifies that ValidateScreenOutput flags screens
// whose raw component output contains <main>.
func TestNestedMainWarning(t *testing.T) {
	screen := NewScreen("/nested", &nestedMainScreen{})
	output := string(screen.Component.Render())

	warnings := ValidateScreenOutput(screen, output)
	if len(warnings) == 0 {
		t.Error("expected warning for nested <main> in screen output")
	}

	found := false
	for _, w := range warnings {
		if strings.Contains(w, "<main>") || strings.Contains(w, "nested") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected nested-main warning, got warnings: %v", warnings)
	}
}

// TestNoNestedMainWarningForValidScreen verifies that a normal screen
// without <main> in its output produces no warnings about nested mains.
func TestNoNestedMainWarningForValidScreen(t *testing.T) {
	screen := NewScreen("/valid", &stubComponent{html: render.Raw("<p>Valid content</p>")})
	output := string(screen.Component.Render())

	warnings := ValidateScreenOutput(screen, output)
	for _, w := range warnings {
		if strings.Contains(w, "main") {
			t.Errorf("unexpected nested-main warning for valid screen: %s", w)
		}
	}
}

// TestNestedMainNotFlaggedForDrawer verifies that <main> in a drawer
// screen's output is NOT flagged — drawers don't get the <main> wrapper.
func TestNestedMainNotFlaggedForDrawer(t *testing.T) {
	drawer := NewDrawer("/drawer", &nestedMainScreen{})
	output := string(drawer.Component.Render())

	warnings := ValidateScreenOutput(drawer, output)
	for _, w := range warnings {
		if strings.Contains(w, "main") {
			t.Errorf("drawer screens should not flag nested <main>: %s", w)
		}
	}
}


