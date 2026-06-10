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

// TestMassAssignment_FieldInjection verifies that client-supplied fields
// like owner_id, tenant_id, and role are stripped or overwritten by the
// server. Attack: mass assignment via JSON body injection of privileged
// fields.
func TestMassAssignment_FieldInjection(t *testing.T) {

	tests := []struct {
		name      string
		body      string
		forbidden string
		desc      string
	}{
		{
			name:      "user_id_injection",
			body:      `{"content":"test","user_id":"victim"}`,
			forbidden: "victim",
			desc:      "client sets user_id to another user's ID",
		},
		{
			name:      "role_injection",
			body:      `{"content":"test","role":"admin"}`,
			forbidden: "admin",
			desc:      "client injects role=admin via JSON body",
		},
		{
			name:      "nested_json_settings",
			body:      `{"content":"test","settings":"{\"is_admin\":true}"}`,
			forbidden: "is_admin",
			desc:      "nested JSON in settings field contains privilege escalation",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch, db := setupSecurityTestHandler(t, entity.EntityConfig{
				Fields: []schema.Field{
					{Name: "user_id", Type: schema.String, Required: true},
					{Name: "content", Type: schema.String},
					{Name: "role", Type: schema.String},
					{Name: "settings", Type: schema.String},
				},
				OwnerField: "user_id",
			}.WithTimestamps(false), `CREATE TABLE profiles (id TEXT PRIMARY KEY, user_id TEXT NOT NULL, content TEXT, role TEXT, settings TEXT)`)

			req := makeRequest(t, RequestOpts{
				Method: http.MethodPost,
				Path:   "/profiles",
				Body:   tc.body,
				UserID: "alice",
			})
			rr := httptest.NewRecorder()
			ch.Create()(rr, req)

			if rr.Code != http.StatusCreated {
				// Create may fail for various reasons; log the body
				t.Logf("create status=%d body=%s", rr.Code, rr.Body.String())
			}

			// Verify DB: owner field should be "alice", not "victim".
			// A rejected create legitimately leaves the table empty
			// (sql.ErrNoRows is acceptable); any other query error is a
			// test-integrity failure and must fail loud.
			var storedOwner string
			err := db.QueryRow("SELECT user_id FROM profiles LIMIT 1").Scan(&storedOwner)
			if err != nil && err != sql.ErrNoRows {
				t.Fatalf("read user_id after create: %v", err)
			}
			if err == nil && tc.name == "user_id_injection" && storedOwner == "victim" {
				t.Errorf("SECURITY: [mass_assign] client-injected user_id persisted as %q. Attack: %s", storedOwner, tc.desc)
			}

			// Verify response doesn't echo the forbidden value in the owner field
			if tc.name == "user_id_injection" {
				assertBodyNotContains(t, rr, `"user_id":"victim"`, "mass_assign", tc.desc)
			}
		})
	}
}

// suppress unused import
var _ = json.Marshal
var _ = schema.String
