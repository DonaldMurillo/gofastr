package desktop

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Group 4: the chokepoint. Served through the real core/router so
// {cap}/{method} path values and the 405 path behave as in production.

// serveBridge builds a battery with an extra "testcap" capability and
// serves the call route through a real router.
func serveBridge(t *testing.T, b *Battery) *httptest.Server {
	t.Helper()
	b.freezeForTest()
	r := router.New()
	r.Get(enterPath, b.enterHandler())
	r.Post(callPattern, http.HandlerFunc(b.handleCall))
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// newChokepointBattery registers a gated and an ungated test method
// plus the ability to observe handler invocation.
//
// The counter is atomic because "concurrent first calls prompt once"
// drives six goroutines through the SAME handler; a plain int there was
// a data race in the double itself, which -race reports before it can
// say anything about the code under test.
func newChokepointBattery(t *testing.T) (*Battery, *fakeShell, *atomic.Int64) {
	t.Helper()
	b, shell := newTestBattery(t)
	var called atomic.Int64
	handlerCalled := &called
	err := b.Register(Capability{
		Name:    "testcap",
		Version: 1,
		Methods: []Method{
			{
				Name: "ungated",
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					called.Add(1)
					return map[string]any{"echo": "ungated"}, nil
				},
			},
			{
				Name:       "gated",
				Permission: "test:thing",
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					called.Add(1)
					return map[string]any{"echo": "gated"}, nil
				},
			},
			{
				Name: "boom",
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					called.Add(1)
					return nil, errors.New("secret detail: /etc/passwd DSN postgres://u:p@h/db")
				},
			},
			{
				Name: "unimplemented",
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					return nil, ErrUnsupported
				},
			},
			{
				Name: "canceller",
				Handler: func(ctx context.Context, in json.RawMessage) (any, error) {
					return nil, context.Canceled
				},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	return b, shell, handlerCalled
}

// postCall posts a body to the chokepoint and returns status + body.
func postCall(t *testing.T, srv *httptest.Server, path, contentType, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestChokepointMethodAndContentTypeChecks(t *testing.T) {
	b, _, _ := newChokepointBattery(t)
	srv := serveBridge(t, b)

	// GET → 405 from the router (only POST is registered).
	resp, err := http.Get(srv.URL + "/__gofastr/desktop/call/testcap/ungated")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET: %d, want 405", resp.StatusCode)
	}

	// Wrong content type → 415.
	if status, _ := postCall(t, srv, "/__gofastr/desktop/call/testcap/ungated", "text/plain", "{}"); status != 415 {
		t.Fatalf("text/plain: %d, want 415", status)
	}
	if status, _ := postCall(t, srv, "/__gofastr/desktop/call/testcap/ungated", "application/x-www-form-urlencoded", "a=b"); status != 415 {
		t.Fatalf("form-encoded: %d, want 415", status)
	}
	// A charset parameter on the right type is fine.
	if status, body := postCall(t, srv, "/__gofastr/desktop/call/testcap/ungated", "application/json; charset=utf-8", "{}"); status != 200 || !strings.Contains(body, `"ok":true`) {
		t.Fatalf("json+charset: %d %s", status, body)
	}
}

func TestChokepointSecFetchSiteRefused(t *testing.T) {
	b, _, _ := newChokepointBattery(t)
	srv := serveBridge(t, b)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/__gofastr/desktop/call/testcap/ungated", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("cross-site: %d, want 403", resp.StatusCode)
	}
	if !strings.Contains(string(body), `"denied"`) {
		t.Fatalf("cross-site body: %s", body)
	}
	// same-origin and none pass; absent header passes.
	for _, v := range []string{"same-origin", "none", ""} {
		req, _ := http.NewRequest(http.MethodPost, srv.URL+"/__gofastr/desktop/call/testcap/ungated", strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		if v != "" {
			req.Header.Set("Sec-Fetch-Site", v)
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("Sec-Fetch-Site %q: %d, want 200", v, resp.StatusCode)
		}
	}
}

func TestChokepointOversizeBody(t *testing.T) {
	b, _, _ := newChokepointBattery(t)
	srv := serveBridge(t, b)
	big := `{"x":"` + strings.Repeat("a", 2<<20) + `"}`
	status, body := postCall(t, srv, "/__gofastr/desktop/call/testcap/ungated", "application/json", big)
	if status != http.StatusBadRequest {
		t.Fatalf("oversize: %d %s, want 400", status, body)
	}
	if !strings.Contains(body, `"invalid_input"`) {
		t.Fatalf("oversize body: %s", body)
	}
}

func TestChokepointUnknownCapabilityAndMethodIdenticalBodies(t *testing.T) {
	b, _, _ := newChokepointBattery(t)
	srv := serveBridge(t, b)
	_, capBody := postCall(t, srv, "/__gofastr/desktop/call/nosuchcap/ungated", "application/json", "{}")
	_, methBody := postCall(t, srv, "/__gofastr/desktop/call/testcap/nosuchmethod", "application/json", "{}")
	if capBody != methBody {
		t.Fatalf("404 bodies differ:\ncap:   %s\nmethod: %s", capBody, methBody)
	}
	if status, _ := postCall(t, srv, "/__gofastr/desktop/call/nosuchcap/ungated", "application/json", "{}"); status != 404 {
		t.Fatalf("unknown capability: %d, want 404", status)
	}
	var env struct {
		OK    bool `json:"ok"`
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(capBody), &env); err != nil {
		t.Fatal(err)
	}
	if env.OK || env.Error.Code != "not_found" {
		t.Fatalf("404 envelope: %s", capBody)
	}
}

func TestChokepointNonObjectBody(t *testing.T) {
	b, _, _ := newChokepointBattery(t)
	srv := serveBridge(t, b)
	for _, body := range []string{`[1,2]`, `5`, `"text"`, `true`} {
		status, respBody := postCall(t, srv, "/__gofastr/desktop/call/testcap/ungated", "application/json", body)
		if status != http.StatusBadRequest || !strings.Contains(respBody, `"invalid_input"`) {
			t.Fatalf("body %q: %d %s, want 400 invalid_input", body, status, respBody)
		}
	}
	// Empty body is fine (treated as {}).
	if status, body := postCall(t, srv, "/__gofastr/desktop/call/testcap/ungated", "application/json", ""); status != 200 || !strings.Contains(body, `"ungated"`) {
		t.Fatalf("empty body: %d %s", status, body)
	}
	// Duplicate top-level keys are refused (strict decode).
	if status, _ := postCall(t, srv, "/__gofastr/desktop/call/testcap/ungated", "application/json", `{"x":1,"x":2}`); status != http.StatusBadRequest {
		t.Fatalf("duplicate keys: %d, want 400", status)
	}
}

func TestChokepointPermissionGate(t *testing.T) {
	path := "/__gofastr/desktop/call/testcap/gated"

	t.Run("ungated method skips prompt", func(t *testing.T) {
		b, shell, _ := newChokepointBattery(t)
		srv := serveBridge(t, b)
		if status, _ := postCall(t, srv, "/__gofastr/desktop/call/testcap/ungated", "application/json", "{}"); status != 200 {
			t.Fatalf("status %d", status)
		}
		if n := len(shell.prompts()); n != 0 {
			t.Fatalf("ungated method prompted %d times", n)
		}
	})

	t.Run("denied grant never reaches handler", func(t *testing.T) {
		b, _, called := newChokepointBattery(t)
		if err := b.grants.Set(context.Background(), "testcap", "test:thing", grantDeny); err != nil {
			t.Fatal(err)
		}
		srv := serveBridge(t, b)
		status, body := postCall(t, srv, path, "application/json", "{}")
		if status != http.StatusForbidden || !strings.Contains(body, `"denied"`) {
			t.Fatalf("deny: %d %s", status, body)
		}
		if called.Load() != 0 {
			t.Fatalf("handler ran %d times behind a denied grant", called.Load())
		}
	})

	t.Run("allowOnce does not persist and prompts again", func(t *testing.T) {
		b, shell, _ := newChokepointBattery(t)
		shell.promptDecisions = []Decision{DecisionAllowOnce, DecisionAllowOnce}
		srv := serveBridge(t, b)
		for i := range 2 {
			if status, _ := postCall(t, srv, path, "application/json", "{}"); status != 200 {
				t.Fatalf("call %d: status != 200", i)
			}
		}
		if n := len(shell.prompts()); n != 2 {
			t.Fatalf("allow-once prompted %d times, want 2 (no persistence)", n)
		}
		if _, found, _ := b.grants.Get(context.Background(), "testcap", "test:thing"); found {
			t.Fatal("allow-once persisted a grant")
		}
	})

	t.Run("allow persists and does not prompt again", func(t *testing.T) {
		b, shell, _ := newChokepointBattery(t)
		shell.promptDecisions = []Decision{DecisionAllow}
		srv := serveBridge(t, b)
		for i := range 2 {
			if status, _ := postCall(t, srv, path, "application/json", "{}"); status != 200 {
				t.Fatalf("call %d failed", i)
			}
		}
		if n := len(shell.prompts()); n != 1 {
			t.Fatalf("allow prompted %d times, want 1", n)
		}
		dec, found, _ := b.grants.Get(context.Background(), "testcap", "test:thing")
		if !found || dec != grantAllow {
			t.Fatalf("grant after allow = (%q, %v)", dec, found)
		}
	})

	t.Run("deny persists", func(t *testing.T) {
		b, shell, _ := newChokepointBattery(t)
		shell.promptDecisions = []Decision{DecisionDeny, DecisionDeny}
		srv := serveBridge(t, b)
		for i := range 2 {
			if status, _ := postCall(t, srv, path, "application/json", "{}"); status != http.StatusForbidden {
				t.Fatalf("call %d: want 403", i)
			}
		}
		if n := len(shell.prompts()); n != 1 {
			t.Fatalf("deny prompted %d times, want 1 (deny must persist)", n)
		}
		dec, found, _ := b.grants.Get(context.Background(), "testcap", "test:thing")
		if !found || dec != grantDeny {
			t.Fatalf("grant after deny = (%q, %v)", dec, found)
		}
	})

	t.Run("prompt error is 500 internal", func(t *testing.T) {
		b, shell, _ := newChokepointBattery(t)
		shell.promptErr = errors.New("shell exploded")
		srv := serveBridge(t, b)
		status, body := postCall(t, srv, path, "application/json", "{}")
		if status != http.StatusInternalServerError {
			t.Fatalf("prompt error: %d, want 500", status)
		}
		if strings.Contains(body, "shell exploded") {
			t.Fatalf("prompt error leaked: %s", body)
		}
	})

	t.Run("concurrent first calls prompt once", func(t *testing.T) {
		b, shell, _ := newChokepointBattery(t)
		shell.promptDecisions = []Decision{DecisionAllow}
		srv := serveBridge(t, b)
		var wg sync.WaitGroup
		for range 6 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				status, _ := postCall(t, srv, path, "application/json", "{}")
				if status != 200 {
					t.Errorf("concurrent call status %d", status)
				}
			}()
		}
		wg.Wait()
		if n := len(shell.prompts()); n != 1 {
			t.Fatalf("concurrent first calls prompted %d times, want 1", n)
		}
	})
}

