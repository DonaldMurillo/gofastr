package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// Tours and inquiries take anonymous creates (the public lead forms), but
// their status is the staff workflow. A visitor may not book a tour that
// is already "confirmed" or file an inquiry already "converted": the
// create starts at the initial state, and staff move it from there.
//
//gofastr:test-reads examples/real-estate/gofastr.yml
func TestVisitorCannotSetLeadStatus(t *testing.T) {
	spec, err := appspec.LoadAppSpec("../../examples/real-estate/gofastr.yml")
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	app := framework.NewApp(framework.WithDB(database))
	for _, declaration := range spec.Entities {
		config, err := declaration.Config()
		if err != nil {
			t.Fatal(err)
		}
		app.Entity(declaration.Name, config)
	}
	if err := framework.AutoMigrate(database, app.Registry); err != nil {
		t.Fatal(err)
	}
	const property = "00000000-0000-4000-8000-000000000001"
	if _, err := database.Exec(`INSERT INTO properties (id, title, slug, property_type, listing_type, price, address, city, status) VALUES (?, 'Home', 'home', 'house', 'sale', 1, 'Test address', 'Test city', 'active')`, property); err != nil {
		t.Fatal(err)
	}
	post := func(ctx context.Context, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body)).WithContext(ctx)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		app.Router().ServeHTTP(response, request)
		return response
	}
	leads := []struct {
		path, base, staffState, move, moved string
	}{
		{"/tours", `"property_id":"` + property + `","visitor_name":"V","visitor_email":"v@x.dev","scheduled_at":"2026-12-03T10:00:00Z"`, "confirmed", "confirm", "confirmed"},
		{"/inquiries", `"property_id":"` + property + `","name":"V","email":"v@x.dev","message":"Hi"`, "converted", "contact", "contacted"},
	}
	policy := access.NewRolePolicy()
	for _, perm := range []access.Permission{"tours:read", "tours:write", "inquiries:read", "inquiries:write"} {
		if err := policy.Grant("staff", perm); err != nil {
			t.Fatal(err)
		}
	}
	staff := access.WithRoles(access.WithPolicy(handler.SetUser(context.Background(), map[string]any{"id": "agent"}), policy), []string{"staff"})
	for _, lead := range leads {
		t.Run(strings.TrimPrefix(lead.path, "/"), func(t *testing.T) {
			if response := post(context.Background(), lead.path, "{"+lead.base+`,"status":"`+lead.staffState+`"}`); response.Code != http.StatusUnprocessableEntity {
				t.Errorf("anonymous create with status %q = %d; want 422: %s", lead.staffState, response.Code, response.Body.String())
			}
			response := post(context.Background(), lead.path, "{"+lead.base+"}")
			if response.Code != http.StatusCreated {
				t.Fatalf("anonymous create = %d; want 201: %s", response.Code, response.Body.String())
			}
			var created struct {
				Data map[string]any `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
				t.Fatal(err)
			}
			id, _ := created.Data["id"].(string)
			if id == "" {
				t.Fatalf("no id in %s", response.Body.String())
			}
			// Staff still run the workflow, through its moves.
			moved := post(staff, lead.path+"/"+id+"/transitions/"+lead.move, "")
			if moved.Code != http.StatusOK {
				t.Fatalf("staff %s = %d; want 200: %s", lead.move, moved.Code, moved.Body.String())
			}
			if !strings.Contains(moved.Body.String(), `"`+lead.moved+`"`) {
				t.Errorf("staff %s left %s; want status %q", lead.move, moved.Body.String(), lead.moved)
			}
		})
	}
}
