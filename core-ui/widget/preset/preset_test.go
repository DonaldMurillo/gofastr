package preset

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
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

func TestDrawerDefaults(t *testing.T) {
	d := Drawer("d").Build()
	if d.Position != widget.Edge {
		t.Errorf("Drawer Position = %q, want %q", d.Position, widget.Edge)
	}
	if !d.Backdrop {
		t.Errorf("Drawer must have Backdrop=true")
	}
	if d.Role != "dialog" {
		t.Errorf("Drawer Role = %q, want dialog", d.Role)
	}
	if !d.CloseOnEscape || !d.CloseOnClickOutside {
		t.Errorf("Drawer must close on ESC + outside")
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

func TestToastStackDefaultIsTopRight(t *testing.T) {
	d := ToastStack("ts").Build()
	if d.Position != widget.TopRight {
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
	// The stack name is rendered into the data-fui-toast-stack
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

func TestBottomSheetDefaults(t *testing.T) {
	d := BottomSheet("bs").Build()
	if d.Position != widget.Bottom {
		t.Errorf("BottomSheet Position = %q, want %q", d.Position, widget.Bottom)
	}
	if !d.Backdrop {
		t.Errorf("BottomSheet must have Backdrop=true")
	}
	if d.Role != "dialog" {
		t.Errorf("BottomSheet Role = %q, want dialog", d.Role)
	}
	if !d.CloseOnEscape || !d.CloseOnClickOutside {
		t.Errorf("BottomSheet must close on ESC + click-outside")
	}
}
