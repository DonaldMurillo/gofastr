package framework

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/gofastr/gofastr/core/query"
)

// serveCursorList handles a cursor-paginated List request. It uses keyset
// pagination on the primary key (ORDER BY pk ASC for forward, DESC for
// backward) and emits a CursorPage envelope. The total count is intentionally
// omitted — cursor pagination's appeal is avoiding count's table scan.
//
// `?sort=` is ignored in cursor mode: keyset pagination requires a strictly
// ordered, unique key, and the entity primary key is the only field this
// handler can guarantee that for. Future per-entity cursor fields could
// relax this.
func (ch *CrudHandler) serveCursorList(ctx context.Context, w http.ResponseWriter, r *http.Request, includes []Relation, filters []ParsedFilter) {
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

	cols := ch.visibleFields()
	qb := query.Select(cols...)
	qb.From(ch.Entity.GetTable())
	applyFiltersToQuery(qb, filters)
	ch.applyTenantScope(qb, r)
	ch.applySoftDeleteFilter(qb, r)

	if cursorValue != "" {
		qb.Cursor(ch.PrimaryKey, cursorValue, direction)
	} else {
		// First page — order by PK; backward starts from the largest values.
		if direction == "backward" {
			qb.Order(ch.PrimaryKey, "DESC")
		} else {
			qb.Order(ch.PrimaryKey, "ASC")
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

	if err := ch.applyIncludes(ctx, results, includes); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "include failed: "+err.Error())
		return
	}

	pkKey := ch.convertKey(ch.PrimaryKey)
	page := NewCursorPage(results, pkKey, limit)

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(page)
}
