package uihost

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// Pins: a response whose body or headers embed request-derived origin
// inputs (r.Host, enum-gated X-Forwarded-Proto via resolveBaseURL)
// declares Vary: Host, X-Forwarded-Proto — on the unsigned A2A card and
// on pages whose Link headers embed the base URL — mirroring
// framework/wellknown.go's varyWellKnown.
// CONTRACT ANSWER: yes — the unsigned card carries Vary.
// (resolveBaseURL's own comment and the wellknown.go sibling contract
// state the cache-poisoning rationale for exactly this dataflow; the
// signed-card variant additionally panics unless BaseURL is pinned.)
// Property: a response whose body or headers embed request-derived origin inputs
// (r.Host, enum-gated X-Forwarded-Proto) declares Vary: Host, X-Forwarded-Proto
// so a shared cache keys on the inputs the output varies on.
// Surfaces: framework/uihost/agentready.go::handleAgentCard (routes
// /.well-known/agent-card.json + /.well-known/agent.json; body embeds
// resolveBaseURL(req) into supportedInterfaces[].url; sets only
// Cache-Control: no-cache, no Vary) and ::writeAgentLinkHeaders (called from
// handlePage uihost.go:1424; Link: <https://<host>/mcp>; rel="service" plus
// sitemap/llms.txt/card absolute URLs on every HTML page; the negotiable arm
// of sibling responses carries Vary: Accept but these carry no origin Vary).
// Finding: with no BaseURL configured, GET /.well-known/agent-card.json with
// Host: attacker.example + X-Forwarded-Proto: https returns 200 no-cache
// advertising https://attacker.example/... endpoints with NO Vary; page Link
// headers name https://attacker.example/mcp the same way. A shared cache that
// stores without honoring revalidation pins attacker-named endpoints for
// later agents.
// Fix direction: mirror framework/wellknown.go's varyWellKnown — add
// Vary: Host + Vary: X-Forwarded-Proto on the card response and on pages
// whose Link headers embed resolveBaseURL output (or pin the base URL the
// way the signed-card arm already requires).

// agentCardVaryFields collects Vary field names across header lines and
// comma-joined lists, case-insensitively (field names are case-insensitive
// per RFC 9110). Same collection shape as the framework/wellknown sibling.
func agentCardVaryFields(rec *httptest.ResponseRecorder) map[string]bool {
	have := map[string]bool{}
	for _, v := range rec.Header().Values("Vary") {
		for _, f := range strings.Split(v, ",") {
			have[strings.ToLower(strings.TrimSpace(f))] = true
		}
	}
	return have
}

// TestAgentCardVaryOnOriginInputs: the unsigned A2A card at both mounted
// well-known paths, built with no pinned BaseURL, embeds resolveBaseURL(req)
// (r.Host + enum-gated X-Forwarded-Proto) into supportedInterfaces[].url —
// it must declare Vary on those two inputs like its framework/wellknown.go
// sibling, or a shared cache can pin one caller's origin into every later
// agent's card.
func TestAgentCardVaryOnOriginInputs(t *testing.T) {
	ds := newISOHost(WithAgentCard(AgentCardConfig{Name: "t", MCPEndpoint: "/mcp"}))

	for _, path := range []string{"/.well-known/agent-card.json", "/.well-known/agent.json"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "attacker.example"
		req.Header.Set("X-Forwarded-Proto", "https")
		rec := httptest.NewRecorder()
		ds.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s: setup broken: status %d, want 200", path, rec.Code)
		}
		// Premise gate: with no BaseURL configured the card body really
		// does reflect the request origin into its service URL.
		if !strings.Contains(rec.Body.String(), "https://attacker.example/mcp") {
			t.Fatalf("%s: setup broken: card body does not embed the request-derived "+
				"origin (no https://attacker.example/mcp in body) — the poisoning "+
				"premise does not hold:\n%s", path, rec.Body.String())
		}

		have := agentCardVaryFields(rec)
		for _, want := range []string{"host", "x-forwarded-proto"} {
			if !have[want] {
				t.Errorf("SECURITY: [agentcard-vary] %s: Vary missing %q (Vary headers: %q) — "+
					"the unsigned card body embeds supportedInterfaces[].url built from "+
					"r.Host and the enum-gated X-Forwarded-Proto (resolveBaseURL) and "+
					"carries only Cache-Control: no-cache, so a shared cache that stores "+
					"without honoring revalidation pins attacker-named endpoints for "+
					"later agents", path, want, rec.Header().Values("Vary"))
			}
		}
	}
}

// TestAgentLinkHeadersVaryOrigin: HTML pages whose Link headers embed
// resolveBaseURL(req) output (rel="service" naming the MCP endpoint, plus
// sitemap/llms.txt/card absolute URLs) must declare Vary on Host and
// X-Forwarded-Proto, or a shared cache can pin the attacker-named
// https://<attacker-host>/mcp endpoint into every later visitor's page.
func TestAgentLinkHeadersVaryOrigin(t *testing.T) {
	ds := newISOHost(
		WithAgentCard(AgentCardConfig{Name: "t", MCPEndpoint: "/mcp"}),
		WithAgentLinkHeaders(),
	)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Host = "attacker.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	ds.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("setup broken: page status %d, want 200", rec.Code)
	}
	// Premise gate: the Link header really does reflect the request origin.
	if !strings.Contains(rec.Header().Get("Link"), "https://attacker.example/mcp") {
		t.Fatalf("setup broken: Link header does not embed the request-derived origin "+
			"(no https://attacker.example/mcp in %q) — the poisoning premise does not hold",
			rec.Header().Get("Link"))
	}

	have := agentCardVaryFields(rec)
	for _, want := range []string{"host", "x-forwarded-proto"} {
		if !have[want] {
			t.Errorf("SECURITY: [agentcard-vary] page Link headers: Vary missing %q (Vary headers: %q) — "+
				"writeAgentLinkHeaders embeds resolveBaseURL(req) (r.Host + enum-gated "+
				"X-Forwarded-Proto) into Link: <https://attacker.example/mcp>; rel=\"service\" "+
				"and the sitemap/llms.txt/card absolute URLs with no origin Vary, so a shared "+
				"cache that stores without honoring revalidation pins attacker-named endpoints "+
				"for later agents", want, rec.Header().Values("Vary"))
		}
	}
}
