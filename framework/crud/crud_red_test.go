//go:build red

package crud

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/outbox"
)

// ---------------------------------------------------------------------------
// Property: a JSON body whose DISTINCT wire keys fold onto the SAME column is
// rejected deterministically, never silently last-wins.
// Surfaces: CrudHandler.Create / CrudHandler.Update (readRequestBody →
// unconvertMapKeys, crud.go:436-461; decodeJSONBody plain Decode,
// crud_upload.go:113-146).
// Finding: decodeJSONBody decodes into map[string]any, then unconvertMapKeys
// case-folds wire keys onto DB columns. Two distinct JSON keys can resolve to
// one column (CaseCamel: "bodyText" via columnOfWire and "body_text" via the
// unconvertKeyRaw fallback both land on body_text; single-word columns fold
// "Title"/"title" the same way). Map iteration order decides which value
// survives: the stored value is nondeterministic per request. No strict-key
// rejection exists anywhere on the path. A client (or a buggy serializer)
// cannot know which of two conflicting values persisted — and an attacker can
// probe the fold to smuggle a value past a reviewer reading one of the two
// keys. Severity: production-facing.
// Fix direction: unconvertMapKeys (or readRequestBody) must detect that two
// distinct input keys resolve to one column and reject the body with 400
// (ValidationError), instead of overwriting. Unknown keys that collide with
// nothing still pass through — that pass-through contract is pinned by
// TestWireName_RoundTripsBothCasings and must not change.
// Round-6 mechanism split: create and update are surfaces of ONE
// mechanism, so the split below is by mechanism, not surface — the exact
// duplicate test pins wire-level last-wins on the same key twice, the
// case-fold test pins two distinct wire keys folding onto one column
// (independently fixable).
// ---------------------------------------------------------------------------

