package framework

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"

	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// An `?include=` is a read of ANOTHER entity's rows. Every other guard hanging
// off the include target was already applied, the Hidden-column scrub, owner
// scope, tenant scope, soft delete, but the target's own Exposure.Access was
// not. So `GET /api/posts?include=author` returned whole rows of an entity
// whose own route answers 403, and `?include=comments.author` dumped the table
// in one request. The same held for `?rel.field=` nested filters, which do not
// return the row but make the result count an oracle for its values.
func includeGateApp(t *testing.T) *App {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := NewApp(WithConfig(AppConfig{Name: "incgate", APIPrefix: "/api"}), WithDB(db))
	// Gated: its own list route refuses an anonymous caller.
	app.Entity("owners", EntityConfig{
		Fields: []schema.Field{{Name: "email", Type: schema.String}},
		Exposure: &entity.ExposureConfig{
			CRUD:   new(true),
			Access: entity.AccessControl{Read: "owners:read"},
		},
	})
	// Open: anonymously readable, and related to the gated entity.
	app.Entity("notes", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "owner_id", Type: schema.Relation, To: "owners"},
		},
		Relations: []entity.Relation{
			{Type: entity.RelManyToOne, Name: "owner", Entity: "owners", ForeignKey: "owner_id"},
		},
		Exposure: &entity.ExposureConfig{CRUD: new(true), Public: true},
	})
	return app
}

//go:fix inline
func boolPtrInc(b bool) *bool { return new(b) }

func TestIncludeRespectsTargetEntityReadGate(t *testing.T) {
	app := includeGateApp(t)
	stop := covStartAndStop(t, app)
	defer stop()
	ta := TestHarness(t, app)

	if _, err := app.DB.Exec(`INSERT INTO owners (id, email) VALUES ('o1','secret@example.com')`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(`INSERT INTO notes (id, title, owner_id) VALUES ('n1','Public note','o1')`); err != nil {
		t.Fatal(err)
	}

	// Baseline: the gated entity refuses, the open one serves.
	if got := ta.Get("/api/owners").Status(); got != http.StatusForbidden {
		t.Fatalf("GET /api/owners = %d, want 403 (the gated baseline this test rests on)", got)
	}
	if got := ta.Get("/api/notes").Status(); got != http.StatusOK {
		t.Fatalf("GET /api/notes = %d, want 200", got)
	}

	// The attack: reach the gated entity through the open one.
	resp := ta.Get("/api/notes?include=owner")
	if resp.Status() != http.StatusForbidden {
		t.Errorf("GET /api/notes?include=owner = %d, want 403 — an include must not serve rows the target's own route refuses", resp.Status())
	}
	if strings.Contains(resp.Body(), "secret@example.com") {
		t.Errorf("include leaked the gated entity's row:\n%s", resp.Body())
	}
}

// Filtering across the relation does not return the row, but the result count
// changes with the guessed value, an oracle over a column the entity refuses
// to serve.
func TestNestedFilterRespectsTargetEntityReadGate(t *testing.T) {
	app := includeGateApp(t)
	stop := covStartAndStop(t, app)
	defer stop()
	ta := TestHarness(t, app)

	if _, err := app.DB.Exec(`INSERT INTO owners (id, email) VALUES ('o1','secret@example.com')`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(`INSERT INTO notes (id, title, owner_id) VALUES ('n1','Public note','o1')`); err != nil {
		t.Fatal(err)
	}

	hit := ta.Get("/api/notes?owner.email=secret@example.com")
	miss := ta.Get("/api/notes?owner.email=wrong@example.com")

	if hit.Status() != http.StatusForbidden || miss.Status() != http.StatusForbidden {
		t.Errorf("nested filter on a gated relation = %d/%d, want 403/403 — equal answers for a right and a wrong guess is the property that kills the oracle",
			hit.Status(), miss.Status())
	}
}

// The gate must not refuse a relation the caller CAN read, or every legitimate
// include breaks.
func TestIncludeAllowedWhenTargetIsReadable(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := NewApp(WithConfig(AppConfig{Name: "incok", APIPrefix: "/api"}), WithDB(db))
	app.Entity("owners", EntityConfig{
		Fields:   []schema.Field{{Name: "email", Type: schema.String}},
		Exposure: &entity.ExposureConfig{CRUD: new(true), Public: true},
	})
	app.Entity("notes", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "owner_id", Type: schema.Relation, To: "owners"},
		},
		Relations: []entity.Relation{
			{Type: entity.RelManyToOne, Name: "owner", Entity: "owners", ForeignKey: "owner_id"},
		},
		Exposure: &entity.ExposureConfig{CRUD: new(true), Public: true},
	})
	stop := covStartAndStop(t, app)
	defer stop()
	ta := TestHarness(t, app)

	if _, err := app.DB.Exec(`INSERT INTO owners (id, email) VALUES ('o1','open@example.com')`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(`INSERT INTO notes (id, title, owner_id) VALUES ('n1','Note','o1')`); err != nil {
		t.Fatal(err)
	}

	resp := ta.Get("/api/notes?include=owner")
	if resp.Status() != http.StatusOK {
		t.Fatalf("GET /api/notes?include=owner = %d, want 200 for a readable relation: %s", resp.Status(), resp.Body())
	}
	if !strings.Contains(resp.Body(), "open@example.com") {
		t.Errorf("a readable include must still be served:\n%s", resp.Body())
	}
}

