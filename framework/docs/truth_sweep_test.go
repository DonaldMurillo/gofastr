package docs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The 2026-07-31 doc truth sweep corrected a batch of claims that
// contradicted the code. Each gate below pins one correction against
// ground truth (the code symbol, the real filename, or the absence of a
// phantom) so the lie cannot silently reappear.

// auth.md once fabricated a `framework/auth` package with argon2id
// primitives. framework/auth still does not exist (hashing lives in
// battery/auth/password.go), but argon2id is now a REAL opt-in hasher, so
// the doc may mention it ONLY when the code actually ships Argon2Hasher.
// Pin both: no phantom package, and any argon2 claim must be backed by the
// symbol — a fabricated claim with no symbol fails.
func TestAuthDocHashingClaimsMatchCode(t *testing.T) {
	doc := readDoc(t, "auth.md")
	if strings.Contains(doc, "framework/auth`") {
		t.Error("auth.md references a nonexistent framework/auth package; hashing lives in battery/auth/password.go")
	}
	if strings.Contains(doc, "argon2") {
		pw := readRepo(t, "battery/auth/password.go")
		if !strings.Contains(pw, "Argon2Hasher") {
			t.Error("auth.md mentions argon2 but battery/auth/password.go has no Argon2Hasher — the claim is fabricated")
		}
	}
	if _, err := os.Stat(filepath.Join("..", "auth")); err == nil {
		t.Error("a framework/auth package now exists — update this gate and the auth.md prose together")
	}
}

// security.md's CORS example listed AllowCredentials/MaxAge fields that
// CORSConfig does not have, and a Tracing(TracingConfig) signature that
// takes no argument. Pin both against the real struct/func.
func TestSecurityDocCORSAndTracingMatchCode(t *testing.T) {
	doc := readDoc(t, "security.md")
	for _, phantom := range []string{"AllowCredentials", "gofastr export", "Tracing(TracingConfig)"} {
		if strings.Contains(doc, phantom) {
			t.Errorf("security.md still references %q, which does not exist in the code", phantom)
		}
	}
	cors := readRepo(t, "core/middleware/cors.go")
	if strings.Contains(cors, "AllowCredentials") {
		t.Error("CORSConfig grew an AllowCredentials field — security.md's CORS example may now be correct; re-check this gate")
	}
}

// app.Use appends middleware; it does NOT disable the default chain.
// security.md used to claim it did.
func TestSecurityDocUseDoesNotDisableDefaults(t *testing.T) {
	doc := readDoc(t, "security.md")
	section := sectionAfter(t, doc, "WithoutDefaultMiddleware()", 400)
	if strings.Contains(section, "app.Use(...)` before") && strings.Contains(section, "disables it") {
		t.Error("security.md claims app.Use disables the default chain; App.Use only appends (framework/app.go)")
	}
}

// notifications.md claimed EmailChannel honours Extra["from"]; the channel
// rejects it as a spoofing vector.
func TestNotificationsDocFromOverrideRejected(t *testing.T) {
	code := readRepo(t, "battery/notify/email_channel.go")
	if !strings.Contains(code, `r.Extra["from"]`) || !strings.Contains(code, "not allowed") {
		t.Fatal("email_channel.go no longer rejects Extra[\"from\"] — re-verify notifications.md's claim")
	}
	doc := readDoc(t, "notifications.md")
	section := sectionAfter(t, doc, `Extra["from"]`, 120)
	if !strings.Contains(section, "rejected") {
		t.Error("notifications.md must state Extra[\"from\"] is rejected, not honoured (email_channel.go)")
	}
}

// X-Forwarded-Host is deliberately not honored (cache-poisoning); the
// well-known origin uses r.Host. agent-ready.md must not imply otherwise.
func TestAgentReadyDocDoesNotHonorForwardedHost(t *testing.T) {
	code := readRepo(t, "framework/wellknown.go")
	if !strings.Contains(code, "X-Forwarded-Host is NOT honored") {
		t.Fatal("wellknown.go changed its X-Forwarded-Host policy — re-verify agent-ready.md")
	}
	// Collapse whitespace so the assertion survives line-wrapping.
	doc := strings.Join(strings.Fields(readDoc(t, "agent-ready.md")), " ")
	if !strings.Contains(doc, "X-Forwarded-Host` is deliberately **not**") {
		t.Error("agent-ready.md must state X-Forwarded-Host is not honored (framework/wellknown.go)")
	}
}

// The seed ledger table is _gofastr_seeded, not seed_ledger.
func TestMigrationsDocNamesRealSeedLedger(t *testing.T) {
	doc := readDoc(t, "migrations.md")
	if strings.Contains(doc, "`seed_ledger`") {
		t.Error("migrations.md names the seed ledger seed_ledger; the real table is _gofastr_seeded (framework/seed.go)")
	}
}

// The shipped doc index must not embed AI-authoring artifacts.
func TestDocIndexHasNoAgentArtifacts(t *testing.T) {
	doc := readDoc(t, "README.md")
	for _, tell := range []string{"created by", "queue agent", "(page created"} {
		if strings.Contains(doc, tell) {
			t.Errorf("content/README.md embeds an AI-authoring artifact (%q) — it ships in the binary", tell)
		}
	}
}

