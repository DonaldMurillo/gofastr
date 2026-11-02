package framework

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/pgtest"
)

// TestPostgresExportReadsOneSnapshot exports a blog while another client
// keeps writing a post and a comment on it in one transaction. The posts
// table is read before the comments table, so an export that reads each
// table on its own sees comments whose post committed after the posts
// were read: the archive names parents it does not hold, and restoring it
// trips the foreign key. Every table is read in one snapshot now.
func TestPostgresExportReadsOneSnapshot(t *testing.T) {
	exportWhileWriting(t, pgtest.DB(t), 200)
}

// The SQLite twin: in WAL mode a reader holds its snapshot while a writer
// commits, so the export reads every table in one deferred read
// transaction and the archive is the database at one moment. The writer
// never pauses and the busy timeout is short, so an export transaction
// that took the write lock (the driver begins IMMEDIATE unless told
// ReadOnly) leaves one side waiting past it and the test fails on that
// side's error. (A rollback-journal database would block its writers for
// the whole export, so it keeps reading table by table; the docs say to
// stop writes there.)
func TestSQLiteWALExportReadsOneSnapshot(t *testing.T) {
	// A short busy timeout: a lock either side waits on fails fast.
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "app.db")+"?_pragma=busy_timeout(50)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var mode string
	if err := db.QueryRow(`PRAGMA journal_mode=WAL`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal_mode=WAL: mode %q, err %v", mode, err)
	}
	exportWhileWriting(t, db, 2000)
}

// exportWhileWriting exports a blog while another client keeps writing a
// post and a comment on it in one transaction, then checks every comment
// in the archive has its post there too. The writer runs until the export
// returns.
func exportWhileWriting(t *testing.T, db *sql.DB, rows int) {
	t.Helper()
	app := newBlogApp(t, db)
	ctx := context.Background()
	seed, err := db.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	for i := range rows {
		if _, err := seed.Exec(`INSERT INTO posts (id, title) VALUES ($1, 't')`, fmt.Sprintf("s%04d", i)); err != nil {
			t.Fatal(err)
		}
		if _, err := seed.Exec(`INSERT INTO comments (id, post_id) VALUES ($1, $2)`, fmt.Sprintf("c-s%04d", i), fmt.Sprintf("s%04d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if err := seed.Commit(); err != nil {
		t.Fatal(err)
	}

	var stop atomic.Bool
	var wg sync.WaitGroup
	var writeErr error
	committed := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		defer func() {
			select {
			case <-committed:
			default:
				close(committed)
			}
		}()
		for n := 0; n < 999999 && !stop.Load(); n++ {
			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				writeErr = err
				return
			}
			// Each post sorts before every earlier one, so the posts read has passed
			// them; their comments sort after, so the comments read meets them.
			id := fmt.Sprintf("a%06d", 999999-n)
			if _, err := tx.Exec(`INSERT INTO posts (id, title) VALUES ($1, 't')`, id); err != nil {
				_ = tx.Rollback()
				writeErr = err
				return
			}
			if _, err := tx.Exec(`INSERT INTO comments (id, post_id) VALUES ($1, $2)`, "z-"+id, id); err != nil {
				_ = tx.Rollback()
				writeErr = err
				return
			}
			if err := tx.Commit(); err != nil {
				writeErr = err
				return
			}
			if n == 0 {
				close(committed)
			}
		}
	}()
	// The export starts once the writer is under way, so the two overlap.
	<-committed

	dir := t.TempDir()
	err = app.ExportData(ctx, dir, WithExportPageSize(1))
	stop.Store(true)
	wg.Wait()
	if err != nil {
		t.Fatalf("ExportData: %v", err)
	}
	if writeErr != nil {
		t.Fatalf("writer: %v", writeErr)
	}

	posts := map[string]bool{}
	for _, r := range readArchiveRows(t, dir, "posts") {
		posts[r["id"].(string)] = true
	}
	for _, r := range readArchiveRows(t, dir, "comments") {
		if pid, _ := r["post_id"].(string); pid != "" && !posts[pid] {
			t.Fatalf("the archive holds comment %v of post %q, which it does not hold: the tables were read at different moments", r["id"], pid)
		}
	}
}

func readArchiveRows(t *testing.T, dir, name string) []map[string]any {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, name+".ndjson"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []map[string]any
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		out = append(out, m)
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return out
}