// The gate is wired on three surfaces. List, read-one, and cursor pagination
// (crud.go, crud_cursor.go), but was tested only through List, so a
// regression on either of the other two would have stayed green.
// The gate walks node.Children, so a gated entity two hops out cannot be
// laundered through a readable first segment.
//
// This needs a real second hop: an earlier version of this test asked for
// `owner.notes`, which the PARSER rejected with 400 because that relation does
// not exist, so it never reached the recursion it claimed to cover, and its
// assertion accepted the 400. The chain here is notes → owner (readable) →
// profile (gated), and only a 403 naming the gated entity passes.
func TestIncludeGateAppliesAtNestedDepth(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := NewApp(WithConfig(AppConfig{Name: "depthgate", APIPrefix: "/api"}), WithDB(db))
	// Two hops out, and gated.
	app.Entity("profiles", EntityConfig{
		Fields: []schema.Field{
			{Name: "bio", Type: schema.String},
			{Name: "owner_id", Type: schema.Relation, To: "owners"},
		},
		Exposure: &entity.ExposureConfig{
			CRUD:   new(true),
			Access: entity.AccessControl{Read: "profiles:read"},
		},
	})
	// One hop out, and readable, the segment that must not launder the next.
	app.Entity("owners", EntityConfig{
		Fields: []schema.Field{{Name: "email", Type: schema.String}},
		Relations: []entity.Relation{
			{Type: entity.RelHasOne, Name: "profile", Entity: "profiles", ForeignKey: "owner_id"},
		},
		Exposure: &entity.ExposureConfig{CRUD: new(true), Public: true},
	})
	app.Entity("notes", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "owner_id", Type: schema.Relation, To: "owners"},
		},
		Relations: []entity.Relation{
			{Type: entity.RelManyToOne, Name: "owner", Entity: "owners", ForeignKey: "owner_id"},
		},
		Exposure: &entity.ExposureConfig{CRUD: new(true), Public: true},
	})
	stop := covStartAndStop(t, app)
	defer stop()
	ta := TestHarness(t, app)

	for _, stmt := range []string{
		`INSERT INTO owners (id, email) VALUES ('o1','owner@example.com')`,
		`INSERT INTO profiles (id, bio, owner_id) VALUES ('p1','secret bio','o1')`,
		`INSERT INTO notes (id, title, owner_id) VALUES ('n1','Public note','o1')`,
	} {
		if _, err := app.DB.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	// The first hop alone is readable and must still work, otherwise a 403
	// below would prove nothing about depth.
	if got := ta.Get("/api/notes?include=owner"); got.Status() != http.StatusOK {
		t.Fatalf("GET /api/notes?include=owner = %d, want 200 — the readable first hop is the baseline this test rests on: %s", got.Status(), got.Body())
	}

	resp := ta.Get("/api/notes?include=owner.profile")
	if resp.Status() != http.StatusForbidden {
		t.Errorf("GET /api/notes?include=owner.profile = %d, want 403 — a gated entity two hops out must not be reachable through a readable parent: %s",
			resp.Status(), resp.Body())
	}
	if !strings.Contains(resp.Body(), "profiles") {
		t.Errorf("the refusal should name the gated entity at the depth that failed: %s", resp.Body())
	}
	if strings.Contains(resp.Body(), "secret bio") {
		t.Errorf("the nested include leaked the gated entity's row:\n%s", resp.Body())
	}
}

