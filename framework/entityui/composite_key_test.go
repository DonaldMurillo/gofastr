package entityui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// An entity keyed on two columns (a join row): its records link, read,
// edit, bulk-delete and export by the key tuple, a value holding "/"
// included, and its key columns are set on create and locked after.

func enrollmentsUI(t *testing.T) *testUI {
	t.Helper()
	installOwnerExtractor(t)
	return newTestUI(t,
		map[string]entity.EntityConfig{"enrollments": {
			PrimaryKey: []string{"student_id", "course_id"},
			Fields: fields(
				schema.Field{Name: "student_id", Type: schema.String, Required: true},
				schema.Field{Name: "course_id", Type: schema.String, Required: true},
				schema.Field{Name: "grade", Type: schema.String},
			),
			Exposure: &entity.ExposureConfig{Public: true},
		}},
		map[string][]map[string]any{"enrollments": {
			{"student_id": "s1", "course_id": "c1", "grade": "A"},
			{"student_id": "s1", "course_id": "c2", "grade": "B"},
			{"student_id": "a/b", "course_id": "c1", "grade": "C"},
		}},
		withAPI(map[string]string{"enrollments": "/api/enrollments"}),
		withSingleConn(),
	)
}

func postBulkTo(t *testing.T, x *testUI, name string, ctx context.Context, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/"+name+"/_bulk", strings.NewReader(string(raw))).WithContext(ctx)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	x.ui.BulkHandler(name).ServeHTTP(rec, req)
	return rec
}

func getExportOf(t *testing.T, x *testUI, name string, ctx context.Context, query string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/"+name+"/_export.csv?"+query, nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	x.ui.ExportHandler(name).ServeHTTP(rec, req)
	return rec
}

func enrollmentKeys(t *testing.T, x *testUI) []string {
	t.Helper()
	rows, err := x.db.Query(`SELECT student_id, course_id FROM enrollments ORDER BY student_id, course_id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s, c string
		if err := rows.Scan(&s, &c); err != nil {
			t.Fatal(err)
		}
		out = append(out, s+"|"+c)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestCompositeListLinksKeyTuple(t *testing.T) {
	x := enrollmentsUI(t)
	html := listHTML(t, x.ui.List("enrollments").Base("/enrollments"), x.userCtx("/enrollments", "", "u1"))
	for _, want := range []string{`href="/enrollments/s1/c1"`, `href="/enrollments/s1/c2"`, `href="/enrollments/a%2Fb/c1"`} {
		if !strings.Contains(html, want) {
			t.Errorf("the list is missing the record link %s:\n%s", want, html)
		}
	}
}

func TestCompositeRecordReadsByTuple(t *testing.T) {
	x := enrollmentsUI(t)
	id := entity.JoinKey([]string{"a/b", "c1"})
	html := string(x.ui.Record("enrollments", id).Base("/enrollments").
		RenderCtx(x.userCtx("/enrollments/a%2Fb/c1", "", "u1")))
	if !strings.Contains(html, `value="C"`) && !strings.Contains(html, ">C<") {
		t.Errorf("the record a/b|c1 did not read its grade C:\n%s", html)
	}
	// The edit form keeps the key: both columns draw read-only, never as
	// inputs the write would carry.
	for _, refuse := range []string{`name="student_id"`, `name="course_id"`} {
		if strings.Contains(html, refuse) {
			t.Errorf("the edit form submits key column %s:\n%s", refuse, html)
		}
	}
	if !strings.Contains(html, "/api/enrollments/a%2Fb/c1") {
		t.Errorf("the edit form does not write to the tuple's path:\n%s", html)
	}
}

// With no Base, a record screen bases at its request path less one
// segment per key column: the same screen Base("/enrollments") draws.
func TestCompositeRecordDefaultBase(t *testing.T) {
	x := enrollmentsUI(t)
	id := entity.JoinKey([]string{"s1", "c1"})
	var want string
	for _, p := range []string{"/enrollments/s1/c1", "/enrollments/s1/c1/"} {
		// One request per render: a request's id scope hands each
		// derived id out once.
		got := string(x.ui.Record("enrollments", id).Delete().Duplicate().RenderCtx(x.userCtx(p, "", "u1")))
		want = string(x.ui.Record("enrollments", id).Delete().Duplicate().Base("/enrollments").RenderCtx(x.userCtx(p, "", "u1")))
		if got != want {
			t.Errorf("at %s the record screen with no Base differs from Base(/enrollments):\n%s\nwant:\n%s", p, got, want)
		}
	}
	if !strings.Contains(want, `"/enrollments/create?`) {
		t.Errorf("the record screen draws no base-derived link, so the comparison proves nothing:\n%s", want)
	}
}

