package minify

import (
	"os"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/config"
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
	if isNonDevEnv(os.Getenv("GOFASTR_ENV")) {
		return true
	}
	return !config.EnvBool("GOFASTR_DEV")
}

func isNonDevEnv(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "production", "prod", "live", "staging":
		return true
	}
	return false
}
