package crud

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/db"
	"github.com/DonaldMurillo/gofastr/framework/file"
)

// Stored-object cleanup for schema.Image / schema.File fields.
//
// A hard delete, a purge, and a write that replaces a file each leave
// objects their row no longer names. Left in storage they stay served at
// /uploads/<key> to anyone holding the link, and storage only grows. Each
// of those writes now deletes the objects its row let go, after its
// transaction commits, with these exceptions:
//
//   - a key another row of the table still names (a soft-deleted row, or
//     one default a seed set on many rows) is kept, renditions and all;
//   - an http(s) URL is an external link and is never deleted;
//   - a soft delete keeps its objects, so a restore gets them back;
//   - a write on a transaction with no commit queue (a bare db.WithTx, or
//     a handler bound to a *sql.Tx) keeps them, since nothing tells it
//     that the commit happened.
//
// A failed delete is logged, not returned: the row change has committed.

// objectsBefore reads the stored objects on the row a write is about to
// change, for releaseObjects to compare against. It reads nothing when the
// handler has no Storage to delete from, or when body (nil for a delete)
// sets no file or variants column. Arguments are rowObjects'.
func (ch *CrudHandler) objectsBefore(ctx context.Context, r *http.Request, id string, key []any, body map[string]any, trashed bool) ([]storedObject, error) {
	if ch.Storage == nil {
		return nil, nil
	}
	if body != nil && !setsMediaColumn(ch.mediaFields(), body) {
		return nil, nil
	}
	return ch.rowObjects(ctx, r, id, key, trashed)
}

func setsMediaColumn(fields [][2]string, body map[string]any) bool {
	for _, mf := range fields {
		if _, ok := body[mf[0]]; ok {
			return true
		}
		if _, ok := body[mf[1]]; ok && mf[1] != "" {
			return true
		}
	}
	return false
}

// releaseObjects queues the delete of the objects before held that the
// row no longer does: all of them when the row is gone, else those the
// row as written (re-read under the same scope) dropped.
func (ch *CrudHandler) releaseObjects(ctx context.Context, r *http.Request, id string, key []any, before []storedObject, gone bool) error {
	if len(before) == 0 {
		return nil
	}
	var after []storedObject
	if !gone {
		var err error
		if after, err = ch.rowObjects(ctx, r, id, key, false); err != nil {
			return err
		}
	}
	keys, err := ch.releasedObjects(ctx, before, after)
	if err != nil {
		return fmt.Errorf("read stored object references: %w", err)
	}
	ch.deleteAfterCommit(ctx, keys)
	return nil
}

// releasedObjects returns the keys of before's objects that after no
// longer holds and that no row of the table still names, each with the
// renditions after does not keep.
func (ch *CrudHandler) releasedObjects(ctx context.Context, before, after []storedObject) ([]string, error) {
	if ch.Storage == nil || len(before) == 0 {
		return nil, nil
	}
	kept := map[string]bool{}
	for _, o := range after {
		kept[o.key] = true
		for _, ref := range o.renditions {
			kept[ref] = true
		}
	}
	var out []string
	for _, o := range before {
		if o.key == "" || kept[o.key] || file.IsExternalURL(o.key) {
			continue
		}
		named, err := ch.tableNamesKey(ctx, o.key)
		if err != nil {
			return nil, err
		}
		if named {
			continue
		}
		kept[o.key] = true
		out = append(out, o.key)
		for _, ref := range o.renditions {
			if !kept[ref] && !file.IsExternalURL(ref) {
				kept[ref] = true
				out = append(out, ref)
			}
		}
	}
	return out, nil
}

// tableNamesKey reports whether any row of the table, live or
// soft-deleted and in any tenant, holds key in a file column. It runs on
// the write's own transaction, after the write, so the row that let the
// key go no longer counts.
func (ch *CrudHandler) tableNamesKey(ctx context.Context, key string) (bool, error) {
	var conds []string
	var args []any
	for i, mf := range ch.mediaFields() {
		conds = append(conds, mf[0]+" = $"+strconv.Itoa(i+1))
		args = append(args, key)
	}
	sqlStr, args := query.Select("1").
		From(ch.Entity.GetTable()).
		Where(strings.Join(conds, " OR "), args...).
		Limit(1).
		Build()
	var hit any
	err := ch.conn(ctx).QueryRowContext(ctx, sqlStr, args...).Scan(&hit)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// deleteAfterCommit deletes keys once the write's transaction commits. A
// rollback drops the queued delete with the rest of the write, and a write
// with no commit queue deletes nothing.
func (ch *CrudHandler) deleteAfterCommit(ctx context.Context, keys []string) {
	if len(keys) == 0 || ch.Storage == nil {
		return
	}
	store := ch.Storage
	run := func() {
		// The queue runs after the handler's own panic net is gone: a
		// backend that panics must not take the process with it.
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("crud: delete of released stored objects panicked: %s", textsafe.Recovered(rec))
			}
		}()
		// The delete outlives the request: a client that hung up after the
		// commit cancelled ctx, and a backend refuses a cancelled Delete.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		for _, key := range keys {
			if err := store.Delete(ctx, key); err != nil && !errors.Is(err, os.ErrNotExist) {
				log.Printf("crud: delete of released stored object %q failed: %v", key, err)
			}
		}
	}
	if q, ok := db.CommitQueueFromContext(ctx); ok {
		q.Add(run)
		return
	}
	// On the pool with no transaction the statement committed on its own.
	if _, inTx := db.TxFromContext(ctx); !inTx {
		if _, pool := ch.DB.(db.Beginner); pool {
			run()
		}
	}
}

// deleteReturningObjects runs a bulk DELETE built by del and releases the
// stored objects of the rows it removed. It returns the row count.
func (ch *CrudHandler) deleteReturningObjects(ctx context.Context, del *query.DeleteBuilder) (int, error) {
	fields := ch.mediaFields()
	cols := mediaColumns(fields)
	if ch.Storage == nil || len(cols) == 0 {
		sqlStr, args := del.Build()
		res, err := ch.conn(ctx).ExecContext(ctx, sqlStr, args...)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		return int(n), nil
	}
	sqlStr, args := del.Returning(cols...).Build()
	rows, err := ch.conn(ctx).QueryContext(ctx, sqlStr, args...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	vals := make([]sql.NullString, len(cols))
	dest := make([]any, len(cols))
	for i := range vals {
		dest[i] = &vals[i]
	}
	var objs []storedObject
	n := 0
	for rows.Next() {
		if err := rows.Scan(dest...); err != nil {
			return 0, err
		}
		n++
		got := make(map[string]string, len(cols))
		for i, c := range cols {
			got[c] = vals[i].String
		}
		objs = appendObjects(objs, fields, got)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	rows.Close()
	keys, err := ch.releasedObjects(ctx, objs, nil)
	if err != nil {
		return n, fmt.Errorf("read stored object references: %w", err)
	}
	ch.deleteAfterCommit(ctx, keys)
	return n, nil
}
