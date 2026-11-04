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
		registry.RegisterHook(hook.AfterList, func(context.Context, any) error { lists++; return nil })
		registry.RegisterHook(hook.AfterGet, func(context.Context, any) error { gets++; return nil })
		childRow := map[string]any{"id": "k1"}
		var attached any = []map[string]any{childRow}
		if toOne {
			attached = childRow
		}
		rows := []map[string]any{{"id": "p1", "kids": attached}}
		handler := rhHandler(t, parent, "kids", registry)
		err := handler.applyChildReadHooks(WithReadHooks(context.Background()), []*IncludeNode{
			rhNode(child, relation), rhNode(child, relation),
		}, rows)
		if err != nil {
			t.Fatal(err)
		}
		if lists != 1 || toOne && gets != 1 || !toOne && gets != 0 {
			t.Errorf("toOne=%v: list=%d get=%d; want one chain per relation", toOne, lists, gets)
		}
	}
}
