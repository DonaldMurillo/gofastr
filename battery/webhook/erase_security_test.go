package webhook

import (
	"bufio"
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	_ "github.com/DonaldMurillo/gofastr/sqlite/stdlib"
)

func eraseTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// exportLines runs App.ExportData and counts the lines of name.ndjson.
func exportLines(t *testing.T, app *framework.App, name string) int {
	t.Helper()
	dir := t.TempDir()
	if err := app.ExportData(context.Background(), dir); err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	f, err := os.Open(filepath.Join(dir, name+".ndjson"))
	if err != nil {
		t.Errorf("the export has no %s: %v", name, err)
		return 0
	}
	defer f.Close()
	n := 0
	for sc := bufio.NewScanner(f); sc.Scan(); {
		n++
	}
	return n
}

// Erasing a user deletes the outbound deliveries published for them and
// keeps everyone else's (S22). A data export carries the delivery table
// under its configured name.
func TestEraseRemovesOwnedDeliveries(t *testing.T) {
	db := eraseTestDB(t)
	store, err := NewSQLStore(db, WithSQLAllowPlaintext(), WithSQLDeliveriesTable("outgoing"))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.AddSubscriber(ctx, Subscriber{ID: "s1", URL: "https://example.com/hook", Secret: "x",
		Events: []string{"order.*"}, Active: true}); err != nil {
		t.Fatal(err)
	}
	m := New(store, Options{})
	for _, owner := range []string{"victim", "bystander", ""} {
		if n, err := m.PublishFor(ctx, owner, "order.created", []byte(`{"for":"`+owner+`"}`)); err != nil || n != 1 {
			t.Fatalf("PublishFor %q = %d, %v", owner, n, err)
		}
	}
	app := framework.NewApp(framework.WithDB(db))
	if n := exportLines(t, app, "webhook_deliveries-outgoing"); n != 3 {
		t.Errorf("export holds %d deliveries, want 3", n)
	}
	if _, err := app.EraseUserData(ctx, "victim"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	ds, err := store.ListDeliveries(ctx, "s1", 10)
	if err != nil {
		t.Fatal(err)
	}
	var owners []string
	for _, d := range ds {
		owners = append(owners, d.Owner+"="+string(d.Payload))
	}
	slices.Sort(owners)
	if want := []string{`={"for":""}`, `bystander={"for":"bystander"}`}; !slices.Equal(owners, want) {
		t.Errorf("deliveries after erase %v, want %v", owners, want)
	}
}

// Erasing a user deletes the inbound envelopes IngestConfig.Owner mapped
// to them and keeps the rest (S22). A data export carries the envelope
// table under its configured name.
func TestEraseRemovesOwnedEnvelopes(t *testing.T) {
	db := eraseTestDB(t)
	store, err := NewSQLInboundStore(db, WithInboundTable("incoming"))
	if err != nil {
		t.Fatal(err)
	}
	h, err := IngestHandler(IngestConfig{
		Source:   "billing",
		Verifier: func(*http.Request, []byte) error { return nil },
		Store:    store,
		Owner:    func(r *http.Request, _ []byte) string { return r.Header.Get("X-Account") },
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, acct := range []string{"victim", "bystander", ""} {
		req := httptest.NewRequest(http.MethodPost, "/hooks/billing", strings.NewReader(`{"acct":"`+acct+`"}`))
		req.Header.Set("X-Account", acct)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("ingest %q: status %d %s", acct, rec.Code, rec.Body)
		}
	}
	ctx := context.Background()
	app := framework.NewApp(framework.WithDB(db))
	if n := exportLines(t, app, "webhook_inbound-incoming"); n != 3 {
		t.Errorf("export holds %d envelopes, want 3", n)
	}
	if _, err := app.EraseUserData(ctx, "victim"); err != nil {
		t.Fatalf("EraseUserData: %v", err)
	}
	es, err := store.ListEnvelopes(ctx, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	var owners []string
	for _, e := range es {
		owners = append(owners, e.Owner+"="+string(e.Payload))
	}
	slices.Sort(owners)
	if want := []string{`={"acct":""}`, `bystander={"acct":"bystander"}`}; !slices.Equal(owners, want) {
		t.Errorf("envelopes after erase %v, want %v", owners, want)
	}
}

// Tables an earlier release created gain the owner column, and opening
// the stores again over them does not fail.
func TestWebhookStoresAddOwnerColumn(t *testing.T) {
	db := eraseTestDB(t)
	for _, stmt := range []string{
		`CREATE TABLE webhook_deliveries (id TEXT PRIMARY KEY, subscriber_id TEXT NOT NULL, event TEXT NOT NULL,
			payload BLOB NOT NULL, attempts INTEGER NOT NULL, status TEXT NOT NULL, last_error TEXT NOT NULL,
			next_attempt_at DATETIME, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
		`CREATE TABLE webhook_inbound (id TEXT PRIMARY KEY, source TEXT NOT NULL, dedupe_key TEXT NOT NULL DEFAULT '',
			headers TEXT NOT NULL DEFAULT '{}', payload BLOB NOT NULL, status TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0, last_error TEXT NOT NULL DEFAULT '',
			received_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	for range 2 {
		if _, err := NewSQLStore(db, WithSQLAllowPlaintext()); err != nil {
			t.Fatalf("NewSQLStore over an old table: %v", err)
		}
		if _, err := NewSQLInboundStore(db); err != nil {
			t.Fatalf("NewSQLInboundStore over an old table: %v", err)
		}
	}
	for _, table := range []string{"webhook_deliveries", "webhook_inbound"} {
		if _, err := db.Exec(`SELECT owner FROM ` + table); err != nil {
			t.Errorf("%s: owner column missing: %v", table, err)
		}
	}
}
