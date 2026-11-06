package crud

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestBelongsToNeedsDeclaredKey(t *testing.T) {
	for _, relation := range []string{"lead", "reviewer"} {
		for _, operation := range []string{"create", "update-new", "update-existing"} {
			t.Run(relation+"/"+operation, func(t *testing.T) {
				foreignKey := relation + "_id"
				database := setupDB(t,
					`CREATE TABLE people (id TEXT PRIMARY KEY, name TEXT)`,
					fmt.Sprintf(`CREATE TABLE parents (id TEXT PRIMARY KEY, title TEXT, %s TEXT)`, foreignKey),
				)
				people := entity.Define("people", entity.EntityConfig{
					Fields:   []schema.Field{{Name: "id", Type: schema.String, AutoGenerate: schema.AutoUUID}, {Name: "name", Type: schema.String}},
					Exposure: &entity.ExposureConfig{Public: true},
				}.WithTimestamps(false))
				parents := entity.Define("parents", entity.EntityConfig{
					Fields:    []schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}},
					Relations: []entity.Relation{entity.BelongsTo(relation, "people", foreignKey).WithCascadeWrite(true)},
					Exposure:  &entity.ExposureConfig{Public: true},
				}.WithTimestamps(false))
				people.SetDB(database)
				parents.SetDB(database)
				handler := NewCrudHandler(parents, database)
				handler.Registry = stubRegistry{byName: map[string]*entity.Entity{"people": people, "parents": parents}}
				seedRows(t, database, "people", []map[string]any{{"id": "old", "name": "original"}})
				if operation != "create" {
					seedRows(t, database, "parents", []map[string]any{{"id": "p1", "title": "before", foreignKey: "old"}})
				}
				childID := "new"
				if operation == "update-existing" {
					childID = "old"
				}
				payload := fmt.Sprintf(`{"id":"p1","title":"after",%q:{"id":%q,"name":"SECRET"}}`, relation, childID)
				if operation == "update-new" {
					payload = fmt.Sprintf(`{"title":"after",%q:{"name":"SECRET"}}`, relation)
				}
				method := http.MethodPost
				write := handler.Create()
				if operation != "create" {
					method = http.MethodPatch
					write = handler.Update()
				}
				request := httptest.NewRequest(method, "/parents/p1", strings.NewReader(payload))
				request.SetPathValue("id", "p1")
				request.Header.Set("Content-Type", "application/json")
				response := httptest.NewRecorder()
				write(response, request)
				body := response.Body.String()
				if response.Code != http.StatusBadRequest || !strings.Contains(body, relation) ||
					!strings.Contains(body, "cascade_key_not_writable") || strings.Contains(body, "SECRET") {
					t.Errorf("%s = %d %s, want refused cascade without child data", method, response.Code, body)
				}
				var count int
				if err := database.QueryRow(`SELECT COUNT(*) FROM people`).Scan(&count); err != nil {
					t.Fatal(err)
				}
				var name string
				if err := database.QueryRow(`SELECT name FROM people WHERE id = 'old'`).Scan(&name); err != nil {
					t.Fatal(err)
				}
				if count != 1 || name != "original" {
					t.Errorf("child rows = %d, old name = %q; want 1 and original", count, name)
				}
				if operation == "create" {
					if err := database.QueryRow(`SELECT COUNT(*) FROM parents`).Scan(&count); err != nil {
						t.Fatal(err)
					}
					if count != 0 {
						t.Errorf("created %d parent rows", count)
					}
				} else {
					var title, reference string
					if err := database.QueryRow(fmt.Sprintf(`SELECT title, %s FROM parents WHERE id = 'p1'`, foreignKey)).Scan(&title, &reference); err != nil {
						t.Fatal(err)
					}
					if title != "before" || reference != "old" {
						t.Errorf("parent changed to %q, %q", title, reference)
					}
				}
			})
		}
	}
}
