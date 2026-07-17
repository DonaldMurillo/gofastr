package crud

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func oversizedJSONValue() string {
	return strings.Repeat("A", 2<<20)
}

func bodyLimitEntityConfig(table string) entity.EntityConfig {
	return entity.EntityConfig{
		Table: table,
		Fields: []schema.Field{
			{Name: "id", Type: schema.String, AutoGenerate: schema.AutoUUID},
			{Name: "title", Type: schema.String},
		},
	}.WithTimestamps(false)
}

func TestCreate_RejectsOversizedJSONBody(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, bodyLimitEntityConfig("posts"), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)
	body, err := json.Marshal(map[string]string{"title": oversizedJSONValue()})
	if err != nil {
		t.Fatal(err)
	}
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/posts", Body: string(body), UserID: "u1"})
	rr := httptest.NewRecorder()
	ch.Create()(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("SECURITY: [body-limit] oversized create body returned %d, want 413. Attack: unbounded JSON body read.", rr.Code)
	}
}

func TestBatchCreate_RejectsOversizedJSONBody(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, bodyLimitEntityConfig("posts"), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)
	body, err := json.Marshal(map[string]any{
		"items": []map[string]string{{"title": oversizedJSONValue()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := makeRequest(t, RequestOpts{Method: http.MethodPost, Path: "/posts/_batch", Body: string(body), UserID: "u1"})
	rr := httptest.NewRecorder()
	ch.BatchCreate()(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("SECURITY: [body-limit] oversized batch-create body returned %d, want 413. Attack: unbounded JSON body read.", rr.Code)
	}
}

func TestBatchUpdate_RejectsOversizedJSONBody(t *testing.T) {
	ch, db := setupSecurityTestHandler(t, bodyLimitEntityConfig("posts"), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)
	seedRows(t, db, "posts", []map[string]any{{"id": "post-1", "title": "safe"}})

	body, err := json.Marshal(map[string]any{
		"items": []map[string]string{{"id": "post-1", "title": oversizedJSONValue()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := makeRequest(t, RequestOpts{Method: http.MethodPatch, Path: "/posts/_batch", Body: string(body), UserID: "u1"})
	rr := httptest.NewRecorder()
	ch.BatchUpdate()(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("SECURITY: [body-limit] oversized batch-update body returned %d, want 413. Attack: unbounded JSON body read.", rr.Code)
	}
}

func TestBatchDelete_RejectsOversizedJSONBody(t *testing.T) {
	ch, _ := setupSecurityTestHandler(t, bodyLimitEntityConfig("posts"), `CREATE TABLE posts (id TEXT PRIMARY KEY, title TEXT)`)
	body, err := json.Marshal(map[string]any{
		"ids": []string{fmt.Sprintf("post-%s", oversizedJSONValue())},
	})
	if err != nil {
		t.Fatal(err)
	}
	req := makeRequest(t, RequestOpts{Method: http.MethodDelete, Path: "/posts/_batch", Body: string(body), UserID: "u1"})
	rr := httptest.NewRecorder()
	ch.BatchDelete()(rr, req)
	if rr.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("SECURITY: [body-limit] oversized batch-delete body returned %d, want 413. Attack: unbounded JSON body read.", rr.Code)
	}
}
