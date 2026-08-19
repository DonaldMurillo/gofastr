package resource

import (
	"context"
	"strings"
	"testing"
)

// A screen's own entity is not the only thing it reads. Relation columns
// resolve the RELATED entity's rows to display labels, reverse-relation
// sections list its rows outright, and dashboard aggregates count over them.
// Each of those is a read of a different entity, governed by that entity's
// posture — not the screen's.
//
// Gating only the screen's own entity left the hole one hop out: a public
// screen with a relation column to a gated entity served the gated entity's
// display values to anonymous callers, on both the full page and the island
// fragment.
func openScreenWithGatedRelation() (Config, *gatedSource) {
	gatedUsers := &gatedSource{
		stubSource: stubSource{rows: []map[string]any{
			{"id": "u1", "name": "Jane Author", "email": "jane@example.com"},
		}},
		canRead: false, // the JSON route for this entity answers 403
	}
	posts := &gatedSource{
		stubSource: stubSource{rows: []map[string]any{
			{"id": "p1", "title": "Hello", "author_id": "u1"},
		}},
		canRead: true, // this screen's own entity is open
	}
	cfg := Config{
		Entity:   "posts",
		Title:    "Posts",
		Singular: "Post",
		BasePath: "/posts",
		APIPath:  "/api/posts",
		Crud:     posts,
		Fields: []Field{
			{Key: "title", Label: "Title", Type: "string"},
			{Key: "author_id", Label: "Author", Type: "relation"},
		},
		Relations: map[string]Relation{
			"author_id": {Crud: gatedUsers, Display: "name"},
		},
	}
	return cfg, gatedUsers
}

func TestRelationLabelsRespectRelatedEntityGate(t *testing.T) {
	cfg, gated := openScreenWithGatedRelation()

	html := string(cfg.List(context.Background()))

	if strings.Contains(html, "Jane Author") {
		t.Errorf("list rendered the related entity's display value while that entity refuses reads:\n%s", html)
	}
	// The screen's own rows must still render — only the relation cell is
	// degraded, and it renders muted rather than falling back to the raw
	// foreign key (see TestGatedRelationRendersMutedNotTheRawID).
	if !strings.Contains(html, "Hello") {
		t.Errorf("gating the relation suppressed the screen's own rows:\n%s", html)
	}
	if len(gated.listCalls) != 0 {
		t.Errorf("related entity was queried %d time(s) despite refusing reads — the check must happen before the read", len(gated.listCalls))
	}
}

func TestRelationLabelsRenderWhenRelatedEntityIsReadable(t *testing.T) {
	cfg, gated := openScreenWithGatedRelation()
	gated.canRead = true

	html := string(cfg.List(context.Background()))

	if !strings.Contains(html, "Jane Author") {
		t.Errorf("a readable relation must still resolve to its display value:\n%s", html)
	}
}

// StatValue and groupCounts read every row of an entity to aggregate it.
// groupCounts is the sharper case: it returns the DISTINCT VALUES of the
// grouped column, so an ungated grouping publishes that column's contents.
func TestDashboardAggregatesRespectEntityGate(t *testing.T) {
	gated := &gatedSource{
		stubSource: stubSource{rows: []map[string]any{
			{"id": "u1", "email": "jane@example.com"},
			{"id": "u2", "email": "bob@example.com"},
		}},
		canRead: false,
	}
	reg := Registry{"users": Config{Entity: "users", Title: "Users", Crud: gated}}

	if got := reg.StatValue(context.Background(), "users", "count", "", "", ""); got != "—" {
		t.Errorf("StatValue over a gated entity = %q, want the empty placeholder", got)
	}
	if got := reg.groupCounts(context.Background(), "users", "email"); len(got) != 0 {
		t.Errorf("groupCounts over a gated entity returned %d value(s) — it publishes the grouped column's contents: %v", len(got), got)
	}
	if len(gated.listCalls) != 0 {
		t.Errorf("gated entity was read %d time(s) by the aggregates", len(gated.listCalls))
	}
}

func TestDashboardAggregatesWorkWhenReadable(t *testing.T) {
	src := &gatedSource{
		stubSource: stubSource{rows: []map[string]any{
			{"id": "u1", "email": "jane@example.com"},
			{"id": "u2", "email": "bob@example.com"},
		}},
		canRead: true,
	}
	reg := Registry{"users": Config{Entity: "users", Title: "Users", Crud: src}}
	if got := reg.StatValue(context.Background(), "users", "count", "", "", ""); got == "—" {
		t.Error("StatValue refused a readable entity")
	}
	if got := reg.groupCounts(context.Background(), "users", "email"); len(got) != 2 {
		t.Errorf("groupCounts over a readable entity returned %d groups, want 2", len(got))
	}
}

