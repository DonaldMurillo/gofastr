//go:build red

package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
)

// Property: duplicate and case-folded JSON object keys are ambiguity, not
// data — a body carrying two values for one field must be rejected at the
// HTTP boundary instead of silently resolved last-wins.
// Surfaces: POST /kiln/agent (mountAgentRoutes handler, agent_http.go:32).
// Finding: the handler decodes {name, custom} with a plain json.Decoder.
// With --allow-custom-agent, args.Custom becomes the entire argv of a
// process kiln spawns. encoding/json folds case-insensitive tags, so a
// body like {"name":"custom","custom":"cmd benign","Custom":"cmd attacker"}
// resolves last-wins with no error and installs the attacker's command as
// the spawn argv — the operator sees one command configured, another runs.
// Fix direction: reject duplicate / case-folded-duplicate keys before the
// switch on args.Name (token-walk the body tracking keys per object level)
// and http.Error 400 on ambiguity.
// Severity: loopback dev tool, and the custom form already requires the
// --allow-custom-agent opt-in — but that opt-in gates one chosen command,
// not "any request may shadow it with a duplicate key", so the last-wins
// resolution still defeats what the operator consented to.
// Round-6 mechanism split: exact duplicates and case-folded duplicates are
// separate top-level tests below (independently fixable mechanisms).

// redAgentRouter mounts the agent routes with --allow-custom-agent forced
// on and returns the router plus the restore func.
func redAgentRouter() (*router.Router, func()) {
	prevAllow := allowCustomAgent
	allowCustomAgent = true
	store := NewAdapterStore(Adapter{})
	r := router.New()
	mountAgentRoutes(r, store, nil)
	return r, func() { allowCustomAgent = prevAllow }
}

// TestCmdKilnRedAgentRejectsDuplicateKeys: exact duplicate "custom" keys —
// wire-level last-wins; the last value becomes the spawned-process argv.
func TestCmdKilnRedAgentRejectsDuplicateKeys(t *testing.T) {
	r, restore := redAgentRouter()
	defer restore()
	body := `{"name":"custom","custom":"/bin/echo benign-first","custom":"/bin/echo attacker-second"}`
	req := httptest.NewRequest(http.MethodPost, "/kiln/agent", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: POST /kiln/agent accepted duplicate-key body for the \"custom\" key (encoding/json resolves it last-wins, and the last value becomes the spawned-process argv): status %d, body %.200s — want 400",
			rec.Code, rec.Body.String())
	}
}

// TestCmdKilnRedAgentRejectsCaseFoldedKeys: "custom"/"Custom" fold onto
// the same struct field via stdlib json's tag-insensitive match — the
// folded spelling installs the attacker's argv; survives a dedup-only fix.
func TestCmdKilnRedAgentRejectsCaseFoldedKeys(t *testing.T) {
	r, restore := redAgentRouter()
	defer restore()
	body := `{"name":"custom","custom":"/bin/echo benign-first","Custom":"/bin/echo attacker-second"}`
	req := httptest.NewRequest(http.MethodPost, "/kiln/agent", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("SECURITY: POST /kiln/agent accepted case-folded-key body for the \"custom\" key (encoding/json resolves it last-wins, and the last value becomes the spawned-process argv): status %d, body %.200s — want 400",
			rec.Code, rec.Body.String())
	}
}
