package minify

import (
	"os"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core/config"
)

// Enabled reports whether this process serves minified code: the
// embedded runtime JavaScript and every stylesheet the framework
// composes (component CSS, app.css, widget chrome). The default is
// "production wins":
//
//   - GOFASTR_ENV set to "production"/"prod"/"live"/"staging" → minify.
//   - GOFASTR_DEV truthy (and GOFASTR_ENV not a non-dev env) → keep raw
//     so browser devtools show readable sources and stack traces.
//   - Neither set → minify (the right default for any app that just
//     `go run`s its binary in production with no env hints).
//   - RUNTIME_NOMINIFY truthy → force raw (manual override; trumps the
//     env detection so a dev can debug a production-config app).
//   - RUNTIME_MINIFY truthy → force minify (manual override; useful when
//     reproducing a prod issue from a dev workstation).
//
// Evaluated once per process; flipping the env later has no effect,
// because the minified payloads are cached on first use anyway.
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
	// Explicit manual overrides win.
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