func TestChokepointErrorMapping(t *testing.T) {
	b, _, _ := newChokepointBattery(t)
	srv := serveBridge(t, b)

	// Internal error: fixed message, no detail leak.
	status, body := postCall(t, srv, "/__gofastr/desktop/call/testcap/boom", "application/json", "{}")
	if status != http.StatusInternalServerError {
		t.Fatalf("boom: %d, want 500", status)
	}
	if !strings.Contains(body, "internal error") {
		t.Fatalf("boom body must carry the fixed message: %s", body)
	}
	if strings.Contains(body, "secret detail") || strings.Contains(body, "postgres://") {
		t.Fatalf("boom body leaked the error: %s", body)
	}

	// ErrUnsupported → 501.
	if status, _ := postCall(t, srv, "/__gofastr/desktop/call/testcap/unimplemented", "application/json", "{}"); status != http.StatusNotImplemented {
		t.Fatalf("unsupported: %d, want 501", status)
	}

	// Context cancellation → 409.
	if status, _ := postCall(t, srv, "/__gofastr/desktop/call/testcap/canceller", "application/json", "{}"); status != http.StatusConflict {
		t.Fatalf("cancelled: %d, want 409", status)
	}
}

func TestChokepointResponseHeaders(t *testing.T) {
	b, _, _ := newChokepointBattery(t)
	srv := serveBridge(t, b)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/__gofastr/desktop/call/testcap/ungated", strings.NewReader("{}"))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q", cc)
	}
}

