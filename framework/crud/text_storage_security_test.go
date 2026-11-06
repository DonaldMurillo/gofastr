package crud

import (
	"bytes"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestTextWritesRefuseBadBytes(t *testing.T) {
	database := setupDB(t, `CREATE TABLE text_items (id TEXT PRIMARY KEY, title TEXT, body_text TEXT, payload TEXT)`)
	definition := entity.Define("text_items", entity.EntityConfig{
		Name: "text_items", Table: "text_items",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String},
			{Name: "body_text", Type: schema.Text},
			{Name: "payload", Type: schema.JSON},
		},
	}.WithTimestamps(false))
	definition.SetDB(database)
	crudHandler := NewCrudHandler(definition, database)
	routes := router.New()
	RegisterCrudRoutes(routes, crudHandler, "/text_items")
	for _, field := range []string{"title", "bodyText", "payload"} {
		for _, encoding := range []string{"json", "multipart"} {
			for _, value := range []string{"a\x00b", "bad\xff", "short\xe2\x82"} {
				if encoding == "json" && value != "a\x00b" {
					continue
				}
				t.Run(field+"/"+encoding+"/"+url.QueryEscape(value), func(t *testing.T) {
					fieldValue := value
					if field == "payload" && encoding != "json" {
						fieldValue = `{"nested":["a\u0000b"]}`
					}
					var body bytes.Buffer
					contentType := "application/" + encoding
					switch encoding {
					case "json":
						var input any = fieldValue
						if field == "payload" {
							input = map[string]any{"nested": []any{fieldValue}}
						}
						if err := json.NewEncoder(&body).Encode(map[string]any{field: input}); err != nil {
							t.Fatal(err)
						}
					case "multipart":
						writer := multipart.NewWriter(&body)
						if err := writer.WriteField(field, fieldValue); err != nil {
							t.Fatal(err)
						}
						if err := writer.Close(); err != nil {
							t.Fatal(err)
						}
						contentType = writer.FormDataContentType()
					}
					request := withTestUser(httptest.NewRequest(http.MethodPost, "/text_items", &body), "alice")
					request.Header.Set("Content-Type", contentType)
					response := httptest.NewRecorder()
					routes.ServeHTTP(response, request)
					if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), `"`+field+`"`) || !strings.Contains(response.Body.String(), "invalid_text") {
						t.Errorf("unstorable text answered %d %s, want 400 naming %s", response.Code, response.Body, field)
					}
				})
			}
		}
	}
	var count int
	if err := database.QueryRow(`SELECT COUNT(*) FROM text_items`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("refused writes left %d records: %v", count, err)
	}
	row, err := crudHandler.CreateOne(withTestUserCtx("alice"), map[string]any{"title": "valid café", "body_text": "line\n\ttwo"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := crudHandler.UpdateOne(withTestUserCtx("alice"), row["id"].(string), map[string]any{"body_text": "a\x00b"}); err == nil {
		t.Error("update accepted unstorable text")
	}
	if _, err := crudHandler.UpsertOne(withTestUserCtx("alice"), map[string]any{"id": row["id"], "title": "a\x00b"}); err == nil {
		t.Error("upsert accepted unstorable text")
	}
	var title, bodyText string
	if err := database.QueryRow(`SELECT title, body_text FROM text_items`).Scan(&title, &bodyText); err != nil || title != "valid café" || bodyText != "line\n\ttwo" {
		t.Fatalf("refusal changed existing row: %q %q: %v", title, bodyText, err)
	}
}
