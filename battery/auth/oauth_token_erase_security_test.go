package auth

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework"
)

// Erasing a user deletes their stored provider tokens, under the table
// name the store was configured with, and a data export carries them.
func TestEraseRemovesOAuthTokens(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	s, err := NewSQLOAuthTokenStore(db, SQLOAuthTokenStoreConfig{
		Table: "provider_tokens", EncryptionKey: []byte("erase-oauth-token-key"),
	})
	if err != nil {
		t.Fatalf("NewSQLOAuthTokenStore: %v", err)
	}
	ctx := context.Background()
	for _, u := range []string{"victim", "bystander"} {
		if err := s.Save(ctx, OAuthTokenRecord{
			UserID: u, Provider: "google", AccessToken: "at-" + u, RefreshToken: "rt-" + u,
			Expiry: time.Now().Add(time.Hour),
		}); err != nil {
			t.Fatalf("Save %s: %v", u, err)
		}
	}

	app := framework.NewApp(framework.WithDB(db))
	dir := t.TempDir()
	if err := app.ExportData(ctx, dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	if rows := ndjsonRows(t, dir, "oauth_tokens"); len(rows) != 2 {
		t.Errorf("export holds %d token row(s), want 2", len(rows))
	}
	if _, err := app.EraseUserData(ctx, "victim"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	if _, err := s.Get(ctx, "victim", "google"); err == nil {
		t.Error("the erased user's provider token is still stored")
	}
	if _, err := s.Get(ctx, "bystander", "google"); err != nil {
		t.Errorf("another user's token: %v", err)
	}
}
