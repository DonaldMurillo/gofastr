package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// These tests are the §10 capability-containment gate (design #37 §5). Each
// names ONE property a module CANNOT obtain through the reverse channel and
// proves it. The broker handlers are invoked the way moduleproto.Peer.
// serveRequest dispatches them: a bare context.Background() (the codec's
// inbound ctx carries NO app state — the broker must reconstruct every caller
// signal from the delegation handle, which is the whole point of the seam).
//
// The gate-only deny tests use an ALWAYS-SUCCEED fake router: if the gate
// failed to deny, the call would reach the router and return data, so an
// observed denial + the router never being hit is positive proof the gate
// fired (not a downstream nil-router refusal). A nil router would deny too
// and mask a missing gate — exactly the regression these tests must catch.

// ----- test doubles -----

type brokerTestUser struct{ id string }

func (u *brokerTestUser) GetID() string { return u.id }

func brokerInstallOwnerExtractor(t *testing.T) {
	t.Helper()
	prev := owner.GetExtractor()
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		raw, ok := handler.GetUser(ctx)
		if !ok || raw == nil {
			return nil, false
		}
		if u, ok := raw.(*brokerTestUser); ok {
			return u.GetID(), true
		}
		return nil, false
	})
	t.Cleanup(func() { owner.SetExtractor(prev) })
}

func brokerSetupDB(t *testing.T, ddl string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	return db
}

func brokerSeedRow(t *testing.T, db *sql.DB, table, id, ownerID, subject string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO `+table+` (id, user_id, subject) VALUES (?, ?, ?)`,
		id, ownerID, subject); err != nil {
		t.Fatal(err)
	}
}

func brokerEntity(name, table string, configure func(*entity.EntityConfig)) *entity.Entity {
	cfg := entity.EntityConfig{
		Table:      table,
		Fields:     []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "subject", Type: schema.String}},
		OwnerField: "user_id",
	}
	if configure != nil {
		configure(&cfg)
	}
	return entity.Define(name, cfg.WithTimestamps(false))
}

func brokerRegistry(ents ...*entity.Entity) *Registry {
	r := NewRegistry()
	for _, e := range ents {
		if err := r.Register(e); err != nil {
			panic(err)
		}
	}
	return r
}

// fakeSuccessRouter is an http.Handler that ALWAYS returns 200 + a one-row
// envelope and records that it was reached. A gate that fails to deny lets the
// call through to here — so (denial && !hit) is positive proof the gate fired.
func fakeSuccessRouter(hit *atomic.Bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hit.Store(true)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"LEAKED"}],"total":1}`))
	})
}

// brokerAuthMiddleware mirrors what battery/auth installs: policy always,
// plus the caller's user id + roles resolved from the re-injected session
// cookie. The broker re-injects Cookie via the delegation snapshot, so this is
// where the caller-authority half of module-grant ∩ caller-authority resolves.
func brokerAuthMiddleware(policy *access.RolePolicy, rolesFor func(string) []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			if policy != nil {
				ctx = access.WithPolicy(ctx, policy)
			}
			if c := r.Header.Get("Cookie"); strings.HasPrefix(c, "sid=") {
				sid := strings.TrimPrefix(c, "sid=")
				ctx = handler.SetUser(ctx, &brokerTestUser{id: sid})
				if rolesFor != nil {
					if roles := rolesFor(sid); len(roles) > 0 {
						ctx = access.WithRoles(ctx, roles)
					}
				}
			}
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func newCrudBrokerEnv(t *testing.T, name, table string, configure func(*entity.EntityConfig), policy *access.RolePolicy, rolesFor func(string) []string) (*Broker, *access.RolePolicy) {
	t.Helper()
	brokerInstallOwnerExtractor(t)
	db := brokerSetupDB(t, `CREATE TABLE `+table+` (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, subject TEXT)`)
	ent := brokerEntity(name, table, configure)
	ent.SetDB(db)
	reg := brokerRegistry(ent)
	ch := crud.NewCrudHandler(ent, db)
	ch.Registry = reg
	ch.JSONCase = crud.CaseSnake
	inner := router.New()
	crud.RegisterCrudRoutes(inner, ch, "/"+table, crud.CrudRouteOptions{NoLLMMD: true})
	if policy == nil {
		policy = access.NewRolePolicy()
	}
	wrapped := brokerAuthMiddleware(policy, rolesFor)(inner)
	broker := NewBroker(wrapped, reg, nil, "", WithBrokerPolicy(policy))
	return broker, policy
}

func brokerMint(t *testing.T, b *Broker, policy *access.RolePolicy, sid string, roles []string) (string, func()) {
	t.Helper()
	mintReq := httptest.NewRequest(http.MethodGet, "/proxy/"+sid, nil)
	ctx := mintReq.Context()
	if policy != nil {
		ctx = access.WithPolicy(ctx, policy)
	}
	if sid != "" {
		mintReq.Header.Set("Cookie", "sid="+sid)
		ctx = handler.SetUser(ctx, &brokerTestUser{id: sid})
	}
	if len(roles) > 0 {
		ctx = access.WithRoles(ctx, roles)
	}
	return b.MintDelegation(mintReq.WithContext(ctx), 1)
}

func callReverse(t *testing.T, h moduleproto.Handler, params any) (any, error) {
	t.Helper()
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}
	return h(context.Background(), raw) // bare ctx — mimics serveRequest
}

