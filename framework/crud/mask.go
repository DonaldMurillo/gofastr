package crud

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

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

// maskRows runs each Mask field of ent over rows in place; keyOf gives a
// field's key in rows.
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
	maskRows(hookCtx(ctx), ch.Entity, ch.convertKey, rows)
}

func (ch *CrudHandler) hasMasks() bool {
	return len(maskFieldsOf(ch.Entity)) > 0
}

func (ch *CrudHandler) carriesMasked(body map[string]any) bool {
	for _, f := range maskFieldsOf(ch.Entity) {
		if _, sent := body[f.Name]; sent {
			return true
		}
	}
	return false
}

// maskedEchoes names the Mask fields body sends back as the masked form
// of stored's value. A stored NULL is never an echo.
func (ch *CrudHandler) maskedEchoes(ctx context.Context, body, stored map[string]any) []string {
	if stored == nil {
		return nil
	}
	var out []string
	for _, f := range maskFieldsOf(ch.Entity) {
		given, sent := body[f.Name]
		held := stored[ch.convertKey(f.Name)]
		if !sent || held == nil {
			continue
		}
		if sameWireValue(runMask(hookCtx(ctx), ch.Entity, f, held), given) {
			out = append(out, f.Name)
		}
	}
	return out
}

// dropMaskedEchoes removes row id's masked echoes from body, reading the
// row under the write scope when the caller has no pre-image of it.
func (ch *CrudHandler) dropMaskedEchoes(ctx context.Context, r *http.Request, id string, body, pre map[string]any) error {
	if !ch.carriesMasked(body) {
		return nil
	}
	if pre == nil {
		var cols []string
		for _, f := range maskFieldsOf(ch.Entity) {
			if _, sent := body[f.Name]; sent {
				cols = append(cols, f.Name)
			}
		}
		stored, err := ch.selectStored(ctx, r, id, cols)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		pre = stored
	}
	for _, name := range ch.maskedEchoes(ctx, body, pre) {
		delete(body, name)
	}
	return nil
}

// sameWireValue reports whether given is what a client read as masked:
// equal as JSON, or its text when a form sent it.
func sameWireValue(masked, given any) bool {
	text, isText := given.(string)
	if m, ok := stringOf(masked); ok {
		return isText && m == text
	}
	a, errA := json.Marshal(masked)
	b, errB := json.Marshal(given)
	if errA != nil || errB != nil {
		return false
	}
	return bytes.Equal(a, b) || (isText && masked != nil && string(a) == text)
}