// The screen's own entity, checked on each render entry point. List had a test
// via the blueprint gate; Table, Detail, and the pre-filled edit Form did not,
// so three of the four guards were pinned only transitively.
func TestEveryRenderEntryPointRefusesAGatedEntity(t *testing.T) {
	newCfg := func(canRead bool) Config {
		src := &gatedSource{
			stubSource: stubSource{rows: []map[string]any{
				{"id": "s1", "name": "classified"},
			}},
			canRead: canRead,
		}
		return Config{
			Entity: "secrets", Title: "Secrets", Singular: "Secret",
			BasePath: "/secrets", APIPath: "/api/secrets",
			Crud:   src,
			Fields: []Field{{Key: "name", Label: "Name", Type: "string"}},
		}
	}

	renders := map[string]func(Config) string{
		"List":   func(c Config) string { return string(c.List(context.Background())) },
		"Table":  func(c Config) string { return string(c.Table(context.Background())) },
		"Detail": func(c Config) string { return string(c.Detail(context.Background(), "s1")) },
		"Form":   func(c Config) string { return string(c.Form(context.Background(), "s1")) },
	}

	for name, render := range renders {
		t.Run(name+" refuses when the entity is gated", func(t *testing.T) {
			if html := render(newCfg(false)); strings.Contains(html, "classified") {
				t.Errorf("%s rendered a row of an entity that refuses reads:\n%s", name, html)
			}
		})
		t.Run(name+" renders when the entity is readable", func(t *testing.T) {
			if html := render(newCfg(true)); !strings.Contains(html, "classified") {
				t.Errorf("%s refused a readable entity — the guard is too tight:\n%s", name, html)
			}
		})
	}
}

// The CREATE form deliberately does not require read access to the entity
// being created — but it still resolves relation pickers, which read a
// DIFFERENT entity.
func TestCreateFormDoesNotLeakGatedRelationOptions(t *testing.T) {
	gatedOwners := &gatedSource{
		stubSource: stubSource{rows: []map[string]any{{"id": "o1", "name": "Jane Author"}}},
		canRead:    false,
	}
	cfg := Config{
		Entity: "notes", Title: "Notes", Singular: "Note",
		BasePath: "/notes", APIPath: "/api/notes",
		Crud: &gatedSource{stubSource: stubSource{}, canRead: true},
		Fields: []Field{
			{Key: "title", Label: "Title", Type: "string"},
			{Key: "owner_id", Label: "Owner", Type: "relation"},
		},
		Relations: map[string]Relation{"owner_id": {Crud: gatedOwners, Display: "name"}},
	}

	html := string(cfg.Form(context.Background(), ""))
	if strings.Contains(html, "Jane Author") {
		t.Errorf("the create form listed options from a relation the caller may not read:\n%s", html)
	}
}

// Degrading a gated relation must not print the raw foreign key. A bare UUID
// where a name belongs is useless to a reader and discloses an internal id.
func TestGatedRelationRendersMutedNotTheRawID(t *testing.T) {
	cfg, _ := openScreenWithGatedRelation()
	html := string(cfg.List(context.Background()))

	if strings.Contains(html, "u1") {
		t.Errorf("the gated relation's raw foreign key leaked into the cell:\n%s", html)
	}
	if strings.Contains(html, "Jane Author") {
		t.Errorf("the gated relation's display value leaked:\n%s", html)
	}
}

// A reverse-relation section the caller may not read is omitted, not announced.
// A permission callout on a public page tells every visitor which entities
// exist and that they are forbidden.
func TestGatedRelatedSectionIsHiddenNotAnnounced(t *testing.T) {
	gatedItems := &gatedSource{
		stubSource: stubSource{rows: []map[string]any{{"id": "i1", "sku": "SKU-1"}}},
		canRead:    false,
	}
	cfg := Config{
		Entity: "products", Title: "Products", Singular: "Product",
		BasePath: "/products", APIPath: "/api/products",
		Crud: &gatedSource{
			stubSource: stubSource{rows: []map[string]any{{"id": "p1", "name": "Widget"}}},
			canRead:    true,
		},
		Fields: []Field{{Key: "name", Label: "Name", Type: "string"}},
		Related: []RelatedList{
			{Title: "Order items", Crud: gatedItems, ForeignKey: "product_id",
				Fields: []Field{{Key: "sku", Label: "SKU", Type: "string"}}},
		},
	}

	html := string(cfg.Detail(context.Background(), "p1"))
	if strings.Contains(html, "SKU-1") {
		t.Errorf("the gated related section leaked its rows:\n%s", html)
	}
	if strings.Contains(html, "do not have permission") || strings.Contains(html, "Order items") {
		t.Errorf("the gated related section announced itself instead of being omitted:\n%s", html)
	}
	if !strings.Contains(html, "Widget") {
		t.Errorf("hiding the related section suppressed the record itself:\n%s", html)
	}
}

