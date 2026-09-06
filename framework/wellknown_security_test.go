package framework

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TestWellKnownCardVariesOnInputs: the MCP server card at both mounted
// paths declares Vary on Host and X-Forwarded-Proto — the request inputs
// resolveWellKnownBase splices into remotes[].url — like every sibling
// well-known response, so a well-formed shared cache keys the card on the
// inputs its body varies on.
func TestWellKnownCardVariesOnInputs(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithMCP()))
	defer cleanup()

	for _, path := range []string{"/.well-known/mcp/server-card.json", "/mcp/server-card"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "cards.example"
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		app.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: status %d, want 200", path, rec.Code)
		}

		// Collect Vary field names across header lines and
		// comma-joined lists, case-insensitively (field names are
		// case-insensitive per RFC 9110).
		have := map[string]bool{}
		for _, v := range rec.Header().Values("Vary") {
			for _, f := range strings.Split(v, ",") {
				have[strings.ToLower(strings.TrimSpace(f))] = true
			}
		}
		for _, want := range []string{"host", "x-forwarded-proto"} {
			if !have[want] {
				t.Errorf("SECURITY: [wellknown] %s: Vary missing %q (Vary headers: %q) — "+
					"the card body embeds remotes[].url built from r.Host and the raw "+
					"X-Forwarded-Proto (resolveWellKnownBase), so a shared cache keyed on "+
					"the URL alone can serve one caller's origin to everyone",
					path, want, rec.Header().Values("Vary"))
			}
		}
	}
}

// TestAgentSkillsIndexNoConfigMutation pins per-request defaulting
// writing into App-held configuration, found by the 2026-09-04
// red-probe round; fixed by defaulting Type onto a per-request copy in
// handleAgentSkillsIndex instead of the slice WithAgentSkills
// installed.
//
// Property: a request handler never writes back into App-held
// configuration; per-request defaults are applied to the emitted
// response, not stored into the shared config slice, so two concurrent
// requests cannot race on host state and no request can observe
// another request's normalization.
// Surfaces: framework/wellknown.go::handleAgentSkillsIndex (the skills
// index is the one well-known handler that defaults missing fields;
// the catalog, server-card, and OAuth handlers build fresh documents).
func TestAgentSkillsIndexNoConfigMutation(t *testing.T) {
	app, cleanup := startApp(t, NewApp(WithAgentSkills([]AgentSkillEntry{
		{Name: "a", URL: "https://ex.test/a.md"},
		{Name: "b", URL: "https://ex.test/b.md"},
		{Name: "c", URL: "https://ex.test/c.md"},
	})))
	defer cleanup()

	// Concurrent burst first: under -race this exposes any
	// unsynchronized write into a.agentSkills. Without -race it is
	// harmless setup for the mutation assertion below, which fails
	// deterministically on its own.
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rec := httptest.NewRecorder()
			app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/agent-skills/index.json", nil))
			if rec.Code != http.StatusOK {
				t.Errorf("concurrent skills index status = %d", rec.Code)
			}
		}()
	}
	wg.Wait()

	rec := httptest.NewRecorder()
	app.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/.well-known/agent-skills/index.json", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("skills index status = %d, want 200", rec.Code)
	}
	// The response must carry the defaulted type — that part is the RFC shape.
	var doc struct {
		Skills []AgentSkillEntry `json:"skills"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode skills index: %v", err)
	}
	if len(doc.Skills) != 3 || doc.Skills[0].Type != "skill-md" {
		t.Fatalf("response skills = %+v, want three entries with Type defaulted to skill-md", doc.Skills)
	}

	// The stored App configuration must be untouched by serving the requests.
	if app.agentSkills[0].Type != "" {
		t.Fatalf("SECURITY: [agent-skills-mutation] GET /.well-known/agent-skills/index.json mutated App-held config in place: WithAgentSkills entry Type %q → %q — per-request defaults were written into the shared slice (handleAgentSkillsIndex), an unsynchronized write to process-global state that concurrent requests turn into a data race on a public unauthenticated route", "", app.agentSkills[0].Type)
	}
}
