package crud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/query"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/hook"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
)

// cursorFields returns the ordered list of columns the handler keysets on:
// EntityConfig.CursorFields, else CursorField, then each primary-key
// column neither names, so paging never skips rows tied on the sort
// value; the primary key alone when neither is set. entity.Define checks
// the same list.
func (ch *CrudHandler) cursorFields() []string {
	var fields []string
	switch p := ch.Entity.Config.Pagination; {
	case len(p.CursorFields) > 0:
		fields = append(fields, p.CursorFields...)
	case p.CursorField != "":
		fields = append(fields, p.CursorField)
	}
	for _, k := range ch.keyCols() {
		if !slices.Contains(fields, k) {
			fields = append(fields, k)
		}
	}
	return fields
}

// serveCursorList handles a cursor-paginated List request. It uses keyset
// pagination on the entity's cursor field(s) and emits a CursorPage envelope.
// The total count is intentionally omitted, cursor pagination's appeal is
// avoiding count's table scan.
//
// One column (the key):  WHERE id > $1 ORDER BY id.
// Several (a sort field and the key tiebreak, or a composite):
//
//	WHERE (f1, f2, …) > ($1, $2, …) ORDER BY f1, f2, …
//
// `?sort=` is ignored in cursor mode: keyset pagination requires a strictly
// ordered, unique-enough key, so the cursor field(s) control ORDER BY.
// cols is the ?fields= projection. extraWhere is every clause the page
// applies (search, ?where= and BeforeList); hookWhere is the BeforeList
// part alone, the scope the anchor lookup keeps.
func (ch *CrudHandler) serveCursorList(ctx context.Context, w http.ResponseWriter, r *http.Request, cols []string, includes []*IncludeNode, filters []filter.ParsedFilter, nested []nestedFilter, extraWhere, hookWhere []hook.WhereClause) {
	// Same no-store posture as List: the cursor page is the same per-caller
	// rows through the keyset arm, and this handler is also reachable via
	// decodeCursorAny paths that bypass List's stamp.
	w.Header().Set("Cache-Control", "no-store")
	cursor, limit, direction := pagination.ParseCursorPagination(r)
	if direction != "forward" && direction != "backward" {
		writeJSONError(w, r, http.StatusBadRequest, reasonBadDirection)
		return
	}
	// ParseCursorPagination clamps only to the global MaxPageSize; the
	// per-entity cap must hold on this path too or ?cursor= becomes a
	// MaxListLimit bypass.
	if limitCap := listLimitCap(ch.Entity.Config.Pagination.MaxListLimit); limit > limitCap {
		limit = limitCap
	}

	fields := ch.cursorFields()
	// The cursor carries the last row's sort values and key when they fit
	// the decoder's 16 KiB cap, and the key alone when they do not: a sort
	// value can be any length (a Text column), and a cursor past the cap
	// stopped paging partway. A key-only cursor reads the sort values back
	// from that row (cursorAnchor). Carrying them when they fit keeps a
	// row edited between pages (an updated_at feed) from moving the
	// position past the rows after it.
	keys := ch.keyCols()
	// A cursor column the projection leaves out is still read, since the
	// next cursor is built from it, and comes off the rows after.
	var unprojected []string
	for _, k := range fields {
		if !slices.Contains(cols, k) {
			unprojected = append(unprojected, k)
		}
	}
	cols = append(slices.Clip(cols), unprojected...)
	qb := query.Select(cols...)
	qb.From(ch.Entity.GetTable())
	filter.ApplyToQuery(qb, filters)
	ch.ApplyTenantScope(qb, r)
	ch.ApplyOwnerScope(qb, r)
	ch.ApplyReadScope(qb, r)
	ch.ApplySoftDeleteFilter(qb, r)
	applyNestedFilters(
		func(sql string, args ...any) { qb.Where(sql, args...) },
		ch.Entity.GetTable(), ch.keyCols(), nested,
	)
	for _, c := range extraWhere {
		qb.Where(c.SQL, c.Args...)
	}

	// Decode cursor (if any) and apply tuple-comparison WHERE.
	isDate := func(f string) bool { return ch.queryField(f).Type == schema.Date }
	if cursor != "" {
		decoded, err := decodeCursorFor(cursor, fields, keys)
		if err != nil {
			// Wrap, not a frame: the decoder's phrased refusal keeps the
			// "invalid cursor:" prefix and still answers its own code.
			writeErrJSON(w, r, http.StatusBadRequest, i18n.Wrap(reasonInvalidCursor, err), i18n.Phrase{})
			return
		}
		// Build the keyset WHERE only. ORDER BY is owned by the single
		// loop below so it is emitted exactly once per field; calling
		// qb.Cursor here would append its OWN ORDER BY and duplicate the
		// single-field clause (ORDER BY f ASC, f ASC).
		op := ">"
		if direction == "backward" {
			op = "<"
		}
		// The cursor is client-borne: a tampered value of the wrong type
		// for its column (?cursor= naming id "abc" on an int key) is a
		// 400 here on both dialects. Bound raw, Postgres refused it as a
		// 500 and SQLite compared it as text and answered a 200.
		carried := make(map[string]any, len(fields))
		for _, f := range fields {
			raw, ok := decoded[f]
			if !ok {
				continue
			}
			v, err := filter.CoerceValue(f, ch.queryField(f).Type, raw)
			if err != nil {
				writeErrJSON(w, r, http.StatusBadRequest, i18n.Wrap(reasonInvalidCursor, err), i18n.Phrase{})
				return
			}
			carried[f] = v
		}
		args, err := ch.cursorAnchor(ctx, r, fields, keys, carried, hookWhere, isDate)
		if errors.Is(err, errCursorAnchorGone) {
			writeErrJSON(w, r, http.StatusBadRequest, i18n.Wrap(reasonInvalidCursor, i18n.NewError(reasonCursorAnchorGone, nil)), i18n.Phrase{})
			return
		}
		if err != nil {
			logf("crud: cursor anchor for %s: %v", ch.Entity.GetTable(), err)
			writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		if len(fields) == 1 {
			qb.Where(fmt.Sprintf("%s %s $1", fields[0], op), args[0])
		} else {
			cols := strings.Join(fields, ", ")
			placeholders := make([]string, len(fields))
			for i := range fields {
				placeholders[i] = fmt.Sprintf("$%d", i+1)
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
	rows, err := ch.conn(ctx).QueryContext(ctx, dataSQL, dataArgs...)
	if err != nil {
		// Redact: raw driver text must not reach the client. Mirrors the
		// non-cursor List and streaming paths.
		logf("crud: cursor query failed: %v", err)
		writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	defer rows.Close()

	results, err := ch.scanMany(rows, cols)
	if err != nil {
		logf("crud: cursor scan failed: %v", err)
		writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}

	// Compute next cursor from the last row using all cursor field columns.
	// buildCursorPage runs against the raw fetch order (DESC for backward),
	// so it trims the extra sentinel row and derives the continue-cursor from
	// the true last row of the keyset window. Only AFTER that do we flip a
	// backward page back into ascending order so a "prev" page reads the same
	// way a "next" page does.
	page, err := buildCursorPage(results, fields, keys, ch.convertKey, isDate, limit)
	if errors.Is(err, errCursorKeyTooLong) {
		logf("crud: cursor page for %s: %v", ch.Entity.GetTable(), err)
		writeJSONError(w, r, http.StatusInternalServerError, reasonCursorKeyTooLong)
		return
	}
	if err != nil {
		logf("crud: cursor page for %s: %v", ch.Entity.GetTable(), err)
		writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
		return
	}
	for _, f := range unprojected {
		k := ch.convertKey(f)
		for _, row := range page.Data {
			delete(row, k)
		}
	}

	// Includes run on the page after the cursor is taken: including a
	// relation that is itself a cursor column (a composite key's
	// team_id) replaces its stored value with the related row, and a
	// cursor encoded from that object pages on garbage.
	if err := ch.applyIncludeTree(withRealRequest(WithReadHooks(ctx), r), page.Data, includes); err != nil {
		writeIncludeError(w, r, "cursor", err)
		return
	}
	if direction == "backward" {
		for i, j := 0, len(page.Data)-1; i < j; i, j = i+1, j-1 {
			page.Data[i], page.Data[j] = page.Data[j], page.Data[i]
		}
	}

	// AfterList, on the same terms as the offset path. Without this, adding
	// ?cursor= to any request skipped a redaction hook entirely and wrote the
	// stored value straight to the wire, direct disclosure, not an oracle.
	// It runs AFTER buildCursorPage on purpose: the continue-cursor has to be
	// derived from the stored keyset values, so a hook that masks the keyset
	// column must not be allowed to corrupt paging.
	if ch.Hooks != nil {
		// Carry extraWhere through so an AfterList hook sees the same payload
		// shape on both paths. The offset path hands over the clauses
		// BeforeList accumulated; constructing a bare payload here made a
		// hook that inspects Where (audit logging the applied scope, say)
		// behave differently the moment a client appended ?cursor=.
		payload := &hook.ListPayload{Request: r, Where: extraWhere, Results: page.Data}
		if err := runHooks(ch.Hooks, ctx, hook.AfterList, payload); err != nil {
			logf("crud: after-list hook failed (cursor): %v", err)
			writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		page.Data = payload.Results
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(page)
}

// decodeCursorFor decodes a cursor in either shape buildCursorPage
// mints: every cursor field (the sort values and the key), or the key
// columns alone. The two never collide: a key-only cursor names fewer
// columns. A cursor that fits neither answers the full shape's refusal.
func decodeCursorFor(cursor string, fields, keys []string) (map[string]string, error) {
	vals, err := decodeCursorAny(cursor, fields)
	if err == nil || len(fields) == len(keys) {
		return vals, err
	}
	if kv, kerr := decodeCursorAny(cursor, keys); kerr == nil {
		return kv, nil
	}
	return nil, err
}

// decodeCursorAny accepts either the single-field cursor or the
// multi-field encoding and returns a map keyed by the DB column name.
// For the single-field form the value lands under the first entry in
// `fields`. Both encodings are first-class, single-field is the
// compact shape for entities cursored by one column; multi-field is
// for composite cursors.
//
// The decoded field names MUST exact-match the expected `fields` set
// (same length, same names, no duplicates). A cursor with mis-cased,
// whitespace-padded, or punctuation-altered names would otherwise feed
// arbitrary column tokens into the keyset WHERE clause, either a
// query error at best, or silent state confusion across cursor revisions.
func decodeCursorAny(cursor string, fields []string) (map[string]string, error) {
	out := map[string]string{}

	// Try multi-cursor first; if it has fields, prefer it.
	if mf, err := pagination.DecodeMultiCursor(cursor); err == nil && len(mf) > 0 {
		if len(fields) <= 1 {
			// Single-field consumer must use the single-field encoding;
			// accepting a composite blob here would let a caller widen
			// the WHERE clause beyond the declared key.
			return nil, i18n.NewError(reasonCursorComposite, map[string]any{"count": len(mf)})
		}
		if len(mf) != len(fields) {
			return nil, i18n.NewError(reasonCursorFieldCount, map[string]any{"count": len(mf), "expected": len(fields)})
		}
		want := make(map[string]struct{}, len(fields))
		for _, f := range fields {
			want[f] = struct{}{}
		}
		for _, kv := range mf {
			if _, ok := want[kv.Name]; !ok {
				return nil, i18n.NewError(reasonCursorUnknownField, map[string]any{"field": strconv.Quote(kv.Name)})
			}
			if _, dup := out[kv.Name]; dup {
				return nil, i18n.NewError(reasonCursorDuplicateField, map[string]any{"field": strconv.Quote(kv.Name)})
			}
			out[kv.Name] = kv.Value
		}
		return out, nil
	}
	// Fall back to single-field cursor. The decoded name must
	// exact-match the expected keyset column: the name selects which
	// column the value keysets on, so a foreign name binds a foreign
	// value under the expected column.
	if name, val, err := pagination.DecodeCursor(cursor); err == nil && len(fields) > 0 {
		if len(fields) > 1 {
			return nil, i18n.NewError(reasonCursorNotComposite, map[string]any{"expected": len(fields)})
		}
		if name != fields[0] {
			return nil, i18n.NewError(reasonCursorFieldMismatch, map[string]any{"field": strconv.Quote(name), "expected": strconv.Quote(fields[0])})
		}
		out[fields[0]] = val
		return out, nil
	}
	return nil, i18n.NewError(reasonCursorFormat)
}

// errCursorAnchorGone is cursorAnchor's answer when the row a cursor
// continues from is gone or out of the caller's reach.
var errCursorAnchorGone = errors.New("cursor anchor row not found")

// cursorAnchor returns the keyset values the next page starts after, one
// per cursor field. carried holds the values the cursor itself held,
// already typed: when it holds every field (the full shape, or an entity
// cursored on its key) no lookup runs. Otherwise the cursor held the key
// alone and the anchor row's values are read back by that key.
//
// The lookup applies the same tenant, owner, read-scope, soft-delete and
// BeforeList predicates as the page, so a cursor naming a row the caller
// cannot read is gone, the same answer as a deleted row, and never tells
// where that row sorts. hookWhere holds the BeforeList clauses alone: the
// client's ?q= and ?where= decide which rows the page shows, not whether
// a readable row still marks a position, so an anchor edited out of the
// search keeps paging the way a plain ?field= filter always did. A gone anchor is errCursorAnchorGone: the caller
// restarts from the first page rather than reading a page from a position
// nobody can name.
func (ch *CrudHandler) cursorAnchor(ctx context.Context, r *http.Request, fields, keys []string, carried map[string]any, hookWhere []hook.WhereClause, isDate func(string) bool) ([]any, error) {
	args := make([]any, len(fields))
	lookup := false
	for i, f := range fields {
		if v, ok := carried[f]; ok {
			args[i] = v
		} else {
			lookup = true
		}
	}
	if !lookup {
		return args, nil
	}
	qb := query.Select(fields...)
	qb.From(ch.Entity.GetTable())
	for _, k := range keys {
		qb.Where(k+" = $1", carried[k])
	}
	ch.ApplyTenantScope(qb, r)
	ch.ApplyOwnerScope(qb, r)
	ch.ApplyReadScope(qb, r)
	ch.ApplySoftDeleteFilter(qb, r)
	for _, c := range hookWhere {
		qb.Where(c.SQL, c.Args...)
	}
	qb.Limit(1)
	sqlStr, sqlArgs := qb.Build()
	rows, err := ch.conn(ctx).QueryContext(ctx, sqlStr, sqlArgs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	found, err := ch.scanMany(rows, fields)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, errCursorAnchorGone
	}
	vals, err := keysetValues(found[0], fields, ch.convertKey, isDate)
	if err != nil {
		return nil, err
	}
	// Bound the way a value the cursor carried was: as its text, checked
	// and typed for its column, so a Timestamp compares in the stored
	// layout on SQLite.
	for i, f := range fields {
		v, err := filter.CoerceValue(f, ch.queryField(f).Type, pagination.KeysetString(vals[f]))
		if err != nil {
			return nil, fmt.Errorf("anchor value of %q: %w", f, err)
		}
		args[i] = v
	}
	return args, nil
}

// keysetValues reads fields from row (keyed by convertKey) for a cursor.
//
// A NULL keyset value is an error, never a cursor: no tuple comparison
// against NULL is true, so the next page would come back empty (or,
// spelled "null", compare as text on SQLite). Define refuses a nullable
// cursor column, so reaching this means the stored data broke the
// column's NOT NULL contract.
//
// isDate names the schema.Date fields. Both drivers read DATE as a
// time.Time, which would carry a full timestamp that the Date check
// refuses; the value is the date instead.
func keysetValues(row map[string]any, fields []string, convertKey func(string) string, isDate func(string) bool) (map[string]any, error) {
	out := map[string]any{}
	for _, f := range fields {
		v, ok := row[convertKey(f)]
		if !ok {
			continue
		}
		if v == nil {
			return nil, fmt.Errorf("cursor field %q is NULL in the last row", f)
		}
		if t, isTime := v.(time.Time); isTime && isDate != nil && isDate(f) {
			v = t.Format(time.DateOnly)
		}
		out[f] = v
	}
	return out, nil
}

// buildCursorPage assembles the CursorPage envelope. The cursor carries
// the last row's cursor fields when that token fits the decoder's cap,
// else its key columns alone (decodeCursorFor reads both). One column
// uses the compact EncodeCursor shape, several the multi-field encoding.
func buildCursorPage(data []map[string]any, fields, keys []string, convertKey func(string) string, isDate func(string) bool, limit int) (pagination.CursorPage, error) {
	hasMore := len(data) > limit
	if hasMore {
		data = data[:limit]
	}
	page := pagination.CursorPage{Data: data, HasMore: hasMore}
	if !hasMore || len(data) == 0 {
		return page, nil
	}
	dbRow, err := keysetValues(data[len(data)-1], fields, convertKey, isDate)
	if err != nil {
		return page, err
	}
	encode := func(cols []string) string {
		if len(cols) == 1 {
			if val, ok := dbRow[cols[0]]; ok {
				return pagination.EncodeCursor(cols[0], val)
			}
			return ""
		}
		return pagination.EncodeMultiCursor(cols, dbRow)
	}
	page.Cursor = encode(fields)
	if !pagination.CursorFits(page.Cursor) && len(keys) < len(fields) {
		page.Cursor = encode(keys)
	}
	// A key past the decoder's cap would mint a token the next request
	// refuses as too long; refuse it here, where the reason can say so.
	if !pagination.CursorFits(page.Cursor) {
		return page, fmt.Errorf("%w: %d bytes", errCursorKeyTooLong, len(page.Cursor))
	}
	return page, nil
}

// errCursorKeyTooLong is buildCursorPage's refusal of a key whose cursor
// would not fit the decoder's size cap.
var errCursorKeyTooLong = errors.New("the last row's key is too long for a cursor")
