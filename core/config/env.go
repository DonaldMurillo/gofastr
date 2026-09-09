package config

import (
	"os"
	"strconv"
)

// EnvBool reports whether the environment variable key is set to a
// strconv.ParseBool-true value. Unset, empty, falsy, and unparseable
// values are all false — the strict spelling for dev-mode and explicit
// opt-in flags that must never turn on by accident in a
// production-leaning environment.
//
// Replaces the three body-identical copies: core-ui/runtime envBool,
// framework/dev (livereload.go) envBool, and the body of framework's
// devMCPExposeAllowed (devmcp_bind.go).
func EnvBool(key string) bool {
	v := os.Getenv(key)
	if v == "" {
		return false
	}
	b, err := strconv.ParseBool(v)
	return err == nil && b
}
