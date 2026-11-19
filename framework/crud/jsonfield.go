package crud

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// bindJSONValue is a column value as the driver binds it: a Timestamp in
// schema.TimestampLayout, a Bool spelling as the boolean, a JSON value as
// its text. Other values pass through.
func (ch *CrudHandler) bindJSONValue(col string, v any) any {
	if v == nil {
		return v
	}
	snap := ch.snapshot()
	if _, ok := snap.timestampColumns[col]; ok {
		return schema.TimestampArg(v)
	}
	if _, ok := snap.boolColumns[col]; ok {
		if b, ok := boolSpelling(v); ok {
			return b
		}
		return v
	}
	if !ch.isJSONColumn(col) {
		return v
	}
	return marshalJSONColumn(v)
}

// marshalJSONColumn encodes a JSON value as text; a string is already the
// document and is stored verbatim.
func marshalJSONColumn(v any) any {
	switch v.(type) {
	case nil, string, []byte, json.RawMessage:
		return v
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	return string(raw)
}

func (ch *CrudHandler) ensureFieldCache() *fieldCacheData {
	return ch.snapshot()
}

func (ch *CrudHandler) isJSONColumn(col string) bool {
	_, ok := ch.snapshot().jsonColumns[col]
	return ok
}

// decodeReadFields decodes a wire-keyed row's JSON, Decimal and Date columns in place.
func (ch *CrudHandler) decodeReadFields(row map[string]any) {
	if row == nil {
		return
	}
	snap := ch.snapshot()
	decodeKeys(row, snap.jsonWireKeys, snap.decimalWireKeys, snap.dateWireKeys)
}

func decodeKeys(row map[string]any, jsonKeys, decimalKeys, dateKeys map[string]struct{}) {
	decodeEach(row, jsonKeys, decodeJSONColumn)
	decodeEach(row, decimalKeys, decimalText)
	decodeEach(row, dateKeys, dateText)
}

func decodeEach(row map[string]any, keys map[string]struct{}, decode func(any) any) {
	for key := range keys {
		if v, ok := row[key]; ok {
			row[key] = decode(v)
		}
	}
}

func (ch *CrudHandler) decodeReadRows(rows []map[string]any) {
	if len(rows) == 0 {
		return
	}
	if snap := ch.snapshot(); len(snap.jsonWireKeys) == 0 && len(snap.decimalWireKeys) == 0 && len(snap.dateWireKeys) == 0 {
		return
	}
	for _, row := range rows {
		ch.decodeReadFields(row)
	}
}

// scanOne and scanMany are the scanners every read path uses: they apply the
// entity's bool columns and decodeReadFields.
func (ch *CrudHandler) scanOne(row *sql.Row, cols []string) (map[string]any, error) {
	result, err := scanRowWithBoolColumns(row, cols, ch.convertKey, ch.boolColumns(cols))
	if err != nil {
		return nil, err
	}
	ch.decodeReadFields(result)
	return result, nil
}

func (ch *CrudHandler) scanMany(rows *sql.Rows, cols []string) ([]map[string]any, error) {
	var fields []schema.Field
	if ch != nil && ch.Entity != nil {
		fields = ch.snapshotFields()
	}
	results, err := scanRowsForEntity(rows, cols, ch.convertKey, fields)
	if err != nil {
		return nil, err
	}
	ch.decodeReadRows(results)
	return results, nil
}

// decodeEntityReadColumns is decodeReadFields for column-keyed rows of
// another entity (eager-loaded includes).
func decodeEntityReadColumns(ent *entity.Entity, rows []map[string]any) {
	if ent == nil || len(rows) == 0 {
		return
	}
	jsonCols, decimalCols, dateCols := map[string]struct{}{}, map[string]struct{}{}, map[string]struct{}{}
	for _, f := range ent.GetFields() {
		switch f.Type {
		case schema.JSON:
			jsonCols[f.Name] = struct{}{}
		case schema.Decimal:
			decimalCols[f.Name] = struct{}{}
		case schema.Date:
			dateCols[f.Name] = struct{}{}
		default:
		}
	}
	for _, row := range rows {
		decodeKeys(row, jsonCols, decimalCols, dateCols)
	}
}

// dateText is a driver's DATE as YYYY-MM-DD, in the value's own location.
func dateText(v any) any {
	switch t := v.(type) {
	case time.Time:
		return t.Format(time.DateOnly)
	case *time.Time:
		if t != nil {
			return t.Format(time.DateOnly)
		}
	}
	return v
}

// decimalText is a driver's DECIMAL as the wire's text.
func decimalText(v any) any {
	switch t := v.(type) {
	case int64:
		return strconv.FormatInt(t, 10)
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	case []byte:
		return string(t)
	}
	return v
}

// decodeJSONColumn parses stored JSON text; anything else returns as it is,
// and a document that is one JSON string returns as its JSON text.
func decodeJSONColumn(v any) any {
	var raw []byte
	switch t := v.(type) {
	case string:
		if t == "" {
			return v
		}
		raw = []byte(t)
	case []byte:
		if len(t) == 0 {
			return v
		}
		raw = t
	default:
		return v
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var out any
	if err := dec.Decode(&out); err != nil {
		return v
	}
	if _, err := dec.Token(); err != io.EOF {
		return v
	}
	if _, isString := out.(string); isString {
		return string(raw)
	}
	return storedJSONNumbers(out)
}

// storedJSONNumbers types a document's numbers as float64, except an integer
// past ±2^53 (int64) or past int64 (json.Number).
func storedJSONNumbers(v any) any {
	switch x := v.(type) {
	case map[string]any:
		for k, item := range x {
			x[k] = storedJSONNumbers(item)
		}
	case []any:
		for i, item := range x {
			x[i] = storedJSONNumbers(item)
		}
	case json.Number:
		if i, err := x.Int64(); err == nil {
			if i > 1<<53 || i < -(1<<53) {
				return i
			}
			return float64(i)
		}
		if strings.ContainsAny(x.String(), ".eE") {
			if f, err := x.Float64(); err == nil && !roundsInteger(f) {
				return f
			}
		}
		return x
	}
	return v
}
