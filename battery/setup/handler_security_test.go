package setup

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
)

// Property: the wizard must not execute a step while the completion
// probe's answer is UNKNOWN, a Complete predicate that returns an
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

// ─── single-use token must not survive the exchange as a cookie ───────

// Property: the one-time setup URL token must stop authenticating the
// moment it is exchanged. first-run.md's contract:
//
//	"It is **single-use**: the first successful visit exchanges it for an
//	 HttpOnly cookie and invalidates the URL form, so a token that leaked
//	 into an access log cannot be replayed."
//
// Surface: Runner.Handler sets cookieSecret = the URL token
// (handler.go), and handleTokenExchange invalidates only r.token while
// issuing a cookie carrying that same value — so the replay the doc
// rules out works verbatim: after the operator exchanges the URL,
// anyone who only ever saw the URL (proxy/access log) sends
// Cookie: gofastr_setup=<token> from a second client and gets the
// wizard for the life of the process, then completes any remaining
// custom steps with attacker-chosen values.
func TestSetupTokenNotReplayableAsCookie(t *testing.T) {
	done := false
	r := buildTestRunner(t, false /* token enabled */, &done)
	h := r.Handler(func() {}, nil, nil)

	// The value printed in the startup URL — assume it leaked into a log.
	leaked := r.token

	// The legitimate operator exchanges the URL token, consuming it
	// (single-use of the URL form is already pinned by
	// TestToken_SingleUse_SecondExchangeForbidden).
	if w := doGet(h, "/setup?token="+leaked); w.Code != http.StatusSeeOther {
		t.Fatalf("sanity: token exchange returned %d, want 303", w.Code)
	}

	// The attacker replays the SAME value as the cookie from a second
	// client. Its only provenance is the leaked URL, so it must be
	// refused now that the token has been exchanged.
	req := httptest.NewRequest(http.MethodGet, "/setup", nil)
	req.AddCookie(&http.Cookie{Name: setupCookieName, Value: leaked})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		body := w.Body.String()
		if len(body) > 120 {
			body = body[:120]
		}
		t.Fatalf("SECURITY: [setup] exchanged-away token still authenticates: GET /setup with Cookie %s=<consumed token> returned %d, want 403 — the wizard rendered for a value whose only provenance is a leaked URL, violating first-run.md's \"a token that leaked into an access log cannot be replayed\". Body starts: %q",
			setupCookieName, w.Code, body)
	}
}

// ─── headless RunSteps must honor Complete ────────────────────────────

// Property: bootstrap steps must never execute once Complete reports
// true. first-run.md: "`Complete` decides everything: while it reports
// false, the app is in setup; the moment it reports true, setup is
// over." The interactive skin enforces exactly that —
// runStepSerialized re-checks Complete under the lock and refuses,
// with the in-package comment: "this guard is the only thing standing
// between a second caller and a re-run of a step that typically
// creates the admin account."
//
// Surface: Runner.RunSteps (setup.go) runs every step unconditionally —
// it never consults Complete. The framework reaches it under
// GOFASTR_SETUP=force even on a completed install, and AdminStep.Run
// unconditionally INSERTs an admin-role user, so a redeploy with force
// still set and rotated env credentials silently mints a second admin
// (or aborts boot with ErrEmailTaken when the email matches).
func TestRunStepsRefusesWhenComplete(t *testing.T) {
	var runs int32
	r := New(Config{
		Complete: func(context.Context) (bool, error) { return true, nil },
		Steps: []Step{{
			Name:   "Create Admin",
			Fields: []Field{{Name: "ADMIN_EMAIL", Label: "Admin email"}},
			Run: func(context.Context, map[string]string) error {
				atomic.AddInt32(&runs, 1)
				return nil
			},
		}},
	})

	// A refusal may surface as an error or a silent skip; the property
	// is that no step body executes on a completed install.
	_ = r.RunSteps(context.Background())

	if got := atomic.LoadInt32(&runs); got != 0 {
		t.Fatalf("SECURITY: [setup] RunSteps executed %d bootstrap step(s) while Complete reported true — the headless skin lacks the re-run guard the interactive skin enforces (runStepSerialized); under GOFASTR_SETUP=force this re-runs AdminStep and mints a second admin-role user on a configured install", got)
	}
}

