package crud

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// covFailWriter is a ResponseWriter whose Write fails after failAfter calls,
// used to exercise the streaming list's mid-write error returns.
type covFailWriter struct {
	hdr       http.Header
	failAfter int
	calls     int
	code      int
}

func (w *covFailWriter) Header() http.Header {
	if w.hdr == nil {
		w.hdr = http.Header{}
	}
	return w.hdr
}
func (w *covFailWriter) WriteHeader(code int) { w.code = code }
func (w *covFailWriter) Write(b []byte) (int, error) {
	w.calls++
	if w.calls > w.failAfter {
		return 0, errors.New("write failed")
	}
	return len(b), nil
}

func TestStream_WriteErrorAborts(t *testing.T) {
	ch, _ := covItems(t, nil, 5)
	// failAfter=0 → the very first Write (the `{"data":[` prefix) fails,
	// hitting the early-return branch.
	w := &covFailWriter{failAfter: 0}
	req := withTestUser(httptest.NewRequest("GET", "/items?stream=true", nil), "u1")
	ch.List()(w, req)
	// No panic; handler returned after the failed prefix write.
}

func TestStream_RowWriteErrorAborts(t *testing.T) {
	ch, _ := covItems(t, nil, 5)
	// Allow the prefix + first row, then fail on a subsequent write so the
	// row-encode / comma-write error branches fire.
	w := &covFailWriter{failAfter: 2}
	req := withTestUser(httptest.NewRequest("GET", "/items?stream=true", nil), "u1")
	ch.List()(w, req)
}

