package desktop

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// PROPERTY
//
//	The bridge chokepoint refuses a request body that cannot cross into
//	the native layer, BEFORE any capability handler runs.
//
// Two shapes are refused, at every method:
//
//   - a NUL inside any decoded string. internal/objc.NSString is the
//     single crossing into Objective-C and it PANICS on an embedded NUL
//     (objc_darwin.go NSString -> cBytes -> panic). That panic runs
//     inside an ffi trampoline entered through runtime.cgocallback with
//     no recover on the path, so one bridge call ends the process. The
//     ungated methods (window.setTitle, tray.setTitle, windows.open,
//     dialogs.saveFile, dialogs.openFile) need no grant at all, so any
//     script in the page is a remote kill switch.
//
//     Note the encoding: a RAW 0x00 byte is not legal inside a JSON
//     string, so the attack arrives as the escape \u0000 and the request
//     BYTES are NUL-free. The check therefore has to look at the DECODED
//     values, not at the body.
//
//   - a JSON `null` body. handler.UnmarshalStrict("null", &map) succeeds
//     with a NIL map and json.Valid("null") is true, so `null` reaches
//     the handler as `input` and every "required" field silently takes
//     its zero value. Nothing is bypassed today only because each core
//     method carries an explicit emptiness check; this is the guard the
//     next method forgets.
//
// The check belongs at the chokepoint, not per handler: it is the one
// place every current AND future capability (including a plugin's)
// passes through.

// nulEsc is the ONLY way a NUL can travel inside a JSON string: the
// six characters backslash u 0 0 0 0. The request bytes stay NUL-free,
// which is why the check has to look at the decoded values.
const nulEsc = `\u0000`

// bridgeInputRouter wires the chokepoint on a frozen registry.
func bridgeInputRouter(t *testing.T) (*Battery, *router.Router) {
	t.Helper()
	b, _ := newTestBattery(t)
	b.freezeForTest()
	r := router.New()
	r.Post(callPattern, http.HandlerFunc(b.handleCall))
	return b, r
}

// postBridge issues one chokepoint call and returns the status + body.
func postBridge(t *testing.T, r *router.Router, capName, method, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, bridgePrefix+"/call/"+capName+"/"+method, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// TestBridgeRefusesNULInBody asserts the property at every page-reachable
// surface that carries a string to the Shell. One attack shape, every
// surface, not one surface with a matrix of control bytes.
func TestBridgeRefusesNULInBody(t *testing.T) {
	_, r := bridgeInputRouter(t)

	surfaces := []struct{ cap, method, body string }{
		{"window", "setTitle", `{"title":"a\u0000b"}`},
		{"tray", "setTitle", `{"title":"a\u0000b"}`},
		{"windows", "open", `{"path":"/x","title":"a\u0000b"}`},
		{"dialogs", "saveFile", `{"defaultName":"a\u0000b"}`},
		{"dialogs", "openFile", `{"filters":[{"name":"n","extensions":["a\u0000b"]}]}`},
		{"clipboard", "writeText", `{"text":"a\u0000b"}`},
		{"notifications", "show", `{"title":"ok","body":"a\u0000b"}`},
		{"fs", "writeText", `{"path":"/tmp/x","text":"a\u0000b"}`},
	}
	for _, s := range surfaces {
		t.Run(s.cap+"."+s.method, func(t *testing.T) {
			code, body := postBridge(t, r, s.cap, s.method, s.body)
			if code != http.StatusBadRequest || !strings.Contains(body, CodeInvalidInput) {
				t.Fatalf("a NUL-bearing body was not refused at the chokepoint: %d %s\n"+
					"on darwin/arm64 that NUL reaches objc.NSString and panics on the "+
					"locked main thread inside an ffi trampoline with no recover, "+
					"which ends the desktop process from one page-issued fetch", code, strings.TrimSpace(body))
			}
		})
	}
}

// TestBridgeKeepsNormalBodies is the anti-vacuity half: the NUL check
// must not refuse ordinary input (astral runes, quotes, newlines).
func TestBridgeKeepsNormalBodies(t *testing.T) {
	_, r := bridgeInputRouter(t)
	for _, body := range []string{
		`{"title":"ok"}`,
		`{"title":"emoji 😀 and \"quotes\""}`,
		`{"title":"line\nbreak\ttab"}`,
	} {
		if code, got := postBridge(t, r, "window", "setTitle", body); code == http.StatusBadRequest {
			t.Errorf("body %s was refused; only a NUL may be: %s", body, strings.TrimSpace(got))
		}
	}
}

// TestBridgeRefusesNullBody pins "object or empty", the comment the
// chokepoint already carries.
func TestBridgeRefusesNullBody(t *testing.T) {
	_, r := bridgeInputRouter(t)

	for _, body := range []string{"null", "123", `"s"`, "[]", "true"} {
		code, got := postBridge(t, r, "window", "title", body)
		if code != http.StatusBadRequest {
			t.Errorf("body %s: status %d, want 400 (the body must be a JSON object or empty); got %s",
				body, code, strings.TrimSpace(got))
		}
	}
	// An empty body and an object body both still reach the handler
	// (which answers unsupported: no window is open in this test).
	for _, body := range []string{"", "{}"} {
		if code, _ := postBridge(t, r, "window", "title", body); code == http.StatusBadRequest {
			t.Errorf("body %q was refused; an object or empty body must reach the handler", body)
		}
	}
}

// TestNULRefusalBeatsThePermissionGate pins the ORDER: a malformed body
// is refused before the permission prompt, so a NUL cannot be used to
// farm permission dialogs.
func TestNULRefusalBeatsThePermissionGate(t *testing.T) {
	b, r := bridgeInputRouter(t)
	shell := b.shell.(*fakeShell)

	if code, _ := postBridge(t, r, "clipboard", "writeText", `{"text":"a\u0000b"}`); code != http.StatusBadRequest {
		t.Fatalf("status %d, want 400", code)
	}
	if got := len(shell.prompts()); got != 0 {
		t.Fatalf("%d permission prompts for a body that never should have been dispatched", got)
	}
}