// A list or record mounted at the root bases at "/": its links join
// without doubling the slash, which a browser reads as a host (//s1/c1
// is the host s1).
func TestRootBaseLinksOneSlash(t *testing.T) {
	x := enrollmentsUI(t)
	list := listHTML(t, x.ui.List("enrollments").Base("/"), x.userCtx("/", "", "u1"))
	id := entity.JoinKey([]string{"s1", "c1"})
	ctx := x.userCtx("/s1/c1", "", "u1")
	for name, html := range map[string]string{
		"list":         list,
		"record":       string(x.ui.Record("enrollments", id).Delete().Duplicate().Base("/").RenderCtx(ctx)),
		"default base": string(x.ui.Record("enrollments", id).Delete().Duplicate().RenderCtx(ctx)),
	} {
		if strings.Contains(html, `"//`) {
			i := strings.Index(html, `"//`)
			t.Errorf("%s links a doubled slash: %.80s", name, html[i:])
		}
	}
	if !strings.Contains(list, `href="/s1/c1"`) {
		t.Errorf("the root list does not link /s1/c1:\n%s", list)
	}
}

func TestCompositeCreateSetsKeyColumns(t *testing.T) {
	x := enrollmentsUI(t)
	html := string(x.ui.Create("enrollments").Base("/enrollments").
		RenderCtx(x.userCtx("/enrollments/create", "", "u1")))
	for _, want := range []string{`name="student_id"`, `name="course_id"`, `name="grade"`} {
		if !strings.Contains(html, want) {
			t.Errorf("the create form is missing %s:\n%s", want, html)
		}
	}
}

func TestCompositeCreatePrefillsKey(t *testing.T) {
	x := enrollmentsUI(t)
	html := string(x.ui.Create("enrollments").Base("/enrollments").
		RenderCtx(x.userCtx("/enrollments/create", "?prefill_student_id=s9", "u1")))
	if !strings.Contains(html, `value="s9"`) {
		t.Errorf("the create form dropped the key prefill s9:\n%s", html)
	}
}

// A duplicate copies the row but not its key: the copy would collide on
// the composite key, as on any other unique index.
func TestCompositeDuplicateBlanksKey(t *testing.T) {
	x := enrollmentsUI(t)
	html := string(x.ui.Create("enrollments").Base("/enrollments").
		RenderCtx(x.userCtx("/enrollments/create", "?duplicate=s1%2Fc2", "u1")))
	if !strings.Contains(html, `value="B"`) {
		t.Errorf("the duplicate did not copy grade B:\n%s", html)
	}
	for _, refuse := range []string{`value="s1"`, `value="c2"`} {
		if strings.Contains(html, refuse) {
			t.Errorf("the duplicate copied key value %s:\n%s", refuse, html)
		}
	}
}

func TestCompositeBulkDeleteByTuple(t *testing.T) {
	x := enrollmentsUI(t)
	ids := []string{entity.JoinKey([]string{"s1", "c2"}), entity.JoinKey([]string{"a/b", "c1"})}
	rec := postBulkTo(t, x, "enrollments", bulkCtx("u1", nil),
		map[string]any{"action": "delete", "scope": "selected", "ids": ids})
	if rec.Code != http.StatusOK {
		t.Fatalf("bulk delete = %d %s", rec.Code, rec.Body.String())
	}
	if got := enrollmentKeys(t, x); !slices.Equal(got, []string{"s1|c1"}) {
		t.Fatalf("rows left = %v, want [s1|c1]", got)
	}
}

