package crud

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/file"
)

type uploadedKeysCtx struct{}

// WithUploadedKeys returns a context recording storage keys the caller just
// saved, adding to any already recorded, so the in-process writes accept them
// in an Image/File field or a `<field>_variants` storage_ref:
//
//	ff, err := file.ProcessFileField(ctx, store, r, name, "profiles", "avatar")
//	...
//	row, err := profiles.CreateOne(crud.WithUploadedKeys(ctx, ff.StorageRef),
//		map[string]any{"avatar": ff.StorageRef})
//
// SECURITY: pass only keys your own code just saved. A key taken from
// request data is exactly what this check exists to refuse.
func WithUploadedKeys(ctx context.Context, keys ...string) context.Context {
	if len(keys) == 0 {
		return ctx
	}
	set := make(map[string]bool, len(keys))
	maps.Copy(set, uploadedKeys(ctx))
	for _, k := range keys {
		if k != "" {
			set[k] = true
		}
	}
	return context.WithValue(ctx, uploadedKeysCtx{}, set)
}

func uploadedKeys(ctx context.Context) map[string]bool {
	m, _ := ctx.Value(uploadedKeysCtx{}).(map[string]bool)
	return m
}

type mediaRef struct {
	field string
	ref   string
}

func (ch *CrudHandler) variantsColumn(name string) string {
	v := name + derivedColumnSuffixes.variants
	for _, f := range ch.snapshotFields() {
		if f.Name == v {
			return v
		}
	}
	return ""
}

func variantRefsOf(v any) []string {
	var raw []byte
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		raw = []byte(t)
	case []byte:
		raw = t
	case json.RawMessage:
		raw = t
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return nil
		}
		raw = b
	}
	refs, _ := file.VariantStorageRefs(raw)
	return refs
}

func (ch *CrudHandler) bodyMediaRefs(body map[string]any) []mediaRef {
	var out []mediaRef
	for _, f := range ch.snapshotFields() {
		if f.Type != schema.Image && f.Type != schema.File {
			continue
		}
		if s, ok := body[f.Name].(string); ok && s != "" {
			out = append(out, mediaRef{field: f.Name, ref: s})
		}
		if vc := ch.variantsColumn(f.Name); vc != "" {
			for _, ref := range variantRefsOf(body[vc]) {
				out = append(out, mediaRef{field: vc, ref: ref})
			}
		}
	}
	return out
}

// checkMediaProvenance refuses a storage key in body that this write did not
// save and the row does not already hold. current reads the row's keys (nil
// for a create); it is called at most once.
func (ch *CrudHandler) checkMediaProvenance(ctx context.Context, body map[string]any, current func() (map[string]bool, error)) error {
	if ch.Entity == nil || serverWrites(ctx) {
		return nil
	}
	saved := uploadedKeys(ctx)
	var cur map[string]bool
	loaded := false
	for _, m := range ch.bodyMediaRefs(body) {
		if file.IsExternalURL(m.ref) || saved[m.ref] {
			continue
		}
		if !loaded && current != nil {
			c, err := current()
			if err != nil {
				return err
			}
			cur, loaded = c, true
		}
		if cur[m.ref] {
			continue
		}
		return &ValidationError{fields: map[string][]schema.Violation{
			m.field: {{Reason: reasonKeyNotUploaded}},
		}}
	}
	return nil
}

// currentMediaKeys returns a reader for the storage keys on the row keyed by
// key, read as rowObjects reads it; id is "" for an upsert.
func (ch *CrudHandler) currentMediaKeys(ctx context.Context, r *http.Request, id string, key []any) func() (map[string]bool, error) {
	return func() (map[string]bool, error) {
		objs, err := ch.rowObjects(ctx, r, id, key, false)
		if err != nil || objs == nil {
			return nil, err
		}
		keys := map[string]bool{}
		for _, o := range objs {
			keys[o.key] = true
			for _, ref := range o.renditions {
				keys[ref] = true
			}
		}
		return keys, nil
	}
}

type storedObject struct {
	key        string
	renditions []string
}

func (ch *CrudHandler) mediaFields() [][2]string {
	var out [][2]string
	for _, f := range ch.snapshotFields() {
		if f.Type == schema.Image || f.Type == schema.File {
			out = append(out, [2]string{f.Name, ch.variantsColumn(f.Name)})
		}
	}
	return out
}

// rowObjects reads the stored objects on the row keyed by key under the
// write's scope; trashed reads the soft-deleted row.
func (ch *CrudHandler) rowObjects(ctx context.Context, r *http.Request, id string, key []any, trashed bool) ([]storedObject, error) {
	fields := ch.mediaFields()
	cols := mediaColumns(fields)
	if len(cols) == 0 || len(key) == 0 {
		return nil, nil
	}
	for _, v := range key {
		if v == nil || fmt.Sprint(v) == "" {
			return nil, nil
		}
	}
	sel := cols
	if id != "" {
		sel = ch.keyedCols(cols)
	}
	kc, ka := ch.keyCond(key)
	qb := query.Select(sel...).
		From(ch.Entity.GetTable()).
		Where(kc, ka...)
	ch.ApplyTenantScope(qb, r)
	applyOwnerScope(ch, qb, r, false)
	if ch.Entity.Config.Scope.SoftDelete {
		if trashed {
			qb.Where("deleted_at IS NOT NULL")
		} else {
			qb.Where("deleted_at IS NULL")
		}
	}
	sqlStr, args := qb.Build()
	vals := make([]sql.NullString, len(sel))
	dest := make([]any, len(sel))
	for i := range vals {
		dest[i] = &vals[i]
	}
	err := ch.conn(ctx).QueryRowContext(ctx, sqlStr, args...).Scan(dest...)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read current file columns: %w", err)
	}
	got := make(map[string]string, len(sel))
	for i, c := range sel {
		got[c] = vals[i].String
	}
	if id != "" {
		row := make(map[string]any, len(sel))
		for c, v := range got {
			row[ch.convertKey(c)] = v
		}
		if !ch.ownRow(id, row, cols) {
			return nil, nil
		}
	}
	return appendObjects([]storedObject{}, fields, got), nil
}

func mediaColumns(fields [][2]string) []string {
	var cols []string
	for _, mf := range fields {
		cols = append(cols, mf[0])
		if mf[1] != "" {
			cols = append(cols, mf[1])
		}
	}
	return cols
}

func appendObjects(objs []storedObject, fields [][2]string, got map[string]string) []storedObject {
	for _, mf := range fields {
		o := storedObject{key: got[mf[0]]}
		if mf[1] != "" {
			o.renditions, _ = file.VariantStorageRefs([]byte(got[mf[1]]))
		}
		if o.key != "" || len(o.renditions) > 0 {
			objs = append(objs, o)
		}
	}
	return objs
}