func wantDenied(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected capability denial, got nil error")
	}
	we := moduleproto.AsError(err)
	if we == nil {
		t.Fatalf("expected *moduleproto.Error denial, got %T: %v", err, err)
	}
	if we.Code != moduleproto.CodeCapabilityDenied {
		t.Errorf("code = %d (%q), want CodeCapabilityDenied", we.Code, we.Message)
	}
}

func wantNotHit(t *testing.T, hit *atomic.Bool) {
	t.Helper()
	if hit.Load() {
		t.Fatalf("gate leaked the call to the router — the deny did NOT fire upstream")
	}
}

func wantOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("expected success, got denial: %v", err)
	}
}

// ----- §5 adversarial gate tests (deny before re-dispatch) -----

// TestBrokerGrantScopeMatchDenies: a module CANNOT exceed descriptor.requested
// ∩ operator.approved. Granted articles:write, asking articles:read → the
// ScopeMatch pre-filter denies and the router is NEVER reached.
func TestBrokerGrantScopeMatchDenies(t *testing.T) {
	reg := brokerRegistry(brokerEntity("articles", "articles", nil))
	var hit atomic.Bool
	b := NewBroker(fakeSuccessRouter(&hit), reg, nil, "")
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"articles:write"}}
	h := b.entityHandler(view, opQuery)
	_, err := callReverse(t, h, moduleproto.EntityQueryParams{Entity: "articles"})
	wantDenied(t, err)
	wantNotHit(t, &hit)
}

// TestBrokerIgnoresChildCapability: a module CANNOT name its own permission
// label. The derived required perm comes from the trusted method + canonical
// resource ONLY. Holding an unrelated grant does not satisfy articles:read and
// the router is never reached; a child cannot inject a capability string (the
// params have no such field and the handler reads none).
func TestBrokerIgnoresChildCapability(t *testing.T) {
	reg := brokerRegistry(brokerEntity("articles", "articles", nil))
	var hit atomic.Bool
	b := NewBroker(fakeSuccessRouter(&hit), reg, nil, "")
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"unrelated:read"}}
	h := b.entityHandler(view, opQuery)
	_, err := callReverse(t, h, moduleproto.EntityQueryParams{Entity: "articles"})
	wantDenied(t, err)
	wantNotHit(t, &hit)
}

// TestBrokerStripsCrossOwnerRead: a module CANNOT obtain CrossOwnerRead by
// riding a delegated caller who legitimately holds it. The entity opts in; the
// caller's role holds the perm; the broker gate denies BEFORE re-dispatch and
// the router is never reached.
func TestBrokerStripsCrossOwnerRead(t *testing.T) {
	reg := brokerRegistry(brokerEntity("tix", "tix", func(c *entity.EntityConfig) {
		c.CrossOwnerRead = "tix:read:all"
	}))
	policy := access.NewRolePolicy()
	if err := policy.Grant("staff", "tix:read", "tix:read:all"); err != nil {
		t.Fatal(err)
	}
	var hit atomic.Bool
	b := NewBroker(fakeSuccessRouter(&hit), reg, nil, "")
	handle, release := brokerMint(t, b, policy, "staff-user", []string{"staff"})
	defer release()
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"tix:read"}}
	h := b.entityHandler(view, opQuery)
	_, err := callReverse(t, h, moduleproto.EntityQueryParams{
		Entity: "tix",
		Caller: moduleproto.Caller{Delegation: handle},
	})
	if err == nil {
		t.Fatal("expected CrossOwnerRead carve-out denial, got nil")
	}
	we := moduleproto.AsError(err)
	if we == nil || !strings.Contains(we.Message, "CrossOwnerRead") {
		t.Errorf("denial must name CrossOwnerRead, got: %v", err)
	}
	wantNotHit(t, &hit)
}

// TestBrokerEntityOutsideGrantDenies: a module CANNOT reach an entity outside
// its grant set. Granted articles:*; asking secrets:read → denied, router
// never reached.
func TestBrokerEntityOutsideGrantDenies(t *testing.T) {
	reg := brokerRegistry(
		brokerEntity("articles", "articles", nil),
		brokerEntity("secrets", "secrets", nil),
	)
	var hit atomic.Bool
	b := NewBroker(fakeSuccessRouter(&hit), reg, nil, "")
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"articles:*"}}
	h := b.entityHandler(view, opQuery)
	_, err := callReverse(t, h, moduleproto.EntityQueryParams{Entity: "secrets"})
	wantDenied(t, err)
	wantNotHit(t, &hit)
}

