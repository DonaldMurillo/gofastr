package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readDoc loads a shipped doc by name from content/. The doc corpus is
// embedded into the gofastr binary, so a claim that is false here is false
// at `gofastr docs` and in the MCP framework_docs_* tools too.
func readDoc(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("content", name))
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

// readRepo loads a repo-relative file (core-ui/ARCHITECTURE.md) so a gate
// can pin a claim that lives outside the embedded doc corpus.
//
//gofastr:test-reads core-ui/ARCHITECTURE.md
func readRepo(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "..", rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}

func sectionAfter(t *testing.T, doc, anchor string, window int) string {
	t.Helper()
	i := strings.Index(doc, anchor)
	if i < 0 {
		t.Fatalf("%q anchor not found in doc", anchor)
	}
	return doc[i:min(i+window, len(doc))]
}

// ConfirmPageData.CSRFField exists because a custom confirmation page
// rendered no CSRF field and its only button 403'd under auth.CSRF()
// (WithBFFPosture mounts that app-wide). auth.md is the one place a host
// learns to write its own auth.Pages, so its example MUST embed
// d.CSRFField.
func TestAuthConfirmPageDocRendersCSRFField(t *testing.T) {
	section := sectionAfter(t, readDoc(t, "auth.md"), "To draw them some other way, implement `auth.Pages`", 2500)
	if !strings.Contains(section, "CSRFField") {
		t.Errorf("auth.md's auth.Pages example never names ConfirmPageData.CSRFField; " +
			"battery/auth/pages.go says a Pages implementation MUST render it, and " +
			"battery/auth/bff.go mounts CSRF app-wide under WithBFFPosture")
	}
}

// rejectCrossSiteForm now keys on isForgeableRequest, which also refuses
// text/plain and a bodyless fetch (no Content-Type). The doc must name
// text/plain: it is the third form enctype and needs no CORS preflight.
func TestAuthCrossSiteGuardDocCoversTextPlain(t *testing.T) {
	section := sectionAfter(t, readDoc(t, "auth.md"), "carry their own cross-site guard", 900)
	if !strings.Contains(section, "text/plain") {
		t.Errorf("auth.md scopes the cross-site guard to urlencoded/multipart only:\n%s\n"+
			"battery/auth/form_decode.go:isForgeableRequest also gates text/plain "+
			"and an absent Content-Type", strings.TrimSpace(section))
	}
}

// Two versions of one entity name share one table, so they must agree on
// who can reach its rows. The registry rejects a mismatch in MultiTenant,
// OwnerField or SoftDelete. The doc must name the rule, not a count that
// goes stale the next time a check is added.
func TestAPIVersioningDocNamesRowIsolation(t *testing.T) {
	doc := readDoc(t, "api-versioning.md")
	if strings.Contains(doc, "four structural invariants") {
		t.Error(`api-versioning.md still says "four structural invariants"; the count is ` +
			"volatile — describe the rule, and registry.go added a fifth " +
			"(MultiTenant/OwnerField/SoftDelete must agree across versions)")
	}
	for _, want := range []string{"MultiTenant", "OwnerField", "SoftDelete"} {
		if !strings.Contains(doc, want) {
			t.Errorf("api-versioning.md never mentions %s, but framework/registry.go "+
				"checkRowIsolationCompat now rejects registration when versions disagree about it", want)
		}
	}
}

// A PATTERN redirect that covers an EXACT screen panics at registration too,
// in either order (core-ui/app/router.go). The doc must not limit the panic
// to dynamic screens.
func TestRedirectDocCoversPatternOverExactScreen(t *testing.T) {
	section := sectionAfter(t, readDoc(t, "ui-getting-started.md"), "Because redirects are consulted", 900)
	if !strings.Contains(section, "exact screen") && !strings.Contains(section, "literal path") {
		t.Errorf("ui-getting-started.md still limits the overlap panic to a dynamic screen:\n%s\n"+
			"core-ui/app/router.go panics when a pattern redirect covers an EXACT screen too",
			strings.TrimSpace(section))
	}
}

