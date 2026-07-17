package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func auditSecretsConfig() entity.EntityConfig {
	return entity.EntityConfig{
		Table: "audit_secrets",
		Fields: []schema.Field{
			{Name: "label", Type: schema.String},
			{Name: "api_token", Type: schema.String},
		},
	}.WithTimestamps(false)
}

func TestAdminAuditRedactsCamelKey(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"auditSecrets": auditSecretsConfig()})
	app.WithAuditLog(framework.AuditConfig{Redact: func(_ string, row map[string]any) map[string]any {
		delete(row, "apiToken")
		return row
	}})

	publicReq := httptest.NewRequest(http.MethodPost, "/audit_secrets", strings.NewReader(`{"label":"public","apiToken":"public-secret"}`))
	publicReq.Header.Set("Content-Type", "application/json")
	publicReq = publicReq.WithContext(handler.SetUser(publicReq.Context(), testUser{"u1"}))
	publicRec := httptest.NewRecorder()
	app.Router().ServeHTTP(publicRec, publicReq)
	if publicRec.Code != http.StatusCreated {
		t.Fatalf("public create = %d: %s", publicRec.Code, publicRec.Body.String())
	}

	h := mountEntityAdmin(t, app, Config{Entities: []string{"auditSecrets"}}, testUser{"u1"})
	adminRec := postForm(h, "/admin/e/audit_secrets/_create", url.Values{
		"label":     {"admin"},
		"api_token": {"admin-secret"},
	})
	if adminRec.Code != http.StatusSeeOther {
		t.Fatalf("admin create = %d: %s", adminRec.Code, adminRec.Body.String())
	}

	rows, err := db.Query(`SELECT diff FROM audit_log WHERE entity = 'auditSecrets'`)
	if err != nil {
		t.Fatalf("query audit rows: %v", err)
	}
	defer rows.Close()

	var diffs []string
	for rows.Next() {
		var diff string
		if err := rows.Scan(&diff); err != nil {
			t.Fatalf("scan audit diff: %v", err)
		}
		diffs = append(diffs, diff)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate audit rows: %v", err)
	}
	if len(diffs) != 2 {
		t.Fatalf("audit rows = %d, want 2", len(diffs))
	}
	joined := strings.Join(diffs, "\n")
	if strings.Contains(joined, "public-secret") {
		t.Fatalf("public audit diff leaked redacted token: %s", joined)
	}
	if strings.Contains(joined, "admin-secret") {
		t.Fatalf("admin audit diff leaked redacted token: %s", joined)
	}
}

func TestAdminRowsUseEntityFieldNames(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"auditSecrets": auditSecretsConfig()})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"auditSecrets"}}, testUser{"u1"})

	createRec := postForm(h, "/admin/e/audit_secrets/_create", url.Values{
		"label":     {"render-check"},
		"api_token": {"indexed-value"},
	})
	if createRec.Code != http.StatusSeeOther {
		t.Fatalf("admin create = %d: %s", createRec.Code, createRec.Body.String())
	}

	listBody := get(h, "/admin/e/audit_secrets").Body.String()
	if !strings.Contains(listBody, "indexed-value") {
		t.Fatalf("list did not index api_token: %s", listBody)
	}

	id := firstID(t, db, "audit_secrets")
	editBody := get(h, "/admin/e/audit_secrets/edit/"+id).Body.String()
	if !strings.Contains(editBody, `value="indexed-value"`) {
		t.Fatalf("edit did not index api_token: %s", editBody)
	}
}

func TestAdminAuditForwardsRequest(t *testing.T) {
	db := newDB(t)
	app := newHostedApp(t, db, map[string]entity.EntityConfig{"auditSecrets": auditSecretsConfig()})
	app.WithAuditLog(framework.AuditConfig{})
	h := mountEntityAdmin(t, app, Config{Entities: []string{"auditSecrets"}}, testUser{"u1"})

	form := url.Values{"label": {"metadata-check"}}
	req := httptest.NewRequest(http.MethodPost, "/admin/e/audit_secrets/_create", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "admin-audit-test/1.0")
	req.RemoteAddr = "203.0.113.42:4321"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("admin create = %d: %s", rec.Code, rec.Body.String())
	}

	var raw string
	if err := db.QueryRow(`SELECT diff FROM audit_log WHERE entity = 'auditSecrets'`).Scan(&raw); err != nil {
		t.Fatalf("query audit diff: %v", err)
	}
	var diff struct {
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal([]byte(raw), &diff); err != nil {
		t.Fatalf("decode audit diff: %v", err)
	}
	if got := diff.Meta["client_ip"]; got != "203.0.113.42" {
		t.Fatalf("client_ip = %v, want 203.0.113.42 (diff=%s)", got, raw)
	}
	if got := diff.Meta["user_agent"]; got != "admin-audit-test/1.0" {
		t.Fatalf("user_agent = %v, want admin-audit-test/1.0 (diff=%s)", got, raw)
	}
}
