package entityui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// The read gate: a default-posture entity (no OwnerField, no Access, no
// Public) requires a session on its JSON route, so its list must not
// render rows to an anonymous caller either — the leak resource.canRead
// closed, ported onto entityui.
func TestListRefusesAnonymousDefaultPosture(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"notes": {
			Fields: fields(
				schema.Field{Name: "title", Type: schema.String},
			),
		}},
		map[string][]map[string]any{"notes": {
			{"id": "n1", "title": "classified note"},
		}},
	)
	html := listHTML(t, x.ui.List("notes"), x.ctx("/notes", ""))
	if strings.Contains(html, "classified note") {
		t.Fatalf("anonymous render served rows of a default-posture entity:\n%s", html)
	}
	if !strings.Contains(html, AccessDeniedTitle) {
		t.Fatalf("the access-denied notice is missing:\n%s", html)
	}
}

// A URL naming an unknown view shows All, never an error: it is a
// bookmark, not a bug.
func TestUnknownViewParamShowsAll(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": {
			Fields: fields(
				schema.Field{Name: "name", Type: schema.String},
				schema.Field{Name: "status", Type: schema.Enum, Values: []string{"open", "paid"}},
			),
			Exposure: &entity.ExposureConfig{Public: true},
			Display: &entity.DisplayConfig{Views: []entity.ListView{
				{Key: "open", Where: `status = "open"`},
			}},
		}},
		map[string][]map[string]any{"orders": {
			{"id": "o1", "name": "paid one", "status": "paid"},
		}},
	)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", "?view=nope"))
	if !strings.Contains(html, "paid one") {
		t.Fatalf("an unknown ?view= hid the All rows:\n%s", html)
	}
	if strings.Contains(html, `aria-current="page" href="/orders?view=open"`) {
		t.Fatalf("an unknown view must not light the open tab:\n%s", html)
	}
}

// A default view hidden from this caller by its Show falls back to All.
func TestHiddenDefaultViewFallsBackToAll(t *testing.T) {
	ext := Extensions{Entities: map[string]Extension{"orders": {
		Views: map[string]ViewFunc{
			"open": {
				Filter: func(context.Context) (*filter.Predicate, error) {
					return &filter.Predicate{Field: "status", Op: filter.OpEq, Value: "open"}, nil
				},
				Show: func(context.Context) bool { return false },
			},
		},
	}}}
	x := newTestUIExt(t,
		map[string]entity.EntityConfig{"orders": {
			Fields: fields(
				schema.Field{Name: "name", Type: schema.String},
				schema.Field{Name: "status", Type: schema.Enum, Values: []string{"open", "paid"}},
			),
			Exposure: &entity.ExposureConfig{Public: true},
			Display: &entity.DisplayConfig{Views: []entity.ListView{
				{Key: "open", Default: true},
			}},
		}},
		map[string][]map[string]any{"orders": {
			{"id": "o1", "name": "paid one", "status": "paid"},
		}},
		ext,
	)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", ""))
	if !strings.Contains(html, "paid one") {
		t.Fatalf("a Show-hidden default view did not fall back to All:\n%s", html)
	}
}

// A view func's predicate naming a Hidden, NoQuery or unknown field is
// refused before SQL: field names are spliced into WHERE clauses, so the
// func's own tree passes the same validation URL input gets, every time.
func TestViewFuncPredicateCheckedBeforeSQL(t *testing.T) {
	ext := Extensions{Entities: map[string]Extension{"orders": {
		Views: map[string]ViewFunc{
			"weird": {Filter: func(context.Context) (*filter.Predicate, error) {
				return &filter.Predicate{Field: "not_a_field", Op: filter.OpEq, Value: "x"}, nil
			}},
		},
	}}}
	x := newTestUIExt(t,
		map[string]entity.EntityConfig{"orders": {
			Fields: fields(
				schema.Field{Name: "name", Type: schema.String},
				schema.Field{Name: "secret", Type: schema.String, Hidden: true},
				schema.Field{Name: "memo", Type: schema.String, NoQuery: true},
			),
			Exposure: &entity.ExposureConfig{Public: true},
			Display:  &entity.DisplayConfig{Views: []entity.ListView{{Key: "weird"}}},
		}},
		map[string][]map[string]any{"orders": {
			{"id": "o1", "name": "visible row", "secret": "s3cret", "memo": "m"},
		}},
		ext,
	)
	html := listHTML(t, x.ui.List("orders").View("weird"), x.ctx("/orders", ""))
	if !strings.Contains(html, "Couldn&#39;t load this section") {
		t.Fatalf("a view func predicate naming an unknown field must fail its slot:\n%s", html)
	}
	if strings.Contains(html, "visible row") || strings.Contains(html, "s3cret") {
		t.Fatalf("rows rendered under a refused view predicate:\n%s", html)
	}
}

