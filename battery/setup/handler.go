package setup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

// maxSetupBodyBytes caps the wizard's urlencoded form bodies. Matches the
// repo's form convention (battery/auth form_decode.go and the CSRF
// middleware's defaultCSRFMaxFormBytes, both 1 MiB) instead of the stdlib's
// 10 MiB urlencoded floor: /setup can be fully unauthenticated (DisableToken),
// so one request must not be able to park megabytes in process memory.
const maxSetupBodyBytes int64 = 1 << 20

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
//     Complete is re-checked on every claim so two racing submissions
//     can't both run the steps.
//   - Completion elsewhere: an app path, or /setup without the setup
//     cookie, re-checks Complete at most once per
//     completeRecheckInterval and swaps when setup finished on another
//     replica or by another path.
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
			// cookieSecret stays EMPTY until an exchange mints one.
			// It used to be the URL token itself, which made the
			// single-use promise hollow: the token was invalidated in
			// its URL form and then handed straight back as the cookie
			// value, so anyone who had only ever seen the URL — a proxy
			// or access log — replayed it as Cookie: gofastr_setup=<tok>
			// from a second client and got the wizard for the life of
			// the process.
			r.cookieSecret = ""
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

	// Health endpoints always pass through, orchestrators need them
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
		// A replica that did not run the wizard learns of completion
		// here: the operator finished on another replica, and this one
		// would otherwise answer 503 forever. The swapped-in router
		// cannot serve this request (it is already inside the setup
		// handler), so send the client straight back: the retry
		// reaches the app. 307 keeps the method and body. The path's
		// leading slashes collapse to one: "//host/x" echoed back is a
		// scheme-relative Location naming another site.
		if r.recheckElsewhere(req.Context()) {
			w.Header().Set("Cache-Control", "no-store")
			loc := "/" + strings.TrimLeft(req.URL.EscapedPath(), "/\\")
			if req.URL.RawQuery != "" {
				loc += "?" + req.URL.RawQuery
			}
			http.Redirect(w, req, textsafe.ScrubControlBytes(loc), http.StatusTemporaryRedirect)
			return
		}
		r.write503(w)
	}
}

