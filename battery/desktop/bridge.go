package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"mime"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/internal/bodylimit"
)

const (
	bridgePrefix   = "/__gofastr/desktop"
	callPattern    = bridgePrefix + "/call/{cap}/{method}"
	manifestPath   = bridgePrefix + "/manifest.json"
	bridgeScriptFn = bridgePrefix + "/bridge.js"
)

// maxCallBody caps a bridge call body at the repo's
// bodylimit.MaxJSONBodyBytes (the unboundedbody posture).
const maxCallBody = bodylimit.MaxJSONBodyBytes

const callBudget = 11 * time.Minute

func (b *Battery) registerBridgeRoutes(r *router.Router) {
	r.Get(enterPath, b.enterHandler())
	r.Get(manifestPath, b.serveManifest())
	r.Get(bridgeScriptFn, b.serveBridgeScript())
	r.Post(callPattern, http.HandlerFunc(b.handleCall))
	r.SetRouteTimeout(http.MethodPost, callPattern, callBudget)
	r.Post(preferencesFormPath, b.handlePreferencesForm())
}

const windowHeader = "X-Gofastr-Window"

var reWindowID = regexp.MustCompile(`^[a-z][a-z0-9]{0,15}$`)

type windowIDCtxKey struct{}

func windowIDFromRequest(r *http.Request) string {
	if id := r.Header.Get(windowHeader); reWindowID.MatchString(id) {
		return id
	}
	return MainWindowID
}

func httpStatusFor(code string) int {
	switch code {
	case CodeDenied:
		return http.StatusForbidden
	case CodeUnsupported:
		return http.StatusNotImplemented
	case CodeInvalidInput:
		return http.StatusBadRequest
	case CodeCancelled:
		return http.StatusConflict
	case CodeNotFound:
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}

//gofastr:allow(rawtext) the bridge answers the app's own JS: code is the stable Code and message developer detail, never shown to a user
func writeBridgeError(w http.ResponseWriter, code, message string) {
	writeBridgeJSON(w, httpStatusFor(code), map[string]any{
		"ok":    false,
		"error": map[string]any{"code": code, "message": message},
	})
}

func writeBridgeResult(w http.ResponseWriter, status int, v any) {
	writeBridgeJSON(w, status, v)
}

func writeBridgeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		body = []byte(`{"ok":false,"error":{"code":"internal","message":"internal error"}}`)
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	w.Write(body)
}

func (b *Battery) bridgeError(capName, method string, err error) (code, message string) {
	var de *Error
	switch {
	case errors.As(err, &de):
		return de.Code, de.Message
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return CodeCancelled, ErrCancelled.Message
	default:
		b.logger.Error("desktop: bridge handler failed",
			"capability", capName, "method", method, "error", err)
		return CodeInternal, InternalErrorMsg
	}
}

var notFoundBody = []byte(`{"ok":false,"error":{"code":"not_found","message":"not found"}}`)

func writeNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	w.Write(notFoundBody)
}

