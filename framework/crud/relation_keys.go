package crud

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// keyInClause renders "a IN ($1, $2)" or "(a, b) IN (($1, $2), ...)" for
// the record ids, numbered from start; an id that does not split into the
// key columns is skipped, and false means none survived.
func keyInClause(cols, ids []string, start int) (string, []any, bool, error) {
	safe, err := query.SafeColumns(cols, "key column")
	if err != nil {
		return "", nil, false, err
	}
	for i, s := range safe {
		safe[i] = query.QuoteKeyword(s)
	}
	var tuples []string
	var args []any
	for _, id := range ids {
		parts, err := entity.SplitKey(id, len(cols))
		if err != nil {
			continue
		}
		ph := make([]string, len(parts))
		for i, p := range parts {
			ph[i] = fmt.Sprintf("$%d", start+len(args))
			args = append(args, p)
		}
		if len(cols) == 1 {
			tuples = append(tuples, ph[0])
		} else {
			tuples = append(tuples, "("+strings.Join(ph, ", ")+")")
		}
	}
	if len(tuples) == 0 {
		return "", nil, false, nil
	}
	lhs := safe[0]
	if len(cols) > 1 {
		lhs = "(" + strings.Join(safe, ", ") + ")"
	}
	return lhs + " IN (" + strings.Join(tuples, ", ") + ")", args, true, nil
}

// oneFK is rel's foreign-key column, or "" unless it has exactly one.
func oneFK(rel entity.Relation) string {
	if len(rel.ForeignKey) != 1 {
		return ""
	}
	return rel.ForeignKey[0]
}

func relationFKs(rel entity.Relation) ([]string, error) {
	fks := rel.ForeignKey
	if len(fks) == 0 {
		return nil, fmt.Errorf("relation %q names no foreign key", rel.Name)
	}
	cols, err := query.SafeColumns(fks, "column")
	if err != nil {
		return nil, fmt.Errorf("relation %q has an invalid FK: %w", rel.Name, err)
	}
	return cols, nil
}

// scannedKey joins the values of cols in one scanned row into a record id;
// false when a column is missing or NULL.
func scannedKey(names []string, vals []any, cols []string) (string, bool) {
	parts := make([]string, len(cols))
	for i, c := range cols {
		j := slices.IndexFunc(names, func(n string) bool { return strings.EqualFold(n, c) })
		if j < 0 || vals[j] == nil {
			return "", false
		}
		if b, ok := vals[j].([]byte); ok {
			parts[i] = string(b)
		} else {
			parts[i] = fmt.Sprintf("%v", vals[j])
		}
	}
	return entity.JoinKey(parts), true
}

// belongsToFKs reads each parent's foreign key, by parent record id, and
// the distinct keys in first-seen order; a key with a NULL column is left
// out.
func belongsToFKs(ctx context.Context, db DBExecutor, table string, cols, fks []string, ids []string) (map[string]string, []string, error) {
	safeTable, err := query.SafeIdent(table)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid parent table %q: %w", table, err)
	}
	safeFKs, err := query.SafeColumns(fks, "column")
	if err != nil {
		return nil, nil, fmt.Errorf("invalid FK: %w", err)
	}
	where, args, ok, err := keyInClause(cols, ids, 1)
	if err != nil || !ok {
		return nil, nil, err
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s",
		strings.Join(query.QuoteKeywords(cols), ", "), strings.Join(query.QuoteKeywords(safeFKs), ", "), query.QuoteKeyword(safeTable), where), args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	bySource := map[string]string{}
	var unique []string
	seen := map[string]bool{}
	for rows.Next() {
		parts := make([]string, len(cols))
		fkVals := make([]sql.NullString, len(fks))
		dest := make([]any, 0, len(cols)+len(fks))
		for i := range parts {
			dest = append(dest, &parts[i])
		}
		for i := range fkVals {
			dest = append(dest, &fkVals[i])
		}
		if err := rows.Scan(dest...); err != nil {
			return nil, nil, err
		}
		fk := make([]string, len(fks))
		absent := false
		for i, v := range fkVals {
			absent = absent || !v.Valid
			fk[i] = v.String
		}
		if absent {
			continue
		}
		key := entity.JoinKey(fk)
		bySource[entity.JoinKey(parts)] = key
		if !seen[key] {
			seen[key] = true
			unique = append(unique, key)
		}
	}
	return bySource, unique, rows.Err()
}

func pivotScopes(ctx context.Context, pivot *entity.Entity) []filter.ParsedFilter {
	if pivot == nil {
		return nil
	}
	return append(eagerScopeFilters(ctx, pivot), readScopeFilters(ctx, pivot)...)
}

// pivotClause renders the join entity's soft-delete and scope conditions
// on its Through table, numbered from startIdx.
func pivotClause(pivot *entity.Entity, scopes []filter.ParsedFilter, safeThrough string, startIdx int) (string, []any) {
	out := ""
	if pivot != nil && pivot.Config.Scope.SoftDelete {
		out = " AND " + query.QuoteKeyword(safeThrough) + ".deleted_at IS NULL"
	}
	clause, args := renderReadScope(scopes, safeThrough, startIdx)
	if clause != "" {
		out += " AND " + clause
	}
	return out, args
}
