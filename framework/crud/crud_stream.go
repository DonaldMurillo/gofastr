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
	"github.com/DonaldMurillo/gofastr/framework/pagination"
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
// N, "totalPages": T}. Streaming applies only to the data array, the
// envelope fields are written before the rows start flowing.
//
// `page` is honoured the same way the non-stream List handler honours it:
// OFFSET (page-1)*limit. Without this, ?page=2&stream=true would re-stream
// page 1 while reporting page 1, silently dropping the offset. An explicit
// ?offset= overrides the page-derived offset, matching the buffered path.
func (ch *CrudHandler) ServeStreamingList(ctx context.Context, w http.ResponseWriter, r *http.Request, cols []string, filters []filter.ParsedFilter, nested []nestedFilter, sorts []filter.ParsedSort, page, limit int, extraWhere []hook.WhereClause) {
	// Same owner+tenant gate the public List handler enforces. Direct
	// callers (in-process or chained from List) must not bypass it,
	// without this the streaming variant would happily return every row to
	// an anonymous caller on an OwnerField entity, or every tenant's rows
	// on a MultiTenant entity with no tenant in context.
	if !ch.requireScope(w, r, opRead) {
		return
	}
	// Same AfterList refusal List() enforces before it delegates here.
	// Streaming never materialises the full slice AfterList runs over, so
	// the hook cannot run at all — and an AfterList registered as a
	// redactor would then be silently BYPASSED, putting the stored values
	// it exists to hide straight on the wire. List() already refuses this
	// combination, but a direct in-process caller reaches this method
	// without passing through it, exactly as it does for the owner/tenant
	// gate above.
	if ch.Hooks != nil && len(ch.Hooks.HooksFor(hook.AfterList)) > 0 {
		writeJSONError(w, http.StatusBadRequest, "streaming list does not support AfterList hooks; drop ?stream=true")
		return
	}
	// Parse the URL query once and thread it through the helpers, mirroring
	// the List() body. ServeStreamingList is called from List() (which has
	// already enforced requireScope) but is also a public entrypoint for
	// direct in-process callers, so it must do its own scoping AND its own
	// single-parse to keep the soft-delete gate and ?offset= read off the
	// same url.Values instead of re-parsing per call.
	q := r.URL.Query()
	// Offset bound, same as List(): the direct-call contract this method
	// maintains for the owner/tenant and AfterList gates holds for the
	// skip side too. When chained from List() the guard has already run
	// and passes again cheaply.
	if !ch.requireBoundedOffset(w, q) {
		return
	}
	// COUNT first so the envelope has the totals up front.
	countQb := query.Count(ch.Entity.GetTable())
	filter.ApplyToCountQuery(countQb, filters)
	ch.ApplyTenantScopeCount(countQb, r)
	ch.ApplyOwnerScopeCount(countQb, r)
	ch.ApplyReadScopeCount(countQb, r)
	// Soft-delete's ?trashed= gate authorizes against the REQUEST user, so it
	// must read r.Context(), not the DB-operation ctx (which callers may seed
	// with a different identity for in-process execution). The buffered List()
	// path uses r.Context() here; keep the stream path identical.
	ch.applySoftDeleteFilterCountQ(countQb, q, r.Context())
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
	ch.ApplyReadScope(qb, r)
	ch.applySoftDeleteFilterQ(qb, q, r.Context())
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
	// buffered List() path, otherwise ?offset=N&stream=true would silently
	// serve page 1 (the process-module broker paginates by raw offset).
	if o, ok := explicitOffsetValues(q); ok {
		if o > 0 {
			qb.Offset(o)
		}
	} else if offset := pagination.OffsetForPage(page, limit); offset > 0 {
		qb.Offset(offset)
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
	boolCols := databaseBoolColumnsForEntity(rows, len(cols), ch.Entity, cols)
	for rows.Next() {
		row, err := scanRowsOne(rows, cols, ch.convertKey, boolCols)
		if err != nil {
			// Mid-stream errors can't change status; we close the array
			// and let the client parse what we sent.
			break
		}
		ch.decodeJSONFields(row)
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
	// Guard the division: ServeStreamingList is an exported in-process
	// entrypoint taking an arbitrary limit, and a limit < 1 used to
	// panic here with "integer divide by zero" (OffsetForPage guards its
	// own division for the same reason). totalPages 0 is the coherent
	// companion of perPage 0 in the envelope.
	totalPages := 0
	if limit > 0 {
		totalPages = total / limit
		if total%limit != 0 {
			totalPages++
		}
	}
	fmt.Fprintf(w, `],"total":%d,"page":%d,"perPage":%d,"totalPages":%d}`, total, page, limit, totalPages)
}

// scanRowsOne pulls a single row from an *sql.Rows that's already been
// positioned (rows.Next returned true). Same column mapping the rest of
// the framework uses.
func scanRowsOne(rows interface {
	Scan(...any) error
}, cols []string, keyFunc func(string) string, boolColumns ...[]bool) (map[string]any, error) {
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return nil, err
	}
	row := make(map[string]any, len(cols))
	boolCols := []bool(nil)
	if len(boolColumns) > 0 {
		boolCols = boolColumns[0]
	}
	for i, c := range cols {
		isBool := i < len(boolCols) && boolCols[i]
		row[keyFunc(c)] = convertDatabaseValue(vals[i], isBool)
	}
	return row, nil
}
