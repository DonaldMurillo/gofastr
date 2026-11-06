package crud

import (
	"context"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

func TestRepeatedIncludeHooksOnce(t *testing.T) {
	for _, toOne := range []bool{false, true} {
		child := rhEntity(t, "kids")
		relation := entity.HasMany("kids", "kids", "parent_id")
		if toOne {
			relation = entity.BelongsTo("kids", "kids", "kid_id")
		}
		parent := rhEntity(t, "parents", relation)
		registry := hook.NewHookRegistry()
		lists, gets := 0, 0
		registry.RegisterHook(hook.AfterList, func(_ context.Context, payload any) error {
			lists++
			if rows := payload.(*hook.ListPayload).Results; len(rows) != 2 {
				t.Errorf("AfterList received %d rows; want both distinct children", len(rows))
			}
			return nil
		})
		registry.RegisterHook(hook.AfterGet, func(context.Context, any) error { gets++; return nil })
		children := []map[string]any{{"id": "k1"}, {"id": "k2"}}
		var rows []map[string]any
		for index, childRow := range children {
			var attached any = []map[string]any{childRow}
			if toOne {
				attached = childRow
			}
			rows = append(rows, map[string]any{"id": index, "kids": attached})
		}
		rows = append(rows, rows[0])
		handler := rhHandler(t, parent, "kids", registry)
		err := handler.applyChildReadHooks(WithReadHooks(context.Background()), []*IncludeNode{
			rhNode(child, relation), rhNode(child, relation),
		}, rows)
		if err != nil {
			t.Fatal(err)
		}
		if lists != 1 || toOne && gets != 2 || !toOne && gets != 0 {
			t.Errorf("toOne=%v: list=%d get=%d; want one list chain and one get chain per distinct to-one row", toOne, lists, gets)
		}
	}
}
