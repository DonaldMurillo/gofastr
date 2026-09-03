//go:build red

package chat

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/journal"
	"github.com/DonaldMurillo/gofastr/kiln/live"
)

// Property: duplicate and case-folded JSON object keys are ambiguity, not
// data — a body carrying two values for one field must be rejected at the
// HTTP boundary instead of silently resolved last-wins.
// Surfaces: POST /kiln/chat/message (serveChatMessage, server.go:391);
// POST /kiln/tool/{name} (serveToolDispatch, server.go:402).
// Finding: both surfaces use plain encoding/json decoding. encoding/json
// resolves exact duplicates AND case-folded duplicates ("text" vs "Text")
// last-wins with no error, so a caller-supplied second value silently
// overwrites the first and the request is accepted with 200. On the tool
// dispatcher, serveToolDispatch additionally ignores the Unmarshal error
// entirely (`_ = json.Unmarshal(body, &args)`), so a MALFORMED body still
// mints a KindToolCall envelope with nil args into the journal before any
// parse validation runs — garbage requests mint journal history.
// Fix direction: validate the body once at the top of each handler with a
// decoder that rejects duplicate / case-folded-duplicate keys (walk
// json.Decoder tokens tracking keys per object level) and 400 on
// ambiguity; journal the tool_call envelope only after the body proves
// parseable.
// Severity: loopback dev tool — kiln serve binds loopback; callers are the
// operator and the agent harness. Still a request/JSON mangle class the
// boundary should not resolve silently.
// Round-6 mechanism split: exact duplicates, case-folded duplicates, and
// the malformed-journal shape are separate top-level tests below —
// independently fixable mechanisms.

func redPost(t *testing.T, l *live.Live, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "http://localhost:8765"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	l.ServeHTTP(rec, req)
	return rec
}

// TestKilnChatRedRejectsDuplicateKeys: exact duplicate "text" keys —
// wire-level last-wins on /kiln/chat/message.
func TestKilnChatRedRejectsDuplicateKeys(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	rec := redPost(t, l, "/kiln/chat/message", `{"role":"user","text":"first","text":"second"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: /kiln/chat/message accepted duplicate-key body (encoding/json resolves it last-wins): status %d, body %.200s — want 400",
			rec.Code, rec.Body.String())
	}
}

// TestKilnChatRedRejectsCaseFoldedKeys: "Text"/"text" fold onto the same
// field via stdlib json's tag-insensitive match — a duplicate modulo
// folding; survives a dedup-only fix.
func TestKilnChatRedRejectsCaseFoldedKeys(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	rec := redPost(t, l, "/kiln/chat/message", `{"role":"user","Text":"first","text":"second"}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: /kiln/chat/message accepted case-folded-key body (encoding/json resolves it last-wins): status %d, body %.200s — want 400",
			rec.Code, rec.Body.String())
	}
}

// TestKilnDispatchRedRejectsDuplicateKeys: exact duplicate "entity" keys —
// wire-level last-wins on the tool dispatcher; the tool runs on the second
// value.
func TestKilnDispatchRedRejectsDuplicateKeys(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	rec := redPost(t, l, "/kiln/tool/add_entity", `{"entity":{"name":"dupSafe","fields":[]},"entity":{"name":"dupEvil","fields":[]}}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: /kiln/tool/add_entity accepted duplicate-key body (encoding/json resolves it last-wins and the tool runs on the second value): status %d, body %.200s — want 400",
			rec.Code, rec.Body.String())
	}
}

// TestKilnDispatchRedRejectsCaseFoldedKeys: "Entity"/"entity" fold onto
// the same field — the tool runs on the second value; survives a
// dedup-only fix.
func TestKilnDispatchRedRejectsCaseFoldedKeys(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	rec := redPost(t, l, "/kiln/tool/add_entity", `{"Entity":{"name":"foldSafe","fields":[]},"entity":{"name":"foldEvil","fields":[]}}`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: /kiln/tool/add_entity accepted case-folded-key body (encoding/json resolves it last-wins and the tool runs on the second value): status %d, body %.200s — want 400",
			rec.Code, rec.Body.String())
	}
}

// TestKilnDispatchRedRejectsMalformed: a different mechanism from the
// duplicate-key family — serveToolDispatch ignores the Unmarshal error
// entirely, so a malformed body still mints a KindToolCall envelope with
// nil args into the journal before any parse validation runs.
func TestKilnDispatchRedRejectsMalformed(t *testing.T) {
	l, _ := newCSRFTestServer(t)
	before := redCountToolCalls(t, l)
	rec := redPost(t, l, "/kiln/tool/add_entity", `{"entity":{"name":`)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: /kiln/tool/add_entity accepted malformed JSON: status %d, body %.200s — want 400",
			rec.Code, rec.Body.String())
	}
	if after := redCountToolCalls(t, l); after != before {
		t.Errorf("SECURITY: malformed JSON body minted %d tool_call journal envelope(s) before parse validation (args land as nil via the ignored Unmarshal error); the envelope must only be journaled after the body proves parseable",
			after-before)
	}
}

func redCountToolCalls(t *testing.T, l *live.Live) int {
	t.Helper()
	entries, err := l.Journal().Read()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if e.Kind == journal.KindToolCall {
			n++
		}
	}
	return n
}
