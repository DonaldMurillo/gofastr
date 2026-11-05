//go:build windows && amd64

package windows

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
)

func TestClosedWindowKeepsCachedTitle(t *testing.T) {
	window := &winWindow{hwnd: 42, title: "Original"}
	window.closed.Store(true)
	for name, action := range map[string]func() error{
		"focus": window.Focus,
		"title": func() error { return window.SetTitle("Changed") },
		"frame": func() error { return window.SetFrame(desktop.Frame{Width: 320, Height: 240}) },
	} {
		if err := action(); err == nil {
			t.Errorf("%s on a closed window succeeded", name)
		}
	}
	if got := window.Title(); got != "Original" {
		t.Errorf("closed title = %q, want cached Original", got)
	}
}
