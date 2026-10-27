package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui/icons"
)

func TestIcon_BuiltInsRegistered(t *testing.T) {
	for _, name := range []string{
		"check", "close", "menu", "search", "chevron-up", "chevron-down",
		"chevron-left", "chevron-right",
		"info", "warning", "danger", "success",
		"home", "user", "users", "file", "receipt", "card", "box", "layers",
		"activity", "shield", "key", "lock", "sliders", "chart", "calendar",
		"clock", "plus", "list", "grid", "database", "inbox", "folder", "tag",
		"mail", "bell", "globe", "repeat", "cpu", "star",
	} {
		if !IconRegistered(name) {
			t.Errorf("expected built-in icon %q to be registered", name)
		}
	}
}

func TestIcon_RendersInlineSVG(t *testing.T) {
	got := string(Icon("check", IconConfig{}))
	if !strings.Contains(got, "<svg") {
		t.Errorf("expected <svg>, got: %s", got)
	}
	if !strings.Contains(got, `stroke="currentColor"`) && !strings.Contains(got, `fill="currentColor"`) {
		t.Errorf("expected currentColor on stroke or fill, got: %s", got)
	}
	if !strings.Contains(got, `viewBox=`) {
		t.Errorf("expected viewBox, got: %s", got)
	}
}

func TestIcon_DefaultIsAriaHidden(t *testing.T) {
	got := string(Icon("check", IconConfig{}))
	if !strings.Contains(got, `aria-hidden="true"`) {
		t.Errorf("expected aria-hidden=\"true\" without AriaLabel, got: %s", got)
	}
}

func TestIcon_AriaLabelSetsRoleImg(t *testing.T) {
	got := string(Icon("check", IconConfig{AriaLabel: "Saved"}))
	if !strings.Contains(got, `role="img"`) {
		t.Errorf("expected role=\"img\" with AriaLabel, got: %s", got)
	}
	if !strings.Contains(got, `aria-label="Saved"`) {
		t.Errorf("expected aria-label value, got: %s", got)
	}
	if strings.Contains(got, `aria-hidden="true"`) {
		t.Errorf("must not set aria-hidden when an explicit label is given, got: %s", got)
	}
}

func TestIcon_SizeOverridesDefault(t *testing.T) {
	got := string(Icon("check", IconConfig{Size: "1.25rem"}))
	if !strings.Contains(got, `width="1.25rem"`) || !strings.Contains(got, `height="1.25rem"`) {
		t.Errorf("expected width/height = 1.25rem, got: %s", got)
	}
}

func TestIcon_CustomClass(t *testing.T) {
	got := string(Icon("check", IconConfig{Class: "my-icon"}))
	if !classTokenPresent(got, "fui-icon") || !classTokenPresent(got, "my-icon") {
		t.Errorf("expected base + custom class, got: %s", got)
	}
}

func TestIcon_UnknownNameRendersEmpty(t *testing.T) {
	got := string(Icon("does-not-exist", IconConfig{}))
	if got != "" {
		t.Errorf("expected empty render for unknown icon, got: %s", got)
	}
}

func TestRegisterIcon_AddsCustomIcon(t *testing.T) {
	RegisterIcon("test-square",
		`<rect x="2" y="2" width="20" height="20" stroke="currentColor" stroke-width="2"/>`)
	if !IconRegistered("test-square") {
		t.Fatal("expected test-square to be registered")
	}
	got := string(Icon("test-square", IconConfig{}))
	if !strings.Contains(got, "<rect") {
		t.Errorf("expected registered SVG body to render, got: %s", got)
	}
}

func TestIconExtraAttrsOnRoot(t *testing.T) {
	h := Icon("check", IconConfig{ExtraAttrs: map[string]string{"data-test": "hook"}})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("icon root missing data-test:\n%s", root)
	}
}

func TestIconBuiltinOverrideKeepsAliasIndependent(t *testing.T) {
	before := string(Icon(icons.Check, IconConfig{}))
	alias := string(Icon("success", IconConfig{}))
	if before == "" || alias != before {
		t.Fatal("check and success must initially share a built-in drawing")
	}
	iconRegistryMu.RLock()
	previous, existed := iconRegistry[icons.Check]
	iconRegistryMu.RUnlock()
	t.Cleanup(func() {
		iconRegistryMu.Lock()
		defer iconRegistryMu.Unlock()
		if existed {
			iconRegistry[icons.Check] = previous
		} else {
			delete(iconRegistry, icons.Check)
		}
	})
	RegisterIcon(icons.Check, `<path d="M2 2h3"/>`)
	if got := string(Icon(icons.Check, IconConfig{})); !strings.Contains(got, `d="M2 2h3"`) || got == before {
		t.Fatal("application registration did not override the built-in")
	}
	if got := string(Icon("success", IconConfig{})); got != alias {
		t.Fatal("overriding check changed the independent success name")
	}
}