// SSEBrokerConfig.Principal decides who may evict a subscriber id. With no
// Principal the broker evicts nothing (a subscriber id is not an identity).
// live-dashboards.md documents the config struct, so it must name the field.
func TestSSEBrokerPrincipalDocumented(t *testing.T) {
	doc := readDoc(t, "live-dashboards.md")
	if !strings.Contains(doc, "SSEBrokerConfig") {
		t.Fatal("live-dashboards.md no longer documents SSEBrokerConfig")
	}
	if !strings.Contains(doc, "Principal") {
		t.Error("live-dashboards.md documents SSEBrokerConfig but never the Principal field; " +
			"core/stream/sse_broker.go makes eviction opt-in and nil Principal evicts nothing")
	}
}

// A meta-delivered CSP ignores frame-ancestors (CSP L3 §3.1), so a static
// export to S3/GitHub Pages has no clickjacking guard unless the host reads
// _headers. The doc must not claim the meta carries "the same policy".
func TestSecurityDocStaticExportCSPCaveat(t *testing.T) {
	section := sectionAfter(t, readDoc(t, "security.md"), "### Static exports", 900)
	if !strings.Contains(section, "frame-ancestors") {
		t.Errorf("security.md's Static exports section omits the frame-ancestors caveat:\n%s\n"+
			"framework/static/builder.go says a meta-delivered policy ignores frame-ancestors",
			strings.TrimSpace(section))
	}
}

// NoQuery refuses every WIRE query surface. The in-process typed API
// (TypedQuery.Where) deliberately accepts caller-built conditions and
// returns stored values: read-modify-write and aggregates need them.
// security.md must not claim "every query surface" without that carve-out.
func TestSecurityDocNoQueryCarvesOutTypedAPI(t *testing.T) {
	sec := readDoc(t, "security.md")
	i := strings.Index(sec, "NoQuery")
	if i < 0 {
		t.Fatal("security.md no longer documents NoQuery")
	}
	section := sec[i:min(i+1200, len(sec))]
	if strings.Contains(section, "every query surface refuses it") {
		t.Error(`security.md still says "every query surface refuses it"; the in-process ` +
			"typed API (TypedQuery.Where) deliberately queries and returns stored values — " +
			"say \"every wire query surface\" and carve the typed API out")
	}
	if !strings.Contains(section, "TypedQuery") {
		t.Error("security.md's NoQuery section never names the TypedQuery carve-out")
	}
}

// RenderNode/RenderKind strip data-action*/data-param-*; the first-party
// pair RenderTrustedNode/RenderTrustedKind does not. The contract doc's
// noderender bullet must name the split (and that data-island is refused
// outright, it is the SSE swap target), or a first-party caller following
// it loses every action attribute.
func TestCoreUIArchitectureDocumentsTrustedRenderers(t *testing.T) {
	doc := readRepo(t, "core-ui/ARCHITECTURE.md")
	i := strings.Index(doc, "core-ui/noderender")
	if i < 0 {
		t.Fatal("core-ui/ARCHITECTURE.md has no core-ui/noderender bullet")
	}
	section := doc[i:min(i+1200, len(doc))]
	for _, want := range []string{"RenderTrustedNode", "RenderTrustedKind", "data-island"} {
		if !strings.Contains(section, want) {
			t.Errorf("core-ui/ARCHITECTURE.md's noderender bullet does not name %s; "+
				"the trust split is mandatory contract, and a first-party caller "+
				"loses action attributes without it", want)
		}
	}
	if !strings.Contains(section, "data-action") {
		t.Error("core-ui/ARCHITECTURE.md's noderender bullet does not name the " +
			"data-action* family that the untrusted entry point strips")
	}
}

// The in-process read surfaces that honour crud.WithReadHooks include the
// typed query: TypedQuery.Find runs AfterList and First runs AfterGet. The
// hooks doc must list them alongside ListAll/CountAll/GetOne.
func TestHooksDocReadSurfacesIncludeTypedQuery(t *testing.T) {
	doc := readDoc(t, "hooks-and-transactions.md")
	i := strings.Index(doc, "In-process reads")
	if i < 0 {
		t.Fatal("hooks-and-transactions.md no longer has an In-process reads row")
	}
	section := doc[i:min(i+600, len(doc))]
	if !strings.Contains(section, "Find") {
		t.Errorf("hooks-and-transactions.md lists in-process reads without TypedQuery.Find/First:\n%s\n"+
			"TypedQuery.Find runs AfterList and First runs AfterGet under crud.WithReadHooks",
			strings.TrimSpace(section))
	}
}
