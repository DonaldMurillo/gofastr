package admin

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/battery/auth"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

// Elevation never lifts owner scope, so the audit page and the
// dashboard's activity must not name, or list the values of, a record
// the caller's owner scope hides: neither a live one the scoped read
// refuses nor a deleted one whose stored copy names another owner. The
// caller's own records, live or deleted, still read in full, and a
// cross-owner caller reads every owner's.
func TestAuditHidesOtherOwnersRecords(t *testing.T) {
	memos := entity.EntityConfig{
		Table: "memos",
		Fields: []schema.Field{
			{Name: "title", Type: schema.String, Required: true},
			{Name: "user_id", Type: schema.String, Hidden: true},
		},
		Scope: &entity.ScopeConfig{OwnerField: "user_id"},
	}.WithTimestamps(false)
	x := setup(t, map[string]entity.EntityConfig{"memos": memos}, Config{Entities: []string{"memos"}}, nil)
	x.insert("memos", map[string]any{"id": "m1", "title": "Mine now", "user_id": "admin-1"})
	x.insert("memos", map[string]any{"id": "m2", "title": "Their plan now", "user_id": "u2"})
	now := time.Now().UTC()
	x.seedAuditDiff("a1", "", "memos", "update", "m1", "admin-1", now,
		`{"old":{"title":"Mine before"},"new":{"title":"Mine now"}}`)
	x.seedAuditDiff("a2", "", "memos", "update", "m2", "u2", now.Add(time.Second),
		`{"old":{"title":"Their plan before"},"new":{"title":"Their plan now"}}`)
	x.seedAuditDiff("a3", "", "memos", "delete", "m3", "u2", now.Add(2*time.Second),
		`{"old":{"title":"Their gone memo","user_id":"u2"}}`)
	x.seedAuditDiff("a4", "", "memos", "delete", "m4", "admin-1", now.Add(3*time.Second),
		`{"old":{"title":"My gone memo","user_id":"admin-1"}}`)
	me := asTenant(x.h, &auth.BasicUser{ID: "admin-1", Email: "admin@example.com", Roles: []string{"admin"}}, "")
	for _, page := range []string{"/admin/audit", "/admin"} {
		body := get(me, page).Body.String()
		for _, hidden := range []string{"Their plan", "Their gone memo"} {
			if strings.Contains(body, hidden) {
				t.Errorf("%s shows %q, a record another owner holds", page, hidden)
			}
		}
		for _, shown := range []string{"Mine now", "Mine before", "My gone memo"} {
			if !strings.Contains(body, shown) {
				t.Errorf("%s lacks %q, the caller's own record", page, shown)
			}
		}
	}
	cross := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		me.ServeHTTP(w, r.WithContext(owner.AllowCrossOwner(r.Context())))
	})
	if body := get(cross, "/admin/audit").Body.String(); !strings.Contains(body, "Their gone memo") {
		t.Error("a cross-owner caller does not see another owner's deleted record")
	}
}
