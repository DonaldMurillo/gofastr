package app_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// stubComp is a minimal component for testing.
type stubComp struct{ html render.HTML }

func (s *stubComp) Render() render.HTML { return s.html }

var _ component.Component = (*stubComp)(nil)

func TestScreenGroupPrefix(t *testing.T) {
	sg := app.NewScreenGroup("/settings", nil)
	if sg.Prefix() != "/settings/" {
		t.Errorf("Prefix() = %q, want %q", sg.Prefix(), "/settings/")
	}
}

func TestScreenGroupNormalizesPrefix(t *testing.T) {
	tests := []struct{ in, want string }{
		{"settings", "/settings/"},
		{"/settings", "/settings/"},
		{"/settings/", "/settings/"},
		{"", "/"},
		{"/", "/"},
	}
	for _, tt := range tests {
		sg := app.NewScreenGroup(tt.in, nil)
		if got := sg.Prefix(); got != tt.want {
			t.Errorf("NewScreenGroup(%q).Prefix() = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestScreenGroupRegistersScreens(t *testing.T) {
	sg := app.NewScreenGroup("/settings", app.NewLayout("settings"))
	sg.Screen(app.NewScreen("profile", &stubComp{html: "profile"}), nil)
	sg.Screen(app.NewScreen("security", &stubComp{html: "security"}), nil)

	screens := sg.Screens()
	if len(screens) != 2 {
		t.Fatalf("len(Screens()) = %d, want 2", len(screens))
	}

	// Paths should be resolved relative to group prefix
	if screens[0].Path != "/settings/profile" {
		t.Errorf("screens[0].Path = %q, want %q", screens[0].Path, "/settings/profile")
	}
	if screens[1].Path != "/settings/security" {
		t.Errorf("screens[1].Path = %q, want %q", screens[1].Path, "/settings/security")
	}
}

func TestScreenGroupLayoutInherited(t *testing.T) {
	layout := app.NewLayout("settings")
	sg := app.NewScreenGroup("/settings", layout)

	s := app.NewScreen("profile", &stubComp{html: "profile"})
	sg.Screen(s, nil)

	if s.Layout != layout {
		t.Error("screen did not inherit group layout")
	}
}

func TestScreenGroupLayoutExplicit(t *testing.T) {
	groupLayout := app.NewLayout("group")
	explicitLayout := app.NewLayout("explicit")
	sg := app.NewScreenGroup("/settings", groupLayout)

	s := app.NewScreen("profile", &stubComp{html: "profile"})
	sg.Screen(s, explicitLayout)

	if s.Layout != explicitLayout {
		t.Error("screen should use explicit layout, not group layout")
	}
}

func TestSubGroup(t *testing.T) {
	parentLayout := app.NewLayout("parent")
	parent := app.NewScreenGroup("/settings", parentLayout)

	childLayout := app.NewLayout("child")
	child := parent.SubGroup("advanced", childLayout)

	child.Screen(app.NewScreen("security", &stubComp{html: "security"}), nil)

	if child.Prefix() != "/settings/advanced/" {
		t.Errorf("child.Prefix() = %q, want %q", child.Prefix(), "/settings/advanced/")
	}

	allScreens := parent.AllScreens()
	if len(allScreens) != 1 {
		t.Fatalf("len(AllScreens()) = %d, want 1", len(allScreens))
	}
	if allScreens[0].Path != "/settings/advanced/security" {
		t.Errorf("screen.Path = %q, want %q", allScreens[0].Path, "/settings/advanced/security")
	}
}

func TestSubGroupInheritsParentLayout(t *testing.T) {
	parentLayout := app.NewLayout("parent")
	parent := app.NewScreenGroup("/settings", parentLayout)

	// Child with nil layout inherits parent's
	child := parent.SubGroup("advanced", nil)
	if child.Layout() != parentLayout {
		t.Error("child should inherit parent layout when nil is passed")
	}
}

func TestRouterScreenGroup(t *testing.T) {
	r := app.NewRouter()
	layout := app.NewLayout("admin").WithHeader(&stubComp{html: "admin header"})

	sg := app.NewScreenGroup("/admin", layout)
	sg.Screen(app.NewScreen("dashboard", &stubComp{html: "dashboard content"}), nil)
	sg.Screen(app.NewScreen("users", &stubComp{html: "users content"}), nil)

	r.ScreenGroup(sg)

	// Both screens should be resolvable
	if _, _, ok := r.Resolve("/admin/dashboard"); !ok {
		t.Error("/admin/dashboard not found in router")
	}
	if _, _, ok := r.Resolve("/admin/users"); !ok {
		t.Error("/admin/users not found in router")
	}
}

func TestScreenGroupRenderLayout(t *testing.T) {
	layout := app.NewLayout("test").WithHeader(&stubComp{html: "<h1>Header</h1>"})
	sg := app.NewScreenGroup("/test", layout)

	content := render.HTML("<p>Content</p>")
	result := sg.RenderLayout(content)

	str := string(result)
	if str == "" {
		t.Fatal("RenderLayout returned empty string")
	}
	// Should contain the group marker
	if !contains(str, "data-fui-screen-group") {
		t.Error("RenderLayout should include data-fui-screen-group attribute")
	}
	// Should contain the layout header
	if !contains(str, "<h1>Header</h1>") {
		t.Error("RenderLayout should include layout header content")
	}
	// Should contain the content
	if !contains(str, "<p>Content</p>") {
		t.Error("RenderLayout should include passed content")
	}
}

func TestScreenGroupRenderLayoutNil(t *testing.T) {
	sg := app.NewScreenGroup("/test", nil)
	content := render.HTML("<p>Content</p>")
	result := sg.RenderLayout(content)

	if result != content {
		t.Error("RenderLayout with nil layout should return content unchanged")
	}
}

func TestComposeLayouts(t *testing.T) {
	outerLayout := app.NewLayout("outer").WithHeader(&stubComp{html: "<h1>Outer</h1>"})
	innerLayout := app.NewLayout("inner").WithHeader(&stubComp{html: "<h2>Inner</h2>"})

	outer := app.NewScreenGroup("/app", outerLayout)
	inner := outer.SubGroup("settings", innerLayout)

	content := render.HTML("<p>Content</p>")
	result := app.ComposeLayouts(inner, content)

	str := string(result)
	if !contains(str, "Outer") {
		t.Error("ComposeLayouts should include outer layout content")
	}
	if !contains(str, "Inner") {
		t.Error("ComposeLayouts should include inner layout content")
	}
	if !contains(str, "<p>Content</p>") {
		t.Error("ComposeLayouts should include original content")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && containsStr(s, sub)
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
