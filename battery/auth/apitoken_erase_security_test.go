package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Erasing a user deletes the API tokens they own, so an account later
// given the same id inherits none of them (A30). A service account that
// shares the id keeps its tokens, and a data export carries the table.
func TestEraseRemovesUserAPITokens(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s, err := NewSQLAPITokenStore(db, WithAPITokenTable("machine_tokens"))
	if err != nil {
		t.Fatalf("NewSQLAPITokenStore: %v", err)
	}
	ctx := context.Background()
	for _, tok := range []APIToken{
		{ID: "t1", Name: "mine", OwnerKind: OwnerKindUser, OwnerID: "u1"},
		{ID: "t2", Name: "svc", OwnerKind: OwnerKindService, OwnerID: "u1"},
		{ID: "t3", Name: "theirs", OwnerKind: OwnerKindUser, OwnerID: "u2"},
	} {
		tok.Prefix, tok.CreatedAt = "gf_", time.Now()
		if err := s.Create(ctx, tok, "hash-"+tok.ID); err != nil {
			t.Fatalf("Create %s: %v", tok.ID, err)
		}
	}

	app := framework.NewApp(framework.WithDB(db))
	dir := t.TempDir()
	if err := app.ExportData(ctx, dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	if rows := ndjsonRows(t, dir, "auth_api_tokens-machine_tokens"); len(rows) != 3 {
		t.Errorf("export holds %d token row(s), want 3", len(rows))
	}
	if _, err := app.EraseUserData(ctx, "u1"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	for hash, want := range map[string]bool{"hash-t1": false, "hash-t2": true, "hash-t3": true} {
		got, err := s.FindByHash(ctx, hash)
		if err != nil {
			t.Fatalf("FindByHash %s: %v", hash, err)
		}
		if (got != nil) != want {
			t.Errorf("token %s stored = %v, want %v", hash, got != nil, want)
		}
	}
}
