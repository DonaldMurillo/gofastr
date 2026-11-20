package preset

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestModalDefaults(t *testing.T) {
	d := Modal("m").Build()
	if d.Position != widget.Center {
		t.Errorf("Modal Position = %q, want %q", d.Position, widget.Center)
	}
	if !d.Backdrop {
		t.Errorf("Modal must have Backdrop=true")
	}
	if !d.CloseOnEscape || !d.CloseOnClickOutside {
		t.Errorf("Modal must close on ESC + outside")
	}
}

// Drawer and BottomSheet share the overlay contract: a backdrop, the
// dialog role, and close on Escape or click-outside.
func TestDrawerAndBottomSheetDefaults(t *testing.T) {
	overlays := []struct {
		name     string
		preset   func(string) *widget.Builder
		position widget.Position
	}{
		{"Drawer", Drawer, widget.Edge},
		{"BottomSheet", BottomSheet, widget.Bottom},
	}
	for _, o := range overlays {
		t.Run(o.name, func(t *testing.T) {
			d := o.preset(strings.ToLower(o.name)).Build()
			if d.Position != o.position {
				t.Errorf("%s Position = %q, want %q", o.name, d.Position, o.position)
			}
			if !d.Backdrop {
				t.Errorf("%s must have Backdrop=true", o.name)
			}
			if d.Role != "dialog" {
				t.Errorf("%s Role = %q, want dialog", o.name, d.Role)
			}
			if !d.CloseOnEscape || !d.CloseOnClickOutside {
				t.Errorf("%s must close on ESC + click-outside", o.name)
			}
		})
	}
}

func TestPopoverDefaults(t *testing.T) {
	d := Popover("p").Build()
	if d.Position != widget.TopRight {
		t.Errorf("Popover default Position = %q, want %q", d.Position, widget.TopRight)
	}
	if !d.Hidden {
		t.Errorf("Popover must be Hidden by default (click-to-open)")
	}
	if !d.CloseOnEscape {
		t.Errorf("Popover must close on Escape")
	}
	if !d.CloseOnClickOutside {
		t.Errorf("Popover must close on click-outside")
	}
	if d.Backdrop {
		t.Errorf("Popover MUST NOT add a backdrop — it floats above content")
	}
}

func TestPopoverPositionOverride(t *testing.T) {
	d := Popover("p").Mount(widget.BottomLeft).Build()
	if d.Position != widget.BottomLeft {
		t.Errorf("Popover position override failed: %q", d.Position)
	}
}

// Toasts rise in the bottom-right corner, clear of a page's header
// actions.
func TestToastStackDefaultIsBottomRight(t *testing.T) {
	d := ToastStack("ts").Build()
	if d.Position != widget.BottomRight {
		t.Errorf("ToastStack default position = %q", d.Position)
	}
}

func TestBannerDefaults(t *testing.T) {
	d := Banner("b").Build()
	if d.Position != widget.Top {
		t.Errorf("Banner position = %q", d.Position)
	}
}

func TestToastStackSlotEscapesName(t *testing.T) {
	// The stack name is rendered into the data-cui-toast-stack
	// attribute. If unescaped, a name containing `"` breaks out of
	// the attribute (and via " onload=… could execute on browsers
	// that load this attr lazily).
	s := clientToastSlot{name: `evil" onload="alert(1)`}
	got := string(s.Render())
	if strings.Contains(got, `evil" onload="alert(1)`) {
		t.Fatalf("ToastStack name rendered unescaped into HTML attr — XSS:\n%s", got)
	}
	// Must contain the escaped form so the slot still works for the
	// legitimate use case.
	if !strings.Contains(got, `&quot;`) {
		t.Fatalf("expected escaped quote in attr value:\n%s", got)
	}
}

func TestToastSlotRendersTheRegisteredTemplate(t *testing.T) {
	registry.IsolateForTest(t)
	bare := string(ToastSlotHTML(context.Background(), "ts"))
	if strings.Contains(bare, "<template") || strings.Contains(bare, "class=") {
		t.Fatalf("with no template registered the slot must be the bare container:\n%s", bare)
	}
	registry.RegisterTemplate(ToastTemplate, func(context.Context) render.HTML {
		return render.HTML(`<template data-hui-toast-template=""><div class="kit-row"></div></template>`)
	})
	got := string(ToastSlotHTML(context.Background(), "ts"))
	want := `<div data-cui-comp="ui-toast-stack" data-cui-toast-stack="ts"><template data-hui-toast-template=""><div class="kit-row"></div></template></div>`
	if got != want {
		t.Fatalf("slot with a registered template:\n got %s\nwant %s", got, want)
	}
}

// The confirm dialog ships only when a kit registered one, inside the
// inert template the runtime clones; with none, the page carries
// nothing and the runtime falls back to window.confirm.
func TestConfirmTemplateWrapsTheRegisteredDialog(t *testing.T) {
	registry.IsolateForTest(t)
	if got := ConfirmTemplateHTML(context.Background()); got != "" {
		t.Fatalf("with no dialog registered the page carries %q", got)
	}
	registry.RegisterTemplate(ConfirmTemplate, func(context.Context) render.HTML {
		return render.HTML(`<dialog class="kit-confirm"></dialog>`)
	})
	got := string(ConfirmTemplateHTML(context.Background()))
	want := `<template data-cui-confirm-dialog=""><dialog class="kit-confirm"></dialog></template>`
	if got != want {
		t.Fatalf("the registered dialog:\n got %s\nwant %s", got, want)
	}
}

func TestIsToastStackRecognisesOnlyThisPackagesStack(t *testing.T) {
	stack := ToastStack("ts").Build()
	if !IsToastStack(&stack) {
		t.Fatal("IsToastStack is false for preset.ToastStack's own build")
	}
	other := Modal("m").Build()
	if IsToastStack(&other) {
		t.Fatal("IsToastStack is true for a modal")
	}
	if IsToastStack(nil) {
		t.Fatal("IsToastStack is true for nil")
	}
}
