// Package setup provides a first-run setup flow for self-hosted GoFastr
// apps. An operator deploying a binary against an empty database gets a
// guided bootstrap: create the initial admin, verify adapter health, and
// any other step the app declares — either headlessly (all values in env)
// or through an SSR wizard.
//
// The package exports Config, Field, Step, and constructors for shipped
// steps (AdminStep, HealthStep). A Config becomes a Runner via New; the
// Runner implements framework.SetupRunner and is wired onto an App via
// framework.WithSetup.
package setup

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// Field describes one input the operator provides during setup.
type Field struct {
	// Name is the form field name AND the default env-var suffix
	// (prefixed with GOFASTR_). e.g. "ADMIN_EMAIL" → form name
	// "ADMIN_EMAIL", env GOFASTR_ADMIN_EMAIL.
	Name string
	// Label is the human-readable label shown in the wizard.
	Label string
	// EnvVar is the full environment variable name (e.g.
	// "GOFASTR_ADMIN_EMAIL"). Empty means the field cannot be resolved
	// from env — it can only come through the wizard.
	EnvVar string
	// Secret renders as a password input. Secret values are never
	// pre-filled from env (the wizard shows an empty password field even
	// when GOFASTR_ADMIN_PASSWORD is set) and never logged.
	Secret bool
	// Validate optionally checks the value before the step's Run fires.
	// Returning an error aborts the step and re-renders the field with
	// the message.
	Validate func(value string) error
}

// Step is one unit of the setup flow.
type Step struct {
	Name   string
	Fields []Field
	// Run executes the step with the resolved field values. Called once
	// per step in order. An error aborts the flow (headless) or
	// re-renders the step (interactive).
	Run func(ctx context.Context, values map[string]string) error
}

// Config is the top-level setup configuration.
type Config struct {
	Steps []Step

	// Complete reports whether setup has already finished (e.g. "at
	// least one user exists"). This is a DERIVED check — never a marker
	// file — so a crash mid-setup re-enters setup on next boot. Required.
	Complete func(ctx context.Context) (bool, error)

	// DisableToken opts out of the one-time setup token. Use only on
	// trusted networks where the first operator to reach /setup is
	// guaranteed to be authorised.
	DisableToken bool

	// Title overrides the wizard page title. Defaults to "Setup".
	Title string

	// HostName overrides the host in the setup URL printed at boot.
	// Defaults to the bound listen address.
	HostName string

	// Theme supplies the shared design tokens (--color-* / --font-*)
	// the wizard renders from. When zero, the framework DefaultTheme
	// is used. Pass the same theme the app uses for a coherent look.
	Theme style.Theme
}

// theme returns the configured theme or the framework default when the
// host didn't supply one.
func (c Config) theme() style.Theme {
	if c.Theme.Colors.Background.Value != "" {
		return c.Theme
	}
	return style.DefaultTheme()
}

// Runner implements framework.SetupRunner. Construct via New.
type Runner struct {
	cfg Config

	// wizard state — protected by mu
	mu          sync.Mutex
	currentStep int

	// token is the one-time URL token; zeroed after the first
	// successful exchange so it can't be replayed from access logs.
	token string
	// cookieSecret is the stable secret backing the setup cookie. It
	// survives the token's single-use invalidation so the already-
	// issued cookie remains valid for the rest of the session.
	cookieSecret string
	tokenEnabled bool

	// swap is stored from Handler() so the wizard can call it when
	// the final step completes.
	swap func()

	// preResolved caches whether all fields resolved from env at boot.
	// Computed once in CanRunHeadless.
	preResolved bool
	preChecked  bool
}

// New constructs a Runner from cfg. Panics if cfg.Complete is nil —
// without a Complete predicate the framework can't tell whether setup
// is done, and a derived check (not a marker file) is the contract.
func New(cfg Config) *Runner {
	if cfg.Complete == nil {
		panic("setup: Config.Complete is required")
	}
	if cfg.Title == "" {
		cfg.Title = "Setup"
	}
	return &Runner{cfg: cfg, tokenEnabled: !cfg.DisableToken}
}

// Incomplete reports whether setup has not yet finished.
func (r *Runner) Incomplete(ctx context.Context) (bool, error) {
	done, err := r.cfg.Complete(ctx)
	if err != nil {
		return true, fmt.Errorf("complete check: %w", err)
	}
	return !done, nil
}

// CanRunHeadless reports whether every field across all steps resolves
// from the environment. A field resolves when its EnvVar is non-empty AND
// the variable is actually set.
func (r *Runner) CanRunHeadless(_ context.Context) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.preChecked {
		return r.preResolved, nil
	}
	r.preChecked = true
	r.preResolved = allFieldsFromEnv(r.cfg.Steps)
	return r.preResolved, nil
}

// RunSteps resolves all fields from env and runs every step in order.
// An error names the step that failed.
func (r *Runner) RunSteps(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, step := range r.cfg.Steps {
		values := resolveFromEnv(step.Fields)
		if err := runValidations(step, values); err != nil {
			return fmt.Errorf("step %q: %w", step.Name, err)
		}
		if err := step.Run(ctx, values); err != nil {
			return fmt.Errorf("step %q: %w", step.Name, err)
		}
	}
	return nil
}

// SetupURL returns the operator-facing setup URL for the startup banner.
func (r *Runner) SetupURL(addr string) string {
	host := r.cfg.HostName
	if host == "" {
		host = addr
	}
	if !r.tokenEnabled || r.token == "" {
		return "http://" + host + "/setup"
	}
	return "http://" + host + "/setup?token=" + r.token
}

// ─── env resolution helpers ─────────────────────────────────────────

// allFieldsFromEnv reports whether every field across all steps has a
// non-empty EnvVar that is set in the environment.
func allFieldsFromEnv(steps []Step) bool {
	for _, step := range steps {
		for _, f := range step.Fields {
			if f.EnvVar == "" {
				return false
			}
			if os.Getenv(f.EnvVar) == "" {
				return false
			}
		}
	}
	return true
}

// resolveFromEnv builds a values map for a step's fields from env.
func resolveFromEnv(fields []Field) map[string]string {
	values := make(map[string]string, len(fields))
	for _, f := range fields {
		if f.EnvVar != "" {
			values[f.Name] = os.Getenv(f.EnvVar)
		}
	}
	return values
}

// runValidations fires each field's Validate function in declaration order.
func runValidations(step Step, values map[string]string) error {
	for _, f := range step.Fields {
		if f.Validate == nil {
			continue
		}
		v := values[f.Name]
		if err := f.Validate(v); err != nil {
			return fmt.Errorf("field %q: %w", f.Name, err)
		}
	}
	return nil
}

// isForceMode reports whether GOFASTR_SETUP=force is set — rescue mode
// where the wizard re-runs even when Complete returns true.
func isForceMode() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("GOFASTR_SETUP")), "force")
}
