package framework

// Adversarial security tests for the process-module capability broker
// (design #37 §5). One file, one property family per finding (F2/F3/F6).
// These pin the production fix; see processmodule_broker.go for the design
// notes. Naming + shape follow .claude/skills/adversarial-tests/SKILL.md.

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// ---------------------------------------------------------------------------
// F2: a module cannot reach entities outside its grant via caller-supplied
// CONTROL KEYS smuggled through the child filter. expandRaw used to forward
// every top-level key of p.Filter into the CRUD list query, so a child could
// inject include/trashed/where/limit (and any undeclared name). The fix
// allow-lists p.Filter against the entity's declared, non-Hidden, non-NoQuery
// fields (mirrors crud/mcp.go listTool).
// ---------------------------------------------------------------------------

// TestBrokerFilterCannotSetControlKeys asserts the property on every distinct
// attack shape in one table: control keys, a valid field, a suffixed valid
// field, a NoQuery field, a Hidden field, and an undeclared name.
func TestBrokerFilterCannotSetControlKeys(t *testing.T) {
	ent := brokerEntity("docs", "docs", func(c *entity.EntityConfig) {
		c.Fields = []schema.Field{
			{Name: "id", Type: schema.String},
			{Name: "status", Type: schema.String},
			{Name: "created_at", Type: schema.String},
			{Name: "ssn", Type: schema.String, NoQuery: true},   // visible but never filterable
			{Name: "secret", Type: schema.String, Hidden: true}, // existence must stay hidden
		}
	})

	// One filter exercising every attack class at once.
	raw := json.RawMessage(`{
		"include":"author","trashed":true,"where":"x","limit":1,
		"page":2,"sort":"id","fields":"id","q":"pw","offset":3,
		"cursor":"c","direction":"asc","stream":1,"per_page":5,
		"status":"open","created_at_gte":"2026-01-01",
		"ssn":"111-22-3333","secret":"x","bogus":"y"
	}`)

	got, err := sanitizeFilter(ent, raw)
	if err != nil {
		t.Fatalf("sanitizeFilter errored: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(got, &m); err != nil {
		t.Fatalf("sanitizeFilter output not a JSON object: %v (out=%s)", err, got)
	}

	// Every control key + non-queryable + undeclared name MUST be dropped.
	banned := []string{
		"include", "trashed", "where", "limit", "page", "sort", "fields",
		"q", "offset", "cursor", "direction", "stream", "per_page",
		"ssn", "secret", "bogus",
	}
	for _, k := range banned {
		if _, ok := m[k]; ok {
			t.Errorf("F2 leak: key %q reached the CRUD query (should be dropped)", k)
		}
	}
	// Declared, non-Hidden, non-NoQuery field keys (with comparison suffix)
	// are the ONLY keys that survive.
	if m["status"] != "open" {
		t.Errorf("declared field %q was dropped: got %v", "status", m["status"])
	}
	if m["created_at_gte"] != "2026-01-01" {
		t.Errorf("suffixed field %q was dropped: got %v", "created_at_gte", m["created_at_gte"])
	}
	if len(m) != 2 {
		t.Errorf("sanitizeFilter left %d keys, want exactly 2: %v", len(m), m)
	}
}

// ---------------------------------------------------------------------------
// F3: a brokered re-dispatch NEVER lifts owner scope, even when the
// re-resolved caller legitimately holds CrossOwnerRead. The root-cause fix
// stamps every brokered re-dispatch context with crud.WithBrokeredCall so
// crossOwnerReadGranted returns false by construction; the gate at broker.gate
// is belt-and-suspenders (now with a b.policy fallback + fail-closed deny).
// ---------------------------------------------------------------------------

func TestBrokerNeverLiftsOwnerScope(t *testing.T) {
	// Entity is owner-scoped AND opts into CrossOwnerRead.
	mkReg := func() *Registry {
		return brokerRegistry(brokerEntity("logs", "logs", func(c *entity.EntityConfig) {
			c.Scope.CrossOwnerRead = "logs:read:all"
		}))
	}
	policy := access.NewRolePolicy()
	if err := policy.Grant("super", "logs:read", "logs:read:all"); err != nil {
		t.Fatal(err)
	}

	// Sub-case 1: the delegated caller legitimately holds CrossOwnerRead and
	// the mint-time snapshot captured a policy. The gate denies BEFORE
	// re-dispatch; the router is never reached.
	t.Run("caller_holds_crossowner_denies", func(t *testing.T) {
		var hit atomic.Bool
		b := NewBroker(fakeSuccessRouter(&hit), mkReg(), nil, "")
		handle, release := brokerMint(t, b, policy, "alice", []string{"super"})
		defer release()
		h := b.entityHandler(ModuleGrantView{Name: "m", Grants: []access.Permission{"logs:read"}}, opQuery)
		_, err := callReverse(t, h, moduleproto.EntityQueryParams{
			Entity: "logs",
			Caller: moduleproto.CallerRef{Delegation: handle},
		})
		wantDenied(t, err)
		wantNotHit(t, &hit)
	})

	// Sub-case 2: the mint-time snapshot carries NO policy (entry.policy ==
	// nil) and the broker has no app-wide policy (b.policy == nil). Today
	// this unevaluatable carve-out silently permitted the re-dispatch to
	// resolve the caller with CrossOwnerRead; it must now DENY fail-closed.
	t.Run("mint_time_policy_nil_fail_closed", func(t *testing.T) {
		var hit atomic.Bool
		b := NewBroker(fakeSuccessRouter(&hit), mkReg(), nil, "") // no WithBrokerPolicy
		req := httptest.NewRequest(http.MethodGet, "/x", nil)
		req.Header.Set("Cookie", "sid=alice")
		mctx := access.WithRoles(req.Context(), []string{"super"}) // NO policy
		handle, release := b.MintDelegation(req.WithContext(mctx), 1)
		defer release()
		h := b.entityHandler(ModuleGrantView{Name: "m", Grants: []access.Permission{"logs:read"}}, opQuery)
		_, err := callReverse(t, h, moduleproto.EntityQueryParams{
			Entity: "logs",
			Caller: moduleproto.CallerRef{Delegation: handle},
		})
		wantDenied(t, err)
		wantNotHit(t, &hit)
	})

	// Sub-case 3: a brokered call that PROCEEDS (no CrossOwnerRead on the
	// entity) must still stamp the re-dispatch context with the brokeredCall
	// marker, the root-cause guarantee that owner scope holds by
	// construction for every brokered call.
	t.Run("redispatch_context_stamped", func(t *testing.T) {
		var sawMarker atomic.Bool
		router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if crud.IsBrokeredCall(r.Context()) {
				sawMarker.Store(true)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[],"total":0}`))
		})
		reg := brokerRegistry(brokerEntity("notes", "notes", nil))
		b := NewBroker(router, reg, nil, "")
		handle, release := brokerMint(t, b, access.NewRolePolicy(), "alice", nil)
		defer release()
		h := b.entityHandler(ModuleGrantView{Name: "m", Grants: []access.Permission{"notes:read"}}, opQuery)
		_, err := callReverse(t, h, moduleproto.EntityQueryParams{
			Entity: "notes",
			Caller: moduleproto.CallerRef{Delegation: handle},
		})
		wantOK(t, err)
		if !sawMarker.Load() {
			t.Fatal("F3: re-dispatch context was not stamped brokeredCall — owner scope is not guaranteed by construction")
		}
	})
}

// ---------------------------------------------------------------------------
// F6: delegation handles are app-global (one Broker.handles map shared by
// every module). A handle minted for module A must be refused when it is
// echoed back under module B's reverse handler. The fix binds each handle to
// the minting module and rejects on mismatch in resolveCaller.
// ---------------------------------------------------------------------------

func TestBrokerHandleBoundToModule(t *testing.T) {
	reg := brokerRegistry(brokerEntity("docs", "docs", nil))
	var hit atomic.Bool
	b := NewBroker(fakeSuccessRouter(&hit), reg, nil, "")

	// Mint a handle bound to moduleA (stamp the module on the mint context,
	// exactly as the supervisor's proxy/tools paths do).
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Cookie", "sid=alice")
	mintCtx := withDelegationModule(req.Context(), "moduleA")
	handle, release := b.MintDelegation(req.WithContext(mintCtx), 1)
	defer release()

	// moduleB presents moduleA's handle → refused, router never reached.
	hB := b.entityHandler(ModuleGrantView{Name: "moduleB", Grants: []access.Permission{"docs:read"}}, opQuery)
	_, err := callReverse(t, hB, moduleproto.EntityQueryParams{
		Entity: "docs",
		Caller: moduleproto.CallerRef{Delegation: handle},
	})
	wantDenied(t, err)
	wantNotHit(t, &hit)

	// Sanity: the SAME handle under moduleA's own handler is accepted, this
	// is a cross-module bind, not a blanket rejection.
	hit.Store(false)
	hA := b.entityHandler(ModuleGrantView{Name: "moduleA", Grants: []access.Permission{"docs:read"}}, opQuery)
	_, err = callReverse(t, hA, moduleproto.EntityQueryParams{
		Entity: "docs",
		Caller: moduleproto.CallerRef{Delegation: handle},
	})
	wantOK(t, err)
	if !hit.Load() {
		t.Fatal("handle bound to moduleA must resolve under moduleA's handler (false positive would over-restrict)")
	}
}
