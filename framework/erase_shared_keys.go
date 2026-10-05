package framework

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/file"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

// Erasure deletes only the stored objects it can attribute to the erased
// user. A key qualifies when the erased user's rows named it and, once
// those rows are gone, no surviving row in any entity names it: not
// another user's row, and not a row of an entity erasure never touches.
//
// Why a reference check rather than an upload ledger or an owner prefix in
// the key: the CRUD write path now refuses a storage key the caller did not
// upload (crud/media_provenance.go), so new rows cannot name another user's
// object. What remains is rows written before that check, and keys host code
// wrote under WithServerWrites. Both are covered by asking the database who
// else names the key, which needs no new table, no change to the key format,
// and still erases every key written before this check existed. A ledger or
// a prefix would leave every existing key unattributed, so erasure would
// stop deleting files it deletes correctly today.
//
// An absolute http(s) URL is an external link, not a storage key, and is
// never deleted.

// fileRefColumns is one physical table's file columns and variants columns,
// across every registered entity, owner-scoped or not.
type fileRefColumns struct {
	table    string
	files    []string
	variants []string
}

// collectFileRefColumns lists, per physical table, every schema.Image /
// schema.File column and every declared "<field>_variants" column that
// exists in the live table.
func (a *App) collectFileRefColumns(ctx context.Context, dialect migrate.Dialect) ([]fileRefColumns, error) {
	if a.Registry == nil {
		return nil, nil
	}
	merged := migrate.UnionEntities(a.registryView())
	names := make([]string, 0, len(merged))
	for n := range merged {
		names = append(names, n)
	}
	sort.Strings(names)
	byTable := map[string]*fileRefColumns{}
	var order []string
	for _, n := range names {
		ent := merged[n]
		if ent == nil {
			continue
		}
		declared := map[string]bool{}
		for _, f := range ent.GetFields() {
			declared[f.Name] = true
		}
		for _, f := range ent.GetFields() {
			if f.Type != schema.Image && f.Type != schema.File {
				continue
			}
			t := ent.GetTable()
			c := byTable[t]
			if c == nil {
				c = &fileRefColumns{table: t}
				byTable[t] = c
				order = append(order, t)
			}
			c.files = appendUnique(c.files, f.Name)
			if v := f.Name + "_variants"; declared[v] {
				c.variants = appendUnique(c.variants, v)
			}
		}
	}
	out := make([]fileRefColumns, 0, len(order))
	for _, t := range order {
		c := byTable[t]
		live, err := migrate.ReadLiveColumns(ctx, a.DB, t, dialect)
		if err != nil {
			return nil, fmt.Errorf("read columns of %q: %w", t, err)
		}
		has := func(col string) bool {
			for name := range live {
				if strings.EqualFold(name, col) {
					return true
				}
			}
			return false
		}
		kept := fileRefColumns{table: t}
		for _, f := range c.files {
			if has(f) {
				kept.files = append(kept.files, f)
			}
		}
		for _, v := range c.variants {
			if has(v) {
				kept.variants = append(kept.variants, v)
			}
		}
		if len(kept.files)+len(kept.variants) > 0 {
			out = append(out, kept)
		}
	}
	return out, nil
}

func appendUnique(s []string, v string) []string {
	for _, x := range s {
		if x == v {
			return s
		}
	}
	return append(s, v)
}

// unsharedObjectKeys returns the keys erasure may delete: keys that are not
// external URLs and that no row left in the database names. It runs after
// the erasure commits, so the erased user's own rows no longer count.
func (a *App) unsharedObjectKeys(ctx context.Context, dialect migrate.Dialect, keys []string) ([]string, error) {
	var candidates []string
	for _, k := range keys {
		if !file.IsExternalURL(k) {
			candidates = append(candidates, k)
		}
	}
	if len(candidates) == 0 {
		return nil, nil
	}
	tables, err := a.collectFileRefColumns(ctx, dialect)
	if err != nil {
		return nil, err
	}
	named := map[string]bool{}
	for _, t := range tables {
		qt := query.QuoteIdent(query.MustIdent(t.table))
		for _, col := range t.files {
			if err := a.markNamedInColumn(ctx, qt, query.QuoteIdent(query.MustIdent(col)), candidates, named); err != nil {
				return nil, fmt.Errorf("check %s.%s: %w", t.table, col, err)
			}
		}
		for _, col := range t.variants {
			var open []string
			for _, k := range candidates {
				if !named[k] {
					open = append(open, k)
				}
			}
			if len(open) == 0 {
				break
			}
			if err := a.markNamedInVariants(ctx, qt, query.QuoteIdent(query.MustIdent(col)), open, named); err != nil {
				return nil, fmt.Errorf("check %s.%s: %w", t.table, col, err)
			}
		}
	}
	var out []string
	for _, k := range candidates {
		if !named[k] {
			out = append(out, k)
		}
	}
	return out, nil
}

