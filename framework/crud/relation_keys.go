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

// keyInClause renders the WHERE condition matching the records ids name,
// with placeholders numbered from start: "a IN ($1, $2)" for a one-column
// key, "(a, b) IN (($1, $2), ($3, $4))" for a composite one. Each id is a
// record id as entity.JoinKey spells it; one that does not split into
// the key columns is skipped. It reports false when no id survives.
// Columns go in validated but unquoted, the way the DDL wrote them.
func keyInClause(cols, ids []string, start int) (string, []any, bool, error) {
	safe := make([]string, len(cols))
	for i, c := range cols {
		s, err := query.SafeIdent(c)
		if err != nil {
			return "", nil, false, fmt.Errorf("invalid key column %q: %w", c, err)
		}
		safe[i] = s
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

// safeColumns validates each of cols with query.SafeIdent. The names go
// into SQL unquoted, the way the DDL wrote them.
func safeColumns(cols []string) ([]string, error) {
	out := make([]string, len(cols))
	for i, c := range cols {
		s, err := query.SafeIdent(c)
		if err != nil {
			return nil, fmt.Errorf("invalid column %q: %w", c, err)
		}
		out[i] = s
	}
	return out, nil
}

// oneFK returns rel's foreign-key column when it has exactly one, as a
// cascade write needs (entity.CheckRelationKeys refuses it several), or
// "" when it has none or several.
func oneFK(rel entity.Relation) string {
	if len(rel.ForeignKey) != 1 {
		return ""
	}
	return rel.ForeignKey[0]
}

// relationFKs returns rel's foreign-key columns validated for SQL: one,
// or one per key column of a composite key it references.
func relationFKs(rel entity.Relation) ([]string, error) {
	fks := rel.ForeignKey
	if len(fks) == 0 {
		return nil, fmt.Errorf("relation %q names no foreign key", rel.Name)
	}
	cols, err := safeColumns(fks)
	if err != nil {
		return nil, fmt.Errorf("relation %q has an invalid FK: %w", rel.Name, err)
	}
	return cols, nil
}

// scannedKey reads the values columns cols hold in one scanned row (names
// the row's columns, matched case-insensitively, vals its values) as one
// record id, the spelling entity.JoinKey gives the ids a loader is handed.
// It reports false when a column is missing or NULL: an absent reference.
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

// belongsToFKs reads the foreign key fks of each parent row ids names,
// from the parent table keyed on cols. It returns each parent's foreign
// key by record id, the key of several columns joined the way
// entity.JoinKey joins a record id, and the distinct keys in first-seen
// order. A foreign key with a NULL column is an absent optional relation
// and is left out.
func belongsToFKs(ctx context.Context, db DBExecutor, table string, cols, fks []string, ids []string) (map[string]string, []string, error) {
	safeTable, err := query.SafeIdent(table)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid parent table %q: %w", table, err)
	}
	safeFKs, err := safeColumns(fks)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid FK: %w", err)
	}
	where, args, ok, err := keyInClause(cols, ids, 1)
	if err != nil || !ok {
		return nil, nil, err
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s",
		strings.Join(cols, ", "), strings.Join(safeFKs, ", "), safeTable, where), args...)
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

// pivotScopes is a many_to_many join entity's own row scope: the owner
// and tenant predicates (eagerScopeFilters) and its read scope.
func pivotScopes(ctx context.Context, pivot *entity.Entity) []filter.ParsedFilter {
	if pivot == nil {
		return nil
	}
	return append(eagerScopeFilters(ctx, pivot), readScopeFilters(ctx, pivot)...)
}

// pivotClause renders the join entity's conditions on its Through table,
// numbered from startIdx: " AND t.deleted_at IS NULL" when it soft
// deletes, and its scopes. A migrator-made pivot (nil) has none.
func pivotClause(pivot *entity.Entity, scopes []filter.ParsedFilter, safeThrough string, startIdx int) (string, []any) {
	out := ""
	if pivot != nil && pivot.Config.Scope.SoftDelete {
		out = " AND " + safeThrough + ".deleted_at IS NULL"
	}
	clause, args := renderReadScope(scopes, safeThrough, startIdx)
	if clause != "" {
		out += " AND " + clause
	}
	return out, args
}
