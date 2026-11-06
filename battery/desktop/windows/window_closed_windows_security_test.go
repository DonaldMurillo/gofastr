//go:build windows && amd64

package windows

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/windowtest"
)

func TestClosedWindowRefusesMutations(t *testing.T) {
	window := &winWindow{shell: New().(*winShell), id: "closed", hwnd: 42, webview: 43, title: "Original"}
	window.closed.Store(true)
	windowtest.Closed(t, window)
}
