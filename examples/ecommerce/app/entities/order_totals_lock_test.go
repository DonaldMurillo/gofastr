package entities

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/framework/db"
	_ "github.com/lib/pq"
)

// Two line items added to one order in concurrent transactions each
// recompute the order's totals. On Postgres (read committed) a
// recompute that reads the items before it locks the order sums a
// stale set: the second writer waits on the first one's UPDATE, then
// writes a total missing the first item. The recompute locks the
// order before it reads.
func TestOrderTotalsLockBeforeRead(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	ctx := context.Background()
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	schemaName := "ordertotals_" + hex.EncodeToString(b)
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.ExecContext(ctx, "CREATE SCHEMA "+schemaName); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec("DROP SCHEMA " + schemaName + " CASCADE") })
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	pool, err := sql.Open("postgres", dsn+sep+"search_path="+schemaName)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	for _, ddl := range []string{
		`CREATE TABLE orders (id TEXT PRIMARY KEY, status TEXT, tax NUMERIC, shipping_cost NUMERIC, subtotal NUMERIC, total NUMERIC)`,
		`CREATE TABLE order_items (id TEXT PRIMARY KEY, order_id TEXT NOT NULL REFERENCES orders(id), unit_price NUMERIC, quantity INT)`,
		`INSERT INTO orders VALUES ('o1', 'pending', 0, 0, 0, 0)`,
	} {
		if _, err := pool.ExecContext(ctx, ddl); err != nil {
			t.Fatal(err)
		}
	}

	addItem := func(id, price string) (*sql.Tx, context.Context) {
		tx, err := pool.BeginTx(ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO order_items VALUES ($1, 'o1', $2, 1)`, id, price); err != nil {
			t.Fatal(err)
		}
		return tx, db.WithTx(ctx, tx)
	}
	txA, ctxA := addItem("i1", "10")
	if err := recomputeOrderTotals(ctxA, "o1"); err != nil {
		t.Fatal(err)
	}
	txB, ctxB := addItem("i2", "5")
	done := make(chan error, 1)
	go func() { done <- recomputeOrderTotals(ctxB, "o1") }()

	// Commit A once B is parked on A's row lock.
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		if err := admin.QueryRowContext(ctx, `SELECT count(*) FROM pg_stat_activity WHERE wait_event_type = 'Lock' AND datname = current_database()`).Scan(&waiting); err != nil {
			t.Fatal(err)
		}
		if waiting > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("second recompute never waited on the order lock")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := txA.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := txB.Commit(); err != nil {
		t.Fatal(err)
	}
	var total string
	if err := pool.QueryRowContext(ctx, `SELECT CAST(total AS TEXT) FROM orders WHERE id = 'o1'`).Scan(&total); err != nil {
		t.Fatal(err)
	}
	if total != "15.0000" && total != "15" {
		t.Fatalf("order total = %s after two concurrent items of 10 and 5, want 15: the second recompute summed a stale item set", total)
	}
}
