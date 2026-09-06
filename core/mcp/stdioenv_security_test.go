package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

// Property: a duplicated or case-folded JSON-RPC envelope key is refused
// by EVERY transport decode of the same server — the executor must never
// resolve envelope ambiguity silently last-wins while a first-occurrence
// parser (audit logger, WAF, stdio wrapper) reads a different method,
// params, or id.
//
// Surfaces: transport.go:ServeStdio (strict decode via
// handler.UnmarshalStrict since the 2026-09-05 round-4 fix), and
// transport.go:decodeMCPRequest (the HTTP POST path, which always ran
// UnmarshalStrict; pinned by params_security_test.go). Found red in the
// 2026-09-05 adversarial pass round 4: the same envelope bytes the HTTP
// transport refused with 400 were silently executed over stdio.
//
// TestStdioEnvelopeAmbiguityRefused feeds three ambiguous envelopes
// through ServeStdio and asserts each is refused as invalid-params, then
// posts the SAME bytes to the HTTP transport to show it refuses them too
// (both transports must uphold the one rule).
func TestStdioEnvelopeAmbiguityRefused(t *testing.T) {
	var execs atomic.Int32
	var mu sync.Mutex
	ranWith := map[string]any{}

	s := NewServer()
	if err := s.RegisterTool("t", "records args",
		map[string]any{"type": "object", "properties": map[string]any{"name": map[string]any{"type": "string"}}},
		func(_ context.Context, params map[string]any) (any, error) {
			execs.Add(1)
			mu.Lock()
			ranWith["name"] = params["name"]
			mu.Unlock()
			return "ran:" + toString(params["name"]), nil
		}); err != nil {
		t.Fatal(err)
	}

	shapes := []struct {
		name string
		line string
	}{
		{
			// Duplicate "method": stdlib keeps the LAST ("ping"); a
			// first-occurrence auditor read "tools/call".
			name: "duplicate method",
			line: `{"jsonrpc":"2.0","id":1,"method":"tools/call","method":"ping","params":{"name":"t","arguments":{"name":"first"}}}`,
		},
		{
			// Case-folded "Params" collides with the params field;
			// stdlib folds key case and keeps the last, so the tool
			// runs with the smuggled ARGUMENTS the proxy never saw.
			name: "case-folded params",
			line: `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"t","arguments":{"name":"first"}},"Params":{"name":"t","arguments":{"name":"smuggled"}}}`,
		},
		{
			// Duplicate "id": the response echoes the LAST id, not the
			// one the caller's log correlates.
			name: "duplicate id",
			line: `{"jsonrpc":"2.0","id":1,"id":2,"method":"tools/call","params":{"name":"t","arguments":{"name":"first"}}}`,
		},
	}

	// --- stdio half: every shape must come back as an error -------------
	var out bytes.Buffer
	var in strings.Builder
	for _, sh := range shapes {
		in.WriteString(sh.line)
		in.WriteByte('\n')
	}
	if err := s.ServeStdio(context.Background(), strings.NewReader(in.String()), &out); err != nil {
		t.Fatalf("ServeStdio: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != len(shapes) {
		t.Fatalf("stdio answered %d lines, want %d (out=%q)", len(lines), len(shapes), out.String())
	}
	for i, sh := range shapes {
		var resp Response
		if err := json.Unmarshal([]byte(lines[i]), &resp); err != nil {
			t.Fatalf("shape %q: parse stdio response %q: %v", sh.name, lines[i], err)
		}
		if resp.Error == nil {
			t.Errorf("SECURITY: [stdio-envelope] shape %q was silently executed over stdio (resp=%s) — the HTTP transport refuses the same bytes; ServeStdio must apply the same no-ambiguity envelope rule (handler.UnmarshalStrict), not resolve duplicate/case-folded keys last-wins", sh.name, lines[i])
		}
	}
	if n := int(execs.Load()); n > 0 {
		t.Errorf("SECURITY: [stdio-envelope] %d of the ambiguous envelopes reached a tool handler (last-wins decode reached the executor)", n)
	}

	// --- HTTP control: the same bytes are refused there too -------------
	for _, sh := range shapes {
		req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(sh.line))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		s.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("control: shape %q: HTTP transport answered %d, want 400 (the envelope rule both transports share)", sh.name, rec.Code)
		}
	}
}

func toString(v any) string {
	s, _ := v.(string)
	return s
}
