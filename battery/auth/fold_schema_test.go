package auth

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// The store creates a mapped "DisplayName" as displayname. The SQLite
// column probe compared names with case, so the second boot added the
// column again and failed on the duplicate.
func TestEnsureSchemaTwiceMixedCaseColumns(t *testing.T) {
	forMixedCaseAuth(t, nil, func(t *testing.T, db *sql.DB) {
		fm := DefaultUserFieldMap()
		fm.Name, fm.EmailVerified = "DisplayName", "EmailVerified"
		for i := range 2 {
			if err := NewEntityUserStore(db, "people", fm).EnsureSchema(context.Background()); err != nil {
				t.Fatalf("EnsureSchema #%d: %v", i+1, err)
			}
		}
	})
}

var testTwoFAKey = EntityTwoFAStoreConfig{EncryptionKey: []byte("0123456789abcdef0123456789abcdef")}

// pgQualifier creates a second schema beside the test's own and returns
// its name; a configured "<it>.Name" is then schema-qualified.
func pgQualifier(t *testing.T, db *sql.DB) string {
	t.Helper()
	var cur string
	if err := db.QueryRow(`SELECT current_schema()`).Scan(&cur); err != nil {
		t.Fatal(err)
	}
	q := cur + "_q"
	mustExec(t, db, "CREATE SCHEMA "+q)
	t.Cleanup(func() { _, _ = db.Exec("DROP SCHEMA " + q + " CASCADE") })
	return q
}

func mustExec(t *testing.T, db *sql.DB, stmts ...string) {
	t.Helper()
	for _, s := range stmts {
		if _, err := db.Exec(s); err != nil {
			t.Fatalf("%s: %v", s, err)
		}
	}
}

func wantRefusal(t *testing.T, label string, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("%s: EnsureSchema = nil, want a refusal naming %q", label, parts)
	}
	for _, p := range parts {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("%s: refusal %q does not name %q", label, err, p)
		}
	}
}

// An older store quoted the mapped columns as written, so a lowercase
// members table kept "PasswordHash"; the folded store reads passwordhash
// and no login works. The guard returned early for a lowercase table,
// and its advice for a case-kept table left such columns behind.
func TestRefusesCaseKeptColumns(t *testing.T) {
	db := pgtest.DB(t)
	ctx := context.Background()
	fm := DefaultUserFieldMap()
	fm.PasswordHash = "PasswordHash"
	cols := ` (id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL, "PasswordHash" TEXT NOT NULL DEFAULT '', roles TEXT NOT NULL DEFAULT '[]', password_set BOOLEAN NOT NULL DEFAULT FALSE)`
	mustExec(t, db, `CREATE TABLE members`+cols, `CREATE TABLE "Crew"`+cols)
	wantRefusal(t, "members", NewEntityUserStore(db, "members", fm).EnsureSchema(ctx),
		`RENAME COLUMN "PasswordHash" TO "passwordhash"`)
	wantRefusal(t, "Crew", NewEntityUserStore(db, "Crew", fm).EnsureSchema(ctx),
		`RENAME TO "crew"`, `RENAME COLUMN "PasswordHash" TO "passwordhash"`)
}

// The older store quoted a configured "q.TwoFA" whole: one table literally
// named q.TwoFA in the current schema. The folded store reads table twofa
// in schema q, so EnsureSchema made an empty one there and every
// enrolment vanished.
func TestRefusesLiteralDottedTable(t *testing.T) {
	db := pgtest.DB(t)
	q := pgQualifier(t, db)
	for _, name := range []string{q + ".TwoFA", q + ".twofa_low"} {
		mustExec(t, db,
			`CREATE TABLE "`+name+`" (user_id TEXT PRIMARY KEY)`,
			`INSERT INTO "`+name+`" (user_id) VALUES ('u1')`)
		store, err := NewEntityTwoFAStore(db, name, testTwoFAKey)
		if err != nil {
			t.Fatal(err)
		}
		wantRefusal(t, name, store.EnsureSchema(context.Background()), `"`+name+`"`)
	}
}

