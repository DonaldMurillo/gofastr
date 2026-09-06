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
)

// Bridge routes. Every native capability call from the page funnels
// through ONE POST endpoint, so the permission check has one
// chokepoint and the transport is identical on every OS.
const (
	bridgePrefix   = "/__gofastr/desktop"
	callPattern    = bridgePrefix + "/call/{cap}/{method}"
	manifestPath   = bridgePrefix + "/manifest.json"
	bridgeScriptFn = bridgePrefix + "/bridge.js"
)

// maxCallBody caps a bridge call body (the unboundedbody posture).
const maxCallBody = 1 << 20

// registerBridgeRoutes wires the four desktop routes onto the app
// router. Called from Init.
func (b *Battery) registerBridgeRoutes(r *router.Router) {
	r.Get(enterPath, b.enterHandler())
	r.Get(manifestPath, b.serveManifest())
	r.Get(bridgeScriptFn, b.serveBridgeScript())
	r.Post(callPattern, http.HandlerFunc(b.handleCall))
}

// windowHeader carries the calling window's id from the page to the
// bridge. The desktop runtime module sets it from the host marker's
// window field on every call.
const windowHeader = "X-Gofastr-Window"

// reWindowID is the grammar a page may claim through windowHeader:
// a lowercase letter then lowercase letters or digits, at most 16
// characters. Every id the battery mints (main, settings, w2..w17)
// matches it.
var reWindowID = regexp.MustCompile(`^[a-z][a-z0-9]{0,15}$`)

// windowIDCtxKey carries the caller's window id inside a handler
// context.
type windowIDCtxKey struct{}

// windowIDFromRequest reads the calling window's id out of the
// X-Gofastr-Window header. It is a CLAIM by the page, not an identity:
// the server cannot tell which window a request came from, so the
// value only selects which window is "the caller" for
// windows.broadcast exclusion and windows.self. A value that is
// absent or off the grammar reads as "main".
func windowIDFromRequest(r *http.Request) string {
	if id := r.Header.Get(windowHeader); reWindowID.MatchString(id) {
		return id
	}
	return mainWindowID
}

// httpStatusFor maps an error code to its HTTP status.
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

// writeBridgeError writes the fixed-shape error envelope.
func writeBridgeError(w http.ResponseWriter, code, message string) {
	writeBridgeJSON(w, httpStatusFor(code), map[string]any{
		"ok":    false,
		"error": map[string]any{"code": code, "message": message},
	})
}

// writeBridgeResult writes the success envelope.
func writeBridgeResult(w http.ResponseWriter, status int, v any) {
	writeBridgeJSON(w, status, v)
}

func writeBridgeJSON(w http.ResponseWriter, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		// Marshaling a handler result is the server's own code path;
		// fall back to the fixed internal envelope rather than an
		// empty 200.
		body = []byte(`{"ok":false,"error":{"code":"internal","message":"internal error"}}`)
		status = http.StatusInternalServerError
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	w.Write(body)
}

// bridgeError converts a handler error into the envelope pair the
// chokepoint serves. Everything that is not a *Error or a context
// cancellation becomes "internal error" on the wire; the real error is
// logged with the capability and method and never reaches the body.
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
		return CodeInternal, internalErrorMsg
	}
}

// notFoundBody is byte-identical for a missing capability and a
// missing method: the 404 must not reveal which half was absent.
var notFoundBody = []byte(`{"ok":false,"error":{"code":"not_found","message":"not found"}}`)

func writeNotFound(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNotFound)
	w.Write(notFoundBody)
}

