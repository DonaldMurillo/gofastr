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

// Storage-key provenance for schema.Image / schema.File fields.
//
// A relative value in a file field is a storage key, and EraseUserData
// deletes every key the erased user's rows name. If a caller could write any
// key it liked, it could copy another user's key (published in that user's
// /uploads/<key> URLs) into its own row and erase itself to delete the other
// user's object. So a write may set a relative value, in the field or in a
// `<field>_variants` storage_ref, only when one of these holds:
//
//   - this write saved the object: the multipart parse put the key in the
//     request's ledger, or host code passed it through WithUploadedKeys;
//   - the key is already on the row being written (an unchanged value sent
//     back on update or upsert), read under the caller's write scope;
//   - the write is trusted server code (WithServerWrites).
//
// An absolute http(s) URL is an external link, not a storage key: any write
// may set one, and erasure never deletes it.

type uploadedKeysCtx struct{}

// WithUploadedKeys returns a context recording keys the caller saved to
// storage for the write it is about to make, so CreateOne, UpdateOne,
// UpsertOne and the batch helpers accept them in an Image/File field or a
// `<field>_variants` storage_ref. Call it with the StorageRef that
// file.ProcessFileField (or your own Storage.Save) returned:
//
//	ff, err := file.ProcessFileField(ctx, store, r, name, "profiles", "avatar")
//	...
//	row, err := profiles.CreateOne(crud.WithUploadedKeys(ctx, ff.StorageRef),
//		map[string]any{"avatar": ff.URL})
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

// mediaRef is one storage reference a write body sets, with the body field
// that carries it (the file field itself or its variants column).
type mediaRef struct {
	field string
	ref   string
}

// variantsColumn returns the declared `<name>_variants` column of a file
// field, or "" when the entity does not declare one.
func (ch *CrudHandler) variantsColumn(name string) string {
	v := name + derivedColumnSuffixes.variants
	for _, f := range ch.snapshotFields() {
		if f.Name == v {
			return v
		}
	}
	return ""
}

// variantRefsOf reads the storage refs of a variants value as it arrives in
// a body: JSON text, or an already-decoded array.
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

// bodyMediaRefs lists every storage reference body sets, in field order.
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

// checkMediaProvenance refuses a storage key the caller did not upload. See
// the comment at the top of this file. current reads the keys already on the
// row being written; it is nil for a create and is called at most once, only
// when a key is not otherwise accounted for.
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
		return &ValidationError{fields: map[string][]string{
			m.field: {"storage key was not uploaded by this request"},
		}}
	}
	return nil
}

// currentMediaKeys returns a reader for the storage keys on row id, under
// the same tenant, owner and soft-delete WHERE the UPDATE applies, so "the
// row's current value" never means a row this caller cannot write.
func (ch *CrudHandler) currentMediaKeys(ctx context.Context, r *http.Request, id any) func() (map[string]bool, error) {
	return func() (map[string]bool, error) {
		var cols []string
		variant := map[string]bool{}
		for _, f := range ch.snapshotFields() {
			if f.Type != schema.Image && f.Type != schema.File {
				continue
			}
			cols = append(cols, f.Name)
			if vc := ch.variantsColumn(f.Name); vc != "" {
				cols = append(cols, vc)
				variant[vc] = true
			}
		}
		if len(cols) == 0 || id == nil || fmt.Sprint(id) == "" {
			return nil, nil
		}
		qb := query.Select(cols...).
			From(ch.Entity.GetTable()).
			Where(ch.PrimaryKey+" = $1", id)
		ch.ApplyTenantScope(qb, r)
		applyOwnerScope(ch, qb, r, false)
		if ch.Entity.Config.Scope.SoftDelete {
			qb.Where("deleted_at IS NULL")
		}
		sqlStr, args := qb.Build()
		vals := make([]sql.NullString, len(cols))
		dest := make([]any, len(cols))
		for i := range vals {
			dest[i] = &vals[i]
		}
		err := ch.DB.QueryRowContext(ctx, sqlStr, args...).Scan(dest...)
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, fmt.Errorf("read current file columns: %w", err)
		}
		keys := map[string]bool{}
		for i, c := range cols {
			if !vals[i].Valid || vals[i].String == "" {
				continue
			}
			if !variant[c] {
				keys[vals[i].String] = true
				continue
			}
			refs, _ := file.VariantStorageRefs([]byte(vals[i].String))
			for _, ref := range refs {
				keys[ref] = true
			}
		}
		return keys, nil
	}
}
