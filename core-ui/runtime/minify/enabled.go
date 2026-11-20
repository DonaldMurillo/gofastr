package minify

import (
	"sync"

	"github.com/DonaldMurillo/gofastr/core/config"
	"github.com/DonaldMurillo/gofastr/internal/devmode"
)

// Enabled reports whether this process serves minified JavaScript and CSS, decided once.
func Enabled() bool {
	enabledOnce.Do(func() {
		enabledVal = decideEnabled()
	})
	return enabledVal
}

var (
	enabledOnce sync.Once
	enabledVal  bool
)

func decideEnabled() bool {
	if config.EnvBool("RUNTIME_NOMINIFY") {
		return false
	}
	if config.EnvBool("RUNTIME_MINIFY") {
		return true
	}
	return !devmode.Enabled()
}
