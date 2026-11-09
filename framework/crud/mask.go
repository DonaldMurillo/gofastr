package crud

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// maskFieldsOf is ent's visible fields that declare a schema.Field Mask.
func maskFieldsOf(ent *entity.Entity) []schema.Field {
	if ent == nil {
		return nil
	}
	var out []schema.Field
	for _, f := range ent.GetFields() {
		if f.Mask != nil && !f.Hidden {
			out = append(out, f)
		}
	}
	return out
}

// maskRows runs each Mask field of ent over rows, in place: the one place
// crud applies a declared mask. Every read exit calls it before the app's
// own After hooks (a get, a list page, a cursor page, a streamed row, an
// included row, a write's response and an SSE event record), so a hook
// sees the masked value too. keyOf names a field's key in rows, the wire
// key of the level the rows came from. A NULL value stays null; Mask is
// not called for it, nor for a related row an include put in its place.
// Callers hand each row once: masking a masked value again is not
// guaranteed to leave it as it was.
func maskRows(ctx context.Context, ent *entity.Entity, keyOf func(string) string, rows []map[string]any) {
	fields := maskFieldsOf(ent)
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
			// An include of a self-named relation replaced the key's
			// value with the related row, which its own entity masks. A
			// map under any other field (a JSON object) is its own value.
			if _, isMap := v.(map[string]any); isMap && related[f.Name] {
				continue
			}
			row[key] = runMask(ctx, ent, f, v)
		}
	}
}

// relationNames is the set of ent's relation names: the keys an include
// writes a related row under.
func relationNames(ent *entity.Entity) map[string]bool {
	out := make(map[string]bool, len(ent.Config.Relations))
	for _, rel := range ent.Config.Relations {
		out[rel.Name] = true
	}
	return out
}

// runMask is f's Mask over v. A Mask that panics reads as null, never as
// the stored value, and the panic is logged: masks run on paths with no
// per-request recover (an _events delivery loop), where a panic would end
// the process. A mask runs as the caller, without a back office's CRUD
// elevation, just as lifecycle hooks do.
func runMask(ctx context.Context, ent *entity.Entity, f schema.Field, v any) (out any) {
	defer func() {
		if rec := recover(); rec != nil {
			logf("crud: mask on %s.%s panicked; reading it as null: %s", ent.GetName(), f.Name, textsafe.Recovered(rec))
			out = nil
		}
	}()
	return f.Mask(WithoutElevation(ctx), v)
}

// maskOwn runs this handler's entity masks over rows keyed the way its
// responses are. ctx reaches each Mask without the read-hook opt-in, as
// a hook's does, so a Mask that reads through crud does not re-enter it.
func (ch *CrudHandler) maskOwn(ctx context.Context, rows ...map[string]any) {
	maskRows(hookCtx(ctx), ch.Entity, ch.convertKey, rows)
}

// hasMasks reports whether the entity declares any Mask.
func (ch *CrudHandler) hasMasks() bool {
	return len(maskFieldsOf(ch.Entity)) > 0
}
