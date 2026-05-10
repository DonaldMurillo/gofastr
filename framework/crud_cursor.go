package framework

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gofastr/gofastr/core/query"
)

// cursorField returns the column the handler keysets on. Defaults to the
// entity primary key, overridden by EntityConfig.CursorField when non-empty.
// The chosen field must be unique-enough that two rows never share the same
// value — keyset pagination breaks the moment ties happen at the boundary.
func (ch *CrudHandler) cursorField() string {
	if ch.Entity.Config.CursorField != "" {
		return ch.Entity.Config.CursorField
	}
	return ch.PrimaryKey
}

// serveCursorList handles a cursor-paginated List request. It uses keyset
// pagination on the entity's cursor field (default: PrimaryKey) and emits a
// CursorPage envelope. The total count is intentionally omitted — cursor
// pagination's appeal is avoiding count's table scan.
//
// `?sort=` is ignored in cursor mode: keyset pagination requires a strictly
// ordered, unique-enough key, so the cursor field controls ORDER BY.
func (ch *CrudHandler) serveCursorList(ctx context.Context, w http.ResponseWriter, r *http.Request, includes []*IncludeNode, filters []ParsedFilter) {
	cursor, limit, direction := ParseCursorPagination(r)
	if direction != "forward" && direction != "backward" {
		writeJSONError(w, http.StatusBadRequest, "direction must be 'forward' or 'backward'")
		return
	}

	var cursorValue string
	if cursor != "" {
		_, val, err := DecodeCursor(cursor)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid cursor: "+err.Error())
			return
		}
		cursorValue = val
	}

	field := ch.cursorField()
	cols := ch.visibleFields()
	qb := query.Select(cols...)
	qb.From(ch.Entity.GetTable())
	applyFiltersToQuery(qb, filters)
	ch.applyTenantScope(qb, r)
	ch.applySoftDeleteFilter(qb, r)

	if cursorValue != "" {
		qb.Cursor(field, cursorValue, direction)
	} else {
		// First page — order by the cursor field; backward starts from the
		// largest values.
		if direction == "backward" {
			qb.Order(field, "DESC")
		} else {
			qb.Order(field, "ASC")
		}
	}

	// Fetch limit+1 to detect HasMore without an extra query.
	qb.Limit(limit + 1)

	dataSQL, dataArgs := qb.Build()
	rows, err := ch.DB.QueryContext(ctx, dataSQL, dataArgs...)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "query failed: "+err.Error())
		return
	}
	defer rows.Close()

	results, err := scanRows(rows, cols, ch.convertKey)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "scan failed: "+err.Error())
		return
	}

	if err := ch.applyIncludeTree(ctx, results, includes); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "include failed: "+err.Error())
		return
	}

	cursorKey := ch.convertKey(field)
	page := NewCursorPage(results, cursorKey, limit)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(page)
}
