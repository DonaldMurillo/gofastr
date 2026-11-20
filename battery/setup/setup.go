// Package setup provides a first-run setup flow for self-hosted GoFastr
// apps.
package setup

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/i18n"
)

// Field describes one input the operator provides during setup.
type Field struct {
	// Name is the form field name AND the default env-var suffix (prefixed
	// with GOFASTR_).
	Name string
	// Label is the field's label in the wizard, in the reader's language.
	Label i18n.Phrase
	// EnvVar is the full environment variable name (e.g.
	// "GOFASTR_ADMIN_EMAIL").
	EnvVar string
	// Secret renders as a password input.
	Secret bool
	// Validate optionally checks the value before the step's Run fires.
	Validate func(value string) error
}

func (f Field) label(ctx context.Context) string {
	if f.Label.IsZero() {
		return f.Name
	}
	return f.Label.Text(ctx)
}

// Step is one unit of the setup flow.
type Step struct {
	// Name identifies the step in logs and headless errors.
	Name string
	// Title is the step's heading and progress label in the wizard, in the
	// reader's language.
	Title  i18n.Phrase
	Fields []Field
	// Run executes the step with the resolved field values.
	Run func(ctx context.Context, values map[string]string) error
}

func (s Step) title(ctx context.Context) string {
	if s.Title.IsZero() {
		return s.Name
	}
	return s.Title.Text(ctx)
}

// Config is the top-level setup configuration.
type Config struct {
	Steps []Step

	// Complete reports whether setup has already finished (e.g. "at least one
	// user exists").
	Complete func(ctx context.Context) (bool, error)

	// DisableToken opts out of the one-time setup token.
	DisableToken bool

	// Title overrides the wizard page title.
	Title i18n.Phrase

	// HostName overrides the host in the setup URL printed at boot.
	HostName string

	// Theme supplies the shared design tokens (--color-* / --font-*) the
	// wizard renders from.
	Theme style.Theme
}

func (c Config) theme() style.Theme {
	if c.Theme.Colors.Background.Value != "" {
		return c.Theme
	}
	return style.DefaultTheme()
}

// Runner implements framework.SetupRunner.
type Runner struct {
	cfg Config

	mu           sync.Mutex
	currentStep  int
	stepInFlight bool

	token        string
	cookieSecret string
	tokenEnabled bool

	swap func()

	preResolved bool
	preChecked  bool

	lastRecheck atomic.Int64
	now         func() time.Time
}

// New constructs a Runner from cfg.
func New(cfg Config) *Runner {
	if cfg.Complete == nil {
		panic("setup: Config.Complete is required")
	}
	if cfg.Title.IsZero() {
		cfg.Title = pageTitle
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
// from the environment.
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
func (r *Runner) RunSteps(ctx context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.cfg.Complete != nil {
		done, err := r.cfg.Complete(ctx)
		if err != nil {
			return fmt.Errorf("setup: completion check failed, refusing to run steps: %w", err)
		}
		if done && !isForceMode() {
			return nil
		}
	}

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
func (r *Runner) SetupURL(scheme, addr string) string {
	host := r.cfg.HostName
	if host == "" {
		host = addr
	}
	if !r.tokenEnabled || r.token == "" {
		return scheme + "://" + host + "/setup"
	}
	return scheme + "://" + host + "/setup?token=" + r.token
}

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

func resolveFromEnv(fields []Field) map[string]string {
	values := make(map[string]string, len(fields))
	for _, f := range fields {
		if f.EnvVar != "" {
			values[f.Name] = os.Getenv(f.EnvVar)
		}
	}
	return values
}

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

func isForceMode() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("GOFASTR_SETUP")), "force")
}
