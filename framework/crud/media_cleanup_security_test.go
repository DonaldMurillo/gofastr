package crud

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/internal/teststorage"
)

// Deleting or purging a record, or replacing its file, left the stored
// object in place and still served at /uploads/<key> to anyone holding
// the link (2026-10-27 S19). The objects a row lets go are now deleted
// once the write commits, unless another row still names them.

const (
	clnKey     = "uploads/cln/photo/a_1.png"
	clnVariant = "uploads/cln/photo/a_1_w320.png"
	clnShared  = "uploads/cln/photo/default.png"
	clnNew     = "uploads/cln/photo/b_2.png"
)

func clnWorld(t *testing.T, softDelete bool) (*CrudHandler, *sql.DB) {
	t.Helper()
	ddl := `CREATE TABLE cln (id TEXT PRIMARY KEY, photo TEXT, photo_variants TEXT, deleted_at TEXT)`
	ch, db := setupSecurityTestHandler(t, makeEntityConfig("cln", "cln", "", []schema.Field{
		{Name: "id", Type: schema.String},
		{Name: "photo", Type: schema.Image},
		{Name: "photo_variants", Type: schema.JSON},
	}, func(c *entity.EntityConfig) { c.Scope.SoftDelete = softDelete }), ddl)
	ch.Storage = teststorage.New(t, t.TempDir())
	for _, k := range []string{clnKey, clnVariant, clnShared, clnNew} {
		if err := ch.Storage.Save(context.Background(), k, strings.NewReader("x")); err != nil {
			t.Fatal(err)
		}
	}
	seedRows(t, db, "cln", []map[string]any{
		{"id": "a", "photo": clnKey, "photo_variants": `[{"storage_ref":"` + clnVariant + `"}]`},
		{"id": "s1", "photo": clnShared},
		{"id": "s2", "photo": clnShared},
	})
	return ch, db
}

func clnExists(t *testing.T, ch *CrudHandler, key string) bool {
	t.Helper()
	ok, err := ch.Storage.Exists(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	return ok
}

func TestDeleteRemovesStoredFile(t *testing.T) {
	ch, _ := clnWorld(t, false)
	if err := ch.DeleteOne(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{clnKey, clnVariant} {
		if clnExists(t, ch, k) {
			t.Errorf("SECURITY: %s survived the delete of its row", k)
		}
	}
}

func TestDeleteKeepsFileAnotherRowNames(t *testing.T) {
	ch, _ := clnWorld(t, false)
	if err := ch.DeleteOne(context.Background(), "s1"); err != nil {
		t.Fatal(err)
	}
	if !clnExists(t, ch, clnShared) {
		t.Fatal("deleting s1 removed the object s2 still names")
	}
	if err := ch.DeleteOne(context.Background(), "s2"); err != nil {
		t.Fatal(err)
	}
	if clnExists(t, ch, clnShared) {
		t.Error("SECURITY: the last row naming the object is gone and the object stayed")
	}
}

func TestUpdateRemovesReplacedFile(t *testing.T) {
	ch, _ := clnWorld(t, false)
	ctx := WithUploadedKeys(context.Background(), clnNew)
	if _, err := ch.UpdateOne(ctx, "a", map[string]any{"photo": clnNew, "photo_variants": "[]"}); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{clnKey, clnVariant} {
		if clnExists(t, ch, k) {
			t.Errorf("SECURITY: replaced %s survived the update", k)
		}
	}
	if !clnExists(t, ch, clnNew) {
		t.Error("the update removed the file it stored")
	}
}

func TestUpdateKeepsUnchangedFile(t *testing.T) {
	ch, _ := clnWorld(t, false)
	if _, err := ch.UpdateOne(context.Background(), "a", map[string]any{"photo": clnKey}); err != nil {
		t.Fatal(err)
	}
	if !clnExists(t, ch, clnKey) {
		t.Error("an update that sent the stored key back removed it")
	}
}

func TestUpsertRemovesReplacedFile(t *testing.T) {
	ch, _ := clnWorld(t, false)
	ctx := WithUploadedKeys(context.Background(), clnNew)
	if _, err := ch.UpsertOne(ctx, map[string]any{"id": "a", "photo": clnNew}); err != nil {
		t.Fatal(err)
	}
	if clnExists(t, ch, clnKey) {
		t.Error("SECURITY: the file an upsert replaced survived it")
	}
}

func TestPurgeRemovesStoredFile(t *testing.T) {
	ch, _ := clnWorld(t, true)
	if err := ch.DeleteOne(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	if !clnExists(t, ch, clnKey) {
		t.Fatal("a soft delete removed the file a restore needs")
	}
	if err := ch.PurgeOne(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{clnKey, clnVariant} {
		if clnExists(t, ch, k) {
			t.Errorf("SECURITY: %s survived the purge of its row", k)
		}
	}
}

func TestDeleteAllRemovesStoredFiles(t *testing.T) {
	ch, _ := clnWorld(t, false)
	type clnRow struct{ ID string }
	n, err := NewTypedQuery[clnRow](ch).Where(entity.NewStringColumn("id").Eq("a")).DeleteAll(context.Background())
	if err != nil || n != 1 {
		t.Fatalf("DeleteAll = %d, %v; want 1 row", n, err)
	}
	for _, k := range []string{clnKey, clnVariant} {
		if clnExists(t, ch, k) {
			t.Errorf("SECURITY: %s survived DeleteAll of its row", k)
		}
	}
	if !clnExists(t, ch, clnShared) {
		t.Error("DeleteAll of row a removed an object rows s1 and s2 name")
	}
}

// deleteLog records the keys a Storage was asked to delete.
type deleteLog struct {
	upload.Storage
	deleted []string
}

func (d *deleteLog) Delete(ctx context.Context, key string) error {
	d.deleted = append(d.deleted, key)
	return d.Storage.Delete(ctx, key)
}

func TestDeleteLeavesExternalURL(t *testing.T) {
	ch, db := clnWorld(t, false)
	seedRows(t, db, "cln", []map[string]any{{"id": "ext", "photo": "https://cdn.example.com/x.png"}})
	log := &deleteLog{Storage: ch.Storage}
	ch.Storage = log
	if err := ch.DeleteOne(context.Background(), "ext"); err != nil {
		t.Fatal(err)
	}
	if len(log.deleted) != 0 {
		t.Errorf("deleting a row with an external link asked storage to delete %v", log.deleted)
	}
}

func TestRolledBackDeleteKeepsFile(t *testing.T) {
	ch, _ := clnWorld(t, false)
	ch.Hooks = hook.NewHookRegistry()
	ch.Hooks.RegisterHook(hook.AfterDelete, func(context.Context, any) error {
		return errors.New("refuse")
	})
	if err := ch.DeleteOne(context.Background(), "a"); err == nil {
		t.Fatal("the delete succeeded past a failing AfterDelete hook")
	}
	if !clnExists(t, ch, clnKey) {
		t.Error("a rolled-back delete removed the row's file")
	}
}
