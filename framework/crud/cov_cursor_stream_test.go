package crud

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// covItems builds an N-row "items" table with the given cursor config.
func covItems(t *testing.T, cfg func(*entity.EntityConfig), n int) (*CrudHandler, *sql.DB) {
	t.Helper()
	db := setupDB(t, `CREATE TABLE items (id TEXT PRIMARY KEY, seq INTEGER, name TEXT)`)
	base := entity.EntityConfig{
		Name: "items", Table: "items",
		Fields: []schema.Field{
			{Name: "seq", Type: schema.Int},
			{Name: "name", Type: schema.String},
		},
	}.WithTimestamps(false)
	if cfg != nil {
		cfg(&base)
	}
	ent := entity.Define("items", base)
	ent.SetDB(db)
	rows := make([]map[string]any, n)
	for i := 0; i < n; i++ {
		rows[i] = map[string]any{"id": string(rune('a' + i)), "seq": i, "name": "n"}
	}
	seedRows(t, db, "items", rows)
	return NewCrudHandler(ent, db).WithJSONCase(CaseSnake), db
}

func TestCursorFields_Defaults(t *testing.T) {
	ch, _ := covItems(t, nil, 0)
	if got := ch.cursorFields(); len(got) != 1 || got[0] != "id" {
		t.Errorf("default cursorFields = %v", got)
	}
}

func TestCursorFields_SingleAndComposite(t *testing.T) {
	ch, _ := covItems(t, func(c *entity.EntityConfig) { c.CursorField = "seq" }, 0)
	if got := ch.cursorFields(); len(got) != 1 || got[0] != "seq" {
		t.Errorf("single cursorField = %v", got)
	}

	ch2, _ := covItems(t, func(c *entity.EntityConfig) { c.CursorFields = []string{"seq"} }, 0)
	got := ch2.cursorFields()
	// PrimaryKey auto-appended.
	if len(got) != 2 || got[0] != "seq" || got[1] != "id" {
		t.Errorf("composite cursorFields = %v", got)
	}

	ch3, _ := covItems(t, func(c *entity.EntityConfig) { c.CursorFields = []string{"seq", "id"} }, 0)
	if got := ch3.cursorFields(); len(got) != 2 {
		t.Errorf("composite with pk = %v", got)
	}
}

func TestCursorList_FirstPageAndNext(t *testing.T) {
	ch, _ := covItems(t, func(c *entity.EntityConfig) { c.CursorField = "seq" }, 5)
	// First page, limit 2.
	req := httptest.NewRequest("GET", "/items?cursor=&limit=2", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cursor page status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var page struct {
		Data    []map[string]any `json:"data"`
		Cursor  string           `json:"cursor"`
		HasMore bool             `json:"hasMore"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Data) != 2 || !page.HasMore || page.Cursor == "" {
		t.Fatalf("first page = %+v", page)
	}

	// Next page using the returned cursor.
	req = httptest.NewRequest("GET", "/items?cursor="+page.Cursor+"&limit=2", nil)
	rec = httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("next page status = %d", rec.Code)
	}
}

func TestCursorList_Composite(t *testing.T) {
	ch, _ := covItems(t, func(c *entity.EntityConfig) { c.CursorFields = []string{"seq", "id"} }, 5)
	req := httptest.NewRequest("GET", "/items?cursor=&limit=2", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("composite cursor status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var page struct {
		Cursor  string `json:"cursor"`
		HasMore bool   `json:"hasMore"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	if page.Cursor == "" {
		t.Fatal("composite cursor empty")
	}
	// Walk to next page with the composite cursor.
	req = httptest.NewRequest("GET", "/items?cursor="+page.Cursor+"&limit=2", nil)
	rec = httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("composite next status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestCursorList_InvalidCursor(t *testing.T) {
	ch, _ := covItems(t, func(c *entity.EntityConfig) { c.CursorField = "seq" }, 3)
	req := httptest.NewRequest("GET", "/items?cursor=not-a-valid-cursor", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid cursor = %d, want 400", rec.Code)
	}
}

func TestCursorList_BackwardDirection(t *testing.T) {
	ch, _ := covItems(t, func(c *entity.EntityConfig) { c.CursorField = "seq" }, 5)
	// Establish a forward cursor first, then walk backward.
	req := httptest.NewRequest("GET", "/items?cursor=&limit=2", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	var page struct {
		Cursor string `json:"cursor"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &page)
	req = httptest.NewRequest("GET", "/items?cursor="+page.Cursor+"&direction=backward&limit=2", nil)
	rec = httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("backward status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestStreamingList_Explicit(t *testing.T) {
	ch, _ := covItems(t, func(c *entity.EntityConfig) { c.MaxListLimit = 500 }, 4)
	req := httptest.NewRequest("GET", "/items?stream=true&limit=10", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stream status = %d", rec.Code)
	}
	body := rec.Body.String()
	var env struct {
		Data    []map[string]any `json:"data"`
		Total   int              `json:"total"`
		PerPage int              `json:"perPage"`
	}
	if err := json.Unmarshal([]byte(body), &env); err != nil {
		t.Fatalf("stream body not valid JSON: %v; body=%s", err, body)
	}
	if env.Total != 4 || len(env.Data) != 4 {
		t.Errorf("stream env = %+v", env)
	}
}

func TestStreamingList_Empty(t *testing.T) {
	ch, _ := covItems(t, nil, 0)
	req := httptest.NewRequest("GET", "/items?stream=true", nil)
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("empty stream status = %d", rec.Code)
	}
	var env struct {
		Data  []map[string]any `json:"data"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("empty stream not JSON: %v", err)
	}
	if env.Total != 0 {
		t.Errorf("empty stream total = %d", env.Total)
	}
}

func TestDecodeCursorAny_ShapeMismatch(t *testing.T) {
	// Single-field consumer fed a non-cursor → error.
	if _, err := decodeCursorAny("garbage", []string{"id"}); err == nil {
		t.Error("garbage cursor should error")
	}
}
