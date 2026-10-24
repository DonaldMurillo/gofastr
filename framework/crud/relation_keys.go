package crud

import (
	"context"
	"database/sql"
	"fmt"
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

// belongsToFKs reads the foreign key fk of each parent row ids names, from
// the parent table keyed on cols. It returns each parent's foreign key
// value by record id, and the distinct values in first-seen order. A NULL
// foreign key is an absent optional relation and is left out.
func belongsToFKs(ctx context.Context, db DBExecutor, table string, cols []string, fk string, ids []string) (map[string]string, []string, error) {
	safeTable, err := query.SafeIdent(table)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid parent table %q: %w", table, err)
	}
	safeFK, err := query.SafeIdent(fk)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid FK %q: %w", fk, err)
	}
	where, args, ok, err := keyInClause(cols, ids, 1)
	if err != nil || !ok {
		return nil, nil, err
	}
	rows, err := db.QueryContext(ctx, fmt.Sprintf("SELECT %s, %s FROM %s WHERE %s",
		strings.Join(cols, ", "), safeFK, safeTable, where), args...)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()

	bySource := map[string]string{}
	var unique []string
	seen := map[string]bool{}
	for rows.Next() {
		parts := make([]string, len(cols))
		var fkVal sql.NullString
		dest := make([]any, 0, len(cols)+1)
		for i := range parts {
			dest = append(dest, &parts[i])
		}
		if err := rows.Scan(append(dest, &fkVal)...); err != nil {
			return nil, nil, err
		}
		if !fkVal.Valid {
			continue
		}
		bySource[entity.JoinKey(parts)] = fkVal.String
		if !seen[fkVal.String] {
			seen[fkVal.String] = true
			unique = append(unique, fkVal.String)
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
