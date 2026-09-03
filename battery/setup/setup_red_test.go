//go:build red

package setup

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// ---------------------------------------------------------------------------
// CONTRACT-QUESTION red: GOFASTR_SETUP=force promises a rescue re-run and
// delivers a silent no-op. Delete or promote per maintainer decision.
// Property: when the operator is told setup steps are being re-run, a
// submitted step either executes or the refusal is stated, never a silent
// success.
// Surfaces: the force banner (render.go:77-88: "You are in rescue mode
// (GOFASTR_SETUP=force). Re-running setup steps."), the operator runbook
// (first-run.md:156: "use `force` to re-enter the wizard on a completed
// install"), versus runStepSerialized's unconditional Complete re-check
// (handler.go:261-267: done ⇒ return nil, step.Run never fires) and
// RunSteps' identical silent skip (setup.go:174-182).
// Finding: under force on a completed install the wizard renders with the
// "Re-running setup steps" banner, but every POST silently skips
// step.Run, advances the wizard, and lands the operator on the completion
// page — the UI claims a re-run that the fail-closed engine refuses to
// perform. The headless skin has the same shape: RunSteps returns nil
// having run nothing. Note the engine's refusal is itself pinned as
// SECURITY-correct for the no-force case (TestRunStepsRefusesWhenComplete:
// a completed install must never auto-execute bootstrap steps); the
// question is only what FORCE means: either it is the documented operator
// escape hatch that actually re-runs (interactive submits are explicit,
// hand-typed confirmations, unlike a headless auto-run), or the banner,
// the runbook line, and the mode's name must stop promising a re-run.
// Severity: LOW-MEDIUM — lying UI / dead rescue mode, not a data exposure.
// Fix direction: in runStepSerialized (and RunSteps), treat force as the
// explicit opt-in to re-run: `if done && !isForceMode() { return nil }`,
// keeping the no-force refusal intact; OR keep the refusal and render it
// explicitly (runStepSerialized returns a named "setup already completed;
// GOFASTR_SETUP=force does not re-run steps" error) while rewording the
// banner and first-run.md. Delete or promote per maintainer decision.
// ---------------------------------------------------------------------------
func TestSetupForceRedMatchesBanner(t *testing.T) {
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
	// banner accepts the operator's POST.
	t.Run("wizard submit", func(t *testing.T) {
		t.Setenv("GOFASTR_SETUP", "force")
		var runs atomic.Int32
		h := newRunner(&runs).Handler(func() {}, nil, nil)

		req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader("probe=1"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		ran := runs.Load() > 0
		refused := rec.Code >= 400 || strings.Contains(rec.Body.String(), "Step failed")
		if !ran && !refused {
			t.Errorf("CONTRACT: [setup-force] POST /setup under GOFASTR_SETUP=force on a completed install returned %d with the step's Run never invoked and no refusal rendered "+
				"(handler.go:265 skips step.Run when Complete=true, while render.go:80 shows \"Re-running setup steps\" and first-run.md tells operators to use force to re-enter the wizard): "+
				"the banner promises a re-run and the engine silently no-ops it. Either honor force as the explicit re-run escape hatch or refuse out loud (and reword the banner).",
				rec.Code)
		}
	})

	// Headless skin: same question without the browser.
	t.Run("headless RunSteps", func(t *testing.T) {
		t.Setenv("GOFASTR_SETUP", "force")
		var runs atomic.Int32
		r := newRunner(&runs)

		err := r.RunSteps(context.Background())
		if runs.Load() == 0 && err == nil {
			t.Errorf("CONTRACT: [setup-force] RunSteps under GOFASTR_SETUP=force on a completed install returned nil having run zero steps (setup.go:179-181): " +
				"the documented rescue mode (first-run.md:156) either re-executes the steps or reports its refusal — a silent no-op leaves an operator believing bootstrap was re-run.")
		}
	})
}
