package crud

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Pins the posture of checkBelongsToScope against relationReachable: a
// belongs_to FK the body names is checked against the target's own
// scopes (owner, tenant, soft delete, ReadScope, Before hooks), but NOT
// against relationReachable, which checkTargetRowScope applies to a
// many_to_many link. That asymmetry is deliberate.
//
// relationReachable refuses a target with Exposure.CRUD=false, and under
// a held-scope token one whose "<table>:read" the token lacks. Applied to
// a belongs_to FK it would refuse every row, so ordinary references stop
// working: posts.author_id onto auth's users (CRUD off by default), or a
// ["items:write"] token creating an item under its own order. An
// m2m link is different: it writes a pivot row, a generated write
// surface onto the target, which CRUD=false opted out of.
//
// Nor is the FK an existence oracle over hidden rows. The write returns
// the caller's own row, never the target's fields. A scoped target is
// looked up under its scopes, so a row another owner holds and a row
// that does not exist answer the same 404 with the same body. An
// unscoped target is not looked up at all, so existing and missing ids
// answer alike (a database FK constraint, when the schema has one,
// answers for the database, the same for every caller).
func TestBelongsToFKSkipsRelationReachableOnPurpose(t *testing.T) {
	ch := relwriteWorld(t)
	off := false
	orders := ch.Registry.All()["orders"]
	orders.Config.Exposure = &entity.ExposureConfig{CRUD: &off}

	post := func(ctx func(context.Context) context.Context, body string) *httptest.ResponseRecorder {
		req := withTestUser(httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(body)), "alice")
		if ctx != nil {
			req = req.WithContext(ctx(req.Context()))
		}
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		ch.Create()(rr, req)
		return rr
	}
	itemsOnly := func(c context.Context) context.Context {
		return access.WithHeldScopes(c, []string{"items:read", "items:write"})
	}

	// An ordinary reference onto a CRUD=false target, and from a token
	// that holds no orders scope, writes.
	if rr := post(nil, `{"id":"i1","label":"a","order_id":"o-alice"}`); rr.Code != http.StatusCreated {
		t.Fatalf("own order on a CRUD=false target = %d (%s), want 201: a belongs_to FK is not a relation read", rr.Code, rr.Body.String())
	}
	if rr := post(itemsOnly, `{"id":"i2","label":"b","order_id":"o-alice"}`); rr.Code != http.StatusCreated {
		t.Fatalf("own order under an items-only token = %d (%s), want 201", rr.Code, rr.Body.String())
	}

	// Hidden and missing answer alike, under both postures.
	for _, ctx := range []func(context.Context) context.Context{nil, itemsOnly} {
		hidden := post(ctx, `{"id":"i3","label":"c","order_id":"o-bob"}`)
		missing := post(ctx, `{"id":"i3","label":"c","order_id":"o-nope"}`)
		if hidden.Code != http.StatusNotFound || missing.Code != http.StatusNotFound {
			t.Fatalf("bob's order = %d, a missing one = %d; want 404 for both", hidden.Code, missing.Code)
		}
		if hidden.Body.String() != missing.Body.String() {
			t.Fatalf("hidden and missing bodies differ:\n %s\n %s", hidden.Body, missing.Body)
		}
	}
}

// An unscoped CRUD=false target is not looked up: an existing and a
// missing id both write, so the status tells the caller nothing about
// the target's rows.
func TestBelongsToFKUnscopedHiddenTargetAnswersAlike(t *testing.T) {
	ch := relwriteWorld(t)
	off := false
	orders := ch.Registry.All()["orders"]
	orders.Config.Exposure = &entity.ExposureConfig{CRUD: &off}
	orders.Config.Scope.OwnerField = ""
	orders.Config.Fields = []schema.Field{{Name: "user_id", Type: schema.String}, {Name: "total", Type: schema.String}}

	for i, fk := range []string{"o-bob", "o-nope"} {
		body := `{"id":"u` + string(rune('0'+i)) + `","label":"x","order_id":"` + fk + `"}`
		req := withTestUser(httptest.NewRequest(http.MethodPost, "/items", strings.NewReader(body)), "alice")
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		ch.Create()(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("order_id %s on an unscoped CRUD=false target = %d (%s), want 201 for existing and missing alike", fk, rr.Code, rr.Body.String())
		}
	}
}