func (b *Battery) handleCall(w http.ResponseWriter, r *http.Request) {
	if !b.reg.isFrozen() {
		writeNotFound(w)
		return
	}

	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusUnsupportedMediaType)
		//gofastr:allow(rawtext) the bridge answers the app's own JS: code is the stable Code and message developer detail, never shown to a user
		io.WriteString(w, `{"ok":false,"error":{"code":"invalid_input","message":"content-type must be application/json"}}`)
		return
	}

	if handler.IsCrossSiteRequestStrict(r) {
		writeBridgeError(w, CodeDenied, "cross-site requests are not allowed")
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxCallBody))
	if err != nil {
		writeBridgeError(w, CodeInvalidInput, "request body too large or unreadable")
		return
	}

	capName := router.Param(r, "cap")
	methodName := router.Param(r, "method")
	cap, ok := b.reg.lookup(capName)
	if !ok {
		writeNotFound(w)
		return
	}
	var method Method
	found := false
	for _, m := range cap.Methods {
		if m.Name == methodName {
			method, found = m, true
			break
		}
	}
	if !found {
		writeNotFound(w)
		return
	}

	input := json.RawMessage(`{}`)
	if len(body) > 0 {
		var probe map[string]json.RawMessage
		if err := handler.UnmarshalStrict(body, &probe); err != nil ||
			!json.Valid(body) {
			writeBridgeError(w, CodeInvalidInput, "request body must be a JSON object")
			return
		}
		if probe == nil {
			writeBridgeError(w, CodeInvalidInput, "request body must be a JSON object")
			return
		}
		if bodyHasNUL(body) {
			writeBridgeError(w, CodeInvalidInput, "request body must not contain a NUL character")
			return
		}
		input = json.RawMessage(body)
	}

	_ = http.NewResponseController(w).SetWriteDeadline(time.Now().Add(callBudget))

	if proceed, derr := b.authorizeCall(r.Context(), cap, method); !proceed {
		writeBridgeError(w, derr, bridgeDenyMessage(derr))
		return
	}

	result, err := method.Handler(context.WithValue(r.Context(), windowIDCtxKey{}, windowIDFromRequest(r)), input)
	if err != nil {
		code, msg := b.bridgeError(capName, methodName, err)
		writeBridgeError(w, code, msg)
		return
	}
	writeBridgeResult(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func bodyHasNUL(body []byte) bool {
	var v any
	//gofastr:allow(floatnum) decoded only to scan strings for NUL; the value is discarded
	if err := handler.UnmarshalStrict(body, &v); err != nil {
		return false
	}
	return valueHasNUL(v)
}

func valueHasNUL(v any) bool {
	switch t := v.(type) {
	case string:
		return strings.IndexByte(t, 0) >= 0
	case []any:
		for _, e := range t {
			if valueHasNUL(e) {
				return true
			}
		}
	case map[string]any:
		for _, k := range slices.Sorted(maps.Keys(t)) {
			if strings.IndexByte(k, 0) >= 0 || valueHasNUL(t[k]) {
				return true
			}
		}
	}
	return false
}

func bridgeDenyMessage(code string) string {
	switch code {
	case CodeDenied:
		return "permission denied"
	case CodeCancelled:
		return ErrCancelled.Message
	}
	return InternalErrorMsg
}

func (b *Battery) promptSlot() chan struct{} {
	b.promptOnce.Do(func() { b.promptSem = make(chan struct{}, 1) })
	return b.promptSem
}

const grantWriteTimeout = 5 * time.Second

func (b *Battery) authorizeCall(ctx context.Context, cap Capability, m Method) (bool, string) {
	if m.Permission == "" {
		return true, ""
	}
	if allowed, code, decided := b.lookupGrant(ctx, cap.Name, m.Permission); decided {
		return allowed, code
	}

	select {
	case b.promptSlot() <- struct{}{}:
	case <-ctx.Done():
		return false, CodeCancelled
	}
	defer func() { <-b.promptSlot() }()
	if allowed, code, decided := b.lookupGrant(ctx, cap.Name, m.Permission); decided {
		return allowed, code
	}

	d, perr := b.shell.Prompt(ctx, PermissionRequest{
		Capability:  cap.Name,
		Method:      m.Name,
		Permission:  m.Permission,
		Description: m.Description,
	})
	if perr != nil {
		b.logger.Error("desktop: permission prompt failed",
			"capability", cap.Name, "permission", m.Permission, "error", perr)
		return false, CodeInternal
	}
	switch d {
	case DecisionAllow:
		b.persistGrant(ctx, cap.Name, m.Permission, grantAllow)
		return true, ""
	case DecisionAllowOnce:
		return true, ""
	default:
		b.persistGrant(ctx, cap.Name, m.Permission, grantDeny)
		return false, CodeDenied
	}
}

func (b *Battery) lookupGrant(ctx context.Context, capability, permission string) (allowed bool, code string, decided bool) {
	decision, found, err := b.grants.Get(ctx, capability, permission)
	if err != nil {
		b.logger.Error("desktop: grant store read failed",
			"capability", capability, "permission", permission, "error", err)
		return false, CodeInternal, true
	}
	if !found {
		return false, "", false
	}
	return decision == grantAllow, denyCode(decision), true
}

func (b *Battery) persistGrant(ctx context.Context, capability, permission, decision string) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), grantWriteTimeout)
	defer cancel()
	if err := b.grants.Set(writeCtx, capability, permission, decision); err != nil {
		b.logger.Error("desktop: grant store write failed",
			"capability", capability, "permission", permission, "error", err)
	}
}

func denyCode(decision string) string {
	if decision == grantDeny {
		return CodeDenied
	}
	if decision == grantAllow {
		return ""
	}
	return CodeDenied
}

func (b *Battery) serveManifest() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, err := b.frozenManifest()
		if err != nil {
			retryShortly(w)
			return
		}
		body, merr := json.Marshal(m)
		if merr != nil {
			writeBridgeError(w, CodeInternal, InternalErrorMsg)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	})
}

func retryShortly(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Retry-After", "1")
	w.WriteHeader(http.StatusServiceUnavailable)
	io.WriteString(w, "desktop bridge is starting")
}

func (b *Battery) frozenManifest() (Manifest, error) {
	b.manifestMu.Lock()
	defer b.manifestMu.Unlock()
	if b.manifest == nil {
		if !b.reg.isFrozen() {
			return Manifest{}, errors.New("desktop: manifest requested before the registry froze")
		}
		m := b.reg.build()
		b.manifest = &m
	}
	return *b.manifest, nil
}

const bridgeInactiveJS = "// desktop bridge: the host is not running in this process\n"

type bridgeScript struct {
	once sync.Once
	body []byte
	err  error
}

func (b *Battery) bridgeScriptURL() string {
	return bridgeScriptFn + "?v=" + b.scriptNonce
}

func (b *Battery) serveBridgeScript() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !b.reg.isFrozen() {
			w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusOK)
			io.WriteString(w, bridgeInactiveJS)
			return
		}
		b.script.once.Do(func() {
			m, err := b.frozenManifest()
			if err != nil {
				b.script.err = err
				return
			}
			b.script.body = BridgeJS(m)
		})
		if b.script.err != nil {
			retryShortly(w)
			return
		}
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		w.Write(b.script.body)
	})
}

type atomicString struct {
	v atomic.Value
}

func (a *atomicString) Load() string {
	s, _ := a.v.Load().(string)
	return s
}

func (a *atomicString) Store(s string) { a.v.Store(s) }