// defaultPostureSource models the asymmetry that caused the original leak: a
// default-posture entity declares no read permission, so the RBAC-only
// predicate answers TRUE for an anonymous caller, while the full read posture
// answers FALSE because auto-CRUD still requires a session.
//
// A stub whose two gates agree cannot detect which one the code consults —
// preferring the narrow gate would keep every other test green while reopening
// exactly this hole.
type defaultPostureSource struct{ stubSource }

func (d *defaultPostureSource) CanRead(context.Context) bool       { return true }
func (d *defaultPostureSource) CanReadScoped(context.Context) bool { return false }

func TestGuardsConsultTheFullReadPostureNotJustRBAC(t *testing.T) {
	src := &defaultPostureSource{stubSource: stubSource{rows: []map[string]any{
		{"id": "s1", "name": "session-required"},
	}}}
	cfg := Config{
		Entity: "notes", Title: "Notes", Singular: "Note",
		BasePath: "/notes", APIPath: "/api/notes",
		Crud:   src,
		Fields: []Field{{Key: "name", Label: "Name", Type: "string"}},
	}
	if html := string(cfg.List(context.Background())); strings.Contains(html, "session-required") {
		t.Errorf("List consulted the RBAC-only gate: a default-posture entity renders to an anonymous caller while its JSON route answers 401.\n%s", html)
	}

	// Same asymmetry, one hop out: a relation whose target is default-posture.
	rel := Config{
		Entity: "posts", Title: "Posts", Singular: "Post",
		BasePath: "/posts", APIPath: "/api/posts",
		Crud: &gatedSource{stubSource: stubSource{rows: []map[string]any{
			{"id": "p1", "title": "Hello", "owner_id": "s1"},
		}}, canRead: true},
		Fields: []Field{
			{Key: "title", Label: "Title", Type: "string"},
			{Key: "owner_id", Label: "Owner", Type: "relation"},
		},
		Relations: map[string]Relation{"owner_id": {Crud: src, Display: "name"}},
	}
	if html := string(rel.List(context.Background())); strings.Contains(html, "session-required") {
		t.Errorf("relation labels consulted the RBAC-only gate for a default-posture target:\n%s", html)
	}
}

// recordAwareSource models a resource-aware Decider: the collection is
// readable, one specific record is not. *crud.CrudHandler answers this through
// CanReadRecordScoped, which passes the id into access.CanResource.
type recordAwareSource struct {
	stubSource
	deniedID string
}

func (r *recordAwareSource) CanRead(context.Context) bool       { return true }
func (r *recordAwareSource) CanReadScoped(context.Context) bool { return true }
func (r *recordAwareSource) CanReadRecordScoped(_ context.Context, id string) bool {
	return id != r.deniedID
}

// The HTTP read-one route passes the path id into the permission check, so a
// decider can allow the listing and deny one row. A screen that asked the
// collection-level question would render a record GET /api/<entity>/{id}
// refuses — the two surfaces disagreeing about the same record.
func TestDetailAndEditFormHonourPerRecordDenial(t *testing.T) {
	src := &recordAwareSource{
		stubSource: stubSource{rows: []map[string]any{
			{"id": "ok1", "name": "visible"},
			{"id": "no1", "name": "restricted"},
		}},
		deniedID: "no1",
	}
	cfg := Config{
		Entity: "records", Title: "Records", Singular: "Record",
		BasePath: "/records", APIPath: "/api/records",
		Crud:   src,
		Fields: []Field{{Key: "name", Label: "Name", Type: "string"}},
	}

	if html := string(cfg.Detail(context.Background(), "no1")); strings.Contains(html, "restricted") {
		t.Errorf("Detail rendered a record the per-record decider denies:\n%s", html)
	}
	if html := string(cfg.Form(context.Background(), "no1")); strings.Contains(html, "restricted") {
		t.Errorf("the edit form pre-filled from a record the per-record decider denies:\n%s", html)
	}
	// And the allowed record must still render, or the guard is useless.
	if html := string(cfg.Detail(context.Background(), "ok1")); !strings.Contains(html, "visible") {
		t.Errorf("Detail refused a record the decider allows:\n%s", html)
	}
}

// narrowOnlySource implements the OLD predicate and not the new one — the
// compatibility branch canRead/canReadCrud fall back to. No other stub reaches
// it, so the fallback shipped unexercised: a source built before CanReadScoped
// existed must still be consulted rather than silently treated as public.
type narrowOnlySource struct {
	stubSource
	canRead bool
}

func (n *narrowOnlySource) CanRead(context.Context) bool { return n.canRead }

