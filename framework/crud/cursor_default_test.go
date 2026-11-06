package crud

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
)

func TestCursorUsesListDefault(t *testing.T) {
	handler, database := setupSecurityTestHandler(t, makeEntityConfig("items", "items", "", []schema.Field{
		{Name: "title", Type: schema.String},
	}, func(config *entity.EntityConfig) {
		config.Pagination = &entity.PaginationConfig{}
	}), `CREATE TABLE items (id TEXT PRIMARY KEY, title TEXT)`)
	for index := range 30 {
		seedRows(t, database, "items", []map[string]any{{"id": fmt.Sprintf("%02d", index), "title": "item"}})
	}
	for _, query := range []string{"cursor=", "cursor=&limit=bad", "cursor=&limit=0"} {
		response := httptest.NewRecorder()
		handler.List()(response, makeRequest(t, RequestOpts{Method: "GET", Path: "/items?" + query, UserID: "alice"}))
		if response.Code != 200 {
			t.Fatalf("list status=%d body=%s", response.Code, response.Body.String())
		}
		var page pagination.CursorPage
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil {
			t.Fatal(err)
		}
		if len(page.Data) != 20 || !page.HasMore {
			t.Fatalf("%s: rows=%d hasMore=%v, want 20/true", query, len(page.Data), page.HasMore)
		}
	}
}