// setupCamelDocsHandlerRed builds an un-gated docs-style entity using
// CaseCamel (the framework default), where the wire key for body_text is
// "bodyText" and the raw fallback re-fold of "body_text" resolves to the
// same column.
func setupCamelDocsHandlerRed(t *testing.T) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE reddocs (id TEXT PRIMARY KEY, body_text TEXT)`)
	ent := entity.Define("reddocs", entity.EntityConfig{
		Table:  "reddocs",
		Fields: []schema.Field{{Name: "body_text", Type: schema.String}},
	}.WithTimestamps(false))
	ent.SetDB(db)
	return NewCrudHandler(ent, db).WithJSONCase(CaseCamel), db
}

func TestCrudRedMapBodyRejectsDuplicateKeys(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		ch, db := setupCamelDocsHandlerRed(t)
		req := httptest.NewRequest(http.MethodPost, "/api/reddocs",
			strings.NewReader(`{"body_text":"wire-form","body_text":"raw-form"}`))
		req.Header.Set("Content-Type", "application/json")
		req = withTestUser(req, "alice")
		rec := httptest.NewRecorder()
		ch.Create()(rec, req)
		if rec.Code != http.StatusBadRequest {
			var stored string
			_ = db.QueryRow(`SELECT body_text FROM reddocs LIMIT 1`).Scan(&stored)
			t.Errorf("SECURITY: [map-key-collision] create with the same wire key \"body_text\" twice returned %d, want 400 (deterministic rejection). The map decode kept last-wins %q. body=%s", rec.Code, stored, rec.Body.String())
		}
	})
	t.Run("update", func(t *testing.T) {
		ch, db := setupCamelDocsHandlerRed(t)
		if _, err := db.Exec(`INSERT INTO reddocs (id, body_text) VALUES ('d1','orig')`); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/reddocs/d1",
			strings.NewReader(`{"body_text":"wire-form","body_text":"raw-form"}`))
		req.Header.Set("Content-Type", "application/json")
		req = withTestUser(req, "alice")
		req.SetPathValue("id", "d1")
		rec := httptest.NewRecorder()
		ch.Update()(rec, req)
		if rec.Code != http.StatusBadRequest {
			var stored string
			_ = db.QueryRow(`SELECT body_text FROM reddocs WHERE id='d1'`).Scan(&stored)
			t.Errorf("SECURITY: [map-key-collision] update with the same wire key \"body_text\" twice returned %d, want 400 (deterministic rejection). The map decode kept last-wins %q. body=%s", rec.Code, stored, rec.Body.String())
		}
	})
}

func TestCrudRedMapBodyRejectsCaseFoldedKeys(t *testing.T) {
	t.Run("create", func(t *testing.T) {
		ch, db := setupCamelDocsHandlerRed(t)
		req := httptest.NewRequest(http.MethodPost, "/api/reddocs",
			strings.NewReader(`{"bodyText":"wire-form","body_text":"raw-form"}`))
		req.Header.Set("Content-Type", "application/json")
		req = withTestUser(req, "alice")
		rec := httptest.NewRecorder()
		ch.Create()(rec, req)
		if rec.Code != http.StatusBadRequest {
			var stored string
			_ = db.QueryRow(`SELECT body_text FROM reddocs LIMIT 1`).Scan(&stored)
			t.Errorf("SECURITY: [map-key-collision] create with two wire keys folding onto body_text returned %d, want 400 (deterministic rejection). Stored value was nondeterministically %q. body=%s", rec.Code, stored, rec.Body.String())
		}
	})
	t.Run("update", func(t *testing.T) {
		ch, db := setupCamelDocsHandlerRed(t)
		if _, err := db.Exec(`INSERT INTO reddocs (id, body_text) VALUES ('d1','orig')`); err != nil {
			t.Fatal(err)
		}
		req := httptest.NewRequest(http.MethodPatch, "/api/reddocs/d1",
			strings.NewReader(`{"bodyText":"wire-form","body_text":"raw-form"}`))
		req.Header.Set("Content-Type", "application/json")
		req = withTestUser(req, "alice")
		req.SetPathValue("id", "d1")
		rec := httptest.NewRecorder()
		ch.Update()(rec, req)
		if rec.Code != http.StatusBadRequest {
			var stored string
			_ = db.QueryRow(`SELECT body_text FROM reddocs WHERE id='d1'`).Scan(&stored)
			t.Errorf("SECURITY: [map-key-collision] update with two wire keys folding onto body_text returned %d, want 400 (deterministic rejection). Stored value was nondeterministically %q. body=%s", rec.Code, stored, rec.Body.String())
		}
	})
}

// ---------------------------------------------------------------------------
// Property: a resource-scoped decision (reading another owner's record) must
// consult the access Decider, not the role policy alone.
// Surfaces: in-process cross-owner reads — ListAll / GetOne via
// crossOwnerReadGranted (owner.go:244-250), the gate ApplyOwnerScope consults.
// Finding: crossOwnerReadGranted decides with `perm != "" && access.Can(ctx,
// perm)` — policy only. Every sibling gate that makes a resource-shaped
// decision routes through access.CanResource so a Decider (issue #80 seam,
// access/decider.go) can deny per resource; requirePermission does
// (owner.go:181-192). Here a DecisionDeny decider for Ref{Type:"ctickets"}
// is never consulted, so a policy-granted caller reads across owners even
// when the resource-aware authority says no. access.Can itself never reads
// the decider (access.go:381-387), so the deny is silently bypassed.
// Severity: production-facing (the decider is the per-resource authority
// surface hosts configure).
// Fix direction: route crossOwnerReadGranted through access.CanResource(ctx,
// perm, access.Ref{Type: ch.Entity.GetName(), ID: recordID}) — collection
// reads pass ID "" like requirePermission does for List.
// ---------------------------------------------------------------------------

func TestCrudRedCrossOwnerReadHonorsDecider(t *testing.T) {
	installOwnerExtractor(t)
	ch, _ := setupCrossOwnerReadHandler(t) // alice t-a, bob t-b; CrossOwnerRead "tickets:read:all"

	deny := func(_ context.Context, _ []string, capability access.Permission, resource access.Ref) access.Decision {
		if capability == access.Permission("tickets:read:all") && resource.Type == "ctickets" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	}
	ctx := access.WithDecider(ctxWithGrant(signedIn("alice"), "tickets:read:all"), deny)

	t.Run("list", func(t *testing.T) {
		rows, err := ch.ListAll(ctx, ListOptions{})
		if err != nil {
			t.Fatalf("ListAll: %v", err)
		}
		for _, row := range rows {
			if row["user_id"] == "bob" {
				t.Errorf("SECURITY: [cross-owner-decider] ListAll with a DecisionDeny decider for ctickets returned bob's row (%v): the CrossOwnerRead lift consults access.Can only and bypasses the resource decider (owner.go:249)", row)
			}
		}
	})
	t.Run("get", func(t *testing.T) {
		row, err := ch.GetOne(ctx, "t-b", nil)
		if err == nil {
			t.Errorf("SECURITY: [cross-owner-decider] GetOne of bob's row with a DecisionDeny decider for ctickets succeeded (%v): the CrossOwnerRead lift consults access.Can only and bypasses the resource decider (owner.go:249)", row)
		}
	})
}

// ---------------------------------------------------------------------------
// Property: lifting a declared row filter via the Unrestricted permission is
// a resource-scoped decision and must consult the access Decider.
// Surfaces: read-scope reads (List route via readScopeFilters →
// readScopeUnrestricted, read_scope.go:107-119).
// Finding: readScopeUnrestricted lifts ALL row filters with
// `access.Can(ctx, access.Permission(rs.Unrestricted))` — policy only. A
// Decider installed in ctx returning DecisionDeny for that capability is
// never consulted (access.Can never reads the decider), so a caller whose
// policy grant is revoked at resource granularity still reads every row the
// scope hides. The sibling gates that widen reads (requirePermission) route
// through access.CanResource; this one does not.
// Severity: production-facing (ReadScope is the row-hiding posture; the
// decider is the per-resource authority surface).
// Fix direction: readScopeUnrestricted should consult access.CanResource(ctx,
// rs.Unrestricted, access.Ref{Type: ent.GetName(), ID: ""}) so a deny keeps
// the caller scoped.
// ---------------------------------------------------------------------------

func TestCrudRedReadScopeHonorsDecider(t *testing.T) {
	postCh, _, _, _ := setupReadScopeWorld(t, true, "posts:review")

	deny := func(_ context.Context, _ []string, capability access.Permission, resource access.Ref) access.Decision {
		if capability == access.Permission("posts:review") && resource.Type == "posts" {
			return access.DecisionDeny
		}
		return access.DecisionAbstain
	}
	req := grantRequest(httptest.NewRequest(http.MethodGet, "/posts", nil), "u1", "posts:review")
	req = req.WithContext(access.WithDecider(req.Context(), deny))

	t.Run("list", func(t *testing.T) {
		ids, total := listPostIDs(t, postCh, req)
		for _, id := range ids {
			if id == "p2" || id == "p4" {
				t.Errorf("SECURITY: [read-scope-decider] list with a DecisionDeny decider for posts:review returned draft %q: the Unrestricted lift consults access.Can only and bypasses the resource decider (read_scope.go:115); ids=%v total=%d", id, ids, total)
			}
		}
	})
	t.Run("get", func(t *testing.T) {
		getReq := httptest.NewRequest(http.MethodGet, "/posts/p2", nil)
		getReq.SetPathValue("id", "p2")
		getReq = grantRequest(getReq, "u1", "posts:review")
		getReq = getReq.WithContext(access.WithDecider(getReq.Context(), deny))
		rec := httptest.NewRecorder()
		postCh.Get()(rec, getReq)
		if rec.Code != http.StatusNotFound {
			t.Errorf("SECURITY: [read-scope-decider] GET of a draft row with a DecisionDeny decider for posts:review returned %d, want 404 (the decider must keep the caller scoped); body=%s", rec.Code, rec.Body.String())
		}
	})
}

// ---------------------------------------------------------------------------
// CONTRACT-QUESTION red: client-supplied pagination inputs are clamped to
// bounded work. Negative offsets are rejected today (crud.go rejects them),
// but explicitOffsetValues (crud.go:1235) accepts any positive int64 up to
// MaxInt64, handing the raw value to OFFSET — a per-request deep-skip scan
// on populated tables (CPU amplification; the limit side IS clamped to
// MaxPageSize). No doc pins unbounded offset as a contract; delete or
// promote per maintainer decision: cap offset (page-cap × limit) and 400
// beyond it, or document deep-paging as an accepted cost.
// ---------------------------------------------------------------------------
func TestCrudRedOffsetBounded(t *testing.T) {
	ch, db := setupCamelDocsHandlerRed(t)
	if _, err := db.Exec(`INSERT INTO reddocs (id, body_text) VALUES ('r1', 'x'), ('r2', 'y')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/reddocs?offset=9223372036854775807&limit=1", nil)
	req = withTestUser(req, "alice")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code == http.StatusOK {
		t.Errorf("SECURITY: [offset-bound] ?offset=9223372036854775807 was accepted with %d (limit is clamped to MaxPageSize, offset is not): a client can force a per-request full-table skip scan (crud.go:1235 explicitOffsetValues)", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// CONTRACT-QUESTION red: an AfterGet mask that holds on every read surface
// (GET, SSE delivery, write responses) does NOT hold on the durable outbox
// lane. Delete or promote per maintainer decision.
// Property: a field hidden by an AfterGet hook never reaches a consumer in
// the clear, on any delivery lane.
// Surfaces: durable outbox delivery — doUpdate → StageEvent stages the RAW
// RETURNING row (crud_ops.go:233, crud_events.go:100-106), and
// outbox.processDelivery hands the staged payload to every declared
// consumer with no masking pass. The SSE lane masks at delivery via
// redactEventRecord (pinned by TestEventDeliveryIsRedacted); the durable
// lane has no equivalent, by design at stage time ("the redacted view
// never reaches ... the durable outbox row", crud_events.go:326-328) and
// by omission at delivery time.
// Finding: a durable consumer (webhook bridge, audit sink, search indexer)
// registered via Outbox.Consume receives body/secret fields in the clear
// that the same app masks for every human-facing surface. Whatever the
// maintainer decides, today's split is undocumented: nothing in
// framework/docs/content/events.md tells a consumer author they are the
// one surface exempt from read hooks. Severity: production-facing (same
// data class the SSE pin treats as SECURITY), but a contract question,
// not a clear bug: masking at delivery would need a per-entity hook seam
// on the relay, and "consumers are trusted machinery, masking is the
// consumer's job" is a defensible documented contract.
// Fix direction: EITHER run the entity's read-hook chain over the staged
// record at delivery (relay-side seam keyed by the envelope's entity
// name), OR document on Outbox.Consume + events.md that durable
// consumers receive the raw row and are responsible for their own
// masking. Delete this red when the contract is written down either way.
// ---------------------------------------------------------------------------
func TestOutboxRedDeliversRedacted(t *testing.T) {
	db := setupDB(t, `CREATE TABLE redout_notes (
		id TEXT PRIMARY KEY,
		owner TEXT NOT NULL,
		body TEXT
	)`)
	if _, err := db.Exec(`INSERT INTO redout_notes (id, owner, body) VALUES
		('n1','alice','SECRET-042')`); err != nil {
		t.Fatal(err)
	}

	ent := entity.Define("redout_notes", entity.EntityConfig{
		Fields: []schema.Field{
			{Name: "owner", Type: schema.String, Required: true},
			{Name: "body", Type: schema.String, NoQuery: true},
		},
	}.WithTimestamps(false))
	ent.SetDB(db)
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterGet, func(_ context.Context, data any) error {
		p, ok := data.(*hook.GetPayload)
		if !ok {
			return nil
		}
		if _, has := p.Result["body"]; has {
			p.Result["body"] = "REDACTED"
		}
		return nil
	})

	ox, err := outbox.New(db, outbox.WithHandlerGrace(0))
	if err != nil {
		t.Fatalf("outbox.New: %v", err)
	}
	ch.Outbox = ox

	got := make(chan event.Event, 1)
	ox.Consume("audit", event.EntityUpdated, func(_ context.Context, e event.Event) error {
		got <- e
		return nil
	})
	stop := ox.StartRelay(context.Background())
	defer stop()

	// Partial PUT: the row keeps its stored secret, RETURNING hands the
	// full row to StageEvent, exactly like TestWriteResponseIsRedacted.
	req := withTestUser(httptest.NewRequest(http.MethodPut, "/redout_notes/n1",
		strings.NewReader(`{"owner":"alice"}`)), "u1")
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", "n1")
	rec := httptest.NewRecorder()
	ch.Update()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("PUT = %d; body=%s", rec.Code, rec.Body.String())
	}

	select {
	case e := <-got:
		data, ok := e.Data.(map[string]any)
		if !ok {
			t.Fatalf("Event.Data type = %T, want map[string]any", e.Data)
		}
		row, _ := data[eventKeyRecord].(map[string]any)
		if row == nil {
			t.Fatalf("event carries no record: %+v", e.Data)
		}
		if row["body"] != "REDACTED" {
			t.Errorf("SECURITY: [outbox-redaction] durable consumer \"audit\" received body=%q in the clear: "+
				"the outbox lane stages the raw RETURNING row (crud_ops.go:233) and processDelivery runs no "+
				"AfterGet pass, while the SSE lane masks the same write at delivery (TestEventDeliveryIsRedacted). "+
				"Either mask at delivery or document that durable consumers see the raw row.", row["body"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for outbox delivery")
	}
}
