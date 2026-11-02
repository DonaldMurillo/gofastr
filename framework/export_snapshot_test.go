package framework

import (
	"bufio"
	"context"
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
	db := pgtest.DB(t)
	app := newBlogApp(t, db)
	ctx := context.Background()
	for i := range 40 {
		if _, err := db.Exec(`INSERT INTO posts (id, title) VALUES ($1, 't')`, fmt.Sprintf("s%03d", i)); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO comments (id, post_id) VALUES ($1, $2)`, fmt.Sprintf("c-s%03d", i), fmt.Sprintf("s%03d", i)); err != nil {
			t.Fatal(err)
		}
	}

	var stop atomic.Bool
	var wg sync.WaitGroup
	var writeErr error
	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 0; n < 400 && !stop.Load(); n++ {
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
		}
	}()

	dir := t.TempDir()
	err := app.ExportData(ctx, dir, WithExportPageSize(1))
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