// handleCall is the chokepoint. Order: host running (404, uniform),
// content-type (415), fetch-site (403), body size (400), lookup (404,
// uniform), body shape (400), permission gate, handler.
func (b *Battery) handleCall(w http.ResponseWriter, r *http.Request) {
	// This is the ONLY bridge route that invokes native code, and it was
	// the one route without the "has the host started" guard its two
	// siblings carry (serveManifest 503s before the freeze,
	// serveBridgeScript serves inert JS). registerBridgeRoutes wires all
	// three in Init, so they answer from the moment the listener binds -
	// and in the documented --serve / --addr / $PORT mode Run never
	// happens, the gate is a pass-through, there is no boot cookie and no
	// Host pin, and a $PORT deployment binds a public interface. That put
	// the capability dispatcher, its permission prompts and its Shell in
	// reach of an unauthenticated caller.
	//
	// The refusal is the SAME uniform 404 a missing capability gets:
	// answering 403/501/200 for a registered capability and 404 for an
	// absent one enumerated the app's plugin inventory to anyone who
	// could reach the port.
	if !b.reg.isFrozen() {
		writeNotFound(w)
		return
	}

	// The router answers 405 for non-POST methods (only POST is
	// registered); this handler only ever sees POST.
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != "application/json" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusUnsupportedMediaType)
		io.WriteString(w, `{"ok":false,"error":{"code":"invalid_input","message":"content-type must be application/json"}}`)
		return
	}

	// A cross-origin form or simple request cannot carry this content
	// type without a preflight this handler never approves; the header
	// check closes the remaining window (sandboxed pages, redirects).
	switch r.Header.Get("Sec-Fetch-Site") {
	case "", "same-origin", "none":
	default:
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

	// Body must be a JSON object or empty. Strict decode refuses
	// duplicate and case-folded top-level keys.
	input := json.RawMessage(`{}`)
	if len(body) > 0 {
		var probe map[string]json.RawMessage
		if err := handler.UnmarshalStrict(body, &probe); err != nil ||
			!json.Valid(body) {
			writeBridgeError(w, CodeInvalidInput, "request body must be a JSON object")
			return
		}
		// "object or empty" has to be checked, not assumed: `null`
		// decodes into a NIL map with no error and json.Valid("null") is
		// true, so it would reach the handler as input and let every
		// "required" field take its zero value.
		if probe == nil {
			writeBridgeError(w, CodeInvalidInput, "request body must be a JSON object")
			return
		}
		// No page-supplied string may carry a NUL. internal/objc.NSString
		// is the single crossing into Objective-C and a NUL cannot cross
		// it (the C string would end there); the crossing runs inside an
		// ffi trampoline where a Go panic is fatal. NSString itself is
		// now fail-soft, but the input is wrong and the page must be told
		// so, and this is the one place every current AND future
		// capability, a plugin's included, passes through.
		if bodyHasNUL(body) {
			writeBridgeError(w, CodeInvalidInput, "request body must not contain a NUL character")
			return
		}
		input = json.RawMessage(body)
	}

	if proceed, derr := b.authorizeCall(r.Context(), cap, method); !proceed {
		writeBridgeError(w, derr, bridgeDenyMessage(derr))
		return
	}

	// The handler sees which window called it: the page's claim from
	// windowIDFromRequest, never verified (see its doc comment).
	result, err := method.Handler(context.WithValue(r.Context(), windowIDCtxKey{}, windowIDFromRequest(r)), input)
	if err != nil {
		code, msg := b.bridgeError(capName, methodName, err)
		writeBridgeError(w, code, msg)
		return
	}
	writeBridgeResult(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

// bodyHasNUL reports whether any string in a decoded JSON document
// carries a NUL.
//
// The walk runs over the DECODED values, not the raw bytes, and that is
// the whole point: a literal 0x00 is not legal inside a JSON string, so
// the attack always arrives as the six-character escape \u0000 and the
// request bytes are NUL-free. A byte scan of body would see nothing.
// Bounded by maxCallBody, so the extra pass is cheap.
func bodyHasNUL(body []byte) bool {
	var v any
	// The strict binder, not encoding/json: the top-level keys were
	// already checked, and GOFASTR1407 wants every body decode to go
	// through the same duplicate- and case-refusing path.
	if err := handler.UnmarshalStrict(body, &v); err != nil {
		// Already refused by the strict decode above.
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
		// Sorted so the walk is deterministic (the mapwriter posture).
		for _, k := range slices.Sorted(maps.Keys(t)) {
			if strings.IndexByte(k, 0) >= 0 || valueHasNUL(t[k]) {
				return true
			}
		}
	}
	return false
}

// bridgeDenyMessage is the fixed message for a refused permission.
func bridgeDenyMessage(code string) string {
	if code == CodeDenied {
		return "permission denied"
	}
	return internalErrorMsg
}

// grantWriteTimeout bounds the detached write that persists a decision.
const grantWriteTimeout = 5 * time.Second

// authorizeCall enforces the permission gate. Ungated methods proceed;
// otherwise the grant store decides, a miss prompts (serialized so two
// concurrent first calls show one dialog), and the decision persists
// for allow/deny but not allow-once.
//
// The grant is keyed on the CAPABILITY as well as the permission. The
// OS alert names the capability, the method and the permission, so the
// user is answering about a capability; keying on the permission string
// alone let a second capability declaring the same string inherit that
// consent with no prompt at all, silently, across app upgrades, since
// grants outlive the process.
//
// The DECISION is persisted on a context detached from the request.
// Go does not stop a handler when the client disconnects, it cancels
// r.Context(), and sqlGrantStore.Set is ExecContext: writing with the
// request context meant a page that aborted its fetch while the alert
// was on screen made the user's Deny unwritable. That defeated exactly
// the rule the deny is persisted FOR ("a page cannot re-prompt in a
// loop", grants.go and shell.go). The decision belongs to the user and
// the shell, not to the page's socket. The READS stay on the request
// context: a read that fails answers CodeInternal, which fails closed.
func (b *Battery) authorizeCall(ctx context.Context, cap Capability, m Method) (bool, string) {
	if m.Permission == "" {
		return true, ""
	}
	decision, found, err := b.grants.Get(ctx, cap.Name, m.Permission)
	if err != nil {
		b.logger.Error("desktop: grant store read failed",
			"capability", cap.Name, "permission", m.Permission, "error", err)
		return false, CodeInternal
	}
	if found {
		return decision == grantAllow, denyCode(decision)
	}

	b.promptMu.Lock()
	defer b.promptMu.Unlock()
	// Re-check under the lock: the request that held the mutex first
	// may have persisted the decision already.
	decision, found, err = b.grants.Get(ctx, cap.Name, m.Permission)
	if err != nil {
		b.logger.Error("desktop: grant store read failed",
			"capability", cap.Name, "permission", m.Permission, "error", err)
		return false, CodeInternal
	}
	if found {
		return decision == grantAllow, denyCode(decision)
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

// persistGrant writes the user's decision on a context the page cannot
// cancel (see authorizeCall), bounded so a wedged store cannot hold the
// prompt mutex forever.
func (b *Battery) persistGrant(ctx context.Context, capability, permission, decision string) {
	writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), grantWriteTimeout)
	defer cancel()
	if err := b.grants.Set(writeCtx, capability, permission, decision); err != nil {
		b.logger.Error("desktop: grant store write failed",
			"capability", capability, "permission", permission, "error", err)
	}
}

// denyCode maps a stored decision to the refusal code (garbage in the
// column reads as deny).
func denyCode(decision string) string {
	if decision == grantDeny {
		return CodeDenied
	}
	if decision == grantAllow {
		return ""
	}
	return CodeDenied
}

// serveManifest serves the frozen manifest. 503 before the freeze (the
// window has not opened yet).
func (b *Battery) serveManifest() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m, err := b.frozenManifest()
		if err != nil {
			retryShortly(w)
			return
		}
		body, merr := json.Marshal(m)
		if merr != nil {
			writeBridgeError(w, CodeInternal, internalErrorMsg)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		w.Write(body)
	})
}

