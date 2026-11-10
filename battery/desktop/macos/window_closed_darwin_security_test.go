//go:build darwin && (arm64 || amd64)

package macos

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/windowtest"
)

func TestClosedWindowRefusesMutations(t *testing.T) {
	windowtest.Closed(t, &darwinWindow{shell: New().(*darwinShell), id: "closed", title: "Original"})
}
