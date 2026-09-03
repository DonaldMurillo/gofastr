//go:build red

// RED TEST — open finding, 2026-09-02 round-2 adversarial pass (tests-only; no fix applied).
package a2a

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// Property: the top-level JSON-RPC envelope rejects duplicate and case-folded keys, so no first-occurrence parser (proxy, WAF, audit logger) can disagree with the executor's last-key-wins decode.
// Surfaces: server.go:271-275 ServeHTTP json.Unmarshal into rpcRequest; server.go:294-321 dispatch on req.Method.
// Finding: the envelope is decoded with plain json.Unmarshal, outside the strict-top-level-keys contract every Bind consumer holds (core/handler/bind.go::validateBodyKeys) and outside the parity the _batch envelopes were pinned to (framework/crud/batch_envelope_security_test.go). Two "method" keys — or "Method"/"method", which stdlib folds onto the same field — silently take the LAST occurrence, so an intermediary validating the first sees a benign GetTask read while the executor dispatches the smuggled SendMessage. Within this server the decoded struct is used consistently (the attack is not a first-read/second-dispatch split in-process), so severity is parity/intermediary-smuggling, not a direct auth bypass; the owner check still precedes dispatch for whichever method wins.
// Fix direction: run the raw body through a validateBodyKeys-equivalent (strict top-level keys for the rpcRequest struct: exact tags, reject duplicates and case-folded variants) before json.Unmarshal, answering CodeParseError/CodeInvalidRequest like the batch surfaces 400.
// redA2ASendParams: valid SendMessage params routed to the echo skill —
// what the smuggled last-wins method needs to actually dispatch.
const redA2ASendParams = `{"message":{"role":"ROLE_USER","parts":[{"text":"hi"}],"metadata":{"skill":"echo"}}}`

// a2aRedEnvelopeRejected drives one raw JSON-RPC envelope body and fails
// unless the server refuses it before dispatch (the strict-top-level-keys
// contract validateBodyKeys pins for Bind consumers).
func a2aRedEnvelopeRejected(t *testing.T, name, body string) {
	t.Helper()
	h := newHarness(t, nil)
	var ran atomic.Bool
	h.setHandler(func(_ context.Context, task TaskContext) error {
		ran.Store(true)
		return task.Complete(TextPart("smuggled"))
	})

	req := httptest.NewRequest(http.MethodPost, h.ts.URL, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Owner", "alice")
	rec := httptest.NewRecorder()
	h.srv.ServeHTTP(rec, req)

	var e env
	_ = json.Unmarshal(rec.Body.Bytes(), &e)
	rejected := rec.Code == http.StatusBadRequest || e.Error != nil
	if !rejected {
		t.Fatalf("SECURITY: [strict-json] %s: duplicated top-level key body was accepted and dispatched (status=%d body=%s) — stdlib json's silent last-key-wins lets the second method run while a first-occurrence validator saw %q; want the parse rejected like validateBodyKeys does for Bind consumers", name, rec.Code, rec.Body.String(), MethodGetTask)
	}
	if ran.Load() {
		t.Fatalf("SECURITY: [strict-json] %s: the skill handler ran behind a duplicated/case-folded method key — the smuggled method was dispatched", name)
	}
}

// Exact duplicate "method" keys — wire-level last-wins. First occurrence
// is the benign read an intermediary sees; stdlib json silently keeps the
// last and dispatches it.
func TestA2ARedRejectsDuplicateKeys(t *testing.T) {
	a2aRedEnvelopeRejected(t, "duplicate method key",
		`{"jsonrpc":"2.0","id":"dup-1","method":"`+MethodGetTask+`","method":"`+MethodSendMessage+`","params":`+redA2ASendParams+`}`)
}

// "Method"/"method" case-fold onto the same struct field via stdlib json's
// tag-insensitive match — a duplicate modulo folding; exact-tag strictness
// rejects it. Survives a dedup-only fix.
func TestA2ARedRejectsCaseFoldedKeys(t *testing.T) {
	a2aRedEnvelopeRejected(t, "case-folded method key",
		`{"jsonrpc":"2.0","id":"fold-1","Method":"`+MethodGetTask+`","method":"`+MethodSendMessage+`","params":`+redA2ASendParams+`}`)
}
