package auth

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
	"github.com/DonaldMurillo/gofastr/internal/pgtest"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

// A host that declares its auth tables as entities gets them from
// AutoMigrate, which writes unquoted DDL: Postgres stores a declared
// "Members" table and "PasswordHash" column folded (members,
// passwordhash). The entity-backed stores name the declared spelling and
// must reach the folded objects. Each test migrates the mixed-case entity
// first, then runs the store's EnsureSchema (what AuthManager.Init does)
// and its reads and writes, on SQLite and on Postgres. A store that quotes
// the declared spelling does not fail on Postgres: its EnsureSchema
// creates a second, case-kept table beside the migrated one, and the
// entity layer never sees its rows. So each test also counts the rows
// through the unquoted name the entity layer uses.

// entityRows counts table's rows the way the entity layer names it:
// unquoted.
func entityRows(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

// noCaseKeptTable fails when Postgres holds table under its case-kept
// spelling: a quoted CREATE beside the migrated, folded table.
func noCaseKeptTable(t *testing.T, db *sql.DB, table string) {
	t.Helper()
	if migrate.DetectDialect(db) != migrate.DialectPostgres {
		return
	}
	var n int
	if err := db.QueryRow(`SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = current_schema() AND table_name = $1`, table).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n > 0 {
		t.Fatalf("EnsureSchema created a case-kept %q beside the migrated table", table)
	}
}

// forMixedCaseAuth runs fn once per dialect with ents migrated.
func forMixedCaseAuth(t *testing.T, ents []*entity.Entity, fn func(t *testing.T, db *sql.DB)) {
	run := func(t *testing.T, db *sql.DB) {
		for _, ent := range ents {
			if err := migrate.MigrateEntity(db, ent); err != nil {
				t.Fatalf("MigrateEntity %s: %v", ent.GetName(), err)
			}
		}
		fn(t, db)
	}
	t.Run("sqlite", func(t *testing.T) {
		db, err := sql.Open("sqlite3", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { db.Close() })
		run(t, db)
	})
	t.Run("postgres", func(t *testing.T) { run(t, pgtest.DB(t)) })
}

func TestMixedCaseUserStore(t *testing.T) {
	members := entity.Define("Members", entity.EntityConfig{
		Table: "Members",
		Fields: []schema.Field{
			{Name: "Email", Type: schema.String, Required: true, Unique: true},
			{Name: "PasswordHash", Type: schema.String, Required: true},
			{Name: "Roles", Type: schema.String, Default: `["user"]`},
			{Name: "PasswordSet", Type: schema.Bool, Default: "false"},
			{Name: "EmailVerified", Type: schema.Bool, Default: "false"},
			{Name: "DisplayName", Type: schema.String, Default: ""},
		},
	}.WithTimestamps(false))
	fm := UserFieldMap{
		ID: "id", Email: "Email", PasswordHash: "PasswordHash", Roles: "Roles",
		PasswordSet: "PasswordSet", EmailVerified: "EmailVerified", Name: "DisplayName",
	}
	forMixedCaseAuth(t, []*entity.Entity{members}, func(t *testing.T, db *sql.DB) {
		ctx := context.Background()
		store := NewEntityUserStore(db, "Members", fm)
		if err := store.EnsureSchema(ctx); err != nil {
			t.Fatalf("EnsureSchema: %v", err)
		}
		noCaseKeptTable(t, db, "Members")
		noCaseKeptTable(t, db, "Members_oauth_links")
		u, err := store.CreateUser(ctx, "a@mc.test", "hash", []string{"user"})
		if err != nil {
			t.Fatalf("CreateUser: %v", err)
		}
		if n := entityRows(t, db, "Members"); n != 1 {
			t.Fatalf("Members holds %d rows after CreateUser, want 1: the store wrote elsewhere", n)
		}
		id := u.GetID()
		if _, _, err := store.FindByEmail(ctx, "a@mc.test"); err != nil {
			t.Fatalf("FindByEmail: %v", err)
		}
		if _, err := store.FindByID(ctx, id); err != nil {
			t.Fatalf("FindByID: %v", err)
		}
		if _, n, err := store.ListUsers(ctx, ListUsersOptions{}); err != nil || n != 1 {
			t.Fatalf("ListUsers = %d, %v", n, err)
		}
		if err := store.UpdateRoles(ctx, id, []string{"admin"}); err != nil {
			t.Fatalf("UpdateRoles: %v", err)
		}
		if err := store.SetUserName(ctx, id, "Ann"); err != nil {
			t.Fatalf("SetUserName: %v", err)
		}
		if name, err := store.UserName(ctx, id); err != nil || name != "Ann" {
			t.Fatalf("UserName = %q, %v", name, err)
		}
		if err := store.MarkEmailVerified(ctx, id); err != nil {
			t.Fatalf("MarkEmailVerified: %v", err)
		}
		if ok, err := store.IsEmailVerified(ctx, id); err != nil || !ok {
			t.Fatalf("IsEmailVerified = %v, %v", ok, err)
		}
		if err := store.SetPassword(ctx, id, "hash2"); err != nil {
			t.Fatalf("SetPassword: %v", err)
		}
		if ok, err := store.HasPassword(ctx, id); err != nil || !ok {
			t.Fatalf("HasPassword = %v, %v", ok, err)
		}
		if err := store.LinkOAuth(ctx, id, "github", "gh-1"); err != nil {
			t.Fatalf("LinkOAuth: %v", err)
		}
		if _, err := store.FindByOAuth(ctx, "github", "gh-1"); err != nil {
			t.Fatalf("FindByOAuth: %v", err)
		}
		if accts, err := store.ListAccounts(ctx, id); err != nil || len(accts) != 1 {
			t.Fatalf("ListAccounts = %v, %v", accts, err)
		}
		if n, err := store.countLinksForProvider(ctx, "github", "gh-1"); err != nil || n != 1 {
			t.Fatalf("countLinksForProvider = %d, %v", n, err)
		}
		if out, err := store.UnlinkOAuthGuarded(ctx, id, "github"); err != nil || out != UnlinkRemoved {
			t.Fatalf("UnlinkOAuthGuarded = %v, %v", out, err)
		}
		if err := store.LinkOAuth(ctx, id, "gitlab", "gl-1"); err != nil {
			t.Fatalf("LinkOAuth: %v", err)
		}
		if err := store.UnlinkOAuth(ctx, id, "gitlab"); err != nil {
			t.Fatalf("UnlinkOAuth: %v", err)
		}
		if _, err := store.FindByOAuth(ctx, "gitlab", "gl-1"); err == nil {
			t.Fatal("FindByOAuth found a link UnlinkOAuth removed")
		}
		if err := store.ClearPassword(ctx, id); err != nil {
			t.Fatalf("ClearPassword: %v", err)
		}
	})
}

func TestMixedCaseSessionStore(t *testing.T) {
	sessions := entity.Define("AuthSessions", entity.EntityConfig{
		Table:  "AuthSessions",
		Fields: SessionEntityFields(),
	}.WithTimestamps(false))
	forMixedCaseAuth(t, []*entity.Entity{sessions}, func(t *testing.T, db *sql.DB) {
		ctx := context.Background()
		store := NewEntitySessionStore(db, "AuthSessions")
		if err := store.EnsureSchema(ctx); err != nil {
			t.Fatalf("EnsureSchema: %v", err)
		}
		noCaseKeptTable(t, db, "AuthSessions")
		s, err := store.Create(ctx, "u1", time.Hour)
		if err != nil {
			t.Fatalf("Create: %v", err)
		}
		if n := entityRows(t, db, "AuthSessions"); n != 1 {
			t.Fatalf("AuthSessions holds %d rows after Create, want 1: the store wrote elsewhere", n)
		}
		if err := store.MarkTwoFactorVerified(ctx, s.Token); err != nil {
			t.Fatalf("MarkTwoFactorVerified: %v", err)
		}
		if got, err := store.Get(ctx, s.Token); err != nil || !got.TwoFactorVerified {
			t.Fatalf("Get = %+v, %v", got, err)
		}
		if _, err := store.Cleanup(ctx); err != nil {
			t.Fatalf("Cleanup: %v", err)
		}
	})
}

func TestMixedCaseTwoFAStore(t *testing.T) {
	twofa := entity.Define("AuthTwoFA", entity.EntityConfig{
		Table:      "AuthTwoFA",
		PrimaryKey: []string{"user_id"},
		Fields:     TwoFAEntityFields(),
	}.WithTimestamps(false))
	forMixedCaseAuth(t, []*entity.Entity{twofa}, func(t *testing.T, db *sql.DB) {
		ctx := context.Background()
		store, err := NewEntityTwoFAStore(db, "AuthTwoFA", EntityTwoFAStoreConfig{
			EncryptionKey: []byte("0123456789abcdef0123456789abcdef"),
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := store.EnsureSchema(ctx); err != nil {
			t.Fatalf("EnsureSchema: %v", err)
		}
		noCaseKeptTable(t, db, "AuthTwoFA")
		st := &TwoFAState{Enabled: true, Secret: "JBSWY3DPEHPK3PXP", Verified: true}
		if err := store.SetTwoFA(ctx, "u1", st); err != nil {
			t.Fatalf("SetTwoFA: %v", err)
		}
		if n := entityRows(t, db, "AuthTwoFA"); n != 1 {
			t.Fatalf("AuthTwoFA holds %d rows after SetTwoFA, want 1: the store wrote elsewhere", n)
		}
		got, err := store.GetTwoFA(ctx, "u1")
		if err != nil || got == nil || !got.Enabled {
			t.Fatalf("GetTwoFA = %+v, %v", got, err)
		}
		next := *got
		next.LastUsedStep = 7
		if ok, err := store.CompareAndSwapTwoFA(ctx, "u1", got, &next); err != nil || !ok {
			t.Fatalf("CompareAndSwapTwoFA = %v, %v", ok, err)
		}
		if ok, err := store.CompareAndSwapTwoFA(ctx, "u2", nil, st); err != nil || !ok {
			t.Fatalf("CompareAndSwapTwoFA from absent = %v, %v", ok, err)
		}
		if err := store.DeleteTwoFA(ctx, "u1"); err != nil {
			t.Fatalf("DeleteTwoFA: %v", err)
		}
	})
}

// An older table holds PasswordSet as INTEGER; EnsureSchema converts it to
// BOOLEAN on Postgres, naming the declared table and column.
func TestMixedCaseLegacyBoolUpgrade(t *testing.T) {
	forMixedCaseAuth(t, nil, func(t *testing.T, db *sql.DB) {
		if _, err := db.Exec(`CREATE TABLE LegacyMembers (id TEXT PRIMARY KEY, Email TEXT UNIQUE NOT NULL,
			PasswordHash TEXT NOT NULL DEFAULT '', Roles TEXT NOT NULL DEFAULT '[]', PasswordSet INTEGER NOT NULL DEFAULT 0)`); err != nil {
			t.Fatal(err)
		}
		store := NewEntityUserStore(db, "LegacyMembers", UserFieldMap{
			ID: "id", Email: "Email", PasswordHash: "PasswordHash", Roles: "Roles", PasswordSet: "PasswordSet",
		})
		if err := store.EnsureSchema(context.Background()); err != nil {
			t.Fatalf("EnsureSchema over a legacy INTEGER flag: %v", err)
		}
		if _, err := store.CreateUser(context.Background(), "b@mc.test", "hash", nil); err != nil {
			t.Fatalf("CreateUser after the upgrade: %v", err)
		}
	})
}

// A table an older battery created quoted, case kept, holds the accounts.
// EnsureSchema refuses to boot past it rather than hide every row behind
// the folded table.
func TestMixedCaseRefusesCaseKeptTable(t *testing.T) {
	forMixedCaseAuth(t, nil, func(t *testing.T, db *sql.DB) {
		if migrate.DetectDialect(db) != migrate.DialectPostgres {
			t.Skip("SQLite names one table for both spellings")
		}
		ctx := context.Background()
		for _, ddl := range []string{
			`CREATE TABLE "OldMembers" (id TEXT PRIMARY KEY, email TEXT UNIQUE NOT NULL, password_hash TEXT NOT NULL DEFAULT '', roles TEXT NOT NULL DEFAULT '[]', password_set BOOLEAN NOT NULL DEFAULT FALSE)`,
			`CREATE TABLE "OldSessions" (id TEXT NOT NULL, token TEXT UNIQUE NOT NULL, user_id TEXT NOT NULL, created_at TIMESTAMP NOT NULL, expires_at TIMESTAMP NOT NULL, two_factor_verified BOOLEAN NOT NULL DEFAULT FALSE, pending_two_factor BOOLEAN NOT NULL DEFAULT FALSE)`,
			`CREATE TABLE "OldTwoFA" (user_id TEXT PRIMARY KEY)`,
			`CREATE TABLE "LinkMembers_oauth_links" (provider TEXT NOT NULL, provider_id TEXT NOT NULL, user_id TEXT NOT NULL)`,
		} {
			if _, err := db.Exec(ddl); err != nil {
				t.Fatal(err)
			}
		}
		twofa, err := NewEntityTwoFAStore(db, "OldTwoFA", EntityTwoFAStoreConfig{EncryptionKey: []byte("0123456789abcdef0123456789abcdef")})
		if err != nil {
			t.Fatal(err)
		}
		for name, ensure := range map[string]func(context.Context) error{
			"users":    NewEntityUserStore(db, "OldMembers").EnsureSchema,
			"links":    NewEntityUserStore(db, "LinkMembers").EnsureSchema,
			"sessions": NewEntitySessionStore(db, "OldSessions").EnsureSchema,
			"twofa":    twofa.EnsureSchema,
		} {
			if err := ensure(ctx); err == nil || !strings.Contains(err.Error(), "upper case kept") {
				t.Errorf("%s: EnsureSchema over a case-kept table = %v, want a refusal", name, err)
			}
		}
	})
}
