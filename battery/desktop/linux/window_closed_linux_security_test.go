//go:build linux && (amd64 || arm64)

package linux

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/windowtest"
)

func TestClosedWindowRefusesMutations(t *testing.T) {
	window := &linuxWindow{shell: New().(*linuxShell), id: "closed", title: "Original", handle: 42, webview: 43}
	window.closed.Store(true)
	windowtest.Closed(t, window)
}
