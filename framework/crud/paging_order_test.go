package crud

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestBackwardPageIsAscending asserts a backward cursor page is returned in
// the same logical (ascending) order a forward page uses, not reversed.
func TestBackwardPageIsAscending(t *testing.T) {
	ch, _ := covItems(t, func(c *entity.EntityConfig) { c.CursorField = "seq" }, 5)

	// Forward to page 2 to obtain a cursor pointing past seq=3.
	req := withTestUser(httptest.NewRequest("GET", "/items?cursor=&limit=2", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	var p1 struct {
		Cursor string `json:"cursor"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &p1)

	req = withTestUser(httptest.NewRequest("GET", "/items?cursor="+p1.Cursor+"&limit=2", nil), "u1")
	rec = httptest.NewRecorder()
	ch.List()(rec, req)
	var p2 struct {
		Data   []map[string]any `json:"data"`
		Cursor string           `json:"cursor"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &p2)
	// Forward page rows must be ascending by seq.
	if got := seqOf(p2.Data); len(got) != 2 || got[0] > got[1] {
		t.Fatalf("forward page 2 not ascending: %v", got)
	}

	// Walk backward from page 2's cursor (seq=3): rows with seq<3 → 1,2.
	req = withTestUser(httptest.NewRequest("GET", "/items?cursor="+p2.Cursor+"&direction=backward&limit=2", nil), "u1")
	rec = httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("backward status=%d body=%s", rec.Code, rec.Body.String())
	}
	var back struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &back); err != nil {
		t.Fatal(err)
	}
	got := seqOf(back.Data)
	if len(got) != 2 {
		t.Fatalf("backward page len=%d data=%v", len(got), back.Data)
	}
	// Must be ascending (1,2), matching forward-page ordering — not (2,1).
	if got[0] > got[1] {
		t.Errorf("backward page reverse-ordered: %v (want ascending)", got)
	}
	if got[0] != 1 || got[1] != 2 {
		t.Errorf("backward page = %v, want [1 2]", got)
	}
}

// seqOf extracts the "seq" column from a row slice as ints.
func seqOf(rows []map[string]any) []int {
	out := make([]int, 0, len(rows))
	for _, r := range rows {
		switch v := r["seq"].(type) {
		case float64:
			out = append(out, int(v))
		case int64:
			out = append(out, int(v))
		case int:
			out = append(out, v)
		}
	}
	return out
}

// TestStreamPageTwoSkipsFirstPage asserts the streaming list path honours
// ?page=2 — it must skip the first page rather than re-stream page 1.
func TestStreamPageTwoSkipsFirstPage(t *testing.T) {
	ch, _ := covItems(t, func(c *entity.EntityConfig) { c.MaxListLimit = 500 }, 5)
	req := withTestUser(httptest.NewRequest("GET", "/items?stream=true&limit=2&page=2", nil), "u1")
	rec := httptest.NewRecorder()
	ch.List()(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stream status=%d", rec.Code)
	}
	var env struct {
		Data []map[string]any `json:"data"`
		Page int              `json:"page"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("stream body not JSON: %v body=%s", err, rec.Body.String())
	}
	got := seqOf(env.Data)
	// Page 2 with perPage 2 over seq 0..4 must be seq 2,3.
	if len(got) != 2 || got[0] != 2 || got[1] != 3 {
		t.Errorf("stream page 2 rows = %v, want [2 3] (page offset ignored?)", got)
	}
	if env.Page != 2 {
		t.Errorf("stream envelope page = %d, want 2", env.Page)
	}
}

// TestUpsertNoUpdatableColsReturnsRow asserts UpsertOne returns the existing
// row (not an error) when the entity has no updatable columns beyond the
// conflict key.
func TestUpsertNoUpdatableColsReturnsRow(t *testing.T) {
	db := setupDB(t, `CREATE TABLE keys_only (id TEXT PRIMARY KEY)`)
	ent := entity.Define("keys_only", entity.EntityConfig{
		Name: "keys_only", Table: "keys_only",
	}.WithTimestamps(false))
	ent.SetDB(db)
	if _, err := db.Exec(`INSERT INTO keys_only (id) VALUES ('k1')`); err != nil {
		t.Fatal(err)
	}
	ch := NewCrudHandler(ent, db).WithJSONCase(CaseSnake)

	res, err := ch.UpsertOne(context.Background(), map[string]any{"id": "k1"})
	if err != nil {
		t.Fatalf("upsert with no updatable cols errored: %v", err)
	}
	if res == nil || res["id"] != "k1" {
		t.Fatalf("upsert returned %v, want existing row id=k1", res)
	}
}
