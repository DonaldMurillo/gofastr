package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
)

// cursorFields returns the ordered list of columns the handler keysets on.
// Defaults to the entity primary key; EntityConfig.CursorField overrides to
// a single named column; EntityConfig.CursorFields overrides to a composite
// (multi-field) cursor with tuple comparison.
//
// Composite cursors should end in a guaranteed-unique tiebreak column
// (typically the primary key) so paging never stalls on ties — the
// framework appends PrimaryKey automatically if it isn't already listed.
func (ch *CrudHandler) cursorFields() []string {
	if len(ch.Entity.Config.CursorFields) > 0 {
		fields := append([]string{}, ch.Entity.Config.CursorFields...)
		hasPK := false
		for _, f := range fields {
			if f == ch.PrimaryKey {
				hasPK = true
				break
			}
		}
		if !hasPK {
			fields = append(fields, ch.PrimaryKey)
		}
		return fields
	}
	if ch.Entity.Config.CursorField != "" {
		return []string{ch.Entity.Config.CursorField}
	}
	return []string{ch.PrimaryKey}
}

// serveCursorList handles a cursor-paginated List request. It uses keyset
// pagination on the entity's cursor field(s) and emits a CursorPage envelope.
// The total count is intentionally omitted — cursor pagination's appeal is
// avoiding count's table scan.
//
// Single-field cursor: WHERE field > $1 ORDER BY field.
// Composite cursor:    WHERE (f1, f2, …) > ($1, $2, …) ORDER BY f1, f2, …
//
// `?sort=` is ignored in cursor mode: keyset pagination requires a strictly
// ordered, unique-enough key, so the cursor field(s) control ORDER BY.
func (ch *CrudHandler) serveCursorList(ctx context.Context, w http.ResponseWriter, r *http.Request, includes []*IncludeNode, filters []filter.ParsedFilter, nested []nestedFilter, extraWhere []hook.WhereClause) {
	cursor, limit, direction := pagination.ParseCursorPagination(r)
	if direction != "forward" && direction != "backward" {
		writeJSONError(w, http.StatusBadRequest, "direction must be 'forward' or 'backward'")
		return
	}

	fields := ch.cursorFields()
	cols := ch.visibleFields()
	qb := query.Select(cols...)
	qb.From(ch.Entity.GetTable())
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

	// Decode cursor (if any) and apply tuple-comparison WHERE.
	if cursor != "" {
		decoded, err := decodeCursorAny(cursor, fields)
		if err != nil {
			writeJSONError(w, http.StatusBadRequest, "invalid cursor: "+err.Error())
			return
		}
		if len(fields) == 1 {
			qb.Cursor(fields[0], decoded[fields[0]], direction)
		} else {
			op := ">"
			if direction == "backward" {
				op = "<"
			}
			cols := strings.Join(fields, ", ")
			placeholders := make([]string, len(fields))
			args := make([]any, len(fields))
			for i, f := range fields {
				placeholders[i] = fmt.Sprintf("$%d", i+1)
				args[i] = decoded[f]
			}
			qb.Where(fmt.Sprintf("(%s) %s (%s)", cols, op, strings.Join(placeholders, ", ")), args...)
		}
	}

	// ORDER BY each cursor field in declared order.
	for _, f := range fields {
		if direction == "backward" {
			qb.Order(f, "DESC")
		} else {
			qb.Order(f, "ASC")
		}
	}

	qb.Limit(limit + 1)

	dataSQL, dataArgs := qb.Build()
	rows, err := ch.DB.QueryContext(ctx, dataSQL, dataArgs...)
	if err != nil {
		// Redact: raw driver text must not reach the client. Mirrors the
		// non-cursor List and streaming paths.
		log.Printf("crud: cursor query failed: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	defer rows.Close()

	results, err := scanRows(rows, cols, ch.convertKey)
	if err != nil {
		log.Printf("crud: cursor scan failed: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := ch.applyIncludeTree(ctx, results, includes); err != nil {
		log.Printf("crud: cursor include failed: %v", err)
		writeJSONError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// Compute next cursor from the last row using all cursor field columns.
	page := buildCursorPage(results, fields, ch.convertKey, limit)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(page)
}

// decodeCursorAny accepts either the single-field cursor or the
// multi-field encoding and returns a map keyed by the DB column name.
// For the single-field form the value lands under the first entry in
// `fields`. Both encodings are first-class — single-field is the
// compact shape for entities cursored by one column; multi-field is
// for composite cursors.
//
// The decoded field names MUST exact-match the expected `fields` set
// (same length, same names, no duplicates). A cursor with mis-cased,
// whitespace-padded, or punctuation-altered names would otherwise feed
// arbitrary column tokens into the keyset WHERE clause — either a
// query error at best, or silent state confusion across cursor revisions.
func decodeCursorAny(cursor string, fields []string) (map[string]string, error) {
	out := map[string]string{}

	// Try multi-cursor first; if it has fields, prefer it.
	if mf, err := pagination.DecodeMultiCursor(cursor); err == nil && len(mf) > 0 {
		if len(fields) <= 1 {
			// Single-field consumer must use the single-field encoding;
			// accepting a composite blob here would let a caller widen
			// the WHERE clause beyond the declared key.
			return nil, fmt.Errorf("cursor shape mismatch: composite cursor with %d fields, expected single-field", len(mf))
		}
		if len(mf) != len(fields) {
			return nil, fmt.Errorf("cursor shape mismatch: %d fields decoded, expected %d", len(mf), len(fields))
		}
		want := make(map[string]struct{}, len(fields))
		for _, f := range fields {
			want[f] = struct{}{}
		}
		for _, kv := range mf {
			if _, ok := want[kv.Name]; !ok {
				return nil, fmt.Errorf("cursor field %q not in expected set", kv.Name)
			}
			if _, dup := out[kv.Name]; dup {
				return nil, fmt.Errorf("cursor field %q appears more than once", kv.Name)
			}
			out[kv.Name] = kv.Value
		}
		return out, nil
	}
	// Fall back to single-field cursor.
	if _, val, err := pagination.DecodeCursor(cursor); err == nil && len(fields) > 0 {
		if len(fields) > 1 {
			return nil, fmt.Errorf("cursor shape mismatch: single-field cursor, expected %d fields", len(fields))
		}
		out[fields[0]] = val
		return out, nil
	}
	return nil, fmt.Errorf("cursor format not recognised")
}

// buildCursorPage assembles the CursorPage envelope. Single-field
// cursors use the compact EncodeCursor shape; composite cursors emit
// the multi-field encoding.
func buildCursorPage(data []map[string]any, fields []string, convertKey func(string) string, limit int) pagination.CursorPage {
	hasMore := len(data) > limit
	if hasMore {
		data = data[:limit]
	}
	page := pagination.CursorPage{Data: data, HasMore: hasMore}
	if !hasMore || len(data) == 0 {
		return page
	}
	last := data[len(data)-1]
	if len(fields) == 1 {
		key := convertKey(fields[0])
		if val, ok := last[key]; ok {
			page.Cursor = pagination.EncodeCursor(fields[0], val)
		}
		return page
	}
	// Composite — build a map keyed by DB column name for EncodeMultiCursor.
	dbRow := map[string]any{}
	for _, f := range fields {
		if v, ok := last[convertKey(f)]; ok {
			dbRow[f] = v
		}
	}
	page.Cursor = pagination.EncodeMultiCursor(fields, dbRow)
	return page
}