// A relation the caller cannot read renders muted, never the related
// record's name and never the raw foreign key. The related entity's own
// posture decides, not the screen's.
func TestRelationCellMutedWhenRefused(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{
			"users": {Fields: fields(schema.Field{Name: "name", Type: schema.String})},
			"posts": {
				Fields: fields(
					schema.Field{Name: "title", Type: schema.String},
					schema.Field{Name: "author_id", Type: schema.Relation, To: "users"},
				),
				// The screen's own entity is public; the relation's is
				// default posture, so anonymous callers may not read it.
				Exposure: &entity.ExposureConfig{Public: true},
			},
		},
		map[string][]map[string]any{
			"users": {{"id": "u1", "name": "Jane Author"}},
			"posts": {{"id": "p1", "title": "Hello", "author_id": "u1"}},
		},
	)
	html := listHTML(t, x.ui.List("posts"), x.ctx("/posts", ""))
	if !strings.Contains(html, "Hello") {
		t.Fatalf("gating the relation suppressed the screen's own rows:\n%s", html)
	}
	if strings.Contains(html, "Jane Author") {
		t.Fatalf("the related entity's display value leaked to a caller it refuses:\n%s", html)
	}
	if strings.Contains(html, ">u1<") {
		t.Fatalf("the raw foreign key leaked where a name belongs:\n%s", html)
	}
}

// A relation facet whose entity the caller cannot read shows no options:
// the facet is absent, not an empty control.
func TestRelationFacetRefusedShowsNoOptions(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{
			"users": {Fields: fields(schema.Field{Name: "name", Type: schema.String})},
			"posts": {
				Fields: fields(
					schema.Field{Name: "title", Type: schema.String},
					schema.Field{Name: "author_id", Type: schema.Relation, To: "users"},
				),
				Exposure: &entity.ExposureConfig{Public: true},
				Display:  &entity.DisplayConfig{Facets: []string{"author_id"}},
			},
		},
		map[string][]map[string]any{
			"users": {{"id": "u1", "name": "Jane Author"}},
			"posts": {{"id": "p1", "title": "Hello", "author_id": "u1"}},
		},
	)
	html := listHTML(t, x.ui.List("posts"), x.ctx("/posts", ""))
	if strings.Contains(html, `name="f_author_id"`) {
		t.Fatalf("a refused relation facet rendered a control:\n%s", html)
	}
	if strings.Contains(html, "Jane Author") {
		t.Fatalf("a refused relation facet listed the gated entity's records:\n%s", html)
	}
}

// A builder's pinned .Where stays inside the caller's owner scope: the
// predicate narrows, the scope fences, and another owner's rows never
// appear.
func TestBuilderWhereKeepsOwnerScope(t *testing.T) {
	installOwnerExtractor(t)
	x := newTestUI(t,
		map[string]entity.EntityConfig{"logs": entity.EntityConfig{
			Fields: fields(
				schema.Field{Name: "user_id", Type: schema.String, Required: true},
				schema.Field{Name: "note", Type: schema.String},
			),
			Scope: &entity.ScopeConfig{OwnerField: "user_id"},
		}.WithTimestamps(false)},
		map[string][]map[string]any{"logs": {
			{"id": "a1", "user_id": "alice", "note": "shared text"},
			{"id": "b1", "user_id": "bob", "note": "shared text"},
		}},
	)
	html := listHTML(t, x.ui.List("logs").Where("note", "shared text"), x.userCtx("/logs", "", "alice"))
	if !strings.Contains(html, "a1") {
		t.Fatalf("alice's own row vanished under her scope:\n%s", html)
	}
	if strings.Contains(html, "b1") {
		t.Fatalf("bob's row leaked through the builder Where inside alice's scope:\n%s", html)
	}
}

