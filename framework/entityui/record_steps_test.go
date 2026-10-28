package entityui

import (
	"context"
	"regexp"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

type stepScreen struct {
	component.ContextOnly
	x  *testUI
	id string
	// list is the list Steps walks; nil draws no steps.
	list func(u *UI) *ListBuilder
}

func (s *stepScreen) SetParams(p map[string]string) { s.id = p["id"] }

func (s *stepScreen) RenderCtx(ctx context.Context) render.HTML {
	b := s.x.ui.Record("invoices", s.id).Base("/rec/invoices")
	if s.list != nil {
		b.Steps(s.list(s.x.ui))
	}
	return b.RenderCtx(ctx)
}

// plainList is the invoices list with no configuration.
func plainList(u *UI) *ListBuilder { return u.List("invoices") }

// stepWorld is u1's three invoices and one of u2's. The token field is
// masked, and its order runs against the ids'. a2 is the one open row,
// outside the drafts view.
func stepWorld(t *testing.T, list func(u *UI) *ListBuilder) (*testUI, *app.App) {
	t.Helper()
	installOwnerExtractor(t)
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Fields = append(inv.Fields, schema.Field{Name: "owner_id", Type: schema.String, Hidden: true})
	inv.Scope = &entity.ScopeConfig{OwnerField: "owner_id"}
	inv.Display.Views = []entity.ListView{{Key: "drafts", Where: `status = "draft"`}}
	inv.Display.Facets = []string{"status"}
	ents["invoices"] = inv
	rows := invoiceRows()
	rows["invoices"] = []map[string]any{
		{"id": "a1", "number": "A-1", "token": "z", "status": "draft", "owner_id": "u1"},
		{"id": "a2", "number": "A-2", "token": "m", "status": "open", "owner_id": "u1"},
		{"id": "a3", "number": "A-3", "token": "a", "status": "draft", "owner_id": "u1"},
		{"id": "b1", "number": "B-1", "token": "b", "status": "draft", "owner_id": "u2"},
	}
	x := newTestUIExt(t, ents, rows, Extensions{}, withAPI(map[string]string{"invoices": "/api/invoices"}))
	a := app.NewApp("rec")
	a.Register("/rec/invoices", &stepScreen{x: x, list: list}, nil)
	a.Register("/rec/invoices/:id", &stepScreen{x: x, list: list}, nil, app.InterceptFrom("/rec/invoices", app.ScreenDrawer))
	return x, a
}

var (
	stepCtlRe  = regexp.MustCompile(`<(?:a|button)[^>]*aria-label="(Previous|Next) record"[^>]*>`)
	stepHrefRe = regexp.MustCompile(`href="([^"]*)"`)
)

// drawerSteps renders id's drawer over origin as u1 and reads its step
// controls: each one's href, "" for a disabled one; ok false when the
// bar draws none.
func drawerSteps(t *testing.T, x *testUI, a *app.App, id, origin string) (prev, next string, ok bool) {
	t.Helper()
	res, err := a.RenderOverlayResult(x.userCtx("/rec/invoices/"+id, "", "u1"), "/rec/invoices/"+id, origin, app.ScreenDrawer)
	if err != nil {
		t.Fatal(err)
	}
	ctls := stepCtlRe.FindAllStringSubmatch(string(res.HTML), -1)
	if len(ctls) == 0 {
		return "", "", false
	}
	if len(ctls) != 2 {
		t.Fatalf("want two step controls, got %d", len(ctls))
	}
	href := func(tag string) string {
		if m := stepHrefRe.FindStringSubmatch(tag); m != nil {
			return m[1]
		}
		return ""
	}
	return href(ctls[0][0]), href(ctls[1][0]), true
}

// A drawer over its list steps through the list's rows in the order the
// list shows them, under the list's own query, as the caller reads them:
// another owner's row is never a neighbour, an end of the list has no
// step past it, and a sort on a masked field is refused the way the list
// refuses it, so the steps never reveal the masked field's order.
func TestDrawerStepsFollowTheList(t *testing.T) {
	x, a := stepWorld(t, plainList)
	for _, tc := range []struct {
		name, id, origin, prev, next string
	}{
		{"ascending", "a2", "/rec/invoices?sort=number", "/rec/invoices/a1", "/rec/invoices/a3"},
		{"descending", "a2", "/rec/invoices?sort=number&dir=desc", "/rec/invoices/a3", "/rec/invoices/a1"},
		{"first", "a1", "/rec/invoices?sort=number", "", "/rec/invoices/a2"},
		{"last, before a foreign row", "a3", "/rec/invoices?sort=number", "/rec/invoices/a2", ""},
		{"masked sort refused", "a2", "/rec/invoices?sort=token", "/rec/invoices/a1", "/rec/invoices/a3"},
	} {
		prev, next, ok := drawerSteps(t, x, a, tc.id, tc.origin)
		if !ok || prev != tc.prev || next != tc.next {
			t.Errorf("%s: steps = %q, %q (drawn %v), want %q, %q", tc.name, prev, next, ok, tc.prev, tc.next)
		}
	}
}

// Steps belong to a drawer over the record's own list: a drawer over
// any other page, the full page, and a builder without Steps draw none.
func TestDrawerStepsOnlyOverTheList(t *testing.T) {
	x, a := stepWorld(t, plainList)
	if _, _, ok := drawerSteps(t, x, a, "a2", "/rec/customers/cus-1?sort=number"); ok {
		t.Error("a drawer over another page drew steps")
	}
	page, err := a.RenderPartialResult(x.userCtx("/rec/invoices/a2", "", "u1"), "/rec/invoices/a2")
	if err != nil {
		t.Fatal(err)
	}
	if stepCtlRe.MatchString(string(page.HTML)) {
		t.Error("the full page drew steps")
	}
	x, a = stepWorld(t, nil)
	if _, _, ok := drawerSteps(t, x, a, "a2", "/rec/invoices"); ok {
		t.Error("a builder without Steps drew steps")
	}
	x, a = stepWorld(t, func(u *UI) *ListBuilder { return u.List("customers") })
	if _, _, ok := drawerSteps(t, x, a, "a2", "/rec/invoices"); ok {
		t.Error("Steps over another entity's list drew steps")
	}
	x, a = stepWorld(t, func(u *UI) *ListBuilder { return u.List("invoices").Key("Inv") })
	if _, _, ok := drawerSteps(t, x, a, "a2", "/rec/invoices"); ok {
		t.Error("Steps over a list whose key the list refuses drew steps")
	}
}

// Steps walk the list they were given: its key names its params, and
// its View and Where pins narrow the rows, so the open a2 is never a
// step from a drafts-only list.
func TestDrawerStepsKeepListConfig(t *testing.T) {
	for _, tc := range []struct {
		name, origin string
		list         func(u *UI) *ListBuilder
	}{
		{"keyed view", "/rec/invoices?inv_sort=number&inv_dir=desc",
			func(u *UI) *ListBuilder { return u.List("invoices").Key("inv").View("drafts") }},
		{"pinned", "/rec/invoices?sort=number&dir=desc",
			func(u *UI) *ListBuilder { return u.List("invoices").Where("status", "draft") }},
		// The list draws no facet for a pinned field, so it ignores one in the query.
		{"pinned, facet on the pin", "/rec/invoices?sort=number&dir=desc&f_status=open",
			func(u *UI) *ListBuilder { return u.List("invoices").Where("status", "draft") }},
	} {
		x, a := stepWorld(t, tc.list)
		prev, next, ok := drawerSteps(t, x, a, "a3", tc.origin)
		if !ok || prev != "" || next != "/rec/invoices/a1" {
			t.Errorf("%s: steps = %q, %q (drawn %v), want \"\", /rec/invoices/a1", tc.name, prev, next, ok)
		}
	}
}
