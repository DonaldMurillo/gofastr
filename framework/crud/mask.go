package crud

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func maskFieldsOf(fields []schema.Field) []schema.Field {
	var out []schema.Field
	for _, f := range fields {
		if f.Mask != nil && !f.Hidden {
			out = append(out, f)
		}
	}
	return out
}

// maskRows runs each Mask field of ent over rows in place; keyOf gives a
// field's key in rows.
func maskRows(ctx context.Context, ent *entity.Entity, keyOf func(string) string, rows []map[string]any) {
	if ent != nil {
		maskDeclared(ctx, ent, ent.GetFields(), keyOf, rows)
	}
}

func maskDeclared(ctx context.Context, ent *entity.Entity, declared []schema.Field, keyOf func(string) string, rows []map[string]any) {
	fields := maskFieldsOf(declared)
	if len(fields) == 0 {
		return
	}
	related := relationNames(ent)
	for _, row := range rows {
		if row == nil {
			continue
		}
		for _, f := range fields {
			key := keyOf(f.Name)
			v, ok := row[key]
			if !ok || v == nil {
				continue
			}
			if _, isMap := v.(map[string]any); isMap && related[f.Name] {
				continue
			}
			row[key] = runMask(ctx, ent, f, v)
		}
	}
}

func relationNames(ent *entity.Entity) map[string]bool {
	out := make(map[string]bool, len(ent.Config.Relations))
	for _, rel := range ent.Config.Relations {
		out[rel.Name] = true
	}
	return out
}

func runMask(ctx context.Context, ent *entity.Entity, f schema.Field, v any) (out any) {
	defer func() {
		if rec := recover(); rec != nil {
			logf("crud: mask on %s.%s panicked; reading it as null: %s", ent.GetName(), f.Name, textsafe.Recovered(rec))
			out = nil
		}
	}()
	return f.Mask(WithoutElevation(ctx), v)
}

func (ch *CrudHandler) maskOwn(ctx context.Context, rows ...map[string]any) {
	if ch.Entity != nil {
		maskDeclared(hookCtx(ctx), ch.Entity, ch.snapshotFields(), ch.convertKey, rows)
	}
}

func (ch *CrudHandler) hasMasks() bool {
	return ch.Entity != nil && len(maskFieldsOf(ch.snapshotFields())) > 0
}
