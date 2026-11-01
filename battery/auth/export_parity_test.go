package auth

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/datexport"
	"github.com/DonaldMurillo/gofastr/framework"
)

// Every table the erase plane reaches is one the export plane dumps, so an
// export and restore keeps what an erasure would remove. The magic-link
// token table is the exception: its rows are short-lived credentials, and a
// restored one would be a live login link.
func TestAuthExportEraseParity(t *testing.T) {
	registerCanonicalErasers()
	registerCanonicalExporters()
	assertAuthExportParity(t)
}

func assertAuthExportParity(t *testing.T) {
	t.Helper()
	exporters := map[string]datexport.DataExporter{}
	for _, ex := range datexport.All() {
		if ex.Source == "auth" {
			exporters[ex.Name] = ex
		}
	}
	for _, e := range datexport.AllErasers() {
		if e.Source != "auth" || strings.HasPrefix(e.Name, "magic_link_tokens") {
			continue
		}
		ex, ok := exporters[e.Name]
		if !ok {
			t.Errorf("eraser %q (table %q) has no exporter: an export and restore drops what it holds", e.Name, e.Table)
			continue
		}
		if ex.Table != e.Table {
			t.Errorf("%s: the exporter dumps %q but the eraser reaches %q", e.Name, ex.Table, e.Table)
		}
	}
	if _, ok := exporters["magic_link_tokens"]; ok {
		t.Error("magic_link_tokens is exported: a restored token is a live login link")
	}
}

// ndjsonRows reads the archive file for name as rows.
func ndjsonRows(t *testing.T, dir, name string) []map[string]any {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, name+".ndjson"))
	if err != nil {
		t.Fatalf("archive has no %s: %v", name, err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		var row map[string]any
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		out = append(out, row)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}

// Under the battery's documented wiring (NewEntityUserStore(db, "users"),
// NewEntitySessionStore(db, "sessions")) the export named only the
// canonical auth_* tables, skipped them as absent, and a restore held no
// accounts. 2FA enrolment and OAuth links were never exported at all, so a
// restored 2FA account fell back to password-only and an OAuth-only
// account was locked out. "members" proves the link table follows the
// configured user table rather than the canonical users_oauth_links.
func TestExportCoversConfiguredStoreTables(t *testing.T) {
	t.Cleanup(func() {
		registerCanonicalErasers()
		registerCanonicalExporters()
	})
	for _, ut := range []string{"users", "members"} {
		t.Run(ut, func(t *testing.T) { exportConfiguredStores(t, ut) })
	}
}

func exportConfiguredStores(t *testing.T, userTable string) {
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	ctx := context.Background()

	users := NewEntityUserStore(db, userTable)
	sessions := NewEntitySessionStore(db, "sessions")
	twofa, err := NewEntityTwoFAStore(db, "user_twofa", testTwoFAStoreConfig())
	if err != nil {
		t.Fatal(err)
	}
	passkeys, err := NewSQLPasskeyStore(db, "passkeys")
	if err != nil {
		t.Fatal(err)
	}
	challenges, err := NewSQLMagicLinkTokenStore(db, "passkey_challenges")
	if err != nil {
		t.Fatal(err)
	}
	mgr := New(AuthConfig{
		JWTSecret: "export-wiring", SessionCookie: "session_id", SessionTTL: time.Hour,
		UserStore: users, SessionStore: sessions,
	})
	mgr.Use(NewCorePlugin().WithTwoFactorURL("/login/2fa"))
	mgr.Use(NewTwoFAPlugin(TwoFAConfig{Store: twofa}))
	mgr.Use(NewPasskeyPlugin(PasskeyConfig{
		RPID: "example.com", Origins: []string{"https://example.com"},
		Store: passkeys, ChallengeStore: challenges, TwoFactorURL: "/2fa",
	}))
	if err := mgr.Init(nil); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := twofa.EnsureSchema(ctx); err != nil {
		t.Fatal(err)
	}

	hash, err := HashPassword("password123")
	if err != nil {
		t.Fatal(err)
	}
	u, err := users.CreateUser(ctx, "export-wiring@example.com", hash, []string{"user"})
	if err != nil {
		t.Fatal(err)
	}
	id := u.GetID()
	if err := users.MarkEmailVerified(ctx, id); err != nil {
		t.Fatal(err)
	}
	if err := users.SetUserName(ctx, id, "Ada"); err != nil {
		t.Fatal(err)
	}
	if _, err := sessions.Create(ctx, id, time.Hour); err != nil {
		t.Fatal(err)
	}
	// Two links under one provider: the table's key is (provider,
	// provider_id), and paging by provider alone would drop one.
	for _, pid := range []string{"g-1", "g-2"} {
		if err := users.LinkOAuth(ctx, id, "google", pid); err != nil {
			t.Fatal(err)
		}
	}
	if err := twofa.SetTwoFA(ctx, id, &TwoFAState{Enabled: true, Secret: GenerateSecret(), Verified: true}); err != nil {
		t.Fatal(err)
	}
	if err := passkeys.AddPasskey(ctx, PasskeyCredential{
		ID: []byte("cred-1"), UserID: id, PublicKey: []byte{1}, Algorithm: coseAlgES256, CreatedAt: time.Now(),
	}, 20); err != nil {
		t.Fatal(err)
	}

	app := framework.NewApp(framework.WithDB(db))
	dir := t.TempDir()
	if err := app.ExportData(ctx, dir, framework.WithExportPageSize(1)); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	tables := map[string]struct {
		table, col string
		rows       int
	}{
		"auth_users":        {userTable, "id", 1},
		"auth_sessions":     {"sessions", "user_id", 1},
		"auth_twofa":        {"user_twofa", "user_id", 1},
		"users_oauth_links": {userTable + "_oauth_links", "user_id", 2},
		"auth_passkeys":     {"passkeys", "user_id", 1},
	}
	for name, m := range tables {
		rows := ndjsonRows(t, dir, name)
		if len(rows) != m.rows {
			t.Errorf("%s holds %d row(s), want %d: %v", name, len(rows), m.rows, rows)
		}
		for _, r := range rows {
			if r[m.col] != id {
				t.Errorf("%s row %v does not name %s", name, r, id)
			}
		}
	}
	if row := ndjsonRows(t, dir, "auth_users")[0]; row["name"] != "Ada" || (row["email_verified"] != true && row["email_verified"] != float64(1)) {
		t.Errorf("the user row lost its name or verified flag: %v", row)
	}
	assertAuthExportParity(t)

	// The restore brings the account back whole.
	for _, m := range tables {
		if _, err := db.Exec(`DELETE FROM "` + m.table + `"`); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.ImportData(ctx, dir); err != nil {
		t.Fatalf("ImportData: %v", err)
	}
	if st, err := twofa.GetTwoFA(ctx, id); err != nil || st == nil || !st.Enabled {
		t.Errorf("2FA after the restore = %+v, %v; want the enrolment back", st, err)
	}
	if lu, err := users.FindByOAuth(ctx, "google", "g-1"); err != nil || lu == nil || lu.GetID() != id {
		t.Errorf("OAuth link after the restore = %v, %v; want %s", lu, err, id)
	}
	if ok, err := users.IsEmailVerified(ctx, id); err != nil || !ok {
		t.Errorf("email verified after the restore = %v, %v; want true", ok, err)
	}
}
