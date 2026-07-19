package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
)

// streamListThreshold is the limit beyond which the List handler
// auto-switches to a streaming JSON encoder so the full result set never
// has to live in memory. Clients can also opt in explicitly via
// ?stream=true regardless of limit.
const streamListThreshold = 1000

// ServeStreamingList writes the list response row-by-row through a
// json.Encoder rather than buffering everything into a slice first. Used
// for very large pages (limit ≥ streamListThreshold) or when the caller
// asks for it via ?stream=true.
//
// The wire shape is identical to the regular list envelope so existing
// clients keep working: {"data": [...], "total": N, "page": P, "perPage":
// N, "totalPages": T}. Streaming applies only to the data array — the
// envelope fields are written before the rows start flowing.
//
// `page` is honoured the same way the non-stream List handler honours it:
// OFFSET (page-1)*limit. Without this, ?page=2&stream=true would re-stream
// page 1 while reporting page 1 — silently dropping the offset. An explicit
// ?offset= overrides the page-derived offset, matching the buffered path.
func (ch *CrudHandler) ServeStreamingList(ctx context.Context, w http.ResponseWriter, r *http.Request, cols []string, filters []filter.ParsedFilter, nested []nestedFilter, sorts []filter.ParsedSort, page, limit int, extraWhere []hook.WhereClause) {
	// Same owner+tenant gate the public List handler enforces. Direct
	// callers (in-process or chained from List) must not bypass it —
	// without this the streaming variant would happily return every row to
	// an anonymous caller on an OwnerField entity, or every tenant's rows
	// on a MultiTenant entity with no tenant in context.
	if !ch.requireScope(w, r, opRead) {
		return
	}
	// COUNT first so the envelope has the totals up front.
	countQb := query.Count(ch.Entity.GetTable())
	filter.ApplyToCountQuery(countQb, filters)
	ch.ApplyTenantScopeCount(countQb, r)
	ch.ApplyOwnerScopeCount(countQb, r)
	ch.ApplySoftDeleteFilterCount(countQb, r)
	applyNestedFilters(
		func(sql string, args ...any) { countQb.Where(sql, args...) },
		ch.Entity.GetTable(), ch.PrimaryKey, nested,
	)
	for _, c := range extraWhere {
		countQb.Where(c.SQL, c.Args...)
	}
	countSQL, countArgs := countQb.Build()
	var total int
	if err := ch.DB.QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		log.Printf("crud: stream count failed: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	qb := query.Select(cols...).From(ch.Entity.GetTable())
	filter.ApplyToQuery(qb, filters)
	ch.ApplyTenantScope(qb, r)
	ch.ApplyOwnerScope(qb, r)
	ch.ApplySoftDeleteFilter(qb, r)
	applyNestedFilters(
		func(sql string, args ...any) { qb.Where(sql, args...) },
		ch.Entity.GetTable(), ch.PrimaryKey, nested,
	)
	for _, c := range extraWhere {
		qb.Where(c.SQL, c.Args...)
	}
	filter.ApplySortToQuery(qb, sorts)
	qb.Limit(limit)
	// An explicit ?offset= overrides the page-derived offset, matching the
	// buffered List() path — otherwise ?offset=N&stream=true would silently
	// serve page 1 (the process-module broker paginates by raw offset).
	if o, ok := explicitOffset(r); ok {
		if o > 0 {
			qb.Offset(o)
		}
	} else if page > 1 {
		qb.Offset((page - 1) * limit)
	}

	dataSQL, dataArgs := qb.Build()
	rows, err := ch.DB.QueryContext(ctx, dataSQL, dataArgs...)
	if err != nil {
		log.Printf("crud: stream query failed: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "application/json")
	flusher, _ := w.(http.Flusher)

	// Manually frame the envelope so we can stream "data" between the
	// opening "[" and closing "]" without holding the slice.
	enc := json.NewEncoder(w)
	if _, err := fmt.Fprintf(w, `{"data":[`); err != nil {
		return
	}

	first := true
	for rows.Next() {
		row, err := scanRowsOne(rows, cols, ch.convertKey)
		if err != nil {
			// Mid-stream errors can't change status; we close the array
			// and let the client parse what we sent.
			break
		}
		if !first {
			if _, err := w.Write([]byte(",")); err != nil {
				return
			}
		}
		if err := enc.Encode(row); err != nil {
			return
		}
		first = false
		// Encoder.Encode writes a trailing newline; we don't strip it because
		// JSON parsers ignore whitespace between tokens, and flushing each
		// row keeps the response shape correct even if a client streams-parses.
		if flusher != nil {
			flusher.Flush()
		}
	}
	totalPages := total / limit
	if total%limit != 0 {
		totalPages++
	}
	fmt.Fprintf(w, `],"total":%d,"page":%d,"perPage":%d,"totalPages":%d}`, total, page, limit, totalPages)
}

// scanRowsOne pulls a single row from an *sql.Rows that's already been
// positioned (rows.Next returned true). Same column mapping the rest of
// the framework uses.
func scanRowsOne(rows interface {
	Scan(...any) error
}, cols []string, keyFunc func(string) string) (map[string]any, error) {
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	row := make(map[string]any, len(cols))
	for i, c := range cols {
		row[keyFunc(c)] = convertValue(vals[i])
	}
	return row, nil
}