func TestNarrowOnlySourceStillGatesViaTheFallback(t *testing.T) {
	newCfg := func(canRead bool) Config {
		return Config{
			Entity: "legacy", Title: "Legacy", Singular: "Legacy",
			BasePath: "/legacy", APIPath: "/api/legacy",
			Crud: &narrowOnlySource{
				stubSource: stubSource{rows: []map[string]any{{"id": "l1", "name": "old-source-row"}}},
				canRead:    canRead,
			},
			Fields: []Field{{Key: "name", Label: "Name", Type: "string"}},
		}
	}
	if html := string(newCfg(false).List(context.Background())); strings.Contains(html, "old-source-row") {
		t.Errorf("a source implementing only CanRead was treated as ungated:\n%s", html)
	}
	if html := string(newCfg(true).List(context.Background())); !strings.Contains(html, "old-source-row") {
		t.Errorf("the fallback refused a source that permits the read:\n%s", html)
	}
}

// And a source implementing NEITHER predicate is ungated by design — the
// documented compatibility contract for custom listers that front no entity.
func TestSourceWithNoPredicateIsUngatedByDesign(t *testing.T) {
	cfg := Config{
		Entity: "computed", Title: "Computed", Singular: "Row",
		BasePath: "/computed", APIPath: "/api/computed",
		Crud:   &stubSource{rows: []map[string]any{{"id": "c1", "name": "computed-row"}}},
		Fields: []Field{{Key: "name", Label: "Name", Type: "string"}},
	}
	if html := string(cfg.List(context.Background())); !strings.Contains(html, "computed-row") {
		t.Errorf("a custom DataSource with no posture to consult must render; refusing it breaks every computed screen:\n%s", html)
	}
}

// relatedRelationLabels resolves the FK columns of a reverse-relation
// section's rows — an invoice listed under a customer still showing its plan
// name. It got the same gate as its twin relationLabels in this change, but
// only the twin was tested: four tests cover the grid-cell resolver and none
// covered this one, so the code read as tested while the detail-page path
// could serve a gated entity's labels with nothing failing.
//
// The section itself is READABLE here. Only the third entity its rows point at
// is gated, which is what isolates this resolver — a gated section would be
// omitted wholesale by an earlier guard and this function would never run.
func TestGatedRelationInsideARelatedSectionRendersMuted(t *testing.T) {
	gatedPlans := &gatedSource{
		stubSource: stubSource{rows: []map[string]any{{"id": "pl1", "name": "Enterprise"}}},
		canRead:    false,
	}
	readablePlans := &gatedSource{
		stubSource: stubSource{rows: []map[string]any{{"id": "pl1", "name": "Enterprise"}}},
		canRead:    true,
	}
	build := func(plans DataSource) string {
		cfg := Config{
			Entity: "customers", Title: "Customers", Singular: "Customer",
			BasePath: "/customers", APIPath: "/api/customers",
			Crud: &gatedSource{
				stubSource: stubSource{rows: []map[string]any{{"id": "c1", "name": "Acme"}}},
				canRead:    true,
			},
			Fields: []Field{{Key: "name", Label: "Name", Type: "string"}},
			Related: []RelatedList{{
				Title: "Invoices", ForeignKey: "customer_id",
				Crud: &gatedSource{
					stubSource: stubSource{rows: []map[string]any{
						{"id": "i1", "ref": "INV-1", "plan_id": "pl1"},
					}},
					canRead: true,
				},
				Fields:    []Field{{Key: "ref", Label: "Ref", Type: "string"}, {Key: "plan_id", Label: "Plan", Type: "string"}},
				Relations: map[string]Relation{"plan_id": {Crud: plans, Display: "name"}},
			}},
		}
		return string(cfg.Detail(context.Background(), "c1"))
	}

	gated := build(gatedPlans)
	// The section is readable, so its own rows must still render — otherwise
	// this test would pass for the wrong reason (nothing rendered at all).
	if !strings.Contains(gated, "INV-1") {
		t.Fatalf("the readable related section did not render its rows, so the label resolver never ran:\n%s", gated)
	}
	if strings.Contains(gated, "Enterprise") {
		t.Errorf("a related section resolved display labels from an entity the caller may not read:\n%s", gated)
	}
	// Muted, not the raw foreign key — the same contract the grid cells hold.
	if strings.Contains(gated, "pl1") {
		t.Errorf("the gated relation fell back to the raw foreign key instead of rendering muted:\n%s", gated)
	}

	// The allow side, or "always redact" would satisfy every check above.
	readable := build(readablePlans)
	if !strings.Contains(readable, "Enterprise") {
		t.Errorf("a readable relation inside a related section rendered no label — the gate is too tight:\n%s", readable)
	}
}
