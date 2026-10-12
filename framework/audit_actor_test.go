package framework

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

type auditActorUser struct{ id string }

func (u *auditActorUser) GetID() string { return u.id }

// With no AuditConfig.Actor the trail names the request's user, so a
// write a signed-in caller made never reads as a system write. A write
// with no user on the request still records no actor.
func TestAuditDefaultActorIsRequestUser(t *testing.T) {
	forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
		app := NewApp(WithDB(db), WithoutDefaultMiddleware())
		app.Entity("posts", entity.EntityConfig{
			Table:    "posts",
			Exposure: &entity.ExposureConfig{Public: true},
			Fields:   []schema.Field{{Name: "title", Type: schema.String, Required: true}},
		}.WithTimestamps(false))
		if err := AutoMigrate(db, app.Registry); err != nil {
			t.Fatalf("automigrate: %v", err)
		}
		app.WithAuditLog(AuditConfig{})
		post := func(user any) {
			req := httptest.NewRequest(http.MethodPost, "/posts", strings.NewReader(`{"title":"x"}`))
			req.Header.Set("Content-Type", "application/json")
			if user != nil {
				req = req.WithContext(handler.SetUser(req.Context(), user))
			}
			rec := httptest.NewRecorder()
			app.Router().ServeHTTP(rec, req)
			if rec.Code != http.StatusCreated {
				t.Fatalf("create = %d %s", rec.Code, rec.Body.String())
			}
		}
		post(&auditActorUser{id: "u-42"})
		post(nil)
		rows := readAuditRows(t, db)
		if len(rows) != 2 {
			t.Fatalf("want 2 audit rows, got %d", len(rows))
		}
		if got := rows[0]["actor_id"]; got != "u-42" {
			t.Errorf("a signed-in write recorded actor %v, want u-42", got)
		}
		if got, ok := rows[1]["actor_id"]; ok {
			t.Errorf("a write with no user recorded actor %v, want none", got)
		}
	})
}