// handleTokenExchange validates the one-time token, sets the cookie, and
// redirects to /setup. The token is single-use: on the first successful
// exchange it is atomically invalidated so a token leaked to access/proxy
// logs can't be replayed -- including as a cookie, which is why the
// cookie carries a freshly minted secret rather than the token itself.
func (r *Runner) handleTokenExchange(w http.ResponseWriter, req *http.Request, tok string) {
	r.mu.Lock()
	if !r.tokenEnabled || r.token == "" {
		r.mu.Unlock()
		i18n.HTTPError(w, req, http.StatusForbidden, reasonSetupTokenUsed)
		return
	}
	if !tokenEqual(tok, r.token) {
		r.mu.Unlock()
		i18n.HTTPError(w, req, http.StatusForbidden, reasonSetupTokenInvalid)
		return
	}
	// First successful exchange: invalidate the token (single-use) and
	// mint an INDEPENDENT cookie secret. The exchanging browser is the
	// only holder of that value, so a leaked URL cannot become a
	// session.
	cookieVal, gerr := generateToken()
	if gerr != nil {
		r.mu.Unlock()
		i18n.HTTPError(w, req, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	r.token = ""
	r.cookieSecret = cookieVal
	r.mu.Unlock()

	setSetupCookie(w, req, cookieVal)
	http.Redirect(w, req, "/setup", http.StatusSeeOther)
}

// handleSetup processes GET (render) and POST (submit) for the wizard.
func (r *Runner) handleSetup(w http.ResponseWriter, req *http.Request) {
	// Auth gate: if token is enabled, require the cookie. A visitor
	// without one still learns that setup finished elsewhere (another
	// replica ran the wizard and this one never issued a cookie): the
	// same rate-limited re-check the app paths use.
	if r.tokenEnabled {
		if !hasSetupCookie(req, r.cookieSecret) {
			if r.recheckElsewhere(req.Context()) {
				r.renderCompletionPage(w, req)
				return
			}
			i18n.HTTPError(w, req, http.StatusForbidden, reasonSetupTokenRequired)
			return
		}
	}

	// Cheap re-check: when the wizard is over (see wizardFinished),
	// show the done page. completeAndSwap also fires the (idempotent)
	// swap so this branch is self-healing: if completion was reached
	// without the swap firing, a transient Complete error on the final
	// POST, or completion via another path, any later request brings
	// the app up instead of leaving it 503 forever. A wizard still in
	// progress, force mode's included, keeps rendering its steps: a
	// swap here would hand the operator's next POST to the real router.
	if _, finished, err := r.completeAndSwap(req.Context()); err == nil && finished {
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
		i18n.HTTPError(w, req, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
	}
}

// handleSubmit validates the current step's fields, runs it, and either
// advances or re-renders with errors.
func (r *Runner) handleSubmit(w http.ResponseWriter, req *http.Request) {
	// Cap the body BEFORE the parse (the auth battery's form_decode
	// spelling): without it the only bound is the stdlib's 10 MiB
	// urlencoded floor, and /setup can be an unauthenticated surface.
	req.Body = http.MaxBytesReader(w, req.Body, maxSetupBodyBytes)
	if err := req.ParseForm(); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			i18n.HTTPError(w, req, http.StatusRequestEntityTooLarge, i18n.StatusPhrase(http.StatusRequestEntityTooLarge))
			return
		}
		i18n.HTTPError(w, req, http.StatusBadRequest, i18n.ReasonInvalidForm)
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
				fieldErrors[f.Name], _ = i18n.ErrorText(req.Context(), err, i18n.Phrase{})
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
		msg, _ := i18n.ErrorText(req.Context(), err, i18n.Phrase{})
		r.renderStepWithErrors(w, req, stepIdx, map[string]string{"_step": msg})
		return
	}

	// runStepSerialized advanced currentStep atomically; read the new
	// value to decide whether this was the final step.
	r.mu.Lock()
	nextStep := r.currentStep
	r.mu.Unlock()

	if nextStep >= len(steps) {
		// All steps done, the completion page is rendered ONLY when the
		// Complete predicate confirms it (and the swap has fired).
		// Claiming "your application is ready" while every route still
		// answers 503 would strand the operator on a lie.
		done, _, err := r.completeAndSwap(req.Context())
		switch {
		case err != nil:
			r.renderIncomplete(req.Context(), w, pageCheckFailed.Text(req.Context(), map[string]any{"cause": err.Error()}))
		case !done:
			r.renderIncomplete(req.Context(), w, pageIncompleteWiring.Text(req.Context()))
		default:
			r.renderCompletionPage(w, req)
		}
		return
	}

	// PRG: redirect to GET /setup to show the next step.
	http.Redirect(w, req, "/setup", http.StatusSeeOther)
}

// runStepSerialized claims one step under the mutex and runs it
// OUTSIDE it. Steps are app-supplied callback code (AdminStep creates
// the admin account over HTTP-round-tripped stores): a blocking or
// panicking step must not wedge every other setup request against
// r.mu (callbackunderlock pins this), and a panic must reach the
// net/http recover net rather than skip a plain Unlock.
//
// The Complete re-check ALSO runs outside the mutex, for the same
// reason: Complete is app-supplied callback code too. It used to fire
// while r.mu was held with no deferred unlock registered yet, so a
// panicking Complete left the mutex locked and wedged every later
// setup request; a slow one blocked them all. Exactly-once survives
// the probe leaving the lock through stepInFlight: the claim
// (currentStep check + stepInFlight check + flag) and the advance are
// each atomic under mu, and a concurrent POST for the same step finds
// the flag set and treats the step as already-advanced. A FAILED step
// clears the flag and leaves currentStep where it was, so the retry
// path is unchanged.
func (r *Runner) runStepSerialized(ctx context.Context, stepIdx int, step Step, values map[string]string) error {
	// Re-check Complete before taking the mutex. GOFASTR_SETUP=force is
	// the operator's explicit opt-in to re-run steps on a completed
	// install (first-run.md): a completed step is skipped only when NOT
	// in force mode, so the wizard's "re-running setup steps" banner
	// never promises a re-run the engine silently refuses.
	//
	// A probe ERROR is not "not done", it is "unknown", and this guard is
	// the only thing standing between a second caller and a re-run of a
	// step that typically creates the admin account. Refuse rather than
	// proceed — under force too: the operator opted into re-running
	// steps, not into running them against a state that cannot be read.
	// A failed setup step is recoverable, a silently re-run one is not.
	//
	// Only the FIRST step is skipped on a completed install. A later
	// step's claim means this process already ran the steps before it,
	// one of which may be what flipped Complete (AdminStep does), and
	// the documented [adminStep, HealthStep(app)] wiring must still run
	// HealthStep.
	done, err := r.cfg.Complete(ctx)
	if err != nil {
		return fmt.Errorf("setup: completion check failed, refusing to run step: %w", err)
	}
	if done && stepIdx == 0 && !isForceMode() {
		return nil
	}

	r.mu.Lock()
	// Re-check: if a concurrent request already advanced past this
	// step, skip (treat as already-advanced).
	if r.currentStep != stepIdx {
		r.mu.Unlock()
		return nil
	}
	// Another request is running this step right now (it released the
	// mutex to do so): treat as already-advanced.
	if r.stepInFlight {
		r.mu.Unlock()
		return nil
	}

	r.stepInFlight = true
	r.mu.Unlock()

	// The deferred clear also runs when the step PANICS: the claim is
	// released while the panic unwinds to the net/http recover net,
	// exactly as it did before the step left the lock, so the wizard
	// is never stuck treating the step as in-flight.
	defer func() {
		r.mu.Lock()
		r.stepInFlight = false
		r.mu.Unlock()
	}()

	// Run outside the lock: a slow or panicking step holds nothing.
	err = step.Run(ctx, values)
	if err != nil {
		return err
	}

	// Advance under the lock, no window between run and advance: only
	// the in-flight runner reaches here (the flag kept every other
	// request out), and the currentStep guard keeps a test-driven
	// reset from double-advancing.
	r.mu.Lock()
	if r.currentStep == stepIdx {
		r.currentStep = stepIdx + 1
	}
	r.mu.Unlock()
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
// (idempotent on the framework side) when the wizard is finished: done
// is the predicate's answer, finished is whether the swap fired. Every
// path that might observe completion routes through here, so completion
// reached on ANY request, not only the final POST, brings the app up.
func (r *Runner) completeAndSwap(ctx context.Context) (done, finished bool, err error) {
	done, err = r.cfg.Complete(ctx)
	if err != nil || !done || !r.wizardFinished() {
		return done, false, err
	}
	if r.swap != nil {
		r.swap()
	}
	return true, true, nil
}

// wizardFinished reports whether, with Complete reporting true, the
// wizard is over in this process:
//
//   - every declared step has run here, or
//   - no step has run (or is running) here and force mode is off:
//     setup finished before this boot or on another replica.
//
// A wizard that has run some of its steps is not finished just because
// Complete flipped: AdminStep flips it, and the steps declared after it
// (HealthStep in the documented wiring) still run. Force mode on a
// completed install is not finished until its steps have re-run.
func (r *Runner) wizardFinished() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.currentStep >= len(r.cfg.Steps) {
		return true
	}
	return r.currentStep == 0 && !r.stepInFlight && !isForceMode()
}

// completeRecheckInterval bounds how often requests without the setup
// cookie probe Complete. They are the requests a 503 storm is made of
// (load balancer traffic, clients retrying), so each may not cost a
// Complete query; one per interval answers "did another replica finish
// setup?" fast enough for an operator.
const completeRecheckInterval = time.Second

// recheckElsewhere probes Complete at most once per
// completeRecheckInterval across all callers and fires the swap when
// the wizard is finished (see wizardFinished). It reports whether it
// swapped. A caller inside the interval, or one that loses the race to
// claim it, skips the probe and reports false.
func (r *Runner) recheckElsewhere(ctx context.Context) bool {
	clock := time.Now
	if r.now != nil {
		clock = r.now
	}
	now := clock().UnixNano()
	last := r.lastRecheck.Load()
	if last != 0 && now-last < int64(completeRecheckInterval) {
		return false
	}
	if !r.lastRecheck.CompareAndSwap(last, now) {
		return false
	}
	_, finished, err := r.completeAndSwap(ctx)
	return err == nil && finished
}
