package widget

import (
	"strings"
	"testing"
)

// A backdrop'd widget's default chrome is an open <dialog> the runtime
// promotes with showModal(); it carries no role="dialog" or aria-modal,
// which the element implies. A widget without a backdrop stays a <div>.
func TestModalChromeIsADialog(t *testing.T) {
	m := New("m").Mount(Center).Build()
	modal := RenderChrome(&m)
	if !strings.HasPrefix(modal, `<dialog class="cui-widget cui-pos-center" data-cui-widget="m" open>`) || !strings.HasSuffix(modal, `</dialog>`) {
		t.Errorf("centered modal chrome is not an open <dialog>:\n%s", modal)
	}
	for _, bad := range []string{`role="dialog"`, `aria-modal`, `closedby`} {
		if strings.Contains(modal, bad) {
			t.Errorf("modal chrome carries %s:\n%s", bad, modal)
		}
	}

	stay := New("s").Mount(Center).Build()
	stay.CloseOnEscape = false
	if got := RenderChrome(&stay); !strings.Contains(got, ` closedby="none"`) {
		t.Errorf("a modal that refuses Escape lacks closedby=\"none\", so the browser's close request would still close it:\n%s", got)
	}

	alert := New("a").Mount(Center).Role("alertdialog").Build()
	if got := RenderChrome(&alert); !strings.Contains(got, `role="alertdialog"`) {
		t.Errorf("a modal's non-dialog role was dropped:\n%s", got)
	}

	p := New("p").Mount(TopRight).Build()
	if got := RenderChrome(&p); !strings.HasPrefix(got, `<div `) {
		t.Errorf("a widget without a backdrop is no longer a <div>:\n%s", got)
	}
}

// The <dialog> reset sits at zero specificity, so every position rule
// still lays the root out, and the scrim is the dialog's ::backdrop.
func TestDialogChromeCSS(t *testing.T) {
	css := widgetCSS(New("m").Mount(Center).Build())
	for _, want := range []string{":where(dialog.cui-widget)", "dialog.cui-widget::backdrop", "dialog.cui-pos-center"} {
		if !strings.Contains(css, want) {
			t.Errorf("widget CSS lacks %s", want)
		}
	}
}