// covRelWorldNoMatch builds a posts world where one post has NO author and
// no comments, exercising the not-present branches of rawRelationValue and
// formatRelationValueDeep.
func TestInclude_NotPresentRelations(t *testing.T) {
	db := setupDB(t,
		`CREATE TABLE npusers (id TEXT PRIMARY KEY, name TEXT)`,
		`CREATE TABLE npposts (id TEXT PRIMARY KEY, title TEXT, author_id TEXT)`,
		`CREATE TABLE npcomments (id TEXT PRIMARY KEY, post_id TEXT, body TEXT)`,
		`CREATE TABLE npprofiles (id TEXT PRIMARY KEY, post_id TEXT, bio TEXT)`,
	)
	// p2 has a dangling author_id and no comments/profile.
	seedRows(t, db, "npusers", []map[string]any{{"id": "u1", "name": "alice"}})
	seedRows(t, db, "npposts", []map[string]any{
		{"id": "p1", "title": "has", "author_id": "u1"},
		{"id": "p2", "title": "orphan", "author_id": "missing"},
	})
	seedRows(t, db, "npcomments", []map[string]any{{"id": "c1", "post_id": "p1", "body": "x"}})
	seedRows(t, db, "npprofiles", []map[string]any{{"id": "pr1", "post_id": "p1", "bio": "b"}})

	usersEnt := entity.Define("npusers", entity.EntityConfig{
		Name: "npusers", Table: "npusers",
		Fields: []schema.Field{{Name: "name", Type: schema.String}},
	}.WithTimestamps(false))
	commentsEnt := entity.Define("npcomments", entity.EntityConfig{
		Name: "npcomments", Table: "npcomments",
		Fields: []schema.Field{{Name: "post_id", Type: schema.String}, {Name: "body", Type: schema.String}},
	}.WithTimestamps(false))
	profilesEnt := entity.Define("npprofiles", entity.EntityConfig{
		Name: "npprofiles", Table: "npprofiles",
		Fields: []schema.Field{{Name: "post_id", Type: schema.String}, {Name: "bio", Type: schema.String}},
	}.WithTimestamps(false))
	postsEnt := entity.Define("npposts", entity.EntityConfig{
		Name: "npposts", Table: "npposts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}, {Name: "author_id", Type: schema.String}},
		Relations: []entity.Relation{
			entity.BelongsTo("author", "npusers", "author_id"),
			entity.HasMany("comments", "npcomments", "post_id"),
			entity.HasOne("profile", "npprofiles", "post_id"),
		},
	}.WithTimestamps(false))
	postsEnt.SetDB(db)
	reg := stubRegistry{byName: map[string]*entity.Entity{
		"npusers": usersEnt, "npcomments": commentsEnt, "npprofiles": profilesEnt, "npposts": postsEnt,
	}}
	ch := NewCrudHandler(postsEnt, db).WithJSONCase(CaseSnake)
	ch.Registry = reg

	req := withTestUser(httptest.NewRequest("GET", "/npposts?include=author,comments,profile", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("not-present include = %d, body=%s", rec.Code, rec.Body.String())
	}
	resp := decodeListResponse(t, rec.Body.String())
	var p2 map[string]any
	for _, row := range resp.Data {
		if row["id"] == "p2" {
			p2 = row
		}
	}
	if p2 == nil {
		t.Fatal("p2 missing")
	}
	// author/profile absent → null; comments absent → empty array.
	if p2["author"] != nil {
		t.Errorf("orphan post author should be null, got %v", p2["author"])
	}
	if comments, ok := p2["comments"].([]any); !ok || len(comments) != 0 {
		t.Errorf("orphan post comments should be empty array, got %v", p2["comments"])
	}
}

func TestDeepConvertMap_JSONArrayField(t *testing.T) {
	// A nested relation row carrying a JSON-array value exercises the []any
	// branch of deepConvertMap. CaseCamel so keys get converted.
	ch, _ := covCamelHandler(t)
	in := map[string]any{
		"full_name": "x",
		"tags_list": []any{
			map[string]any{"tag_id": 1},
			"plain",
		},
	}
	out := ch.deepConvertMap(in).(map[string]any)
	arr, ok := out["tagsList"].([]any)
	if !ok {
		t.Fatalf("array field not preserved: %+v", out)
	}
	first, _ := arr[0].(map[string]any)
	if _, ok := first["tagId"]; !ok {
		t.Errorf("nested array map keys not converted: %+v", arr)
	}
}

func TestRawRelationValue_WrongTypes(t *testing.T) {
	// present=true but value is the wrong type → falls back to empty/nil.
	hm := entity.HasMany("comments", "c", "post_id")
	if got := rawRelationValue(hm, "not-a-slice", true); got == nil {
		t.Error("HasMany wrong type should return empty slice, not nil")
	}
	bt := entity.BelongsTo("author", "u", "author_id")
	if got := rawRelationValue(bt, "not-a-map", true); got != nil {
		t.Errorf("BelongsTo wrong type should return nil, got %v", got)
	}
	// not present.
	if got := rawRelationValue(hm, nil, false); got == nil {
		t.Error("HasMany not-present should return empty slice")
	}
	if got := rawRelationValue(bt, nil, false); got != nil {
		t.Errorf("BelongsTo not-present should return nil, got %v", got)
	}
}

func TestFormatRelationValueDeep_WrongTypes(t *testing.T) {
	ch, _ := covNotesHandler(t)
	hm := entity.HasMany("comments", "c", "post_id")
	if got := ch.formatRelationValueDeep(hm, "bad", true); got == nil {
		t.Error("HasMany wrong type should be empty slice")
	}
	bt := entity.BelongsTo("author", "u", "author_id")
	if got := ch.formatRelationValueDeep(bt, "bad", true); got != nil {
		t.Errorf("BelongsTo wrong type should be nil, got %v", got)
	}
	if got := ch.formatRelationValueDeep(bt, nil, false); got != nil {
		t.Errorf("BelongsTo not-present should be nil, got %v", got)
	}
}

func TestMarshalStructToRow_UnmarshalError(t *testing.T) {
	// json.Marshal succeeds (a JSON scalar) but Unmarshal into map fails
	// because the top-level value isn't an object.
	if _, err := MarshalEntity(42); err == nil {
		t.Error("MarshalEntity of a scalar should fail to unmarshal into a map")
	}
}

var _ = context.Background
