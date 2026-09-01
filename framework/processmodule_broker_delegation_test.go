package framework

// Delegation-credential contract tests (issue #360). The original audit
// finding, kept as evidence: the broker's delegation snapshot persisted only
// 2 of the 4 credential headers the framework recognizes. framework/app.go
// credentialFingerprint enumerates Authorization, X-API-Key, the embed grant
// (X-Gofastr-Embed), and Cookie; crud.runToolRequest copied the first three
// plus Cookie from the request stashed via mcp.WithRequest; but
// Broker.MintDelegation stashed only Cookie (first header field only) and
// Authorization, and snapshotRequest rebuilt a request carrying just those. A
// delegated caller authenticated by API key or embed grant therefore
// re-resolved ANONYMOUS on every brokered host.entity.* re-dispatch, and a
// caller whose session cookie lived in a second Cookie header field lost it
// the same way. Fail-closed (the owner gate refuses), but the module-grant ∩
// caller-authority contract was broken for those callers: authority they
// legitimately hold never reached the reverse path. These tests pin the fixed
// contract: the snapshot carries EVERY field of EVERY credential header, and
// nothing else.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// delegationCredMiddleware mirrors how the framework's real auth surfaces
// resolve identity, one branch per credential:
//   - X-API-Key (battery/auth apitoken),
//   - the embed grant header, whose verdict is FINAL when presented
//     (framework/embed middleware: a grant suppresses ambient cookies),
//   - session cookies, scanning EVERY Cookie header field — the exact
//     multi-field lesson app.go:888 documents for credentialFingerprint.
//
// Every authenticated caller gets the "owner" role so the entity's
// Access.Read permission resolves; identity (and therefore owner scope)
// still comes from the user alone.
//
// saw, when non-nil, receives a clone of the raw inbound headers of every
// request that reaches the middleware — the instrument the no-extra-authority
// test uses to inspect exactly what a brokered re-dispatch carried.
func delegationCredMiddleware(policy *access.RolePolicy, saw func(http.Header)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if saw != nil {
				saw(r.Header.Clone())
			}
			ctx := r.Context()
			if policy != nil {
				ctx = access.WithPolicy(ctx, policy)
			}
			authenticated := false
			if k := r.Header.Get("X-API-Key"); k != "" {
				ctx = handler.SetUser(ctx, &brokerTestUser{id: "keyuser-" + k})
				authenticated = true
			}
			if g := r.Header.Get("X-Gofastr-Embed"); g != "" {
				ctx = handler.SetUser(ctx, &brokerTestUser{id: "embeduser-" + g})
				authenticated = true
			} else if !authenticated {
				for _, field := range r.Header.Values("Cookie") {
					for _, part := range strings.Split(field, ";") {
						part = strings.TrimSpace(part)
						if sid, ok := strings.CutPrefix(part, "sid="); ok && sid != "" {
							ctx = handler.SetUser(ctx, &brokerTestUser{id: sid})
							authenticated = true
						}
					}
				}
			}
			if authenticated {
				ctx = access.WithRoles(ctx, []string{"owner"})
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// newDelegationCredEnv builds a real CRUD chokepoint (owner-scoped entity +
// Access.Read permission) behind delegationCredMiddleware, wired into a
// Broker exactly like newCrudBrokerEnv but with the multi-credential
// middleware. saw is passed through to the middleware (nil = no instrument).
func newDelegationCredEnv(t *testing.T, saw func(http.Header)) (*Broker, *access.RolePolicy) {
	t.Helper()
	brokerInstallOwnerExtractor(t)
	db := brokerSetupDB(t, `CREATE TABLE logs (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, subject TEXT)`)
	ent := brokerEntity("logs", "logs", func(c *entity.EntityConfig) {
		c.Exposure.Access.Read = "logs:read"
	})
	ent.SetDB(db)
	reg := brokerRegistry(ent)
	ch := crud.NewCrudHandler(ent, db)
	ch.Registry = reg
	ch.JSONCase = crud.CaseSnake
	inner := router.New()
	crud.RegisterCrudRoutes(inner, ch, "/logs", crud.CrudRouteOptions{NoLLMMD: true})
	policy := access.NewRolePolicy()
	if err := policy.Grant("owner", "logs:read"); err != nil {
		t.Fatal(err)
	}
	b := NewBroker(delegationCredMiddleware(policy, saw)(inner), reg, nil, "", WithBrokerPolicy(policy))
	return b, policy
}

// mintDelegationFor mints a handle from a request shaped by shape, bound to
// module "m" the way the supervisor's proxy does (withDelegationModule).
func mintDelegationFor(t *testing.T, b *Broker, shape func(*http.Request)) (string, func()) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/m/logs", nil)
	if shape != nil {
		shape(req)
	}
	req = req.WithContext(withDelegationModule(req.Context(), "m"))
	return b.MintDelegation(req, 1)
}

// TestBrokerDelegationReattachesAPIKeyCredential: a caller authenticated by
// X-API-Key holds logs:read and owns a row. The direct REST path returns the
// row. The brokered reverse host.entity.query call — minted from THAT EXACT
// request — must equally re-attach the caller and return the row.
//
// The gap spanned BOTH layers: crud.runToolRequest's re-injection list
// (crud/mcp.go) copied Cookie, Authorization, and the embed grant but NOT
// X-API-Key, and the broker's snapshot (MintDelegation + snapshotRequest)
// carried only Cookie + Authorization. Neither layer alone preserved an API
// key; the direct REST control is the honest baseline.
func TestBrokerDelegationReattachesAPIKeyCredential(t *testing.T) {
	b, _ := newDelegationCredEnv(t, nil)
	logsEnt, err := b.entities.Get("logs")
	if err != nil {
		t.Fatal(err)
	}
	brokerSeedRow(t, logsEnt.DB, "logs", "l-1", "keyuser-ak-alice", "Alpha")

	shape := func(r *http.Request) { r.Header.Set("X-API-Key", "ak-alice") }

	// Control: the direct REST path authenticates the API-key caller.
	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	shape(req)
	rec := httptest.NewRecorder()
	b.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("control: direct REST request with X-API-Key: status %d: %s", rec.Code, rec.Body.String())
	}

	handle, release := mintDelegationFor(t, b, shape)
	defer release()
	h := b.entityHandler(ModuleGrantView{Name: "m", Grants: []access.Permission{"logs:read"}}, opQuery)
	res, err := callReverse(t, h, moduleproto.EntityQueryParams{
		Entity: "logs",
		Caller: moduleproto.CallerRef{Delegation: handle},
	})
	if err != nil {
		t.Fatalf("FINDING: brokered reverse query for an API-key caller was denied even though the caller holds logs:read and owns the row: %v", err)
	}
	qr, ok := res.(moduleproto.EntityQueryResult)
	if !ok || qr.Total != 1 {
		t.Fatalf("brokered query result = %#v, want the caller's 1 owner-scoped row", res)
	}
}

