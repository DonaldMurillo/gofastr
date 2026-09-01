package crud

// #358: read-only mounts must not advertise routes they do not serve.
//
// RegisterCrudRoutes with CrudRouteOptions{ReadOnly: true} (what App.View
// uses) mounts only GET /x, GET /x/{id}, GET /x/_events plus llm.md. The
// doc generator and the registry index both consult the mount options now:
// the served llm.md omits the seven write/batch endpoints that answer
// 404/405, the field table drops its Create/Update columns, and the index
// counts and labels the three routes actually served. Same precedent as
// #266 (EntityCRUDMounted): the truth lives in the mount, not in the
// entity declaration.

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// writeRouteHeadings are the doc headings for every endpoint a ReadOnly
// mount does NOT register.
var writeRouteHeadings = []string{
	"### POST {p}",
	"### PUT {p}/{id}",
	"### PATCH {p}/{id}",
	"### DELETE {p}/{id}",
	"### POST {p}/_batch",
	"### PATCH {p}/_batch",
	"### DELETE {p}/_batch",
}

// TestLLMMD_ReadOnlyMountMustNotAdvertiseWriteRoutes asserts the SERVED
// doc against the MOUNTED routes: every write route is first confirmed
// absent from the router, then confirmed absent from the llm.md that same
// router serves. Kept from the #136 audit slice that proved the finding.
func TestLLMMD_ReadOnlyMountMustNotAdvertiseWriteRoutes(t *testing.T) {
	ent, db, r := covSimpleEntity(t)
	ch := NewCrudHandler(ent, db)
	RegisterCrudRoutes(r, ch, "/widgets", CrudRouteOptions{ReadOnly: true})

	// Reality check: the write routes really are not mounted.
	writeReqs := []struct {
		method, path string
	}{
		{http.MethodPost, "/widgets"},
		{http.MethodPut, "/widgets/w1"},
		{http.MethodPatch, "/widgets/w1"},
		{http.MethodDelete, "/widgets/w1"},
		{http.MethodPost, "/widgets/_batch"},
		{http.MethodPatch, "/widgets/_batch"},
		{http.MethodDelete, "/widgets/_batch"},
	}
	// Require the ROUTER'S ABSENT-ROUTE STATUS, not merely "not 200".
	//
	// This block is the premise of everything below it: the doc is allowed
	// to omit these routes because the server does not serve them. Rejecting
	// only 200 does not establish that. A write route that IS mounted
	// answers 400 for the `{}` body below, or 401 before it ever reaches the
	// handler, and neither trips a not-200 check — so the premise would hold
	// whether or not the routes were mounted, and a change that re-mounted
	// writes on a read-only view would leave this test green.
	for _, w := range writeReqs {
		req := httptest.NewRequest(w.method, w.path, strings.NewReader("{}"))
		req = req.WithContext(handler.SetUser(req.Context(), &testUser{id: "u1"}))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("fixture broken: %s %s answered %d; a read-only mount must give the absent-route status (404/405). Anything else means the route exists and merely rejected this request, which is not the same thing", w.method, w.path, rec.Code)
		}
	}

	// The served llm.md (authed, same gate as List).
	req := httptest.NewRequest(http.MethodGet, "/widgets/llm.md", nil)
	req = req.WithContext(handler.SetUser(req.Context(), &testUser{id: "u1"}))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("ReadOnly mount must still serve llm.md (App.View path relies on it): %d", rec.Code)
	}
	doc := rec.Body.String()
	for _, advertised := range writeRouteHeadings {
		advertised = strings.ReplaceAll(advertised, "{p}", "/widgets")
		if strings.Contains(doc, advertised) {
			t.Errorf("ReadOnly llm.md advertises %q, a route that is not mounted", advertised)
		}
	}
}

// TestEntityLLMMD_ReadOnlyOmitsWriteSections pins the generator itself:
// read-only drops the write sections and the Create/Update columns, while
// the default document keeps both (guard against always-omitting).
func TestEntityLLMMD_ReadOnlyOmitsWriteSections(t *testing.T) {
	ent := entity.Define("widgets", entity.EntityConfig{
		Name:  "widgets",
		Table: "widgets",
		Fields: []schema.Field{
			{Name: "name", Type: schema.String, Required: true},
		},
	}.WithTimestamps(false))

	ro := EntityLLMMD(ent, LLMMDOptions{ReadOnly: true})
	for _, heading := range writeRouteHeadings {
		heading = strings.ReplaceAll(heading, "{p}", "/widgets")
		if strings.Contains(ro, heading) {
			t.Errorf("read-only doc must omit %q:\n%s", heading, ro)
		}
	}
	if strings.Contains(ro, "| Create |") || strings.Contains(ro, "| Update |") {
		t.Errorf("read-only doc must drop the Create/Update columns (no write endpoints exist to describe):\n%s", ro)
	}
	for _, want := range []string{
		"This resource is served read-only",
		"### GET /widgets\n",
		"### GET /widgets/{id}\n",
		"### GET /widgets/_events\n",
	} {
		if !strings.Contains(ro, want) {
			t.Errorf("read-only doc must keep %q:\n%s", want, ro)
		}
	}

	full := EntityLLMMD(ent)
	for _, heading := range writeRouteHeadings {
		heading = strings.ReplaceAll(heading, "{p}", "/widgets")
		if !strings.Contains(full, heading) {
			t.Errorf("default doc must keep %q (ReadOnly must not leak into the full document):\n%s", heading, full)
		}
	}
	if !strings.Contains(full, "| Create |") {
		t.Errorf("default doc must keep the Create/Update columns:\n%s", full)
	}
}

// TestRegistryLLMMD_ReadOnlyMountCountsReadRoutes pins the index: a
// read-only mount is counted as the three routes it serves and labelled
// read-only, instead of inheriting the 8-endpoint CRUD count — five of
// which would 404/405.
func TestRegistryLLMMD_ReadOnlyMountCountsReadRoutes(t *testing.T) {
	reg := stubRegistry{byName: map[string]*entity.Entity{
		"widgets": entity.Define("widgets", entity.EntityConfig{
			Name:  "widgets",
			Table: "widgets",
			Fields: []schema.Field{
				{Name: "name", Type: schema.String},
			},
		}.WithTimestamps(false)),
	}}
	crudMount := func(e *entity.Entity) MountInfo {
		return MountInfo{Mounted: true, ReadOnly: e.GetTable() == "widgets"}
	}

	md := RegistryLLMMD(reg, "MyApp", crudMount)

	wantRow := "| [widgets](/widgets/llm.md) | `/widgets` | 3 | read-only |"
	if !strings.Contains(md, wantRow) {
		t.Errorf("read-only mount must be counted as 3 routes, linked and labelled read-only:\n%s", md)
	}
	if strings.Contains(md, "`/widgets` | 8 ") {
		t.Errorf("read-only mount must not inherit the 8-endpoint count:\n%s", md)
	}
}