// The gate must not depend on whether the parent table happens to have rows.
// It once sat below an early return for an empty parent, so the same request
// answered 200 while the table was empty and 403 after the first row existed:
// a table-emptiness oracle, and inconsistent with the nested-filter check,
// which refuses before querying anything.
func TestIncludeGateDoesNotDependOnParentEmptiness(t *testing.T) {
	app := includeGateApp(t)
	stop := covStartAndStop(t, app)
	defer stop()
	ta := TestHarness(t, app)

	// No rows anywhere yet.
	empty := ta.Get("/api/notes?include=owner")
	if empty.Status() != http.StatusForbidden {
		t.Errorf("GET /api/notes?include=owner with an empty parent = %d, want 403 — the posture answer must not depend on row counts: %s",
			empty.Status(), empty.Body())
	}

	if _, err := app.DB.Exec(`INSERT INTO owners (id, email) VALUES ('o1','secret@example.com')`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(`INSERT INTO notes (id, title, owner_id) VALUES ('n1','Public note','o1')`); err != nil {
		t.Fatal(err)
	}

	populated := ta.Get("/api/notes?include=owner")
	if populated.Status() != empty.Status() {
		t.Errorf("include posture changed from %d to %d once the parent had a row — seeding one row must not flip the answer",
			empty.Status(), populated.Status())
	}
}

func TestIncludeGateAppliesToReadOneAndCursor(t *testing.T) {
	app := includeGateApp(t)
	stop := covStartAndStop(t, app)
	defer stop()
	ta := TestHarness(t, app)

	if _, err := app.DB.Exec(`INSERT INTO owners (id, email) VALUES ('o1','secret@example.com')`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(`INSERT INTO notes (id, title, owner_id) VALUES ('n1','Public note','o1')`); err != nil {
		t.Fatal(err)
	}

	for _, probe := range []struct{ label, path string }{
		{"read-one", "/api/notes/n1?include=owner"},
		{"cursor", "/api/notes?include=owner&limit=1&cursor="},
	} {
		resp := ta.Get(probe.path)
		if resp.Status() != http.StatusForbidden {
			t.Errorf("%s: GET %s = %d, want 403 — the include gate must cover every surface that resolves includes",
				probe.label, probe.path, resp.Status())
		}
		if strings.Contains(resp.Body(), "secret@example.com") {
			t.Errorf("%s: leaked the gated entity's row:\n%s", probe.label, resp.Body())
		}
	}
}

// An owner-scoped target is the case the first version of this gate missed.
// It used a narrow predicate that skipped owner and tenant, on the reasoning
// that the include path scopes rows per node. That was true there and false
// here, because buildExistsSubquery emitted no owner or tenant predicate at
// all, so `?rel.field=` counted across every owner's rows while the target's
// own route answered 401.
//
// The subquery now carries the caller's owner predicate, so a signed-in owner
// gets 200 with only their own rows counted rather than a blanket 403. The
// property under test is unchanged and is the only one that matters: a guess
// that hits another owner's row must be indistinguishable from a guess that
// hits nothing. Status alone is too weak an assertion for that now, since both
// answers are 200 — the BODIES have to match too.
func TestNestedFilterRespectsOwnerScopedTarget(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := NewApp(WithConfig(AppConfig{Name: "ownergate", APIPrefix: "/api"}), WithDB(db))
	// Owner-scoped: its own route refuses a caller with no owner in context.
	app.Entity("notes", EntityConfig{
		Fields: []schema.Field{
			{Name: "body", Type: schema.String},
			{Name: "owner_id", Type: schema.String},
			{Name: "board_id", Type: schema.Relation, To: "boards"},
		},
		Scope:    &entity.ScopeConfig{OwnerField: "owner_id"},
		Exposure: &entity.ExposureConfig{CRUD: new(true)},
	})
	// Public parent that relates to it.
	app.Entity("boards", EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Relations: []entity.Relation{
			{Type: entity.RelHasMany, Name: "notes", Entity: "notes", ForeignKey: "board_id"},
		},
		Exposure: &entity.ExposureConfig{CRUD: new(true), Public: true},
	})
	stop := covStartAndStop(t, app)
	defer stop()
	ta := TestHarness(t, app)

	if _, err := app.DB.Exec(`INSERT INTO boards (id, title) VALUES ('b1','Public board')`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(`INSERT INTO notes (id, body, owner_id, board_id) VALUES ('n1','someone secret note','u-other','b1')`); err != nil {
		t.Fatal(err)
	}

	if got := ta.Get("/api/notes").Status(); got != http.StatusUnauthorized && got != http.StatusForbidden {
		t.Fatalf("GET /api/notes = %d, want 401/403 — the owner-scoped baseline this test rests on", got)
	}

	// A right guess and a wrong one must be indistinguishable, and that has to
	// hold for a SIGNED-IN caller too, not just an anonymous one. CanReadScoped
	// only establishes that the caller has an owner; it is the owner predicate
	// inside the subquery that decides which rows the count can see. Without
	// it an owner-bearing caller enumerates every other owner's rows one guess
	// at a time.
	// A caller who genuinely HAS an owner: without registering an extractor the
	// probe would fail owner resolution and refuse for the wrong reason, which
	// cannot distinguish "narrowed to nothing" from "no owner at all".
	prev := owner.GetExtractor()
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		if u, ok := handler.GetUser(ctx); ok && u != nil {
			return "u-self", true
		}
		return nil, false
	})
	t.Cleanup(func() { owner.SetExtractor(prev) })

	signedIn := ta.AsUser(struct{ ID string }{ID: "u-self"})
	sHit := signedIn.Get("/api/boards?notes.body=someone+secret+note")
	sMiss := signedIn.Get("/api/boards?notes.body=not+the+value")
	if sHit.Status() != sMiss.Status() || sHit.Body() != sMiss.Body() {
		t.Errorf("signed-in nested filter distinguishes a correct guess from a wrong one, so the count is an oracle over another owner's rows.\nhit  %d %s\nmiss %d %s",
			sHit.Status(), sHit.Body(), sMiss.Status(), sMiss.Body())
	}
	// n1 belongs to u-other, so the narrowed subquery matches nothing and the
	// board must not come back. A 200 that still contained b1 would mean the
	// predicate reached another owner's row.
	if strings.Contains(sHit.Body(), "b1") {
		t.Errorf("the signed-in filter matched a board through another owner's note:\n%s", sHit.Body())
	}

	// Anonymous is refused earlier and for a different reason: an owner-scoped
	// entity with nobody to scope to is not readable at all, so CanReadScoped
	// rejects before any narrowing happens.
	hit := ta.Get("/api/boards?notes.body=someone+secret+note")
	miss := ta.Get("/api/boards?notes.body=not+the+value")
	if hit.Status() != miss.Status() {
		t.Errorf("anonymous nested filter answers %d for a correct guess and %d for a wrong one — the row count is an oracle over rows the target's route refuses",
			hit.Status(), miss.Status())
	}
	if hit.Status() != http.StatusForbidden {
		t.Errorf("anonymous nested filter on an owner-scoped target = %d, want 403", hit.Status())
	}
}

