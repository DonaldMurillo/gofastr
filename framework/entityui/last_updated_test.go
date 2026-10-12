package entityui

import (
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// LastUpdated reads the newest updated_at the caller may read: another
// owner's later write never shows on a dashboard card as "Updated just
// now", an anonymous caller gets nothing, and an entity without
// timestamps or rows reports no date.
func TestLastUpdatedStaysInScope(t *testing.T) {
	installOwnerExtractor(t)
	notes := entity.EntityConfig{
		Table:  "notes",
		Fields: []schema.Field{{Name: "title", Type: schema.String}, {Name: "user_id", Type: schema.String, Hidden: true}},
		Scope:  &entity.ScopeConfig{OwnerField: "user_id"},
	}
	tags := entity.EntityConfig{Table: "tags", Fields: []schema.Field{{Name: "name", Type: schema.String}}}
	empty := entity.EntityConfig{Table: "drafts", Fields: []schema.Field{{Name: "name", Type: schema.String}}}
	x := newTestUI(t, map[string]entity.EntityConfig{
		"notes": notes, "tags": tags.WithTimestamps(false), "drafts": empty,
	}, map[string][]map[string]any{
		"notes": {
			{"id": "n1", "title": "mine", "user_id": "u1", "updated_at": "2026-01-02 10:00:00"},
			{"id": "n2", "title": "older", "user_id": "u1", "updated_at": "2025-12-01 10:00:00"},
			{"id": "n3", "title": "theirs", "user_id": "u2", "updated_at": "2026-06-01 10:00:00"},
		},
		"tags": {{"id": "t1", "name": "a"}},
	})
	got, ok := x.ui.LastUpdated(x.userCtx("/dash", "", "u1"), "notes")
	if want := time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC); !ok || !got.Equal(want) {
		t.Fatalf("SECURITY: u1's last update = %v %v, want %v (u2's later write is out of scope)", got, ok, want)
	}
	if got, ok := x.ui.LastUpdated(x.ctx("/dash", ""), "notes"); ok {
		t.Fatalf("SECURITY: an anonymous caller read a last update: %v", got)
	}
	if got, ok := x.ui.LastUpdated(x.userCtx("/dash", "", "u1"), "tags"); ok {
		t.Fatalf("an entity without timestamps reported %v", got)
	}
	if got, ok := x.ui.LastUpdated(x.userCtx("/dash", "", "u1"), "drafts"); ok {
		t.Fatalf("an entity with no rows reported %v", got)
	}
}