// A schema-qualified table named its indexes "q"."members_…": Postgres
// refuses a qualified index name (an index lands in its table's schema).
func TestQualifiedTableIndexes(t *testing.T) {
	db := pgtest.DB(t)
	q := pgQualifier(t, db)
	ctx := context.Background()
	if err := NewEntityUserStore(db, q+".Members").EnsureSchema(ctx); err != nil {
		t.Errorf("user store: %v", err)
	}
	if err := NewEntitySessionStore(db, q+".Sessions").EnsureSchema(ctx); err != nil {
		t.Errorf("session store: %v", err)
	}
	pk, err := NewSQLPasskeyStore(db, q+".Passkeys")
	if err != nil {
		t.Fatal(err)
	}
	if err := pk.EnsureSchema(ctx); err != nil {
		t.Errorf("passkey store: %v", err)
	}
	// One users index, one passkeys index, and three on sessions
	// (expires_at, token, user_id: EnsureSchema creates the last two
	// whoever made the table).
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM pg_indexes WHERE schemaname = $1 AND indexname NOT LIKE '%pkey' AND indexname NOT LIKE '%key'`, q).Scan(&n); err != nil || n != 5 {
		t.Fatalf("store indexes in %s = %d (%v), want 5", q, n, err)
	}
}

// The INTEGER-to-BOOLEAN upgrade matched the whole "q.Members" against
// table_name, found nothing, and left a qualified table's legacy columns
// INTEGER, where every bool bind fails.
func TestQualifiedLegacyBoolUpgrade(t *testing.T) {
	db := pgtest.DB(t)
	q := pgQualifier(t, db)
	mustExec(t, db, `CREATE TABLE `+q+`.members (id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL DEFAULT '', roles TEXT NOT NULL DEFAULT '[]', password_set INTEGER NOT NULL DEFAULT 0)`)
	if err := NewEntityUserStore(db, q+".Members").EnsureSchema(context.Background()); err != nil {
		t.Fatal(err)
	}
	var typ string
	if err := db.QueryRow(`SELECT data_type FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'members' AND column_name = 'password_set'`, q).Scan(&typ); err != nil || typ != "boolean" {
		t.Fatalf("password_set is %q (%v), want boolean", typ, err)
	}
}

// Erasure and export fold a configured "Passkeys" to passkeys; the passkey
// and magic-link stores kept the case, so on Postgres erasure failed on a
// missing relation. They fold like the other auth stores, and refuse a
// case-kept table an older version made.
func TestTokenStoresFoldTable(t *testing.T) {
	forMixedCaseAuth(t, nil, func(t *testing.T, db *sql.DB) {
		ctx := context.Background()
		pk, err := NewSQLPasskeyStore(db, "Passkeys")
		if err != nil {
			t.Fatal(err)
		}
		if err := pk.EnsureSchema(ctx); err != nil {
			t.Fatal(err)
		}
		cred := PasskeyCredential{ID: []byte("k1"), UserID: "u1", PublicKey: []byte("pk"), Algorithm: -7, CreatedAt: time.Now()}
		if err := pk.AddPasskey(ctx, cred, 20); err != nil {
			t.Fatal(err)
		}
		if n := entityRows(t, db, "Passkeys"); n != 1 {
			t.Fatalf("Passkeys holds %d rows, want 1", n)
		}
		noCaseKeptTable(t, db, "Passkeys")
		ml, err := NewSQLMagicLinkTokenStore(db, "MagicTokens")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ml.CreateToken(ctx, "a@x.test", time.Hour); err != nil {
			t.Fatal(err)
		}
		if n := entityRows(t, db, "MagicTokens"); n != 1 {
			t.Fatalf("MagicTokens holds %d rows, want 1", n)
		}
		noCaseKeptTable(t, db, "MagicTokens")
		if migrate.DetectDialect(db) != migrate.DialectPostgres {
			return
		}
		mustExec(t, db, `CREATE TABLE "OldPasskeys" (id TEXT PRIMARY KEY)`, `CREATE TABLE "OldTokens" (token TEXT PRIMARY KEY)`)
		old, err := NewSQLPasskeyStore(db, "OldPasskeys")
		if err == nil {
			err = old.EnsureSchema(ctx)
		}
		wantRefusal(t, "OldPasskeys", err, "upper case kept")
		_, err = NewSQLMagicLinkTokenStore(db, "OldTokens")
		wantRefusal(t, "OldTokens", err, "upper case kept")
	})
}

// The rename advice named the folded target bare, so a reserved word such
// as order read as a keyword and the statement failed.
func TestRenameAdviceQuotesTarget(t *testing.T) {
	db := pgtest.DB(t)
	mustExec(t, db, `CREATE TABLE "Order" (user_id TEXT PRIMARY KEY)`)
	store, err := NewEntityTwoFAStore(db, "Order", testTwoFAKey)
	if err != nil {
		t.Fatal(err)
	}
	wantRefusal(t, "Order", store.EnsureSchema(context.Background()), `ALTER TABLE "Order" RENAME TO "order"`)
}

// The OAuth token, API token, service account and rate-limit stores
// quoted their configured table whole until they folded it. Over a
// case-kept table an older build left, each refuses and prints the rename.
func TestSQLStoresRefuseCaseKeptTable(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		older string
		open  func(db *sql.DB) error
	}{
		{"Tokens", func(db *sql.DB) error {
			_, err := NewSQLOAuthTokenStore(db, SQLOAuthTokenStoreConfig{Table: "Tokens", EncryptionKey: []byte("k")})
			return err
		}},
		{"ApiTokens", func(db *sql.DB) error {
			_, err := NewSQLAPITokenStore(db, WithAPITokenTable("ApiTokens"))
			return err
		}},
		{"Robots", func(db *sql.DB) error {
			_, err := NewSQLServiceAccountStore(db, WithServiceAccountTable("Robots"))
			return err
		}},
		{"Limits", func(db *sql.DB) error { return NewSQLRateLimitStore(db, "Limits").EnsureSchema(ctx) }},
		{"Limits_attempts", func(db *sql.DB) error { return NewSQLRateLimitStore(db, "Limits").EnsureSchema(ctx) }},
	} {
		t.Run(tc.older, func(t *testing.T) {
			db := pgtest.DB(t)
			mustExec(t, db, `CREATE TABLE "`+tc.older+`" (id TEXT PRIMARY KEY)`)
			wantRefusal(t, tc.older, tc.open(db), `RENAME TO "`+strings.ToLower(tc.older)+`"`)
		})
	}
}
