package crud

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
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

// cursorFields returns the keyset columns: CursorFields, else CursorField,
// then every key column they do not name.
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

// serveCursorList serves a keyset page ordered by cursorFields. extraWhere
// is every clause the page applies; hookWhere is its BeforeList part.
func (ch *CrudHandler) serveCursorList(ctx context.Context, w http.ResponseWriter, r *http.Request, q url.Values, cols []string, includes []*IncludeNode, filters []filter.ParsedFilter, nested []nestedFilter, limit int, extraWhere, hookWhere []hook.WhereClause) {
	w.Header().Set("Cache-Control", "no-store")
	cursor, direction := q.Get("cursor"), q.Get("direction")
	if !directionParamValid(q["direction"]) {
		writeJSONError(w, r, http.StatusBadRequest, reasonBadDirection)
		return
	}
	fields := ch.cursorFields()
	keys := ch.keyCols()
	var unprojected []string
	for _, k := range fields {
		if !slices.Contains(cols, k) {
			unprojected = append(unprojected, k)
		}
	}
	cols = append(slices.Clip(cols), unprojected...)
	textKeys := ch.storedTimestampKeys(ctx, fields)
	cols = append(cols, storedTextColumns(fields, textKeys)...)
	qb := query.Select(cols...)
	qb.From(ch.Entity.GetTable())
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

	isDate := func(f string) bool { return ch.queryField(f).Type == schema.Date }
	if cursor != "" {
		decoded, err := decodeCursorFor(cursor, fields, keys)
		if err != nil {
			writeErrJSON(w, r, http.StatusBadRequest, i18n.Wrap(reasonInvalidCursor, err), i18n.Phrase{})
			return
		}
		op := ">"
		if direction == "backward" {
			op = "<"
		}
		carried := make(map[string]any, len(fields))
		for _, f := range fields {
			raw, ok := decoded[f]
			if !ok {
				continue
			}
			v, err := filter.CoerceValue(f, ch.queryField(f).Type, raw)
			if _, stored := textKeys[f]; stored {
				v, err = storedTimestampText(f, raw)
			}
			if err != nil {
				writeErrJSON(w, r, http.StatusBadRequest, i18n.Wrap(reasonInvalidCursor, err), i18n.Phrase{})
				return
			}
			carried[f] = v
		}
		args, err := ch.cursorAnchor(ctx, r, q, fields, keys, carried, hookWhere, isDate, textKeys)
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
			qb.Where(fmt.Sprintf("%s %s $1", query.QuoteKeyword(fields[0]), op), args[0])
		} else {
			cols := strings.Join(query.QuoteKeywords(fields), ", ")
			placeholders := make([]string, len(fields))
			for i := range fields {
				placeholders[i] = fmt.Sprintf("$%d", i+1)
			}
			qb.Where(fmt.Sprintf("(%s) %s (%s)", cols, op, strings.Join(placeholders, ", ")), args...)
		}
	}

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

	page, err := buildCursorPage(results, fields, keys, ch.convertKey, isDate, textKeys, limit)
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
	for _, textKey := range textKeys {
		for _, row := range page.Data {
			delete(row, textKey)
		}
	}

	if err := ch.applyIncludeTree(withRealRequest(WithReadHooks(ctx), r), page.Data, includes); err != nil {
		writeIncludeError(w, r, "cursor", err)
		return
	}
	if direction == "backward" {
		for i, j := 0, len(page.Data)-1; i < j; i, j = i+1, j-1 {
			page.Data[i], page.Data[j] = page.Data[j], page.Data[i]
		}
	}

	ch.maskOwn(ctx, page.Data...)
	if ch.Hooks != nil {
		payload := &hook.ListPayload{Request: r, Where: extraWhere, Results: page.Data}
		if err := ch.applyAfterList(ctx, payload); err != nil {
			logf("crud: after-list hook failed (cursor): %v", err)
			writeJSONError(w, r, http.StatusInternalServerError, i18n.StatusPhrase(http.StatusInternalServerError))
			return
		}
		page.Data = payload.Results
	}

	writeJSONStatus(w, r, http.StatusOK, page)
}