// A soft-deleted target row is hidden by every other read surface, the routes
// via ApplySoftDeleteFilter, the eager loaders via their soft-delete argument,
// and GET by id with a 404. The EXISTS subquery counted them, so `?rel.field=`
// confirmed values in trashed rows one guess at a time.
func TestNestedFilterHidesSoftDeletedTargetRows(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := NewApp(WithConfig(AppConfig{Name: "sdgate", APIPrefix: "/api"}), WithDB(db))
	app.Entity("snotes", EntityConfig{
		Fields: []schema.Field{
			{Name: "body", Type: schema.String},
			{Name: "pub_id", Type: schema.Relation, To: "spubs"},
		},
		Scope:    &entity.ScopeConfig{SoftDelete: true},
		Exposure: &entity.ExposureConfig{CRUD: new(true), Public: true},
	})
	app.Entity("spubs", EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Relations: []entity.Relation{
			{Type: entity.RelHasMany, Name: "snotes", Entity: "snotes", ForeignKey: "pub_id"},
		},
		Exposure: &entity.ExposureConfig{CRUD: new(true), Public: true},
	})
	stop := covStartAndStop(t, app)
	defer stop()
	ta := TestHarness(t, app)

	for _, stmt := range []string{
		`INSERT INTO spubs (id, title) VALUES ('p1','Public')`,
		`INSERT INTO snotes (id, body, pub_id) VALUES ('c1','live-body','p1')`,
		`INSERT INTO snotes (id, body, pub_id, deleted_at) VALUES ('c2','trashed-secret','p1','2026-01-01T00:00:00Z')`,
	} {
		if _, err := app.DB.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}

	// Baseline: the trashed row is hidden everywhere else.
	if body := ta.Get("/api/snotes").Body(); strings.Contains(body, "trashed-secret") {
		t.Fatalf("the list route already leaks the trashed row — this test's premise is wrong:\n%s", body)
	}

	// A guess that matches only the trashed row must not be distinguishable
	// from a guess that matches nothing.
	trashed := ta.Get("/api/spubs?snotes.body=trashed-secret")
	nothing := ta.Get("/api/spubs?snotes.body=no-such-value")
	if trashed.Body() != nothing.Body() {
		t.Errorf("a nested filter matching a soft-deleted row answers differently from one matching nothing — trashed values are enumerable:\ntrashed: %s\nnothing: %s",
			trashed.Body(), nothing.Body())
	}
	// And a live row must still be findable, or the predicate is too broad.
	if live := ta.Get("/api/spubs?snotes.body=live-body"); live.Body() == nothing.Body() {
		t.Errorf("a nested filter on a LIVE row stopped matching — the soft-delete predicate is too broad:\n%s", live.Body())
	}

	// The `_in` suffix is parsed by a SEPARATE branch that builds its own
	// nestedFilter. Only the single-value branch was covered, so the flag
	// could regress on this one with nothing failing.
	trashedIn := ta.Get("/api/spubs?snotes.body_in=trashed-secret")
	nothingIn := ta.Get("/api/spubs?snotes.body_in=no-such-value")
	if trashedIn.Body() != nothingIn.Body() {
		t.Errorf("an _in nested filter matching a soft-deleted row answers differently from one matching nothing:\ntrashed: %s\nnothing: %s",
			trashedIn.Body(), nothingIn.Body())
	}
	if liveIn := ta.Get("/api/spubs?snotes.body_in=live-body"); liveIn.Body() == nothingIn.Body() {
		t.Errorf("an _in nested filter on a LIVE row stopped matching:\n%s", liveIn.Body())
	}
}

