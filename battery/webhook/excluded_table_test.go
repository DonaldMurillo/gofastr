package webhook

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// A subscribers table named excluded collides with the EXCLUDED pseudo-row
// of ON CONFLICT DO UPDATE: the bare name is ambiguous, so the second
// AddSubscriber for an id (the update arm) failed. The upsert aliases its
// target so the pseudo-row is the only thing EXCLUDED names.
func TestSubscribersTableNamedExcluded(t *testing.T) {
	for _, tc := range []struct {
		name string
		db   func(t *testing.T) *sql.DB
	}{
		{"sqlite", func(t *testing.T) *sql.DB {
			db, err := sql.Open("sqlite3", ":memory:")
			if err != nil {
				t.Fatalf("open: %v", err)
			}
			db.SetMaxOpenConns(1)
			t.Cleanup(func() { db.Close() })
			return db
		}},
		{"postgres", pgtest.DB},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, err := NewSQLStore(tc.db(t), WithSQLAllowPlaintext(), WithSQLSubscribersTable("excluded"))
			if err != nil {
				t.Fatalf("new store: %v", err)
			}
			ctx := context.Background()
			sub := Subscriber{ID: "s1", URL: "https://a.example/hook", Secret: "k", Events: []string{"e"}, Active: true, Created: time.Now()}
			if err := s.AddSubscriber(ctx, sub); err != nil {
				t.Fatalf("add: %v", err)
			}
			sub.URL = "https://b.example/hook"
			if err := s.AddSubscriber(ctx, sub); err != nil {
				t.Fatalf("upsert over an existing id: %v", err)
			}
			got, err := s.GetSubscriber(ctx, "s1")
			if err != nil || got == nil {
				t.Fatalf("get: %v, %v", got, err)
			}
			if got.URL != sub.URL {
				t.Errorf("url after upsert = %q, want %q", got.URL, sub.URL)
			}
		})
	}
}
