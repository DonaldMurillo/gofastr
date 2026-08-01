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

// readRepo loads a repo-relative file (CHANGELOG.md, core-ui/ARCHITECTURE.md)
// so a gate can pin a claim that lives outside the embedded doc corpus.
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

// ConfirmPageData.CSRFField exists because a custom ConfirmPage rendered no
// CSRF field and its only button 403'd under auth.CSRF() (WithBFFPosture
// mounts that app-wide). auth.md is the one place a host learns to write a
// custom ConfirmPage, so its example MUST embed d.CSRFField.
func TestAuthConfirmPageDocRendersCSRFField(t *testing.T) {
	section := sectionAfter(t, readDoc(t, "auth.md"), "MagicLinkConfig.ConfirmPage", 2000)
	if !strings.Contains(section, "CSRFField") {
		t.Errorf("auth.md's ConfirmPage example never names ConfirmPageData.CSRFField; " +
			"battery/auth/magiclink.go says a custom ConfirmPage MUST render it, and " +
			"battery/auth/bff.go mounts CSRF app-wide under WithBFFPosture")
	}
}

// rejectCrossSiteForm now keys on isForgeableRequest, which also refuses
// text/plain and a bodyless fetch (no Content-Type). The doc must name
// text/plain — it is the third form enctype and needs no CORS preflight.
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
// returns stored values — read-modify-write and aggregates need them.
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
// outright — it is the SSE swap target), or a first-party caller following
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

// CHANGELOG.md promises breaking changes are marked **BREAKING**, and
// cmd/gofastr/upgrades.yml records the same releases for `gofastr upgrade`.
// Those two must agree: the changelog section is what feeds
// `gh release create --notes-file`, and the registry is what an upgrading
// project actually reads. A release marked breaking in one and not the other
// means somebody reads the wrong story.
//
// This replaced a gate pinned to the Unreleased heading, which stopped
// looking the moment the section was rolled to its version number — the one
// commit in a release where the marker matters most.
func TestChangelogAgreesWithUpgradesOnBreaking(t *testing.T) {
	breakingByVersion := upgradeRegistryBreaking(t)

	for _, ver := range changelogVersions(t) {
		known, ok := breakingByVersion[ver.version]
		if !ok {
			// A release with no migration-relevant change gets no
			// upgrades.yml entry; that is the documented shape, and it is
			// also how a release with no breaking change looks.
			known = false
		}
		marked := marksBreaking(ver.body)
		switch {
		case known && !marked:
			t.Errorf("cmd/gofastr/upgrades.yml marks %s breaking, but its CHANGELOG.md "+
				"section never says BREAKING — the release notes come from that section, "+
				"so an upgrading reader is not warned", ver.version)
		case marked && !known:
			t.Errorf("CHANGELOG.md marks %s BREAKING, but cmd/gofastr/upgrades.yml has no "+
				"breaking note for it — `gofastr upgrade` will not surface the change. "+
				"upgrades.yml's own maintenance rule: a release that lands BREAKING "+
				"changes adds its entry there", ver.version)
		}
	}
}

// marksBreaking reports whether a changelog section flags a breaking change.
//
// The marker is the all-caps word, which the file spells four ways across its
// history — `**BREAKING:` at the head of an entry, `BREAKING.**` at the tail
// of one, a `### BREAKING` heading, and plain prose ("Most entries below are
// BREAKING"). Matching the word covers all four. Two uses are not markers and
// are removed first: a section stating it has none, and the v0.26.1 entry
// describing the release process's own "release/BREAKING section".
func marksBreaking(body string) bool {
	for _, notAMarker := range []string{"No BREAKING", "no BREAKING", "release/BREAKING"} {
		body = strings.ReplaceAll(body, notAMarker, "")
	}
	return strings.Contains(body, "BREAKING")
}

type changelogSection struct {
	version string // "v0.52.0"
	body    string
}

// changelogVersions returns every `## [X.Y.Z]` section of CHANGELOG.md with
// its body. `## [Unreleased]` is skipped: it carries no version to compare
// against, and it is empty between releases.
func changelogVersions(t *testing.T) []changelogSection {
	t.Helper()
	lines := strings.Split(readRepo(t, "CHANGELOG.md"), "\n")
	var out []changelogSection
	cur := -1
	for _, line := range lines {
		if after, ok := strings.CutPrefix(line, "## ["); ok {
			name, _, ok := strings.Cut(after, "]")
			if !ok || name == "Unreleased" {
				cur = -1
				continue
			}
			out = append(out, changelogSection{version: "v" + name})
			cur = len(out) - 1
			continue
		}
		if cur >= 0 {
			out[cur].body += line + "\n"
		}
	}
	if len(out) == 0 {
		t.Fatal("CHANGELOG.md has no `## [X.Y.Z]` sections")
	}
	return out
}

// upgradeRegistryBreaking reports, per release in cmd/gofastr/upgrades.yml,
// whether any of its notes is `breaking: true`. The file is deliberately
// simple YAML (its header says the parser has no block scalars), so a line
// scan reads it the same way the CLI's does.
func upgradeRegistryBreaking(t *testing.T) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	cur := ""
	for line := range strings.SplitSeq(readRepo(t, "cmd/gofastr/upgrades.yml"), "\n") {
		trimmed := strings.TrimSpace(line)
		if v, ok := strings.CutPrefix(trimmed, "- version:"); ok {
			cur = strings.TrimSpace(v)
			out[cur] = false
			continue
		}
		if cur != "" && trimmed == "breaking: true" {
			out[cur] = true
		}
	}
	if len(out) == 0 {
		t.Fatal("cmd/gofastr/upgrades.yml has no `- version:` entries")
	}
	return out
}
