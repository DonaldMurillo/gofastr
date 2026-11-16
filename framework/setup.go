package framework

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// SetupRunner is a first-run setup implementation, wired with WithSetup.
// Start consults Incomplete after plugin init; when setup is incomplete it
// runs RunSteps inline if CanRunHeadless, and serves Handler otherwise.
type SetupRunner interface {
	Incomplete(ctx context.Context) (bool, error)

	// CanRunHeadless reports whether every required value resolves from
	// the environment.
	CanRunHeadless(ctx context.Context) (bool, error)

	// RunSteps runs every step; an error aborts Start.
	RunSteps(ctx context.Context) error

	// Handler returns the wizard. Call swap once setup completes: it
	// switches to the real handler and returns once the deferred OnStart
	// hooks have run. healthz and readyz serve the health routes meanwhile.
	Handler(swap func(), healthz, readyz http.HandlerFunc) http.Handler

	// SetupURL returns the banner's setup link for the bound addr, or ""
	// when there is none.
	SetupURL(scheme, addr string) string
}

// WithSetup wires a first-run setup runner. GOFASTR_SETUP=off never
// enters setup, =force enters it even when complete, and any other
// value but auto fails Start.
func WithSetup(r SetupRunner) AppOption {
	if r == nil {
		panic("framework: WithSetup(nil)")
	}
	return func(a *App) {
		a.setup = r
	}
}

// SetSetup is WithSetup for a runner that needs the App, such as one
// holding setup.HealthStep(app). Call it before Start.
func (a *App) SetSetup(r SetupRunner) *App {
	if r == nil {
		panic("framework: SetSetup(nil)")
	}
	a.setup = r
	return a
}

type setupEnvMode int

const (
	setupAuto setupEnvMode = iota
	setupOff
	setupForce
)

func resolveSetupEnv() (setupEnvMode, error) {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("GOFASTR_SETUP"))) {
	case "", "auto":
		return setupAuto, nil
	case "off":
		return setupOff, nil
	case "force":
		return setupForce, nil
	default:
		return 0, fmt.Errorf("GOFASTR_SETUP=%q: invalid value (want off|force|auto)", os.Getenv("GOFASTR_SETUP"))
	}
}