// A caller who may already read every row of the target learns nothing from a
// hit/miss count, so the subquery must not be narrowed for them. Narrowing
// would remove a capability without protecting anything. The exemption is the
// same pair the routes honour.
func TestNestedFilterAllowsCrossOwnerCallers(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := NewApp(WithConfig(AppConfig{Name: "xogate", APIPrefix: "/api"}), WithDB(db))
	app.Entity("notes", EntityConfig{
		Fields: []schema.Field{
			{Name: "body", Type: schema.String},
			{Name: "owner_id", Type: schema.String},
			{Name: "board_id", Type: schema.Relation, To: "boards"},
		},
		Scope: &entity.ScopeConfig{
			OwnerField: "owner_id",
			// The opt-in the exemption keys on. Without it the entity never
			// grants cross-owner reads and the refusal is unconditional.
			CrossOwnerRead: "notes:read:all",
		},
		Exposure: &entity.ExposureConfig{CRUD: new(true)},
	})
	app.Entity("boards", EntityConfig{
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
		Relations: []entity.Relation{
			{Type: entity.RelHasMany, Name: "notes", Entity: "notes", ForeignKey: "board_id"},
		},
		Exposure: &entity.ExposureConfig{CRUD: new(true), Public: true},
	})
	// What a generated app installs: the policy travels on the request
	// context, so crossOwnerReadGranted sees it through the real HTTP chain
	// rather than a hand-built context a unit test could fake.
	policy := access.NewRolePolicy()
	policy.Grant("auditor", "notes:read:all")
	app.Use(access.Middleware(policy, func(ctx context.Context) []string {
		if u, ok := handler.GetUser(ctx); ok && u != nil {
			if rh, ok := u.(interface{ GetRoles() []string }); ok {
				return rh.GetRoles()
			}
		}
		return nil
	}))
	stop := covStartAndStop(t, app)
	defer stop()

	if _, err := app.DB.Exec(`INSERT INTO boards (id, title) VALUES ('b1','Board')`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(`INSERT INTO notes (id, body, owner_id, board_id) VALUES ('n1','someone note','u-other','b1')`); err != nil {
		t.Fatal(err)
	}

	// Owner identity comes from the signed-in user, so the two callers below
	// are genuinely different owners, and neither of them owns n1.
	prev := owner.GetExtractor()
	owner.SetExtractor(func(ctx context.Context) (any, bool) {
		if u, ok := handler.GetUser(ctx); ok && u != nil {
			if idh, ok := u.(interface{ GetID() string }); ok {
				return idh.GetID(), true
			}
		}
		return nil, false
	})
	t.Cleanup(func() { owner.SetExtractor(prev) })

	ta := TestHarness(t, app)

	// An ordinary owner is narrowed to their own rows, so n1 (owned by u-other)
	// stays invisible. This is the oracle case the exemption must not widen.
	ordinary := ta.AsUser(xoUser{id: "u-self"}).Get("/api/boards?notes.body=someone+note")
	if ordinary.Status() != http.StatusOK {
		t.Fatalf("an ordinary owner = %d, want 200: %s", ordinary.Status(), ordinary.Body())
	}
	if strings.Contains(ordinary.Body(), "b1") {
		t.Fatalf("an ordinary owner reached another owner's note through the subquery:\n%s", ordinary.Body())
	}

	// The caller holding the entity's declared CrossOwnerRead keeps the
	// capability. This is the arm that pins the exemption: delete the
	// cross-scope condition in nested_filter.go and only this fails.
	granted := ta.AsUser(xoUser{id: "u-self", roles: []string{"auditor"}}).
		Get("/api/boards?notes.body=someone+note")
	if granted.Status() != http.StatusOK {
		t.Fatalf("a caller holding notes:read:all = %d, want 200 — the exemption was lost: %s",
			granted.Status(), granted.Body())
	}
	// 200 alone would also be the answer if the filter silently matched
	// nothing, so require the row the grant is supposed to reach.
	if !strings.Contains(granted.Body(), "b1") {
		t.Errorf("the granted nested filter matched no board — it did not reach the other owner's note:\n%s", granted.Body())
	}
	// And the predicate must still discriminate: a body no note has must not
	// match, or the 200 above proves nothing about the subquery.
	miss := ta.AsUser(xoUser{id: "u-self", roles: []string{"auditor"}}).
		Get("/api/boards?notes.body=no-such-body")
	if strings.Contains(miss.Body(), "b1") {
		t.Errorf("a nested filter matching no note still returned the board — the subquery is inert:\n%s", miss.Body())
	}
}

// xoUser carries the id and roles the cross-owner exemption is keyed on.
type xoUser struct {
	id    string
	roles []string
}

func (u xoUser) GetID() string      { return u.id }
func (u xoUser) GetEmail() string   { return u.id + "@example.test" }
func (u xoUser) GetRoles() []string { return u.roles }

// Relation.Entity is the registry KEY; the table can differ whenever a host
// declares Name != Table. Every check in nested_filter.go is made against the
// RESOLVED target, so the subquery must read that target's table, otherwise
// validation describes one table while the query reads another, which is
// silently wrong data when a same-named table exists and a 500 when it does
// not.
func TestNestedFilterQueriesTheResolvedTargetTable(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := NewApp(WithConfig(AppConfig{Name: "tablegate", APIPrefix: "/api"}), WithDB(db))
	// Entity NAME "authors", TABLE "acct_authors".
	app.Entity("authors", EntityConfig{
		Table:    "acct_authors",
		Fields:   []schema.Field{{Name: "name", Type: schema.String}},
		Exposure: &entity.ExposureConfig{CRUD: new(true), Public: true},
	})
	app.Entity("articles", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "author_id", Type: schema.Relation, To: "authors"},
		},
		Relations: []entity.Relation{
			{Type: entity.RelManyToOne, Name: "author", Entity: "authors", ForeignKey: "author_id"},
		},
		Exposure: &entity.ExposureConfig{CRUD: new(true), Public: true},
	})
	stop := covStartAndStop(t, app)
	defer stop()
	ta := TestHarness(t, app)

	if _, err := app.DB.Exec(`INSERT INTO acct_authors (id, name) VALUES ('a1','alice')`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(`INSERT INTO articles (id, title, author_id) VALUES ('r1','Post','a1')`); err != nil {
		t.Fatal(err)
	}
	// A stale table named after the ENTITY, holding different data. Reading it
	// instead of acct_authors is the bug: the filter would answer from here.
	if _, err := app.DB.Exec(`CREATE TABLE authors (id TEXT PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(`INSERT INTO authors (id, name) VALUES ('a1','mallory')`); err != nil {
		t.Fatal(err)
	}

	hit := ta.Get("/api/articles?author.name=alice")
	if hit.Status() != http.StatusOK {
		t.Fatalf("GET /api/articles?author.name=alice = %d, want 200: %s", hit.Status(), hit.Body())
	}
	if !strings.Contains(hit.Body(), "\"id\":\"r1\"") {
		t.Errorf("the real table's value did not match — the subquery read the entity NAME as a table instead of the resolved target's table:\n%s", hit.Body())
	}
	if shadow := ta.Get("/api/articles?author.name=mallory"); strings.Contains(shadow.Body(), "\"id\":\"r1\"") {
		t.Errorf("a value that exists only in the same-named shadow table matched — the subquery is reading the wrong table:\n%s", shadow.Body())
	}
}

// Every other include test reaches its 403 through a declared read permission
// or owner scoping. Neither exercises the gate's THIRD reason to refuse: the
// baseline session requirement that a default-posture entity carries, no
// Access, no Public, no OwnerField, which is exactly what `gofastr init`
// scaffolds and therefore the most common entity shape there is. With that arm
// dead, an anonymous `?include=` serves whole rows of a session-required entity
// and the rest of this file stays green.
func TestIncludeGateRefusesADefaultPostureTarget(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Skip("sqlite3 driver not available")
	}
	t.Cleanup(func() { db.Close() })

	app := NewApp(WithConfig(AppConfig{Name: "defposture", APIPrefix: "/api"}), WithDB(db))
	// Default posture: no Access, no Public, no OwnerField. Auto-CRUD requires
	// a session for every operation.
	app.Entity("profiles", EntityConfig{
		Fields:   []schema.Field{{Name: "email", Type: schema.String}},
		Exposure: &entity.ExposureConfig{CRUD: new(true)},
	})
	app.Entity("notes", EntityConfig{
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "profile_id", Type: schema.Relation, To: "profiles"},
		},
		Relations: []entity.Relation{
			{Type: entity.RelManyToOne, Name: "profile", Entity: "profiles", ForeignKey: "profile_id"},
		},
		Exposure: &entity.ExposureConfig{CRUD: new(true), Public: true},
	})
	stop := covStartAndStop(t, app)
	defer stop()
	ta := TestHarness(t, app)

	if _, err := app.DB.Exec(`INSERT INTO profiles (id, email) VALUES ('p1','secret@example.com')`); err != nil {
		t.Fatal(err)
	}
	if _, err := app.DB.Exec(`INSERT INTO notes (id, title, profile_id) VALUES ('n1','Note','p1')`); err != nil {
		t.Fatal(err)
	}

	// Baseline: the target's own route refuses an anonymous caller for the
	// session reason alone, no permission is declared anywhere here.
	if got := ta.Get("/api/profiles").Status(); got != http.StatusUnauthorized {
		t.Fatalf("GET /api/profiles = %d, want 401 — this test rests on the session gate, not on RBAC", got)
	}
	if got := ta.Get("/api/notes").Status(); got != http.StatusOK {
		t.Fatalf("GET /api/notes = %d, want 200", got)
	}

	resp := ta.Get("/api/notes?include=profile")
	if resp.Status() != http.StatusForbidden {
		t.Errorf("GET /api/notes?include=profile = %d, want 403 — an include must not serve rows of a session-required entity to an anonymous caller", resp.Status())
	}
	if strings.Contains(resp.Body(), "secret@example.com") {
		t.Errorf("include leaked a default-posture entity's row:\n%s", resp.Body())
	}

	// A signed-in caller may read it, so the gate must not be a blanket refusal.
	signedIn := ta.AsUser(xoUser{id: "u1"}).Get("/api/notes?include=profile")
	if signedIn.Status() != http.StatusOK {
		t.Errorf("signed-in GET /api/notes?include=profile = %d, want 200 — the session gate must lift once there is a session: %s",
			signedIn.Status(), signedIn.Body())
	}
	if !strings.Contains(signedIn.Body(), "secret@example.com") {
		t.Errorf("signed-in include returned no profile row — the include did not resolve:\n%s", signedIn.Body())
	}
}
