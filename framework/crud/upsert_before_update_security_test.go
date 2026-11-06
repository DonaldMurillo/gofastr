package crud

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func TestUpsertBeforeUpdateConflictFailsClosed(t *testing.T) {
	for _, behavior := range []string{"reject", "stamp"} {
		t.Run(behavior, func(t *testing.T) {
			handler, mock := upsertHookMock(t)
			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT id, title FROM notes WHERE id = \$1`).WithArgs("existing").
				WillReturnRows(sqlmock.NewRows([]string{"id", "title"}).AddRow("existing", "old"))
			mock.ExpectQuery(`INSERT INTO notes .* ON CONFLICT .* DO UPDATE SET`).WithArgs("existing", "first").
				WillReturnRows(sqlmock.NewRows([]string{"id", "title"}).AddRow("existing", "first"))
			mock.ExpectCommit()
			if _, err := handler.UpsertOne(context.Background(), map[string]any{"id": "existing", "title": "first"}); err != nil {
				t.Fatalf("establish conflict fixture: %v", err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatal(err)
			}

			calls := 0
			handler.Hooks = hook.NewHookRegistry()
			handler.Hooks.RegisterHook(hook.BeforeUpdate, func(_ context.Context, data any) error {
				calls++
				if behavior == "reject" {
					return errors.New("update rejected")
				}
				data.(map[string]any)["title"] = "stamped"
				return nil
			})
			for _, phase := range []hook.HookType{hook.BeforeCreate, hook.AfterCreate, hook.AfterUpdate} {
				handler.Hooks.RegisterHook(phase, func(context.Context, any) error {
					calls++
					return nil
				})
			}
			body := map[string]any{"id": "existing", "title": "bypass"}
			result, err := handler.UpsertOne(context.Background(), body)
			if err == nil || !strings.Contains(err.Error(), "BeforeUpdate") || !strings.Contains(err.Error(), "UpdateOne") {
				t.Fatalf("upsert must refuse the hook-bearing conflict before SQL, got result=%v err=%v", result, err)
			}
			if result != nil || calls != 0 || !reflect.DeepEqual(body, map[string]any{"id": "existing", "title": "bypass"}) {
				t.Fatalf("refused upsert caused side effects: result=%v hooks=%d body=%v", result, calls, body)
			}
		})
	}
}

func TestUpsertBeforeUpdateNewKeyFailsClosed(t *testing.T) {
	handler, _ := upsertHookMock(t)
	handler.Hooks = hook.NewHookRegistry()
	handler.Hooks.RegisterHook(hook.BeforeUpdate, func(context.Context, any) error { return nil })
	if _, err := handler.UpsertOne(WithServerWrites(context.Background()), map[string]any{"id": "new", "title": "new"}); err == nil || !strings.Contains(err.Error(), "BeforeUpdate") {
		t.Fatalf("an apparently new key must not bypass the fail-closed guard: %v", err)
	}
}

func upsertHookMock(t *testing.T) (*CrudHandler, sqlmock.Sqlmock) {
	t.Helper()
	database, mock, err := sqlmock.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	ent := entity.Define("notes", entity.EntityConfig{
		Table:  "notes",
		Fields: []schema.Field{{Name: "id", Type: schema.String}, {Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	return NewCrudHandler(ent, database).WithJSONCase(CaseSnake), mock
}