// retryShortly answers 503 for a bridge artifact requested before the
// registry froze.
func retryShortly(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Retry-After", "1")
	w.WriteHeader(http.StatusServiceUnavailable)
	io.WriteString(w, "desktop bridge is starting")
}

// frozenManifest returns the manifest built once at freeze time.
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

// bridgeInactiveJS is the bridge script before Run froze the registry
// (or forever, when the app serves over HTTP and never Runs): the page
// keeps working, __gofastr.desktop.available stays false, and nothing
// is a syntax or MIME error.
const bridgeInactiveJS = "// desktop bridge: the host is not running in this process\n"

// bridgeScript holds the lazily generated bridge.js bytes.
type bridgeScript struct {
	once sync.Once
	body []byte
	err  error
}

// scriptNonce is the per-process random query on the bridge script
// URL, minted at New (RegisterExternalScript refuses late
// registrations, and a content hash would need the bytes at Init).
func (b *Battery) bridgeScriptURL() string {
	return bridgeScriptFn + "?v=" + b.scriptNonce
}

// serveBridgeScript serves the generated typed bridge. Lazy: the bytes
// are produced from the frozen registry on first request, with
// Cache-Control: no-store (a hash-addressed immutable URL would need
// the bytes at Init time, before plugins have registered).
func (b *Battery) serveBridgeScript() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !b.reg.isFrozen() {
			// The script tag is on every page the moment Init runs,
			// but the registry only freezes in Run. An app serving
			// itself over HTTP (never Run) must get valid JavaScript
			// here, not a 503 the browser reports as a MIME error on
			// every page load.
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

// atomicString is a tiny atomic holder for the Host pin.
type atomicString struct {
	v atomic.Value
}

func (a *atomicString) Load() string {
	s, _ := a.v.Load().(string)
	return s
}

func (a *atomicString) Store(s string) { a.v.Store(s) }