// TestBrokerDelegationReattachesEmbedGrant: same shape for a caller whose
// credential is the embed grant header (X-Gofastr-Embed). crud/mcp.go copied
// this header specifically ("the embed grant is a credential like the two
// above"), so only the broker's snapshot layer dropped it.
func TestBrokerDelegationReattachesEmbedGrant(t *testing.T) {
	b, _ := newDelegationCredEnv(t, nil)
	logsEnt, err := b.entities.Get("logs")
	if err != nil {
		t.Fatal(err)
	}
	brokerSeedRow(t, logsEnt.DB, "logs", "l-1", "embeduser-emg-alice", "Alpha")

	shape := func(r *http.Request) { r.Header.Set("X-Gofastr-Embed", "emg-alice") }

	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	shape(req)
	rec := httptest.NewRecorder()
	b.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("control: direct REST request with embed grant: status %d: %s", rec.Code, rec.Body.String())
	}

	// Control 2: crud.Redispatch DOES re-inject the embed grant when the
	// originating request is stashed on ctx (crud/mcp.go exists for exactly
	// this credential) — proving the re-dispatch machinery supports it and
	// the broker's delegation snapshot was the layer dropping it.
	orig := httptest.NewRequest(http.MethodGet, "/logs", nil)
	shape(orig)
	if _, err := crud.Redispatch(mcp.WithRequest(context.Background(), orig), b.router, http.MethodGet, "/logs", nil); err != nil {
		t.Fatalf("control: redispatch with stashed embed-grant request failed: %v", err)
	}

	handle, release := mintDelegationFor(t, b, shape)
	defer release()
	h := b.entityHandler(ModuleGrantView{Name: "m", Grants: []access.Permission{"logs:read"}}, opQuery)
	if _, err := callReverse(t, h, moduleproto.EntityQueryParams{
		Entity: "logs",
		Caller: moduleproto.CallerRef{Delegation: handle},
	}); err != nil {
		t.Fatalf("FINDING: brokered reverse query for an embed-grant caller was denied even though the caller holds logs:read and owns the row: %v", err)
	}
}

