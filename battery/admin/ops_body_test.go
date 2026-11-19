package admin

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// An oversize ops post answers through the same envelope as every other
// ops refusal: a form RPC gets the JSON body the runtime toasts, and a
// plain post gets the 303 with ?result=body-too-large, never a bare
// text/plain status line.
func TestOpsOversizeBodyAnswersTheOpsEnvelope(t *testing.T) {
	x := modulesEnv(t, &fakeModules{})
	big := func() string { return strings.Repeat("x", 2<<20) }

	rr := rpc(x.as(theAdmin), "/admin/modules/_enable", map[string]any{"module": big()})
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("RPC oversize = %d, want 413; body=%s", rr.Code, rr.Body.String())
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("RPC oversize Content-Type = %q, want the JSON envelope", ct)
	}
	if !strings.Contains(rr.Body.String(), `"error"`) {
		t.Errorf("RPC oversize body is not the envelope: %s", rr.Body.String())
	}

	plain := post(x.as(theAdmin), "/admin/modules/_enable", url.Values{"module": {big()}})
	if got := resultOf(t, plain); got != "body-too-large" {
		t.Fatalf("plain oversize result = %q, want body-too-large (%d %s)", got, plain.Code, plain.Body.String())
	}
	if len(x.auditOps("module")) != 0 {
		t.Fatal("a refused body was audited")
	}
}

// A malformed RPC body and an oversize one answer with the same shape:
// one endpoint, one body shape.
func TestOpsBadInputAnswersRefuseShape(t *testing.T) {
	x := modulesEnv(t, &fakeModules{})
	req := httptest.NewRequest(http.MethodPost, "/admin/modules/_enable", strings.NewReader(`{"module":`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	x.as(theAdmin).ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || rec.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("malformed RPC = %d %s, want 400 application/json; body=%s",
			rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
	}
}
