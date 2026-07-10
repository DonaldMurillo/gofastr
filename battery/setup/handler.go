package setup

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

// Handler implements framework.SetupRunner.Handler. It returns the
// interactive setup surface: the wizard + /healthz + /readyz; every other
// path returns 503 with a short "setup required" body.
//
// The returned handler owns:
//   - Token exchange: GET /setup?token=<t> sets a cookie and redirects
//     to /setup; wrong/missing token → 403.
//   - Wizard navigation: GET /setup renders the current step; POST /setup
//     validates, runs the step, and advances or re-renders on error.
//   - Atomic exit: when the final step succeeds, swap() is called to
//     switch to the real app handler. Step execution is serialized and
//     Complete is re-checked under the mutex so two racing submissions
//     can't both run the steps.
func (r *Runner) Handler(swap func(), healthz, readyz http.HandlerFunc) http.Handler {
	r.mu.Lock()
	r.swap = swap
	r.currentStep = 0
	if r.tokenEnabled {
		tok, err := generateToken()
		if err != nil {
			r.token = ""
			r.cookieSecret = ""
		} else {
			r.token = tok
			// cookieSecret backs the cookie; it survives the
			// token's single-use invalidation so the already-
			// issued cookie stays valid for the session.
			r.cookieSecret = tok
		}
	}
	r.mu.Unlock()

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.serve(w, req, healthz, readyz)
	})
}

// serve dispatches the setup surface.
func (r *Runner) serve(w http.ResponseWriter, req *http.Request, healthz, readyz http.HandlerFunc) {
	path := req.URL.Path

	// Health endpoints always pass through — orchestrators need them
	// during setup to know the process is alive.
	if path == "/healthz" && healthz != nil {
		healthz(w, req)
		return
	}
	if path == "/readyz" && readyz != nil {
		readyz(w, req)
		return
	}

	// CSS for the wizard page.
	if path == "/__setup/style.css" {
		r.serveCSS(w)
		return
	}
	// Token exchange: ?token=<t> on ANY path sets the cookie and
	// redirects to /setup. Must be checked before the path switch so
	// /setup?token= also works.
	if tok := req.URL.Query().Get("token"); tok != "" && r.tokenEnabled {
		r.handleTokenExchange(w, req, tok)
		return
	}

	switch path {
	case "/setup":
		r.handleSetup(w, req)
	default:
		r.write503(w)
	}
}

// handleTokenExchange validates the one-time token, sets the cookie, and
// redirects to /setup. The token is single-use: on the first successful
// exchange it is atomically invalidated so a token leaked to access/proxy
// logs can't be replayed. The cookie (backed by cookieSecret) survives.
func (r *Runner) handleTokenExchange(w http.ResponseWriter, req *http.Request, tok string) {
	r.mu.Lock()
	if !r.tokenEnabled || r.token == "" {
		r.mu.Unlock()
		http.Error(w, "forbidden: setup token already used or expired. Continue in your original browser session, or restart the app to mint a fresh token.", http.StatusForbidden)
		return
	}
	if !tokenEqual(tok, r.token) {
		r.mu.Unlock()
		http.Error(w, "forbidden: invalid or expired setup token. Check the server startup log for the setup URL.", http.StatusForbidden)
		return
	}
	// First successful exchange: invalidate the token (single-use).
	r.token = ""
	cookieVal := r.cookieSecret
	r.mu.Unlock()

	setSetupCookie(w, req, cookieVal)
	http.Redirect(w, req, "/setup", http.StatusSeeOther)
}