// ── 2026-07-31 review round: docs that taught APIs the same release deleted ──

// widgets.md's opening quickstart taught Builder.RPCWithSignal and listed
// Definition.Bootstrap — both removed in 0.55.0. A quickstart that does not
// compile is the worst class of doc bug: it is the first code a new user runs.
func TestWidgetsDocDoesNotTeachDeletedBuilderAPI(t *testing.T) {
	doc := readDoc(t, "widgets.md")
	for _, gone := range []string{"RPCWithSignal", "BootstrapMode", "Definition.Bootstrap"} {
		if strings.Contains(doc, gone) {
			t.Errorf("widgets.md still teaches %q, deleted in 0.55.0", gone)
		}
	}
	src := readRepo(t, "core-ui/widget/widget.go")
	if strings.Contains(src, "func (b *Builder) RPCWithSignal") {
		t.Error("RPCWithSignal is back — restore the widgets.md prose alongside it")
	}
}

// Manager.AuthorizeTopic / OnPresenceChange became setters in 0.55.0 (the
// fields are unexported and stored atomically). presence.md is the SECURITY
// page for roster disclosure, so its remediation snippet in particular has
// to compile.
func TestPresenceDocsUseTheSetters(t *testing.T) {
	for _, name := range []string{"presence.md", "live-dashboards.md"} {
		doc := readDoc(t, name)
		for _, assignment := range []string{
			"AuthorizeTopic = func", "OnPresenceChange = func",
		} {
			if strings.Contains(doc, assignment) {
				t.Errorf("%s assigns to %q; the exported fields are gone — call SetAuthorizeTopic/SetOnPresenceChange", name, assignment)
			}
		}
	}
	mgr := readRepo(t, "core-ui/island/manager.go")
	for _, setter := range []string{"func (m *Manager) SetAuthorizeTopic", "func (m *Manager) SetOnPresenceChange"} {
		if !strings.Contains(mgr, setter) {
			t.Errorf("missing %s — the docs were rewritten to call it", setter)
		}
	}
}

// semantic-search.md's route table listed 200s with no mention of auth,
// while every route fails closed without a bearer token.
func TestSemanticDocStatesTheAuthRequirement(t *testing.T) {
	doc := readDoc(t, "semantic-search.md")
	for _, want := range []string{"WithAuthToken", "bearer token"} {
		if !strings.Contains(doc, want) {
			t.Errorf("semantic-search.md does not mention %q, but every route 401s without a token", want)
		}
	}
	routes := readRepo(t, "battery/semantic/routes.go")
	if !strings.Contains(routes, `writeErr(w, http.StatusUnauthorized, "authentication required")`) {
		t.Error("the semantic fail-closed path changed — recheck semantic-search.md's auth section")
	}
}

// RedisClient.RPop gained a mandatory sentinel contract: an adapter must map
// its driver's nil-sentinel onto queue.ErrRedisEmpty, or every empty poll
// becomes a backend error and the worker loop handles a zero-valued Job.
func TestQueueDocDocumentsTheEmptySentinel(t *testing.T) {
	doc := readDoc(t, "queue.md")
	if !strings.Contains(doc, "ErrRedisEmpty") {
		t.Error("queue.md tells you to write a RedisClient adapter without naming ErrRedisEmpty, which RPop must return for an empty list")
	}
	if !strings.Contains(readRepo(t, "battery/queue/redis.go"), "ErrRedisEmpty") {
		t.Error("ErrRedisEmpty is gone from battery/queue — update queue.md with it")
	}
}

// isolation.md told you to configure isolation in gofastr.yml. The file was
// renamed to gofastr.isolation.yml, which WINS the discovery order — so
// following the doc silently configured nothing.
func TestIsolationDocNamesTheRealConfigFile(t *testing.T) {
	doc := readDoc(t, "isolation.md")
	if strings.Contains(doc, "configured in `gofastr.yml`") {
		t.Error("isolation.md still points at gofastr.yml; gofastr.isolation.yml wins the discovery order")
	}
	if !strings.Contains(readRepo(t, "framework/isolation/isolation.go"), "gofastr.isolation.yml") {
		t.Error("the isolation config filename changed again — update isolation.md with it")
	}
}

// access-control.md used framework.Wildcard. Wildcard lives in
// framework/access and is not among the framework facade's re-exports.
func TestAccessControlDocUsesTheRealWildcardPath(t *testing.T) {
	doc := readDoc(t, "access-control.md")
	if strings.Contains(doc, "framework.Wildcard") {
		t.Error("access-control.md uses framework.Wildcard, which does not exist; Wildcard is in framework/access")
	}
	if strings.Contains(readRepo(t, "framework/reexports_access.go"), "Wildcard") {
		t.Error("Wildcard is now re-exported on the framework facade — access-control.md may use framework.Wildcard again")
	}
}
