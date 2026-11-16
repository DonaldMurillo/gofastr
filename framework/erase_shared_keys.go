package framework

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/file"
	"github.com/DonaldMurillo/gofastr/framework/migrate"
)

type fileRefColumns struct {
	table    string
	files    []string
	variants []string
}

func (a *App) collectFileRefColumns(ctx context.Context, dialect migrate.Dialect) ([]fileRefColumns, error) {
	if a.Registry == nil {
		return nil, nil
	}
	g, err := a.buildDataGraph()
	if err != nil {
		return nil, err
	}
	var order []string
	for _, t := range g.sorted {
		if dt := g.tables[t]; len(dt.fileCols)+len(dt.variantCols) > 0 {
			order = append(order, t)
		}
	}
	out := make([]fileRefColumns, 0, len(order))
	for _, t := range order {
		dt := g.tables[t]
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
		for _, f := range dt.fileCols {
			if has(f) {
				kept.files = append(kept.files, f)
			}
		}
		for _, v := range dt.variantCols {
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
		qt := query.QuoteFolded(query.MustIdent(t.table))
		for _, col := range t.files {
			if err := a.markNamedInColumn(ctx, qt, query.QuoteFolded(query.MustIdent(col)), candidates, named); err != nil {
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
			if err := a.markNamedInVariants(ctx, qt, query.QuoteFolded(query.MustIdent(col)), open, named); err != nil {
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
			args = append(args, `%\\u%`)
			likes = append(likes, fmt.Sprintf(`CAST(%s AS TEXT) LIKE $%d ESCAPE '\'`, qc, len(args)))
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