// handleSetup processes GET (render) and POST (submit) for the wizard.
func (r *Runner) handleSetup(w http.ResponseWriter, req *http.Request) {
	// Auth gate: if token is enabled, require the cookie.
	if r.tokenEnabled {
		if !hasSetupCookie(req, r.cookieSecret) {
			http.Error(w, "forbidden: setup token required. Check the server startup log for the setup URL.", http.StatusForbidden)
			return
		}
	}

	// Cheap re-check: if setup already completed AND not in force
	// (rescue) mode, show a done page. completeAndSwap also fires the
	// (idempotent) swap so this branch is self-healing: if completion
	// was reached without the swap firing — a transient Complete error
	// on the final POST, or completion via another path — any later
	// request brings the app up instead of leaving it 503 forever.
	force := isForceMode()
	done, err := r.completeAndSwap(req.Context())
	if err == nil && done && !force {
		r.renderCompletionPage(w, req)
		return
	}

	switch req.Method {
	case http.MethodGet:
		r.renderStep(w, req)
	case http.MethodPost:
		// CSRF: reject cross-site form posts (login-CSRF convention).
		if rejectCrossSiteForm(w, req) {
			return
		}
		r.handleSubmit(w, req)
	default:
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleSubmit validates the current step's fields, runs it, and either
// advances or re-renders with errors.
func (r *Runner) handleSubmit(w http.ResponseWriter, req *http.Request) {
	if err := req.ParseForm(); err != nil {
		http.Error(w, "bad form data", http.StatusBadRequest)
		return
	}

	r.mu.Lock()
	stepIdx := r.currentStep
	steps := r.cfg.Steps
	r.mu.Unlock()

	if stepIdx >= len(steps) {
		http.Redirect(w, req, "/setup", http.StatusSeeOther)
		return
	}
	step := steps[stepIdx]

	// Collect submitted values.
	values := make(map[string]string, len(step.Fields))
	for _, f := range step.Fields {
		values[f.Name] = strings.TrimSpace(req.FormValue(f.Name))
	}

	// Validate.
	fieldErrors := make(map[string]string)
	for _, f := range step.Fields {
		if f.Validate != nil {
			if err := f.Validate(values[f.Name]); err != nil {
				fieldErrors[f.Name] = err.Error()
			}
		}
	}
	if len(fieldErrors) > 0 {
		r.renderStepWithErrors(w, req, stepIdx, fieldErrors)
		return
	}

	// Run the step under the mutex and advance currentStep in the same
	// critical section so a racing POST on an intermediate step can't
	// re-run it between the run and the advance.
	if err := r.runStepSerialized(req.Context(), stepIdx, step, values); err != nil {
		r.renderStepWithErrors(w, req, stepIdx, map[string]string{"_step": err.Error()})
		return
	}

	// runStepSerialized advanced currentStep atomically; read the new
	// value to decide whether this was the final step.
	r.mu.Lock()
	nextStep := r.currentStep
	r.mu.Unlock()

	if nextStep >= len(steps) {
		// All steps done — the completion page is rendered ONLY when the
		// Complete predicate confirms it (and the swap has fired).
		// Claiming "your application is ready" while every route still
		// answers 503 would strand the operator on a lie.
		done, err := r.completeAndSwap(req.Context())
		switch {
		case err != nil:
			r.renderIncomplete(w, "Every step ran, but the completion check failed: "+err.Error()+
				". Fix the underlying issue and reload this page — setup finishes automatically once the check passes.")
		case !done:
			r.renderIncomplete(w, "Every step ran, but setup still reports incomplete. "+
				"The Complete predicate does not observe the steps' writes — e.g. AdminStep configured "+
				"with a different users table than the auth store writes to. Fix the wiring and restart.")
		default:
			r.renderCompletionPage(w, req)
		}
		return
	}

	// PRG: redirect to GET /setup to show the next step.
	http.Redirect(w, req, "/setup", http.StatusSeeOther)
}

// runStepSerialized runs one step under the mutex and advances
// currentStep in the same critical section. Checking currentStep ==
// stepIdx under the lock before running (and advancing before
// releasing) eliminates the window in which a concurrent POST could
// re-run an intermediate step.
func (r *Runner) runStepSerialized(ctx context.Context, stepIdx int, step Step, values map[string]string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Re-check: if a concurrent request already advanced past this
	// step, skip (treat as already-advanced).
	if r.currentStep != stepIdx {
		return nil
	}

	// Re-check Complete: if setup already finished (concurrent path),
	// don't re-run.
	done, err := r.cfg.Complete(ctx)
	if err == nil && done {
		return nil
	}

	if err := step.Run(ctx, values); err != nil {
		return err
	}

	// Advance under the same lock — no window between run and advance.
	r.currentStep = stepIdx + 1
	return nil
}

// write503 emits the "setup required" body for any non-setup path.
func (r *Runner) write503(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Retry-After", "60")
	w.WriteHeader(http.StatusServiceUnavailable)
	fmt.Fprintln(w, "Service is in first-run setup mode.")
	fmt.Fprintln(w, "Complete setup at /setup to enable this service.")
}

// completeAndSwap re-checks the Complete predicate and fires the swap
// (idempotent on the framework side) when it reports true. Every path
// that might observe completion routes through here, so completion
// reached on ANY request — not just the final POST — brings the app up.
func (r *Runner) completeAndSwap(ctx context.Context) (bool, error) {
	done, err := r.cfg.Complete(ctx)
	if err == nil && done && r.swap != nil {
		r.swap()
	}
	return done, err
}