// ─── the wizard's form body must be capped like every sibling surface ──

// Property: request bodies on the (possibly unauthenticated) setup surface
// are size-capped by the app before parsing, at the repo's 1 MiB form
// convention rather than the stdlib's 10 MiB urlencoded floor. The step's
// Run must never execute for an over-cap body.
func TestSetupFormCapsBody(t *testing.T) {
	var runs atomic.Int32
	r := New(Config{
		DisableToken: true,
		Complete:     func(context.Context) (bool, error) { return false, nil },
		Steps: []Step{{
			Name: "probe",
			Run: func(context.Context, map[string]string) error {
				runs.Add(1)
				return nil
			},
		}},
	})
	h := r.Handler(func() {}, nil, nil)

	// 4 MiB urlencoded body — 4x the auth sibling's cap, comfortably under
	// the stdlib's 10 MiB floor so the only thing that could refuse it is
	// an app-level cap. No Origin/Sec-Fetch headers: a plain non-browser
	// client, the shape rejectCrossSiteForm deliberately lets through.
	body := "pad=" + strings.Repeat("a", 4<<20)
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusRequestEntityTooLarge || runs.Load() != 0 {
		t.Errorf("SECURITY: [setup-body-cap] POST /setup with a 4 MiB urlencoded body returned %d with the step's Run invoked %d time(s); want 413 with the step never invoked",
			rec.Code, runs.Load())
	}
}

// ─── GOFASTR_SETUP=force is the explicit re-run opt-in ──────────────────

// Property: force mode re-runs completed steps on BOTH skins — the wizard
// banner says "Re-running setup steps" and first-run.md tells operators to
// use force to re-enter the wizard, so the engine must honour it or the
// rescue mode is a silent no-op that leaves operators believing bootstrap
// ran. The NO-force refusal stays pinned by TestRunStepsRefusesWhenComplete.
func TestSetupForceReRunsCompletedSteps(t *testing.T) {
	newRunner := func(runs *atomic.Int32) *Runner {
		return New(Config{
			DisableToken: true,
			Complete:     func(context.Context) (bool, error) { return true, nil },
			Steps: []Step{{
				Name: "probe",
				Run: func(context.Context, map[string]string) error {
					runs.Add(1)
					return nil
				},
			}},
		})
	}

	// Interactive skin: the wizard with the "Re-running setup steps"
	// banner accepts the operator's POST and executes the step.
	t.Run("wizard submit", func(t *testing.T) {
		t.Setenv("GOFASTR_SETUP", "force")
		var runs atomic.Int32
		h := newRunner(&runs).Handler(func() {}, nil, nil)

		req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader("probe=1"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if runs.Load() == 0 {
			t.Errorf("CONTRACT: [setup-force] POST /setup under GOFASTR_SETUP=force on a completed install returned %d with the step's Run never invoked: the \"Re-running setup steps\" banner promises a re-run the engine must deliver (or refuse out loud — never a silent success)",
				rec.Code)
		}
	})

	// Headless skin: RunSteps re-executes instead of returning nil having
	// run nothing.
	t.Run("headless RunSteps", func(t *testing.T) {
		t.Setenv("GOFASTR_SETUP", "force")
		var runs atomic.Int32
		r := newRunner(&runs)

		if err := r.RunSteps(context.Background()); err != nil {
			t.Fatalf("RunSteps under force: %v", err)
		}
		if runs.Load() == 0 {
			t.Errorf("CONTRACT: [setup-force] RunSteps under GOFASTR_SETUP=force on a completed install ran zero steps: the documented rescue mode either re-executes the steps or reports its refusal — a silent no-op leaves an operator believing bootstrap was re-run.")
		}
	})
}