// Bad filter text does not fail the list: a warning names that the
// filter could not be applied and the rows list without it.
func TestBadFilterWarnsAndStillLists(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": {
			Fields:   fields(schema.Field{Name: "name", Type: schema.String}),
			Exposure: &entity.ExposureConfig{Public: true},
		}},
		map[string][]map[string]any{"orders": {{"id": "o1", "name": "visible row"}}},
	)
	html := listHTML(t, x.ui.List("orders"), x.ctx("/orders", "?filter=nope+%3D"))
	if !strings.Contains(html, "visible row") {
		t.Fatalf("a bad filter failed the whole list:\n%s", html)
	}
	if !strings.Contains(html, "Filter not applied") {
		t.Fatalf("the filter warning is missing:\n%s", html)
	}
}

// A page with two unkeyed lists, or two sharing a key, must be refused:
// the second list with a taken key fails its slot.
func TestDuplicateListKeyFailsSecondSlot(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"orders": {
			Fields:   fields(schema.Field{Name: "name", Type: schema.String}),
			Exposure: &entity.ExposureConfig{Public: true},
		}},
		map[string][]map[string]any{"orders": {{"id": "o1", "name": "first"}}},
	)
	ctx := x.ctx("/page", "")
	first := listHTML(t, x.ui.List("orders"), ctx)
	if !strings.Contains(first, "first") {
		t.Fatalf("the first unkeyed list must render:\n%s", first)
	}
	second := listHTML(t, x.ui.List("orders"), ctx)
	if !strings.Contains(second, "Couldn&#39;t load this section") {
		t.Fatalf("a second unkeyed list on one page must fail its slot:\n%s", second)
	}
	keyed := listHTML(t, x.ui.List("orders").Key("due"), ctx)
	if !strings.Contains(keyed, "first") {
		t.Fatalf("a keyed list beside an unkeyed one must render:\n%s", keyed)
	}
	dupe := listHTML(t, x.ui.List("orders").Key("due"), ctx)
	if !strings.Contains(dupe, "Couldn&#39;t load this section") {
		t.Fatalf("a repeated key must fail its slot:\n%s", dupe)
	}
}

// staticComp is a component that renders fixed HTML.
type staticComp string

func (c staticComp) Render() render.HTML                   { return render.HTML(c) }
func (c staticComp) RenderCtx(context.Context) render.HTML { return render.HTML(c) }

// A replaced list body is the app's component, built under the same read
// gate: an anonymous caller sees the gate's notice, not the app's body.
func TestExtensionListReplacesBody(t *testing.T) {
	ext := Extensions{Entities: map[string]Extension{"notes": {
		List: func(ListContext) (component.Component, error) { return staticComp("<p>app list body</p>"), nil },
	}}}
	x := newTestUIExt(t,
		map[string]entity.EntityConfig{"notes": {
			Fields: fields(schema.Field{Name: "title", Type: schema.String}),
		}},
		map[string][]map[string]any{"notes": {{"id": "n1", "title": "row"}}},
		ext,
	)
	// Anonymous: the entity is default posture, so even the replaced
	// body stays behind the read gate.
	if html := listHTML(t, x.ui.List("notes"), x.ctx("/notes", "")); !strings.Contains(html, AccessDeniedTitle) {
		t.Fatalf("a replaced list body escaped the read gate:\n%s", html)
	}
	x2 := newTestUIExt(t,
		map[string]entity.EntityConfig{"notes": {
			Fields:   fields(schema.Field{Name: "title", Type: schema.String}),
			Exposure: &entity.ExposureConfig{Public: true},
		}},
		map[string][]map[string]any{"notes": {{"id": "n1", "title": "row"}}},
		ext,
	)
	if html := listHTML(t, x2.ui.List("notes"), x2.ctx("/notes", "")); !strings.Contains(html, "app list body") {
		t.Fatalf("the app's replaced list body did not render for an allowed caller:\n%s", html)
	}
}
