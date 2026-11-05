package entityui

import (
	"context"
	"net/http"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func TestTrashWritesHonorBeforeGet(t *testing.T) {
	for _, operation := range []string{"_restore", "_purge"} {
		t.Run(operation, func(t *testing.T) {
			fixture := deletedUI(t)
			if _, err := fixture.db.Exec(`INSERT INTO notes (id, name, status, deleted_at) VALUES ('n3', 'visible trash', 'open', CURRENT_TIMESTAMP)`); err != nil {
				t.Fatal(err)
			}
			crudHandler, err := fixture.host.Crud(mustEntity(t, fixture, "notes"))
			if err != nil {
				t.Fatal(err)
			}
			crudHandler.Hooks = hook.NewHookRegistry()
			crudHandler.Hooks.RegisterHook(hook.BeforeGet, func(_ context.Context, data any) error {
				data.(*hook.GetPayload).AddWhere("status = $1", "open")
				return nil
			})
			hidden := postDeletedWrite(t, fixture, "/api/notes/n2/"+operation, "/notes?view=deleted")
			if hidden.Code != http.StatusNotFound {
				t.Errorf("hidden trash write = %d, want 404", hidden.Code)
			}
			var count int
			if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM notes WHERE id = 'n2' AND deleted_at IS NOT NULL`).Scan(&count); err != nil || count != 1 {
				t.Errorf("hidden row changed: count=%d err=%v", count, err)
			}
			visible := postDeletedWrite(t, fixture, "/api/notes/n3/"+operation, "/notes?view=deleted")
			if visible.Code != http.StatusSeeOther {
				t.Errorf("visible trash write = %d, want 303", visible.Code)
			}
			if err := fixture.db.QueryRow(`SELECT COUNT(*) FROM notes WHERE id = 'n3' AND deleted_at IS NOT NULL`).Scan(&count); err != nil || count != 0 {
				t.Errorf("visible row stayed trashed: count=%d err=%v", count, err)
			}
		})
	}
}
