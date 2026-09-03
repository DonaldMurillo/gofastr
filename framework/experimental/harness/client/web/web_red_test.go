//go:build red

package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Property: inbound JSON on operator-reachable loopback endpoints is
// decoded under strict top-level key rules (duplicate keys rejected,
// keys must exact-match the struct's json tags) — the repo standard
// core/handler/bind.go validateBodyKeys enforces on production Bind
// surfaces.
// Surfaces: client/web/web.go handleInput POST /input (SendInput into
// the live agent session) — dev-only operator sidecar on a random
// loopback port, reachable by any page the operator visits.
// Finding: web.go:152 caps the body with MaxBytesReader but decodes it
// with a plain json.NewDecoder(...).Decode — no DisallowUnknownFields,
// no duplicate/case-fold rejection. A body {"text":"a","text":"b"} is
// resolved last-wins and {"TEXT":"b"} matches the tag
// case-insensitively, so both smuggle shapes POST a prompt into the
// session at model authority while any first-read intermediary parsed
// a different request than the engine executed.
// Fix direction: run the buffered body through a
// validateBodyKeys-equivalent before Decode and answer 400 on
// duplicate or case-folded top-level keys.
// Round-6 mechanism split: exact duplicates and case-folded keys are
// separate top-level tests below (independently fixable mechanisms).

// TestHarnessWebRedRejectsDuplicateKeys: exact duplicate top-level "text"
// keys — wire-level last-wins.
func TestHarnessWebRedRejectsDuplicateKeys(t *testing.T) {
	// Happy guard on its own stack (multiplex allows one
	// originator per session): the well-formed body is
	// accepted (202), so the 400 demanded below can only come
	// from key strictness, not plumbing.
	hs := newWiredServer(t)
	req := httptest.NewRequest(http.MethodPost, "/input", strings.NewReader(`{"text":"hello"}`))
	req.Host = "127.0.0.1:8901"
	rec := httptest.NewRecorder()
	hs.handleInput(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("happy path: well-formed body must reach the engine (status %d: %.200s)", rec.Code, rec.Body.String())
	}

	s := newWiredServer(t)
	req = httptest.NewRequest(http.MethodPost, "/input", strings.NewReader(`{"text":"first","text":"second"}`))
	req.Host = "127.0.0.1:8901"
	rec = httptest.NewRecorder()
	s.handleInput(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [web-strict-keys] POST /input accepted a body with a smuggled key "+
			"shape (duplicate top-level text key): status %d, want 400. web.go:152 decodes the maxWebSendBody-capped body "+
			"with a plain Decode: duplicate top-level keys resolve last-wins, "+
			"so the prompt enters the session at model authority. "+
			"Apply the core/handler validateBodyKeys standard (reject duplicates + "+
			"case-folded keys) before Decode.", rec.Code)
	}
}

// TestHarnessWebRedRejectsCaseFoldedKeys: "TEXT" case-folds onto the
// tagged "text" field via stdlib json's tag-insensitive match — the
// prompt still enters the session; survives a dedup-only fix.
func TestHarnessWebRedRejectsCaseFoldedKeys(t *testing.T) {
	// Happy guard on its own stack (multiplex allows one
	// originator per session): the well-formed body is
	// accepted (202), so the 400 demanded below can only come
	// from key strictness, not plumbing.
	hs := newWiredServer(t)
	req := httptest.NewRequest(http.MethodPost, "/input", strings.NewReader(`{"text":"hello"}`))
	req.Host = "127.0.0.1:8901"
	rec := httptest.NewRecorder()
	hs.handleInput(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("happy path: well-formed body must reach the engine (status %d: %.200s)", rec.Code, rec.Body.String())
	}

	s := newWiredServer(t)
	req = httptest.NewRequest(http.MethodPost, "/input", strings.NewReader(`{"TEXT":"hello"}`))
	req.Host = "127.0.0.1:8901"
	rec = httptest.NewRecorder()
	s.handleInput(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: [web-strict-keys] POST /input accepted a body with a smuggled key "+
			"shape (case-folded top-level key): status %d, want 400. web.go:152 decodes the maxWebSendBody-capped body "+
			"with a plain Decode: case-folded keys still match the tag, "+
			"so the prompt enters the session at model authority. "+
			"Apply the core/handler validateBodyKeys standard (reject duplicates + "+
			"case-folded keys) before Decode.", rec.Code)
	}
}
