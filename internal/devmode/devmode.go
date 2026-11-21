// Package devmode implements the process environment's development-mode gate.
package devmode

import (
	"os"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/config"
)

// Enabled reports whether GOFASTR_DEV is truthy outside a production-like environment.
func Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GOFASTR_ENV"))) {
	case "production", "prod", "live", "staging":
		return false
	}
	return config.EnvBool("GOFASTR_DEV")
}
