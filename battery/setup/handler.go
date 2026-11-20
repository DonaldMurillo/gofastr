package setup

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

const maxSetupBodyBytes int64 = 1 << 20

// Handler implements framework.SetupRunner.Handler.
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
			r.cookieSecret = ""
		}
	}
	r.mu.Unlock()

	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.serve(w, req, healthz, readyz)
	})
}

func (r *Runner) serve(w http.ResponseWriter, req *http.Request, healthz, readyz http.HandlerFunc) {
	path := req.URL.Path

	if path == "/healthz" && healthz != nil {
		healthz(w, req)
		return
	}
	if path == "/readyz" && readyz != nil {
		readyz(w, req)
		return
	}

	if path == "/__setup/style.css" {
		r.serveCSS(w)
		return
	}
	if tok := req.URL.Query().Get("token"); tok != "" && r.tokenEnabled {
		r.handleTokenExchange(w, req, tok)
		return
	}

	switch path {
	case "/setup":
		r.handleSetup(w, req)
	default:
		if r.recheckElsewhere(req.Context()) {
			w.Header().Set("Cache-Control", "no-store")
			loc := "/" + strings.TrimLeft(req.URL.EscapedPath(), "/\\")
			if req.URL.RawQuery != "" {
				loc += "?" + req.URL.RawQuery
			}
			http.Redirect(w, req, textsafe.ScrubControlBytes(loc), http.StatusTemporaryRedirect)
			return
		}
		r.write503(w, req)
	}
}

func (r *Runner) handleTokenExchange(w http.ResponseWriter, req *http.Request, tok string) {
	if (req.Method == http.MethodGet || req.Method == http.MethodHead) && handler.NeedsSameSiteHop(req) {
		if target, ok := sameOriginTarget(req); ok && topLevelNavigation(req) {
			r.renderHop(w, req, handler.SameSiteHopTarget(req, target))
			return
		}
		i18n.HTTPError(w, req, http.StatusForbidden, i18n.StatusPhrase(http.StatusForbidden))
		return
	}
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

func (r *Runner) handleSetup(w http.ResponseWriter, req *http.Request) {
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

	if _, finished, err := r.completeAndSwap(req.Context()); err == nil && finished {
		r.renderCompletionPage(w, req)
		return
	}

	switch req.Method {
	case http.MethodGet:
		r.renderStep(w, req)
	case http.MethodPost:
		if rejectCrossSiteForm(w, req) {
			return
		}
		r.handleSubmit(w, req)
	default:
		w.Header().Set("Allow", "GET, POST")
		i18n.HTTPError(w, req, http.StatusMethodNotAllowed, i18n.StatusPhrase(http.StatusMethodNotAllowed))
	}
}

func (r *Runner) handleSubmit(w http.ResponseWriter, req *http.Request) {
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

	values := make(map[string]string, len(step.Fields))
	for _, f := range step.Fields {
		v := req.FormValue(f.Name)
		if !f.Secret {
			v = strings.TrimSpace(v)
		}
		values[f.Name] = v
	}

	fieldErrors := make(map[string]string)
	for _, f := range step.Fields {
		if f.Validate != nil {
			if err := f.Validate(values[f.Name]); err != nil {
				fieldErrors[f.Name], _ = i18n.ErrorText(req.Context(), err, i18n.Phrase{})
			}
		}
	}
	if len(fieldErrors) > 0 {
		r.renderStepWithErrors(w, req, stepIdx, fieldErrors, values)
		return
	}

	if err := r.runStepSerialized(req.Context(), stepIdx, step, values); err != nil {
		msg, _ := i18n.ErrorText(req.Context(), err, i18n.Phrase{})
		r.renderStepWithErrors(w, req, stepIdx, map[string]string{"_step": msg}, values)
		return
	}

	r.mu.Lock()
	nextStep := r.currentStep
	r.mu.Unlock()

	if nextStep >= len(steps) {
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

	http.Redirect(w, req, "/setup", http.StatusSeeOther)
}

func (r *Runner) runStepSerialized(ctx context.Context, stepIdx int, step Step, values map[string]string) error {
	done, err := r.cfg.Complete(ctx)
	if err != nil {
		return fmt.Errorf("setup: completion check failed, refusing to run step: %w", err)
	}
	if done && stepIdx == 0 && !isForceMode() {
		return nil
	}

	r.mu.Lock()
	if r.currentStep != stepIdx {
		r.mu.Unlock()
		return nil
	}
	if r.stepInFlight {
		r.mu.Unlock()
		return nil
	}

	r.stepInFlight = true
	r.mu.Unlock()

	defer func() {
		r.mu.Lock()
		r.stepInFlight = false
		r.mu.Unlock()
	}()

	err = step.Run(ctx, values)
	if err != nil {
		return err
	}

	r.mu.Lock()
	if r.currentStep == stepIdx {
		r.currentStep = stepIdx + 1
	}
	r.mu.Unlock()
	return nil
}

func (r *Runner) write503(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Retry-After", "60")
	w.WriteHeader(http.StatusServiceUnavailable)
	fmt.Fprintln(w, pageUnavailable.Text(req.Context()))
	fmt.Fprintln(w, pageUnavailableHelp.Text(req.Context()))
}

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

func (r *Runner) wizardFinished() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.currentStep >= len(r.cfg.Steps) {
		return true
	}
	return r.currentStep == 0 && !r.stepInFlight && !isForceMode()
}

const completeRecheckInterval = time.Second

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

func sameOriginTarget(req *http.Request) (string, bool) {
	path := req.URL.EscapedPath()
	if !handler.IsSafeRelativePath(path) {
		return "", false
	}
	if req.URL.RawQuery != "" {
		path += "?" + req.URL.RawQuery
	}
	return textsafe.ScrubControlBytes(path), true
}

func topLevelNavigation(req *http.Request) bool {
	dest := req.Header.Get("Sec-Fetch-Dest")
	return dest == "" || dest == "document"
}
