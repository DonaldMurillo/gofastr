package setup

import (
	"context"
	"errors"
	"net/url"
	"sync/atomic"
	"testing"
)

// Property: the wizard must not execute a step while the completion
// probe's answer is UNKNOWN — a Complete predicate that returns an
// error means the framework cannot tell whether the app is already
// set up, so running bootstrap steps (e.g. AdminStep → CreateUser)
// in that state can mint a second admin on an already-configured app.
//
// Surfaces (both fail closed only when Complete says false; an error
// is currently treated as "not done" and execution proceeds):
//   - Runner.serve → handleSetup: `if err == nil && done && !force`
//     falls through to POST handling on a probe error.
//   - Runner.handleSubmit → runStepSerialized: `if err == nil && done`
//     skips the already-complete short-circuit on a probe error and
//     runs step.Run anyway.
//
// Attack path: an app deployed with DisableToken (documented
// "trusted network" posture) or any deployment where the attacker
// holds the setup cookie, plus a Complete probe that errors exactly
// while the users table already has rows (e.g. restored DB, probe
// flap). The POST executes AdminStep.Run with attacker-chosen
// credentials.
func TestSetupProbeErrorFailsClosed(t *testing.T) {
	var runs int32
	r := New(Config{
		DisableToken: true, // isolate the probe-error property from the token gate
		Steps: []Step{{
			Name: "Create Admin",
			Fields: []Field{{
				Name:     "ADMIN_EMAIL",
				Label:    "Admin email",
				EnvVar:   "GOFASTR_ADMIN_EMAIL",
				Validate: func(string) error { return nil },
			}},
			Run: func(context.Context, map[string]string) error {
				atomic.AddInt32(&runs, 1)
				return nil
			},
		}},
		// The probe is DOWN: unknown, not "incomplete". The wizard must
		// not run steps while it cannot know whether setup already
		// finished.
		Complete: func(context.Context) (bool, error) {
			return false, errors.New("probe unreachable")
		},
	})
	h := r.Handler(func() {}, nil, nil)

	form := url.Values{"ADMIN_EMAIL": {"attacker@example.com"}}
	w := doPost(h, "/setup", form.Encode(), nil)

	if atomic.LoadInt32(&runs) != 0 {
		t.Fatalf("step.Run executed %d time(s) while the completion probe errored — probe error must fail closed (status %d)", runs, w.Code)
	}
}