// TestBrokerUnknownEntityDenies: an entity name the registry does not know is
// denied (fail-closed) — never silently empty, and never reaches the router.
func TestBrokerUnknownEntityDenies(t *testing.T) {
	reg := brokerRegistry(brokerEntity("articles", "articles", nil))
	var hit atomic.Bool
	b := NewBroker(fakeSuccessRouter(&hit), reg, nil, "")
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"*:read"}} // grant matches any resource
	h := b.entityHandler(view, opQuery)
	_, err := callReverse(t, h, moduleproto.EntityQueryParams{Entity: "nope"})
	wantDenied(t, err)
	wantNotHit(t, &hit)
}

// ----- delegation handle lifecycle -----

// TestBrokerUnknownHandleDenies: a reverse call echoing a handle the broker
// never minted is denied; the router is never reached.
func TestBrokerUnknownHandleDenies(t *testing.T) {
	reg := brokerRegistry(brokerEntity("articles", "articles", nil))
	var hit atomic.Bool
	b := NewBroker(fakeSuccessRouter(&hit), reg, nil, "")
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"articles:read"}}
	h := b.entityHandler(view, opQuery)
	_, err := callReverse(t, h, moduleproto.EntityQueryParams{
		Entity: "articles",
		Caller: moduleproto.Caller{Delegation: "deadbeef-not-a-real-handle"},
	})
	wantDenied(t, err)
	wantNotHit(t, &hit)
}

// TestBrokerReleasedHandleDenies: after release() the handle is gone — a late
// reverse call echoing it is denied and never reaches the router.
func TestBrokerReleasedHandleDenies(t *testing.T) {
	reg := brokerRegistry(brokerEntity("articles", "articles", nil))
	var hit atomic.Bool
	b := NewBroker(fakeSuccessRouter(&hit), reg, nil, "")
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"articles:read"}}
	handle, release := brokerMint(t, b, access.NewRolePolicy(), "alice", []string{"x"})
	release() // parent module.http returned; handle purged
	h := b.entityHandler(view, opQuery)
	_, err := callReverse(t, h, moduleproto.EntityQueryParams{
		Entity: "articles",
		Caller: moduleproto.Caller{Delegation: handle},
	})
	wantDenied(t, err)
	wantNotHit(t, &hit)
}

// TestBrokerExpiredHandleDenies: a leaked-but-unreleased handle is bounded by
// its TTL; after expiry the broker denies (and purges) it.
func TestBrokerExpiredHandleDenies(t *testing.T) {
	reg := brokerRegistry(brokerEntity("articles", "articles", nil))
	var hit atomic.Bool
	now := time.Now()
	b := NewBroker(fakeSuccessRouter(&hit), reg, nil, "",
		WithBrokerHandleTTL(time.Minute),
		WithBrokerClock(func() time.Time { return now }))
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"articles:read"}}
	handle, release := brokerMint(t, b, access.NewRolePolicy(), "alice", []string{"x"})
	defer release()
	b.now = func() time.Time { return now.Add(2 * time.Minute) } // past TTL
	h := b.entityHandler(view, opQuery)
	_, err := callReverse(t, h, moduleproto.EntityQueryParams{
		Entity: "articles",
		Caller: moduleproto.Caller{Delegation: handle},
	})
	wantDenied(t, err)
	wantNotHit(t, &hit)
}

// ----- re-dispatch caller-authority (real CRUD chokepoint) -----

// TestBrokerCallerAuthorityDenies: a module CANNOT exceed the delegated
// caller's authority. Module grant allows logs:read; the caller's role does
// NOT hold logs:read → requirePermission 403 → broker surfaces a denial.
func TestBrokerCallerAuthorityDenies(t *testing.T) {
	policy := access.NewRolePolicy()
	if err := policy.Grant("member", access.Permission("other:read")); err != nil {
		t.Fatal(err) // notably NOT logs:read
	}
	b, _ := newCrudBrokerEnv(t, "logs", "logs", func(c *entity.EntityConfig) {
		c.Access.Read = "logs:read"
	}, policy, func(string) []string { return []string{"member"} })
	handle, release := brokerMint(t, b, policy, "alice", []string{"member"})
	defer release()
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"logs:read"}}
	h := b.entityHandler(view, opQuery)
	_, err := callReverse(t, h, moduleproto.EntityQueryParams{
		Entity: "logs",
		Caller: moduleproto.Caller{Delegation: handle},
	})
	wantDenied(t, err) // 403 → CodeCapabilityDenied
}

