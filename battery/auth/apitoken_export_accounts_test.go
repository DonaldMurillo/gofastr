package auth

import (
	"context"
	"database/sql"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// NewSQLAPITokenStore registers its table with
// App.ExportData, and the archive carries every token, the service
// accounts' included. NewSQLServiceAccountStore registered nothing, so the
// table those tokens' owners live in was left out of the same archive. A
// restore brought each service token back pointing at an owner that no
// longer exists: TokenMiddleware answers it owner_missing, and every
// machine credential the deployment issued is dead after the restore.
func exportTokenStores(t *testing.T) (*sql.DB, *SQLAPITokenStore, *SQLServiceAccountStore) {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	tokens, err := NewSQLAPITokenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	accounts, err := NewSQLServiceAccountStore(db)
	if err != nil {
		t.Fatal(err)
	}
	return db, tokens, accounts
}

func TestExportRestoreKeepsServiceAccountsTheirTokensName(t *testing.T) {
	ctx := context.Background()
	srcDB, srcTokens, srcAccounts := exportTokenStores(t)
	sa := NewServiceAccount("ci-bot", []string{"deployer"})
	if err := srcAccounts.Create(ctx, sa); err != nil {
		t.Fatal(err)
	}
	plaintext, _, err := IssueToken(ctx, srcTokens, TokenSpec{
		Name: "ci", OwnerKind: OwnerKindService, OwnerID: sa.ID, Scopes: []string{"deploy:write"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if u, reason := resolveTokenOwnerForTest(t, ctx, srcTokens, srcAccounts, plaintext); u == nil {
		t.Fatalf("setup: the token does not resolve before the export: %s", reason)
	}

	dir := t.TempDir()
	if err := framework.NewApp(framework.WithDB(srcDB)).ExportData(ctx, dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	dstDB, dstTokens, dstAccounts := exportTokenStores(t)
	if err := framework.NewApp(framework.WithDB(dstDB)).ImportData(ctx, dir); err != nil {
		t.Fatalf("ImportData: %v", err)
	}

	// The token itself came back...
	tok, err := dstTokens.FindByHash(ctx, sha256hex(plaintext))
	if err != nil || tok == nil {
		t.Fatalf("setup: the service token was not restored: %v", err)
	}
	// ...so its owner has to come back with it.
	if got, _ := dstAccounts.Get(ctx, sa.ID); got == nil {
		t.Errorf("the restore holds service token %q (owner %s %q) but not the service account it names: auth_service_accounts is not in the archive",
			tok.Name, tok.OwnerKind, tok.OwnerID)
	}
	if u, reason := resolveTokenOwnerForTest(t, ctx, dstTokens, dstAccounts, plaintext); u == nil {
		t.Errorf("after export and restore the service token no longer authenticates: %s", reason)
	}
}

// resolveTokenOwnerForTest resolves plaintext the way TokenMiddleware does:
// the token by its hash, then its owner.
func resolveTokenOwnerForTest(t *testing.T, ctx context.Context, tokens APITokenStore, accounts ServiceAccountStore, plaintext string) (User, string) {
	t.Helper()
	tok, err := tokens.FindByHash(ctx, sha256hex(plaintext))
	if err != nil || tok == nil {
		return nil, "token_missing"
	}
	return resolveTokenOwner(ctx, tok, nil, accounts, nil)
}
