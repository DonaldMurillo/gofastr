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
	x     *testUI
	id    string
	steps bool
}

func (s *stepScreen) SetParams(p map[string]string) { s.id = p["id"] }

func (s *stepScreen) RenderCtx(ctx context.Context) render.HTML {
	b := s.x.ui.Record("invoices", s.id).Base("/rec/invoices")
	if s.steps {
		b.Steps()
	}
	return b.RenderCtx(ctx)
}

// stepWorld is u1's three invoices and one of u2's. The token field is
// masked, and its order runs against the ids'.
func stepWorld(t *testing.T, steps bool) (*testUI, *app.App) {
	t.Helper()
	installOwnerExtractor(t)
	ents := invoiceEntities()
	inv := ents["invoices"]
	inv.Fields = append(inv.Fields, schema.Field{Name: "owner_id", Type: schema.String, Hidden: true})
	inv.Scope = &entity.ScopeConfig{OwnerField: "owner_id"}
	ents["invoices"] = inv
	rows := invoiceRows()
	rows["invoices"] = []map[string]any{
		{"id": "a1", "number": "A-1", "token": "z", "status": "draft", "owner_id": "u1"},
		{"id": "a2", "number": "A-2", "token": "m", "status": "draft", "owner_id": "u1"},
		{"id": "a3", "number": "A-3", "token": "a", "status": "draft", "owner_id": "u1"},
		{"id": "b1", "number": "B-1", "token": "b", "status": "draft", "owner_id": "u2"},
	}
	x := newTestUIExt(t, ents, rows, Extensions{}, withAPI(map[string]string{"invoices": "/api/invoices"}))
	a := app.NewApp("rec")
	a.Register("/rec/invoices", &stepScreen{x: x, steps: steps}, nil)
	a.Register("/rec/invoices/:id", &stepScreen{x: x, steps: steps}, nil, app.InterceptFrom("/rec/invoices", app.ScreenDrawer))
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
	x, a := stepWorld(t, true)
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
	x, a := stepWorld(t, true)
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
	x, a = stepWorld(t, false)
	if _, _, ok := drawerSteps(t, x, a, "a2", "/rec/invoices"); ok {
		t.Error("a builder without Steps drew steps")
	}
}
