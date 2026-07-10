package framework

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// SetupRunner is the interface a first-run setup implementation provides.
// framework defines it (rather than importing battery/setup) to avoid a
// layering cycle: battery/setup implements it and the host wires the
// concrete runner via WithSetup.
//
// Lifecycle inside App.Start:
//
//  1. Incomplete is consulted after plugin init and before consumer start.
//     When true (and GOFASTR_SETUP != "off"), Start enters setup mode.
//  2. CanRunHeadless distinguishes the two skins. When true every required
//     field resolves from the environment and RunSteps executes inline
//     before the port binds — no wizard is ever served.
//  3. When CanRunHeadless is false the interactive skin is served: Handler
//     returns the wizard surface and swap is invoked once the final step
//     succeeds, atomically switching to the real app handler and starting
//     deferred background consumers.
type SetupRunner interface {
	// Incomplete reports whether first-run setup has not yet finished.
	Incomplete(ctx context.Context) (bool, error)

	// CanRunHeadless reports whether every required field across all
	// steps resolves from the environment, so bootstrap can run inline.
	CanRunHeadless(ctx context.Context) (bool, error)

	// RunSteps runs all steps synchronously. Called only when
	// CanRunHeadless returns true. An error aborts Start.
	RunSteps(ctx context.Context) error

	// Handler returns the interactive setup surface. swap is called
	// once setup completes to switch to the real handler. healthz and
	// readyz are the app's existing health handlers, passed so the
	// setup surface can serve /healthz and /readyz during setup.
	Handler(swap func(), healthz, readyz http.HandlerFunc) http.Handler

	// SetupURL returns the operator-facing URL (with token) for the
	// startup banner. addr is the bound listen address. Returns ""
	// when not applicable (headless path or token disabled).
	SetupURL(addr string) string
}

// WithSetup wires a first-run setup runner. When the runner reports
// incomplete setup at boot, Start either runs the steps headlessly (env
// provides every required value) or serves an interactive wizard until
// setup finishes — then atomically swaps to the real app router.
//
// Overrides via the GOFASTR_SETUP env var:
//
//   - "off":    never enter setup mode (Start proceeds normally)
//   - "force":  enter setup mode even if Complete reports done (rescue)
//   - invalid:  Start fails loudly
func WithSetup(r SetupRunner) AppOption {
	if r == nil {
		panic("framework: WithSetup(nil)")
	}
	return func(a *App) {
		a.setup = r
	}
}

// setupEnvMode is the resolved GOFASTR_SETUP value.
type setupEnvMode int

const (
	setupAuto  setupEnvMode = iota // GOFASTR_SETUP unset or empty — check Incomplete
	setupOff                       // "off" — never enter setup mode
	setupForce                     // "force" — enter setup even if Complete
)

// resolveSetupEnv parses the GOFASTR_SETUP env var, mirroring role.go's
// fail-loud-on-unknown-value convention so a typo doesn't silently run
// the wrong boot path.
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
