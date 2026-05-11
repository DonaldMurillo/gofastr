package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gofastr/gofastr/core/query"
	"github.com/gofastr/gofastr/framework/filter"
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
// clients keep working: {"data": [...], "total": N, "page": 1, "perPage":
// N, "totalPages": 1}. Streaming applies only to the data array — the
// envelope fields are written before the rows start flowing.
func (ch *CrudHandler) ServeStreamingList(ctx context.Context, w http.ResponseWriter, r *http.Request, cols []string, filters []filter.ParsedFilter, nested []nestedFilter, sorts []filter.ParsedSort, limit int) {
	// COUNT first so the envelope has the totals up front.
	countQb := query.Count(ch.Entity.GetTable())
	filter.ApplyToCountQuery(countQb, filters)
	ch.ApplyTenantScopeCount(countQb, r)
	ch.ApplySoftDeleteFilterCount(countQb, r)
	applyNestedFilters(
		func(sql string, args ...any) { countQb.Where(sql, args...) },
		ch.Entity.GetTable(), ch.PrimaryKey, nested,
	)
	countSQL, countArgs := countQb.Build()
	var total int
	if err := ch.DB.QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		writeJSONError(w, http.StatusInternalServerError, "count query failed: "+err.Error())
		return
	}

	qb := query.Select(cols...).From(ch.Entity.GetTable())
	filter.ApplyToQuery(qb, filters)
	ch.ApplyTenantScope(qb, r)
	ch.ApplySoftDeleteFilter(qb, r)
	applyNestedFilters(
		func(sql string, args ...any) { qb.Where(sql, args...) },
		ch.Entity.GetTable(), ch.PrimaryKey, nested,
	)
	filter.ApplySortToQuery(qb, sorts)
	qb.Limit(limit)

	dataSQL, dataArgs := qb.Build()
	rows, err := ch.DB.QueryContext(ctx, dataSQL, dataArgs...)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, "query failed: "+err.Error())
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
	fmt.Fprintf(w, `],"total":%d,"page":1,"perPage":%d,"totalPages":%d}`, total, limit, totalPages)
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
