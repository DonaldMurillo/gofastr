package framework

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// Every route answers a refusal in one envelope (issue #652): the CRUD
// routes' flat {"error": "<text>", "code", "reason", "fields",
// "field_reasons", "success": false}. A declared endpoint's gate, a
// typed handler's error and its bind and body-cap refusals went out
// through core/handler.WriteError, which nested code, reason and message
// under "error", so a client parsing one shape read "[object Object]"
// or nothing at all from the other.
func TestErrorEnvelope_OneShapeOnEveryRoute(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db))
		type pingIn struct {
			Name string `json:"name"`
		}
		typed := handler.HandlerAdapter(func(_ context.Context, in pingIn) (map[string]any, error) {
			switch in.Name {
			case "phrased":
				return nil, handler.Fail(http.StatusConflict, i18n.StatusPhrase(http.StatusConflict))
			case "own":
				return nil, handler.Errorf(http.StatusTeapot, "short and stout")
			case "fields":
				return nil, handler.ValidationError(map[string][]schema.Violation{"name": {{Reason: schema.ReasonRequired}}})
			}
			return map[string]any{"ok": true}, nil
		})
		app.Entity("people", entity.EntityConfig{
			Table:  "people",
			Fields: []schema.Field{{Name: "email", Type: schema.String, Required: true, Unique: true}},
			Endpoints: []entity.Endpoint{
				{Method: "POST", Path: "ping", Handler: typed},
			},
		})
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		t.Cleanup(covStartAndStop(t, app))
		ta := TestHarness(t, app).AsUser(struct{ ID string }{ID: "u1"})
		anon := TestHarness(t, app)
		ta.Post("/people", map[string]any{"email": "a@x.test"}).AssertStatus(t, http.StatusCreated)

		jsonReq := func(h *TestApp, method, path, body string) *TestResponse {
			return h.Request(method, path, strings.NewReader(body)).WithHeader("Content-Type", "application/json").Execute()
		}
		huge := `{"email":"` + strings.Repeat("a", 2<<20) + `"}`
		cases := []struct {
			name       string
			resp       *TestResponse
			status     int
			reason     string
			wantFields bool
		}{
			{"crud 404", ta.Get("/people/00000000-0000-0000-0000-000000000000"), 404, "not_found", false},
			{"crud 409", ta.Post("/people", map[string]any{"email": "a@x.test"}), 409, "conflict", true},
			{"crud validation 400", ta.Post("/people", map[string]any{}), 400, "validation_failed", true},
			{"crud body cap 413", jsonReq(ta, http.MethodPost, "/people", huge), 413, "body_too_large", false},
			{"endpoint gate 401", jsonReq(anon, http.MethodPost, "/people/ping", `{}`), 401, "authentication_required", false},
			{"endpoint cross-site 403", ta.Request(http.MethodPost, "/people/ping", strings.NewReader("x")).WithHeader("Content-Type", "text/plain").WithHeader("Sec-Fetch-Site", "cross-site").Execute(), 403, "cross_site", false},
			{"custom endpoint handler.Fail", jsonReq(ta, http.MethodPost, "/people/ping", `{"name":"phrased"}`), 409, "conflict", false},
			{"custom endpoint handler.Errorf", jsonReq(ta, http.MethodPost, "/people/ping", `{"name":"own"}`), 418, "", false},
			{"custom endpoint validation 400", jsonReq(ta, http.MethodPost, "/people/ping", `{"name":"fields"}`), 400, "validation_failed", true},
			{"custom endpoint bind 400", jsonReq(ta, http.MethodPost, "/people/ping", `{"name":`), 400, "invalid_json", false},
			{"custom endpoint body cap 413", jsonReq(ta, http.MethodPost, "/people/ping", `{"name":"`+strings.Repeat("a", 2<<20)+`"}`), 413, "body_too_large", false},
		}
		for _, c := range cases {
			t.Run(c.name, func(t *testing.T) {
				if c.resp.Status() != c.status {
					t.Fatalf("status %d, want %d (the fixture no longer reaches the refusal): %s", c.resp.Status(), c.status, c.resp.Body())
				}
				var body map[string]any
				if err := json.Unmarshal([]byte(c.resp.Body()), &body); err != nil {
					t.Fatalf("body is not JSON: %v: %s", err, c.resp.Body())
				}
				if msg, ok := body["error"].(string); !ok || msg == "" {
					t.Errorf(`"error" = %#v, want the message text: %s`, body["error"], c.resp.Body())
				}
				if code, ok := body["code"].(float64); !ok || int(code) != c.status {
					t.Errorf(`"code" = %#v, want %d: %s`, body["code"], c.status, c.resp.Body())
				}
				if s, ok := body["success"].(bool); !ok || s {
					t.Errorf(`"success" = %#v, want false: %s`, body["success"], c.resp.Body())
				}
				if got, _ := body["reason"].(string); got != c.reason {
					t.Errorf(`"reason" = %#v, want %q: %s`, body["reason"], c.reason, c.resp.Body())
				}
				if _, has := body["message"]; has {
					t.Errorf(`a nested-envelope "message" key at the top level: %s`, c.resp.Body())
				}
				if f, ok := body["fields"].(map[string]any); c.wantFields && (!ok || len(f) == 0) {
					t.Errorf(`"fields" = %#v, want the field errors: %s`, body["fields"], c.resp.Body())
				}
			})
		}
	})
}