// markNamedInColumn sets named[k] for every candidate key some row's column
// holds verbatim.
func (a *App) markNamedInColumn(ctx context.Context, qt, qc string, keys []string, named map[string]bool) error {
	const chunk = 200
	for start := 0; start < len(keys); start += chunk {
		part := keys[start:min(start+chunk, len(keys))]
		ph := make([]string, len(part))
		args := make([]any, len(part))
		for i, k := range part {
			ph[i] = fmt.Sprintf("$%d", i+1)
			args[i] = k
		}
		rows, err := a.DB.QueryContext(ctx,
			fmt.Sprintf("SELECT %s FROM %s WHERE %s IN (%s)", qc, qt, qc, strings.Join(ph, ", ")), args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var v sql.NullString
			if err := rows.Scan(&v); err != nil {
				rows.Close()
				return err
			}
			if v.Valid {
				named[v.String] = true
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// markNamedInVariants sets named[k] for every candidate key some row's
// variants column lists as a storage_ref. One query per chunk of keys: a
// LIKE on a literal run of each key narrows the rows (it may over-match:
// case folding, other keys sharing the run), and each row is then parsed
// with file.VariantStorageRefs, the same parser the erase read and the CRUD
// write path use, so JSON spacing or escaping cannot hide a reference. A
// chunk holding a key with no usable literal run scans every non-null row.
func (a *App) markNamedInVariants(ctx context.Context, qt, qc string, keys []string, named map[string]bool) error {
	const chunk = 100
	for start := 0; start < len(keys); start += chunk {
		part := keys[start:min(start+chunk, len(keys))]
		want := make(map[string]bool, len(part))
		var likes []string
		var args []any
		scanAll := false
		for _, k := range part {
			want[k] = true
			run := likeRun(k)
			if run == "" {
				scanAll = true
				continue
			}
			args = append(args, "%"+run+"%")
			likes = append(likes, fmt.Sprintf(`CAST(%s AS TEXT) LIKE $%d ESCAPE '\'`, qc, len(args)))
		}
		stmt := fmt.Sprintf("SELECT CAST(%s AS TEXT) FROM %s WHERE %s IS NOT NULL", qc, qt, qc)
		if scanAll {
			args = nil
		} else {
			stmt += " AND (" + strings.Join(likes, " OR ") + ")"
		}
		if err := a.scanVariantRefs(ctx, stmt, args, want, named); err != nil {
			return err
		}
	}
	return nil
}

func (a *App) scanVariantRefs(ctx context.Context, stmt string, args []any, want, named map[string]bool) error {
	rows, err := a.DB.QueryContext(ctx, stmt, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var v sql.NullString
		if err := rows.Scan(&v); err != nil {
			return err
		}
		refs, _ := file.VariantStorageRefs([]byte(v.String))
		for _, r := range refs {
			if want[r] {
				named[r] = true
			}
		}
	}
	return rows.Err()
}

// likeRun returns the longest run of the key's characters that JSON writes
// verbatim in every encoder (letters, digits, '.', '-', '_'), LIKE-escaped.
// The run sits between path separators, which some encoders escape as "\/".
func likeRun(key string) string {
	best, cur := "", strings.Builder{}
	flush := func() {
		if cur.Len() > len(best) {
			best = cur.String()
		}
		cur.Reset()
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-' || c == '_' {
			cur.WriteByte(c)
			continue
		}
		flush()
	}
	flush()
	return strings.ReplaceAll(best, "_", `\_`)
}
