package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
)

// streamListThreshold is the limit at which List streams its rows.
const streamListThreshold = 1000

// ServeStreamingList writes the list envelope List answers, encoding the
// rows one at a time instead of buffering them. It applies List's scope,
// query and offset checks, and refuses an entity with AfterList hooks.
// An explicit ?offset= overrides the offset page implies.
func (ch *CrudHandler) ServeStreamingList(ctx context.Context, w http.ResponseWriter, r *http.Request, cols []string, filters []filter.ParsedFilter, nested []nestedFilter, sorts []filter.ParsedSort, page, limit int, extraWhere []hook.WhereClause) {
	w.Header().Set("Cache-Control", "no-store")
	if !ch.requireScope(w, r, opRead) {
		return
	}
	if ch.Hooks != nil && len(ch.Hooks.HooksFor(hook.AfterList)) > 0 {
		writeJSONError(w, r, http.StatusBadRequest, reasonStreamAfterList)
		return
	}
	q, err := ch.readQuery(r)
	if err != nil {
		writeErrJSON(w, r, http.StatusBadRequest, err, reasonInvalidQuery)
		return
	}
	ch.serveStreamingList(ctx, w, r, q, cols, filters, nested, sorts, page, limit, extraWhere)
}

func (ch *CrudHandler) serveStreamingList(ctx context.Context, w http.ResponseWriter, r *http.Request, q url.Values, cols []string, filters []filter.ParsedFilter, nested []nestedFilter, sorts []filter.ParsedSort, page, limit int, extraWhere []hook.WhereClause) {
	streamPage, streamPerPage := parsePaginationValues(q, ch.Entity.Config.Pagination.MaxListLimit)
	if !ch.requireBoundedOffset(w, r, q, streamPage, streamPerPage) {
		return
	}
	countQb := query.Count(ch.Entity.GetTable())
	filter.ApplyToCountQuery(countQb, filters)
	ch.ApplyTenantScopeCount(countQb, r)
	ch.ApplyOwnerScopeCount(countQb, r)
	ch.ApplyReadScopeCount(countQb, r)
	ch.applySoftDeleteFilterCountQ(countQb, q, r.Context())
	applyNestedFilters(
		func(sql string, args ...any) { countQb.Where(sql, args...) },
		ch.Entity.GetTable(), ch.keyCols(), nested,
	)
	for _, c := range extraWhere {
		countQb.Where(c.SQL, c.Args...)
	}
	countSQL, countArgs := countQb.Build()
	var total int
	if err := ch.conn(ctx).QueryRowContext(ctx, countSQL, countArgs...).Scan(&total); err != nil {
		logf("crud: stream count failed: %v", err)
		writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
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
		ch.Entity.GetTable(), ch.keyCols(), nested,
	)
	for _, c := range extraWhere {
		qb.Where(c.SQL, c.Args...)
	}
	filter.ApplySortToQuery(qb, ch.withKeyTiebreak(sorts))
	qb.Limit(limit)
	if o, ok := explicitOffsetValues(q); ok {
		if o > 0 {
			qb.Offset(o)
		}
		if limit > 0 {
			page = o/limit + 1
		}
	} else if offset := pagination.OffsetForPage(page, limit); offset > 0 {
		qb.Offset(offset)
	}

	dataSQL, dataArgs := qb.Build()
	rows, err := ch.conn(ctx).QueryContext(ctx, dataSQL, dataArgs...)
	if err != nil {
		logf("crud: stream query failed: %v", err)
		writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	defer rows.Close()

	w.Header().Set("Content-Type", "application/json")
	flusher, _ := w.(http.Flusher)

	enc := json.NewEncoder(w)
	if _, err := fmt.Fprintf(w, `{"data":[`); err != nil {
		return
	}

	first := true
	boolCols := databaseBoolColumnsForEntity(rows, len(cols), ch.snapshotFields(), cols)
	for rows.Next() {
		row, err := scanRowsOne(rows, cols, ch.convertKey, boolCols)
		if err != nil {
			logf("crud: stream scan failed: %v", err)
			return
		}
		ch.decodeReadFields(row)
		ch.maskOwn(ctx, row)
		if !first {
			if _, err := w.Write([]byte(",")); err != nil {
				return
			}
		}
		if err := enc.Encode(row); err != nil {
			return
		}
		first = false
		if flusher != nil {
			flusher.Flush()
		}
	}
	if err := rows.Err(); err != nil {
		logf("crud: stream rows failed: %v", err)
		return
	}
	totalPages := 0
	if limit > 0 {
		totalPages = total / limit
		if total%limit != 0 {
			totalPages++
		}
	}
	fmt.Fprintf(w, `],"total":%d,"page":%d,"perPage":%d,"totalPages":%d}`, total, page, limit, totalPages)
}

// scanRowsOne scans the current row of rows into a map keyed by keyFunc.
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
