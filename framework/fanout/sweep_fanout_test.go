package fanout

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

func TestPostgres_InlineMessageStaysOnItsTable(t *testing.T) {
	dsn := pgtest.FreshDatabaseDSN(t)
	mk := func(table string) *PostgresFanout {
		db, err := sql.Open("postgres", dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		f, err := NewPostgres(dsn, db, WithTableName(table))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = f.Close() })
		return f
	}
	fA, fB := mk("fanout_a_msgs"), mk("fanout_b_msgs")
	var gotA, gotB atomic.Int64
	ca, _ := fA.Subscribe("topic", func([]byte) { gotA.Add(1) })
	defer ca()
	cb, _ := fB.Subscribe("topic", func([]byte) { gotB.Add(1) })
	defer cb()
	if err := fA.Publish(context.Background(), "topic", []byte("small")); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && gotA.Load() < 1 {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	if gotA.Load() != 1 || gotB.Load() != 0 {
		t.Fatalf("A got %d, B got %d; want 1 and 0", gotA.Load(), gotB.Load())
	}
}

func TestLongTableNameRefusedForItsLength(t *testing.T) {
	db, err := sql.Open("postgres", "postgres://unused/unused")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = NewPostgres("postgres://unused/unused", db, WithTableName(strings.Repeat("a", 64)))
	if err == nil || !strings.Contains(err.Error(), "63") {
		t.Fatalf("NewPostgres with a 64-byte table = %v, want the 63-byte identifier limit", err)
	}
}

func TestPurgeAgeSQLMatchesPurgeAge(t *testing.T) {
	if want := fmt.Sprintf("%d minutes", int(pgPurgeAge.Minutes())); pgPurgeAgeSQL != want {
		t.Fatalf("pgPurgeAgeSQL = %q, want %q to match pgPurgeAge", pgPurgeAgeSQL, want)
	}
}