// decodeCursorFor decodes a cursor naming every cursor field or the key
// columns alone; one that fits neither answers the full shape's refusal.
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

// decodeCursorAny decodes the single-field encoding when fields has one
// entry and the multi-field one otherwise, keyed by column name.
func decodeCursorAny(cursor string, fields []string) (map[string]string, error) {
	out := map[string]string{}

	if mf, err := pagination.DecodeMultiCursor(cursor); err == nil && len(mf) > 0 {
		if len(fields) <= 1 {
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

var errCursorAnchorGone = errors.New("cursor anchor row not found")

// cursorAnchor returns the keyset values the next page starts after: the
// carried ones, or for a key-only cursor the anchor row's, read under the
// page's scopes and hookWhere.
func (ch *CrudHandler) cursorAnchor(ctx context.Context, r *http.Request, q url.Values, fields, keys []string, carried map[string]any, hookWhere []hook.WhereClause, isDate func(string) bool, textKeys map[string]string) ([]any, error) {
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
	selected := append(slices.Clip(fields), storedTextColumns(fields, textKeys)...)
	qb := query.Select(selected...)
	qb.From(ch.Entity.GetTable())
	for _, k := range keys {
		qb.Where(query.QuoteKeyword(k)+" = $1", carried[k])
	}
	ch.ApplyTenantScope(qb, r)
	ch.ApplyOwnerScope(qb, r)
	ch.ApplyReadScope(qb, r)
	ch.applySoftDeleteFilterQ(qb, q, r.Context())
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
	found, err := ch.scanMany(rows, selected)
	if err != nil {
		return nil, err
	}
	if len(found) == 0 {
		return nil, errCursorAnchorGone
	}
	vals, err := keysetValues(found[0], fields, ch.convertKey, isDate, textKeys)
	if err != nil {
		return nil, err
	}
	for i, f := range fields {
		v, err := filter.CoerceValue(f, ch.queryField(f).Type, pagination.KeysetString(vals[f]))
		if _, stored := textKeys[f]; stored {
			v, err = storedTimestampText(f, pagination.KeysetString(vals[f]))
		}
		if err != nil {
			return nil, fmt.Errorf("anchor value of %q: %w", f, err)
		}
		args[i] = v
	}
	return args, nil
}

// keysetValues reads fields from row for a cursor; a NULL is an error.
func keysetValues(row map[string]any, fields []string, convertKey func(string) string, isDate func(string) bool, textKeys map[string]string) (map[string]any, error) {
	out := map[string]any{}
	for _, f := range fields {
		key := convertKey(f)
		if textKey, stored := textKeys[f]; stored {
			key = textKey
		}
		v, ok := row[key]
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

// buildCursorPage assembles the CursorPage envelope, its cursor naming
// every cursor field when that fits the decoder's cap, else the key alone.
func buildCursorPage(data []map[string]any, fields, keys []string, convertKey func(string) string, isDate func(string) bool, textKeys map[string]string, limit int) (pagination.CursorPage, error) {
	hasMore := len(data) > limit
	if hasMore {
		data = data[:limit]
	}
	page := pagination.CursorPage{Data: data, HasMore: hasMore}
	if !hasMore || len(data) == 0 {
		return page, nil
	}
	dbRow, err := keysetValues(data[len(data)-1], fields, convertKey, isDate, textKeys)
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
	if !pagination.CursorFits(page.Cursor) {
		return page, fmt.Errorf("%w: %d bytes", errCursorKeyTooLong, len(page.Cursor))
	}
	return page, nil
}

var errCursorKeyTooLong = errors.New("the last row's key is too long for a cursor")

func directionParamValid(values []string) bool {
	for _, v := range values {
		if v != "" && v != "forward" && v != "backward" {
			return false
		}
	}
	return true
}