// TestBrokerDelegationPreservesEveryCookieField: Cookie routinely arrives as
// SEVERAL header fields (a proxy prepends its own; app.go:889-896 exists
// precisely because Get() sees only the first). MintDelegation used
// Header.Get("Cookie"), so a session cookie living in the second field was
// dropped from the snapshot; the brokered re-dispatch re-resolved anonymous.
func TestBrokerDelegationPreservesEveryCookieField(t *testing.T) {
	b, _ := newDelegationCredEnv(t, nil)
	logsEnt, err := b.entities.Get("logs")
	if err != nil {
		t.Fatal(err)
	}
	brokerSeedRow(t, logsEnt.DB, "logs", "l-1", "alice", "Alpha")

	shape := func(r *http.Request) {
		r.Header.Add("Cookie", "edge=blue") // proxy-prepended field
		r.Header.Add("Cookie", "sid=alice") // the session, second field
	}

	// Control: the middleware scans every Cookie field, request succeeds.
	req := httptest.NewRequest(http.MethodGet, "/logs", nil)
	shape(req)
	rec := httptest.NewRecorder()
	b.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("control: direct REST request with two Cookie fields: status %d: %s", rec.Code, rec.Body.String())
	}

	handle, release := mintDelegationFor(t, b, shape)
	defer release()
	h := b.entityHandler(ModuleGrantView{Name: "m", Grants: []access.Permission{"logs:read"}}, opQuery)
	if _, err := callReverse(t, h, moduleproto.EntityQueryParams{
		Entity: "logs",
		Caller: moduleproto.CallerRef{Delegation: handle},
	}); err != nil {
		t.Fatalf("FINDING: brokered reverse query lost a session cookie that lived in a second Cookie header field: %v", err)
	}
}

// TestBrokerDelegationSnapshotCarriesEveryMintedCredentialField pins the
// snapshot's shape directly, at the MintDelegation → snapshotRequest seam:
// every field of every credential header survives (multi-field Cookie in
// field order, X-API-Key, the embed grant), and NOTHING beyond the four
// credential names rides along. The X-Internal-Debug header stands in for
// any non-credential header the minting request happened to carry; the
// snapshot re-presents the caller's authority, never the request's ambient
// headers.
//
// The second half is the clone guard: re-dispatch middleware mutates request
// headers (framework/embed's grant path deletes Cookie/Authorization/
// X-API-Key once the grant verdict is final), so a snapshot sharing the
// stashed header map would let one re-dispatch's verdict erase the
// credentials of every LATER reverse call on the same handle.
func TestBrokerDelegationSnapshotCarriesEveryMintedCredentialField(t *testing.T) {
	b := NewBroker(nil, nil, nil, "")
	handle, release := b.MintDelegation(httptest.NewRequest(http.MethodGet, "/m/anything", nil), 1)
	defer release()

	minted := httptest.NewRequest(http.MethodGet, "/m/logs", nil)
	minted.Header.Add("Cookie", "edge=blue")
	minted.Header.Add("Cookie", "sid=alice")
	minted.Header.Set("Authorization", "Bearer jwt-alice")
	minted.Header.Set("X-API-Key", "ak-alice")
	minted.Header.Set("X-Gofastr-Embed", "emg-alice")
	minted.Header.Set("X-Internal-Debug", "1")
	handle2, release2 := b.MintDelegation(minted, 2)
	defer release2()

	b.mu.Lock()
	entry := b.handles[handle2]
	b.mu.Unlock()

	snap := snapshotRequest(entry)

	if got := snap.Header.Get("X-API-Key"); got != "ak-alice" {
		t.Fatalf("snapshot X-API-Key = %q, want the minted API key (issue #360: this credential was dropped)", got)
	}
	if got := snap.Header.Get("X-Gofastr-Embed"); got != "emg-alice" {
		t.Fatalf("snapshot embed grant = %q, want the minted grant (issue #360: this credential was dropped)", got)
	}
	if got := snap.Header.Get("Authorization"); got != "Bearer jwt-alice" {
		t.Fatalf("snapshot Authorization = %q, want the minted bearer", got)
	}
	fields := snap.Header.Values("Cookie")
	if len(fields) != 2 || fields[0] != "edge=blue" || fields[1] != "sid=alice" {
		t.Fatalf("snapshot Cookie fields = %q, want both minted fields in order", fields)
	}
	if got := snap.Header.Get("X-Internal-Debug"); got != "" {
		t.Fatalf("snapshot carried non-credential header X-Internal-Debug = %q: a snapshot must never carry more than the caller's credentials", got)
	}
	if n := len(snap.Header); n != 4 {
		t.Fatalf("snapshot carries %d header names (%v), want exactly the 4 credential names", n, snap.Header)
	}

	// The ambient (nil-request) handle still snapshots to a request with no
	// credentials at all — ambient stays caller-less.
	b.mu.Lock()
	ambient := b.handles[handle]
	b.mu.Unlock()
	if n := len(snapshotRequest(ambient).Header); n != 0 {
		t.Fatalf("ambient snapshot carries %d header names, want none", n)
	}

	// Clone guard: mutate the snapshot the way re-dispatch middleware would
	// (embed's grant path deletes the competing credential headers), then
	// re-snapshot the same entry and require the credentials intact.
	snap.Header.Del("Cookie")
	snap.Header.Set("Authorization", "Bearer mutated")
	again := snapshotRequest(entry)
	if got := again.Header.Get("Authorization"); got != "Bearer jwt-alice" {
		t.Fatalf("a re-dispatch's header mutation poisoned the stashed entry: Authorization = %q, want %q", got, "Bearer jwt-alice")
	}
	if fields := again.Header.Values("Cookie"); len(fields) != 2 {
		t.Fatalf("a re-dispatch's header mutation poisoned the stashed entry: Cookie fields = %q, want both", fields)
	}
}

