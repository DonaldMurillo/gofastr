package admin

import (
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// An entity keyed on two columns: its record screen, crumbs and write
// routes take one path segment per key column, and a value holding "/"
// is escaped in its segment.

func enrollmentsEnv(t *testing.T) *env {
	t.Helper()
	x := setup(t, map[string]entity.EntityConfig{"enrollments": entity.EntityConfig{
		Table:        "enrollments",
		PrimaryKey:   []string{"student_id", "course_id"},
		SearchFields: []string{"grade"},
		Fields: []schema.Field{
			{Name: "student_id", Type: schema.String, Required: true},
			{Name: "course_id", Type: schema.String, Required: true},
			{Name: "grade", Type: schema.String},
		},
	}.WithTimestamps(false)}, Config{Entities: []string{"enrollments"}}, nil)
	x.insert("enrollments", map[string]any{"student_id": "s1", "course_id": "c1", "grade": "Alpha"})
	x.insert("enrollments", map[string]any{"student_id": "a/b", "course_id": "c1", "grade": "Gamma"})
	return x
}

func (x *env) grade(student, course string) string {
	x.t.Helper()
	var g string
	err := x.db.QueryRow(`SELECT grade FROM enrollments WHERE student_id = ? AND course_id = ?`, student, course).Scan(&g)
	if err != nil {
		return "<" + err.Error() + ">"
	}
	return g
}

func TestCompositeRecordScreen(t *testing.T) {
	x := enrollmentsEnv(t)
	rr := get(x.as(theAdmin), "/admin/entities/enrollments/s1/c1")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Alpha") {
		t.Errorf("GET s1/c1 = %d, want the record holding Alpha", rr.Code)
	}
	// A value holding "/" reaches its record escaped; unescaped, or one
	// segment short, the path names no record.
	rr = get(x.as(theAdmin), "/admin/entities/enrollments/a%2Fb/c1")
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Gamma") {
		t.Errorf("GET a%%2Fb/c1 = %d, want the record holding Gamma", rr.Code)
	}
	for _, path := range []string{"/admin/entities/enrollments/s1", "/admin/entities/enrollments/a/b/c1"} {
		if rr := get(x.as(theAdmin), path); rr.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rr.Code)
		}
	}
}

// The record crumb takes the id from the route match: a key value
// holding "/" names its record there, where the decoded path splits it.
func TestCompositeCrumbNamesSlashKey(t *testing.T) {
	x := enrollmentsEnv(t)
	body := get(x.as(theAdmin), "/admin/entities/enrollments/a%2Fb/c1").Body.String()
	if !strings.Contains(body, `aria-current="page" class="fui-breadcrumbs__link">a/b</span>`) {
		t.Errorf("the record crumb is not the a/b record's title:\n%s", body)
	}
}

func TestCompositeAdminWrites(t *testing.T) {
	x := enrollmentsEnv(t)
	rr := serve(x.as(theAdmin), jsonReq(http.MethodPatch, "/admin/api/enrollments/a%2Fb/c1", `{"grade":"Delta"}`))
	if rr.Code != http.StatusOK {
		t.Fatalf("PATCH = %d %s", rr.Code, rr.Body.String())
	}
	if got := x.grade("a/b", "c1"); got != "Delta" {
		t.Errorf("a/b|c1 grade = %q, want Delta", got)
	}
	if got := x.grade("s1", "c1"); got != "Alpha" {
		t.Errorf("the PATCH reached s1|c1: grade = %q", got)
	}
	rr = serve(x.as(theAdmin), jsonReq(http.MethodDelete, "/admin/api/enrollments/s1/c1", ""))
	if rr.Code/100 != 2 {
		t.Fatalf("DELETE = %d %s", rr.Code, rr.Body.String())
	}
	if got := x.grade("s1", "c1"); !strings.HasPrefix(got, "<") {
		t.Errorf("s1|c1 survived the delete: grade = %q", got)
	}
}

// Saved views sit beside a two-column key's record routes: the admin
// mounts both (a delete route of _views/_delete/{id} and the key's
// {a}/{b}/_restore once collided in the mux) and a saved view deletes.
func TestCompositeSavedViewsMount(t *testing.T) {
	x := setup(t, map[string]entity.EntityConfig{"enrollments": entity.EntityConfig{
		Table:      "enrollments",
		PrimaryKey: []string{"student_id", "course_id"},
		Scope:      &entity.ScopeConfig{SoftDelete: true},
		Fields: []schema.Field{
			{Name: "student_id", Type: schema.String, Required: true},
			{Name: "course_id", Type: schema.String, Required: true},
			{Name: "grade", Type: schema.String},
		},
	}.WithTimestamps(false)}, Config{Entities: []string{"enrollments"}, SavedViews: true}, nil)
	x.insert("enrollments", map[string]any{"student_id": "s1", "course_id": "c1", "grade": "Alpha"})
	h := x.as(theAdmin)
	rr := serve(h, newPost("/admin/api/enrollments/_views", url.Values{
		"name": {"Alphas"}, "filter": {`grade = "Alpha"`}, "back": {"/admin/entities/enrollments"},
	}))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("save view = %d %s", rr.Code, rr.Body.String())
	}
	saved, err := url.Parse(rr.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	id := saved.Query().Get("saved")
	rr = serve(h, newPost("/admin/api/enrollments/_views/_delete?id="+url.QueryEscape(id), url.Values{"back": {"/admin/entities/enrollments"}}))
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("delete view = %d %s", rr.Code, rr.Body.String())
	}
	if page := get(h, "/admin/entities/enrollments").Body.String(); strings.Contains(page, "Alphas") {
		t.Fatal("the deleted view is still listed")
	}
	rr = serve(h, jsonReq(http.MethodDelete, "/admin/api/enrollments/s1/c1", ""))
	if rr.Code/100 != 2 {
		t.Fatalf("DELETE = %d %s", rr.Code, rr.Body.String())
	}
	if rr := serve(h, jsonReq(http.MethodPost, "/admin/api/enrollments/s1/c1/_restore", `{}`)); rr.Code != http.StatusOK {
		t.Fatalf("restore = %d %s", rr.Code, rr.Body.String())
	}
}

// A composite id is already one escaped segment per key column: the
// record links the palette, the activity feed and the audit log draw
// keep it as is, never escaping it a second time.
func TestCompositeRecordHref(t *testing.T) {
	x := enrollmentsEnv(t)
	x.insert("enrollments", map[string]any{"student_id": "a b", "course_id": "c1", "grade": "Epsilon"})
	e, _ := x.b.exposedNamed("enrollments")
	if got, want := x.b.recordHref(e, entity.JoinKey([]string{"a b", "c1"})), "/admin/entities/enrollments/a%20b/c1"; got != want {
		t.Errorf("recordHref = %q, want %q", got, want)
	}
	for _, bad := range []string{"s1", "a%2520b/c1/x", "%61/c1"} {
		if got := x.b.recordHref(e, bad); got != "" {
			t.Errorf("recordHref(%q) = %q, want none", bad, got)
		}
	}
	body := post(x.as(theAdmin), "/admin/_palette", url.Values{"q": {"Epsilon"}}).Body.String()
	if !strings.Contains(body, "/admin/entities/enrollments/a%20b/c1") {
		t.Errorf("the palette does not link the record by its key:\n%s", body)
	}
	if strings.Contains(body, "%2520") {
		t.Error("the palette escaped the composite id twice")
	}
	if rr := get(x.as(theAdmin), "/admin/entities/enrollments/a%20b/c1"); rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "Epsilon") {
		t.Errorf("GET the linked record = %d, want the record holding Epsilon", rr.Code)
	}
}
