package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/access"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

//gofastr:test-reads examples/real-estate/gofastr.yml
func TestPublicListingsOnlyActive(t *testing.T) {
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
	statuses := []string{"draft", "active", "pending", "sold", "rented", "withdrawn"}
	for index, status := range statuses {
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", index)
		if _, err := database.Exec(`INSERT INTO properties (id, title, slug, property_type, listing_type, price, address, city, status) VALUES (?, ?, ?, 'house', 'sale', 1, 'Test address', 'Test city', ?)`, id, status, status, status); err != nil {
			t.Fatal(err)
		}
	}
	policy := access.NewRolePolicy()
	if err := policy.Grant("staff", "properties:write"); err != nil {
		t.Fatal(err)
	}
	reader := handler.SetUser(context.Background(), map[string]any{"id": "reader"})
	staff := access.WithRoles(access.WithPolicy(reader, policy), []string{"staff"})
	for name, caller := range map[string]context.Context{"anonymous": context.Background(), "reader": reader, "staff": staff} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/properties", nil).WithContext(caller)
			response := httptest.NewRecorder()
			app.Router().ServeHTTP(response, request)
			var envelope struct {
				Data []map[string]any `json:"data"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil || response.Code != http.StatusOK {
				t.Fatalf("list = %d, %v: %s", response.Code, err, response.Body.String())
			}
			want := 1
			if name == "staff" {
				want = len(statuses)
			}
			if len(envelope.Data) != want {
				t.Errorf("%s sees %d listings; want %d", name, len(envelope.Data), want)
			}
			for index, status := range statuses {
				request := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/properties/00000000-0000-4000-8000-%012d", index), nil).WithContext(caller)
				response := httptest.NewRecorder()
				app.Router().ServeHTTP(response, request)
				want := http.StatusNotFound
				if name == "staff" || status == "active" {
					want = http.StatusOK
				}
				if response.Code != want {
					t.Errorf("%s reads %s = %d; want %d: %s", name, status, response.Code, want, response.Body.String())
				}
			}
		})
	}
}