func TestCompositeExportCarriesKeys(t *testing.T) {
	x := enrollmentsUI(t)
	rec := getExportOf(t, x, "enrollments", bulkCtx("u1", nil), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("export = %d %s", rec.Code, rec.Body.String())
	}
	lines := strings.Split(strings.TrimSpace(rec.Body.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("export has %d lines, want a header and 3 rows:\n%s", len(lines), rec.Body.String())
	}
	if !strings.HasPrefix(lines[0], "student_id,course_id,") {
		t.Errorf("export header = %q, want the key columns first", lines[0])
	}
	if want := "student_id,course_id,Grade,Created At,Updated At"; lines[0] != want {
		t.Errorf("export header = %q, want %q (each key column once)", lines[0], want)
	}
	if !strings.Contains(rec.Body.String(), "a/b,c1,C") {
		t.Errorf("export lost the a/b row:\n%s", rec.Body.String())
	}
}

// A named export reads an integer key value only in canonical decimal:
// the database coerces "01" onto the row keyed 1, which a record fetch
// by that id refuses, so the export must too.
func TestExportRefusesIntKeyAlias(t *testing.T) {
	installOwnerExtractor(t)
	x := newTestUI(t,
		map[string]entity.EntityConfig{
			"seats": {
				PrimaryKey: []string{"room_no", "seat_no"},
				Fields: fields(
					schema.Field{Name: "room_no", Type: schema.Int, Required: true},
					schema.Field{Name: "seat_no", Type: schema.Int, Required: true},
					schema.Field{Name: "label", Type: schema.String},
				),
				Exposure: &entity.ExposureConfig{Public: true},
			},
			"rooms": {
				Fields:   fields(schema.Field{Name: "id", Type: schema.Int, Required: true}, schema.Field{Name: "label", Type: schema.String}),
				Exposure: &entity.ExposureConfig{Public: true},
			},
		},
		map[string][]map[string]any{
			"seats": {{"room_no": 1, "seat_no": 2, "label": "window"}},
			"rooms": {{"id": 7, "label": "hall"}},
		},
		withAPI(map[string]string{"seats": "/api/seats", "rooms": "/api/rooms"}),
		withSingleConn(),
	)
	for _, c := range []struct{ name, alias, canon, label string }{
		{"seats", "_id=01%2F2&_id=1%2F%2B2&_id=1%2F2.0", "_id=1%2F2", "window"},
		{"rooms", "_id=07&_id=%2B7&_id=7.0", "_id=7", "hall"},
	} {
		if body := getExportOf(t, x, c.name, bulkCtx("u1", nil), c.canon).Body.String(); !strings.Contains(body, c.label) {
			t.Fatalf("%s: the canonical id exported no row, so the alias check proves nothing:\n%s", c.name, body)
		}
		rec := getExportOf(t, x, c.name, bulkCtx("u1", nil), c.alias)
		if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), c.label) {
			t.Errorf("%s: an alias id exported the row (%d):\n%s", c.name, rec.Code, rec.Body.String())
		}
	}
}

// withDDL runs stmts after the entities migrate, before rows seed: an
// Unmanaged entity's table, with a column type the entity cannot state.
func withDDL(stmts ...string) testUIOption {
	return func(x *testUI) {
		for _, s := range stmts {
			if _, err := x.db.Exec(s); err != nil {
				x.tb.Fatalf("ddl %q: %v", s, err)
			}
		}
	}
}

// A named export lists only rows whose own id it was asked for: a NOCASE
// key column matches "ABC" to the row keyed "abc", which the record route
// refuses, so the export must too.
func TestExportRefusesCollationAlias(t *testing.T) {
	installOwnerExtractor(t)
	x := newTestUI(t,
		map[string]entity.EntityConfig{"tags": entity.EntityConfig{
			PrimaryKey: []string{"code", "scope"},
			Fields: fields(
				schema.Field{Name: "code", Type: schema.String, Required: true},
				schema.Field{Name: "scope", Type: schema.String, Required: true},
				schema.Field{Name: "label", Type: schema.String},
			),
			Unmanaged: true,
			Exposure:  &entity.ExposureConfig{Public: true},
		}.WithTimestamps(false)},
		map[string][]map[string]any{"tags": {{"code": "abc", "scope": "s", "label": "kept"}}},
		withDDL(`CREATE TABLE tags (code TEXT COLLATE NOCASE, scope TEXT, label TEXT, PRIMARY KEY (code, scope))`),
		withAPI(map[string]string{"tags": "/api/tags"}),
		withSingleConn(),
	)
	if body := getExportOf(t, x, "tags", bulkCtx("u1", nil), "_id=abc%2Fs").Body.String(); !strings.Contains(body, "kept") {
		t.Fatalf("the row's own id exported nothing, so the alias check proves nothing:\n%s", body)
	}
	if body := getExportOf(t, x, "tags", bulkCtx("u1", nil), "_id=ABC%2Fs").Body.String(); strings.Contains(body, "kept") {
		t.Errorf("the id ABC/s exported the row keyed abc/s:\n%s", body)
	}
	// 21 records fill a two-column key's predicate, so the alias lands in
	// the next one and reaches the row again there.
	q := "_id=abc%2Fs"
	for i := range 21 {
		q += fmt.Sprintf("&_id=n%d%%2Fs", i)
	}
	q += "&_id=ABC%2Fs"
	if body := getExportOf(t, x, "tags", bulkCtx("u1", nil), q).Body.String(); strings.Count(body, "kept") != 1 {
		t.Errorf("the row exported %d times, want once:\n%s", strings.Count(body, "kept"), body)
	}
}