// TestBrokerDelegationSnapshotGrantsNoExtraAuthority pins the fail-closed
// direction of the fix: the snapshot carries no authority the minting request
// did not present. A cookie-only caller must re-dispatch with exactly their
// cookie — no Authorization, X-API-Key, or embed grant appears on the
// re-dispatch, no non-credential header rides along, and the owner scope
// stays the cookie user's: a row owned by an identity reachable only via a
// credential the caller did NOT present (ak-stolen) stays invisible.
func TestBrokerDelegationSnapshotGrantsNoExtraAuthority(t *testing.T) {
	var mu sync.Mutex
	var saw http.Header
	record := func(h http.Header) { mu.Lock(); saw = h; mu.Unlock() }

	b, _ := newDelegationCredEnv(t, record)
	logsEnt, err := b.entities.Get("logs")
	if err != nil {
		t.Fatal(err)
	}
	brokerSeedRow(t, logsEnt.DB, "logs", "l-mine", "alice", "Alpha")
	brokerSeedRow(t, logsEnt.DB, "logs", "l-not-mine", "keyuser-ak-stolen", "Stolen")

	// Mint from a cookie-only request that also carries a non-credential
	// header: neither the credentials the caller lacks nor the ambient
	// header may reach the re-dispatch.
	handle, release := mintDelegationFor(t, b, func(r *http.Request) {
		r.Header.Add("Cookie", "sid=alice")
		r.Header.Set("X-Internal-Debug", "1")
	})
	defer release()

	h := b.entityHandler(ModuleGrantView{Name: "m", Grants: []access.Permission{"logs:read"}}, opQuery)
	res, err := callReverse(t, h, moduleproto.EntityQueryParams{
		Entity: "logs",
		Caller: moduleproto.CallerRef{Delegation: handle},
	})
	if err != nil {
		t.Fatalf("brokered query for the cookie caller failed: %v", err)
	}
	qr, ok := res.(moduleproto.EntityQueryResult)
	if !ok || qr.Total != 1 {
		t.Fatalf("brokered query result = %#v, want exactly the caller's own row — a credential she never held must not unlock ak-stolen's row", res)
	}

	mu.Lock()
	defer mu.Unlock()
	if saw == nil {
		t.Fatal("the re-dispatch never reached the credentialed router — nothing was observed")
	}
	if got := saw.Values("Cookie"); len(got) != 1 || got[0] != "sid=alice" {
		t.Fatalf("re-dispatch Cookie fields = %q, want exactly the minted session field", got)
	}
	for _, name := range []string{"Authorization", "X-API-Key", "X-Gofastr-Embed", "X-Internal-Debug"} {
		if v := saw.Get(name); v != "" {
			t.Fatalf("re-dispatch carried %s = %q, which the minting request never presented: the snapshot granted extra authority", name, v)
		}
	}
}
