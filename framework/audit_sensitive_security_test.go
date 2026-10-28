package framework

import (
	"context"
	"database/sql"
	"net/http"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// TestAuditScrubsEverySpelling pins the default audit scrub against
// every casing the wire keys arrive in (2026-10-27 bug bash S18). The
// app default is camelCase JSON, so hook rows carry oauthToken, not
// oauth_token; a suffix walk over the snake form alone stored the raw
// credential in diff.old and diff.new.
func TestAuditScrubsEverySpelling(t *testing.T) {
	secret := []string{
		"password", "oauth_token", "recovery_secret", "api_key",
		"oauthToken", "resetToken", "recoverySecret", "userPassword",
		"clientSecret", "OAuthToken", "user-password", "APIKey",
		"passwordHash", "session.key",
	}
	for _, k := range secret {
		if !isDefaultSensitiveField(k) {
			t.Errorf("isDefaultSensitiveField(%q) = false, want true", k)
		}
	}
	for _, k := range []string{"name", "email", "status", "tokenCount", "sortKey", "keyboard"} {
		if isDefaultSensitiveField(k) {
			t.Errorf("isDefaultSensitiveField(%q) = true, want false", k)
		}
	}
}

// TestAuditDropsCamelCaseToken drives the wire shape: a reset_token
// column posted as resetToken under the default camelCase JSON never
// reaches the audit diff.
func TestAuditDropsCamelCaseToken(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("accounts", entity.EntityConfig{
			Table:    "accounts",
			Exposure: &entity.ExposureConfig{Public: true},
			Fields: []schema.Field{
				{Name: "title", Type: schema.String, Required: true},
				{Name: "reset_token", Type: schema.String},
			},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		app.WithAuditLog(AuditConfig{Actor: func(context.Context) string { return "rachel" }})
		ta := TestHarness(t, app)
		ta.Post("/accounts", map[string]any{"title": "a", "resetToken": "rt-raw-credential"}).AssertStatus(t, http.StatusCreated)

		rows := readAuditRows(t, db)
		if len(rows) != 1 {
			t.Fatalf("expected 1 audit row, got %d", len(rows))
		}
		diff, _ := rows[0]["diff"].(string)
		if !strings.Contains(diff, `"title"`) {
			t.Fatalf("diff %s carries no title: the row never reached the audit", diff)
		}
		if strings.Contains(diff, "rt-raw-credential") {
			t.Errorf("SECURITY: the audit diff stored the camelCase reset token: %s", diff)
		}
	})
}
