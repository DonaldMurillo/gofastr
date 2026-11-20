// Package preset holds one-call widget builders; each returns a *widget.Builder to customise further.
package preset

import (
	"context"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/registry"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// FloatingPanel is a persistent bottom-right panel with no backdrop.
func FloatingPanel(name string) *widget.Builder {
	return widget.New(name).
		Mount(widget.BottomRight).
		Surface()
}

// Modal is a centered dialog with a backdrop that closes on Escape and click-outside.
func Modal(name string) *widget.Builder {
	return widget.New(name).
		Mount(widget.Center)
}

// ToastStack is an empty bottom-right stack the runtime appends toasts into.
func ToastStack(name string) *widget.Builder {
	return widget.New(name).
		Mount(widget.BottomRight).
		Slot("items", clientToastSlot{name: name})
}

// ToastTemplate is the registry name of the toast row template a kit registers.
const ToastTemplate = "toast-stack"

// ConfirmTemplate is the registry name of the data-cui-confirm dialog a kit registers.
const ConfirmTemplate = "confirm-dialog"

// ConfirmTemplateHTML wraps the registered confirm dialog in <template data-cui-confirm-dialog>, or is "".
func ConfirmTemplateHTML(ctx context.Context) render.HTML {
	tpl, ok := registry.Template(ctx, ConfirmTemplate)
	if !ok || tpl == "" {
		return ""
	}
	return render.Tag("template", map[string]string{"data-cui-confirm-dialog": ""}, tpl)
}

// ToastSlotHTML renders the stack container, with the kit's row template when one is registered.
func ToastSlotHTML(ctx context.Context, name string) render.HTML {
	var b strings.Builder
	b.WriteString(`<div data-cui-comp="ui-toast-stack" data-cui-toast-stack="`)
	b.WriteString(render.Escape(name))
	b.WriteString(`">`)
	if tpl, ok := registry.Template(ctx, ToastTemplate); ok {
		b.WriteString(string(tpl))
	}
	b.WriteString(`</div>`)
	return render.HTML(b.String())
}

// IsToastStack reports whether d is a stack ToastStack built.
func IsToastStack(d *widget.Definition) bool {
	if d == nil {
		return false
	}
	for _, sl := range d.Slots {
		if _, ok := sl.Component.(clientToastSlot); ok {
			return true
		}
	}
	return false
}

type clientToastSlot struct{ name string }

func (s clientToastSlot) Render() render.HTML {
	return ToastSlotHTML(context.Background(), s.name)
}

func (s clientToastSlot) RenderCtx(ctx context.Context) render.HTML {
	return ToastSlotHTML(ctx, s.name)
}

var (
	_ component.Component        = clientToastSlot{}
	_ component.ContextComponent = clientToastSlot{}
)

// Drawer is a left-edge dialog panel with a backdrop that closes on Escape and click-outside.
func Drawer(name string) *widget.Builder {
	b := widget.New(name).
		Mount(widget.Edge).
		Backdrop().
		Role("dialog")
	d := b.Definition()
	d.CloseOnEscape = true
	d.CloseOnClickOutside = true
	return b
}

// Banner is a persistent strip along the top of the page.
func Banner(name string) *widget.Builder {
	return widget.New(name).
		Mount(widget.Top)
}

// BottomSheet is a bottom-edge Drawer that the user can drag down to dismiss.
func BottomSheet(name string) *widget.Builder {
	b := widget.New(name).
		Mount(widget.Bottom).
		Backdrop().
		Role("dialog").
		DragDismiss()
	d := b.Definition()
	d.CloseOnEscape = true
	d.CloseOnClickOutside = true
	return b
}

// Popover is a hidden top-right panel with no backdrop, opened by data-cui-open and anchored to its trigger by data-cui-popover-anchor.
func Popover(name string) *widget.Builder {
	b := widget.New(name).
		Mount(widget.TopRight).
		Hidden().
		Surface()
	d := b.Definition()
	d.CloseOnEscape = true
	d.CloseOnClickOutside = true
	return b
}