// TestBrokerAmbientOwnerScopedDenies: an ambient (caller-less) reverse read of
// an owner-scoped entity is denied. No handle ⇒ no owner id in the
// re-dispatched context ⇒ requireAuthenticated/RequireOwner refuse — the
// safe-by-construction proof that a background module cannot read owner rows.
func TestBrokerAmbientOwnerScopedDenies(t *testing.T) {
	policy := access.NewRolePolicy()
	if err := policy.Grant("module/m", "logs:read"); err != nil {
		t.Fatal(err)
	}
	b, _ := newCrudBrokerEnv(t, "logs", "logs", nil, policy, nil)
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"logs:read"}}
	h := b.entityHandler(view, opQuery)
	_, err := callReverse(t, h, moduleproto.EntityQueryParams{Entity: "logs"})
	wantDenied(t, err)
}

// TestBrokerDelegatedReadSucceeds: positive control — module grant + caller
// authority both hold ⇒ the re-dispatch returns the caller's owner-scoped
// rows ONLY (bob's row is filtered out). Proves the gate is not
// over-restrictive, so the deny tests above are meaningful denials.
func TestBrokerDelegatedReadSucceeds(t *testing.T) {
	policy := access.NewRolePolicy()
	if err := policy.Grant("owner", "logs:read"); err != nil {
		t.Fatal(err)
	}
	b, _ := newCrudBrokerEnv(t, "logs", "logs", func(c *entity.EntityConfig) {
		c.Access.Read = "logs:read"
	}, policy, func(string) []string { return []string{"owner"} })
	logsEnt, err := b.entities.Get("logs")
	if err != nil {
		t.Fatal(err)
	}
	brokerSeedRow(t, logsEnt.DB, "logs", "l-a", "alice", "Alpha")
	brokerSeedRow(t, logsEnt.DB, "logs", "l-b", "bob", "Beta")

	handle, release := brokerMint(t, b, policy, "alice", []string{"owner"})
	defer release()
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"logs:read"}}
	h := b.entityHandler(view, opQuery)
	res, err := callReverse(t, h, moduleproto.EntityQueryParams{
		Entity: "logs",
		Caller: moduleproto.Caller{Delegation: handle},
	})
	wantOK(t, err)
	qr, ok := res.(moduleproto.EntityQueryResult)
	if !ok {
		t.Fatalf("result type %T, want EntityQueryResult", res)
	}
	if qr.Total != 1 {
		t.Errorf("total = %d, want 1 (alice's row only — owner scope held)", qr.Total)
	}
	if !strings.Contains(string(qr.Rows), "Alpha") {
		t.Errorf("rows = %s, want alice's Alpha row", qr.Rows)
	}
	if strings.Contains(string(qr.Rows), "Beta") {
		t.Errorf("rows leaked bob's row across owners: %s", qr.Rows)
	}
}

// ----- search + event surfaces -----

// TestBrokerSearchOutsideGrantDenies: a module without search:query cannot
// invoke host.search.query.
func TestBrokerSearchOutsideGrantDenies(t *testing.T) {
	b := NewBroker(nil, nil, nil, "")
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"articles:read"}}
	h := b.searchHandler(view)
	_, err := callReverse(t, h, moduleproto.SearchQueryParams{Query: "hi"})
	wantDenied(t, err)
}

// TestBrokerTopicOutsideGrantDenies: a module CANNOT emit an event topic
// outside its grant set. Granted alerts:emit; emitting billing:emit → denied.
func TestBrokerTopicOutsideGrantDenies(t *testing.T) {
	bus := event.NewEventBus()
	b := NewBroker(nil, nil, bus, "")
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"alerts:emit"}}
	h := b.eventHandler(view)
	_, err := callReverse(t, h, moduleproto.EventEmitParams{
		Topic:   "billing",
		Payload: json.RawMessage(`{}`),
	})
	wantDenied(t, err)
}

// TestBrokerEventEmitsWithGrant: positive control — a topic in the grant set
// is emitted on the host bus.
func TestBrokerEventEmitsWithGrant(t *testing.T) {
	bus := event.NewEventBus()
	received := make(chan string, 1)
	bus.On("alerts.fired", func(_ context.Context, e event.Event) error {
		select {
		case received <- e.Type:
		default:
		}
		return nil
	})
	b := NewBroker(nil, nil, bus, "")
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"alerts.fired:emit"}}
	h := b.eventHandler(view)
	_, err := callReverse(t, h, moduleproto.EventEmitParams{
		Topic:   "alerts.fired",
		Payload: json.RawMessage(`{"k":"v"}`),
	})
	wantOK(t, err)
	select {
	case topic := <-received:
		if topic != "alerts.fired" {
			t.Errorf("delivered topic = %q, want alerts.fired", topic)
		}
	case <-time.After(time.Second):
		t.Fatal("event not delivered to bus within 1s")
	}
}