// The override handler reads a composite record's id from its key
// segments, mounted the way the admin mounts it.
func TestCompositeOverrideByTuple(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"steps": entity.EntityConfig{
			PrimaryKey: []string{"flow", "step"},
			Fields: fields(
				schema.Field{Name: "flow", Type: schema.String, Required: true},
				schema.Field{Name: "step", Type: schema.String, Required: true},
				schema.Field{Name: "status", Type: schema.Enum, Default: "draft", Values: []string{"draft", "live"}},
			),
			States: &entity.StatesConfig{
				Field:       "status",
				Initial:     []string{"draft"},
				Transitions: []entity.Transition{{Key: "publish", From: []string{"draft"}, To: "live"}},
			},
		}.WithTimestamps(false)},
		map[string][]map[string]any{"steps": {
			{"flow": "f1", "step": "a", "status": "draft"},
			{"flow": "f1", "step": "b", "status": "draft"},
		}},
		withAPI(map[string]string{"steps": "/api/steps"}),
		withAudit(nullAudit{}),
		withSingleConn(),
	)
	mustEntity(t, x, "steps").MarkAudited()
	mux := http.NewServeMux()
	mux.Handle("POST /api/steps/{flow}/{step}/_override", x.ui.OverrideHandler("steps"))
	req := httptest.NewRequest(http.MethodPost, "/api/steps/f1/b/_override", strings.NewReader(`{"state":"live","reason":"fix"}`))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(overrideCtx(overridePolicy(t, "steps:override_state"), "boss"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("override = %d %s", rec.Code, rec.Body.String())
	}
	got := map[string]string{}
	rows, err := x.db.Query(`SELECT step, status FROM steps`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var s, st string
		if err := rows.Scan(&s, &st); err != nil {
			t.Fatal(err)
		}
		got[s] = st
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if got["b"] != "live" || got["a"] != "draft" {
		t.Errorf("statuses = %v, want only b live", got)
	}
}

// A field's WireName is the key the CRUD handler returns it under: a key
// column carrying one still names the record, and a plain field still
// shows its value.
func TestCompositeKeyWireName(t *testing.T) {
	x := newTestUI(t,
		map[string]entity.EntityConfig{"seats": {
			PrimaryKey: []string{"room_id", "seat_no"},
			Fields: fields(
				schema.Field{Name: "room_id", Type: schema.String, Required: true, WireName: "room"},
				schema.Field{Name: "seat_no", Type: schema.String, Required: true},
				schema.Field{Name: "holder_name", Type: schema.String, WireName: "holder"},
			),
			Exposure: &entity.ExposureConfig{Public: true},
		}},
		map[string][]map[string]any{"seats": {{"room_id": "r1", "seat_no": "7", "holder_name": "Quinn"}}},
		withAPI(map[string]string{"seats": "/api/seats"}),
	)
	html := listHTML(t, x.ui.List("seats").Base("/seats"), x.userCtx("/seats", "", "u1"))
	for _, want := range []string{`href="/seats/r1/7"`, "Quinn"} {
		if !strings.Contains(html, want) {
			t.Errorf("the list is missing %s:\n%s", want, html)
		}
	}
}