func TestBridgeScriptAndManifestBeforeFreeze503(t *testing.T) {
	b, _ := newTestBattery(t) // NOT frozen
	r := router.New()
	r.Get(manifestPath, b.serveManifest())
	r.Get(bridgeScriptFn, b.serveBridgeScript())
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("manifest before freeze: %d, want 503", resp.StatusCode)
	}

	// The script tag is on every page from Init on, and an app that
	// serves over HTTP never freezes: bridge.js must be valid, inert
	// JavaScript, never a 503 the browser reports as a MIME error.
	sresp, err := http.Get(srv.URL + bridgeScriptFn)
	if err != nil {
		t.Fatal(err)
	}
	sbody, _ := io.ReadAll(sresp.Body)
	sresp.Body.Close()
	if sresp.StatusCode != http.StatusOK {
		t.Fatalf("bridge.js before freeze: %d, want 200 with an inert script", sresp.StatusCode)
	}
	if ct := sresp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/javascript") {
		t.Fatalf("bridge.js before freeze Content-Type = %q", ct)
	}
	if !strings.Contains(string(sbody), "not running") || strings.Contains(string(sbody), "D.manifest") {
		t.Fatalf("bridge.js before freeze must be the inert script, got: %.120s", sbody)
	}
}

func TestBridgeScriptAndManifestAfterFreeze(t *testing.T) {
	b, _ := newTestBattery(t)
	b.freezeForTest()
	r := router.New()
	r.Get(manifestPath, b.serveManifest())
	r.Get(bridgeScriptFn, b.serveBridgeScript())
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("manifest: %d", resp.StatusCode)
	}
	if !bytes.Contains(body, []byte(`"clipboard"`)) || !bytes.Contains(body, []byte(`"schema":1`)) {
		t.Fatalf("manifest body: %s", body)
	}
	if cc := resp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("manifest Cache-Control = %q", cc)
	}

	sresp, err := http.Get(srv.URL + bridgeScriptFn)
	if err != nil {
		t.Fatal(err)
	}
	sbody, _ := io.ReadAll(sresp.Body)
	sresp.Body.Close()
	if sresp.StatusCode != 200 {
		t.Fatalf("bridge.js: %d", sresp.StatusCode)
	}
	if !bytes.Contains(sbody, []byte("JSON.parse")) || !bytes.Contains(sbody, []byte(`D["clipboard"]`)) {
		t.Fatalf("bridge.js body: %s", sbody[:min(200, len(sbody))])
	}
	if cc := sresp.Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("bridge.js Cache-Control = %q", cc)
	}
}

// Silence an unused helper warning if the fixture moves.
var _ = filepath.Join
