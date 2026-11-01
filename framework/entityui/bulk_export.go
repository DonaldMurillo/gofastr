package entityui

import (
	"context"
	"encoding/csv"
	"errors"
	"log/slog"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
)

// exportKeyParam names the list key on an export link: the list query's
// own parameters are namespaced by it.
const exportKeyParam = "_list"

// exportIDParam names one row to export. Repeated, the export holds
// those rows (a bulk bar's Copy CSV of the checked rows) instead of the
// list query's, at most InRequestCap.
const exportIDParam = "_id"

// ExportHandler serves GET <api>/_export.csv for the entity: every row the
// list's query matches, as CSV, at most EveryMatchCap. The bulk bar links
// to it with the list's own query, so the file holds what the list
// narrowed to. Rows are read through the scoped CRUD handler with read
// hooks, the same values the list shows; NoQuery fields (masked columns
// among them), omitted and JSON fields are left out. A cell that a
// spreadsheet would run as a formula is prefixed with a quote. It
// answers 404 where BulkHandler does and 403 when the caller may not
// read the entity.
func (u *UI) ExportHandler(entityName string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		ctx := r.Context()
		m, err := u.meta(entityName)
		if err != nil || !bulkOn(m) {
			writeBulkError(w, r, http.StatusNotFound, i18n.StatusPhrase(http.StatusNotFound))
			return
		}
		if m.tr != nil {
			ctx = i18n.WithTranslator(ctx, m.tr)
		}
		if !canRead(ctx, m.ch) {
			writeBulkError(w, r, http.StatusForbidden, reasonAccessDenied)
			return
		}
		q := r.URL.Query()
		key := q.Get(exportKeyParam)
		q.Del(exportKeyParam)
		cols := exportColumns(m)
		fields := append(slices.Clone(m.keys), cols...)
		var rows []map[string]any
		if named := q[exportIDParam]; len(named) > 0 {
			rows, err = u.namedRows(ctx, m, named, fields)
		} else {
			rows, err = u.matchRows(crud.WithReadHooks(ctx), m, key, q, fields)
		}
		if err != nil {
			if ref, ok := errors.AsType[*bulkRefusal](err); ok {
				writeBulkError(w, r, ref.status, ref.reason, ref.params)
				return
			}
			slog.ErrorContext(ctx, "entityui: export read", "entity", m.name, "error", err)
			writeBulkError(w, r, http.StatusInternalServerError, reasonBulkFailed)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+exportFilename(m.e.GetTable())+`.csv"`)
		cw := csv.NewWriter(w)
		header := make([]string, len(fields))
		copy(header, m.keys)
		for i, f := range cols {
			header[len(m.keys)+i] = csvSafe(m.label(ctx, f))
		}
		if err := cw.Write(header); err != nil {
			slog.Debug("entityui: export write", "error", err)
			return
		}
		rec := make([]string, len(fields))
		for _, row := range rows {
			for i, f := range fields {
				rec[i] = csvCell(m, f, cell(m.rowValue(row, f)))
			}
			if err := cw.Write(rec); err != nil {
				slog.Debug("entityui: export write", "error", err)
				return
			}
		}
		cw.Flush()
	})
}

// exportColumns are the fields an export carries after the key columns:
// every field the API returns that is queryable, not omitted, not JSON.
func exportColumns(m *meta) []string {
	var out []string
	for _, f := range m.fields {
		if m.isKey(f.Name) || f.NoQuery || m.omitted(f.Name) || f.Type == schema.JSON {
			continue
		}
		out = append(out, f.Name)
	}
	return out
}

// csvSafe defuses a cell a spreadsheet would evaluate: one starting with
// a tab, carriage return or line feed, or whose first non-space rune is
// = + - @ or a full-width form of one, gets a leading quote.
func csvSafe(v string) string {
	if v != "" && strings.ContainsRune("\t\r\n", rune(v[0])) {
		return "'" + v
	}
	rest := strings.TrimLeftFunc(v, unicode.IsSpace)
	if r, _ := utf8.DecodeRuneInString(rest); rest != "" && strings.ContainsRune("=+-@\uFF1D\uFF0B\uFF0D\uFF20", r) {
		return "'" + v
	}
	return v
}

// csvCell is one exported cell. A number in an Int, Float or Decimal
// column is written as it is, so a spreadsheet can sum and sort "-12.5";
// every other cell, a numeric column's non-number among them, goes
// through csvSafe.
func csvCell(m *meta, name, v string) string {
	if f, ok := m.field(name); ok && (f.Type == schema.Int || f.Type == schema.Float || f.Type == schema.Decimal) && plainNumber(v) {
		return v
	}
	return csvSafe(v)
}

// plainNumber reports whether v is a decimal number and nothing else: an
// optional minus, digits with at most one point, an optional exponent.
func plainNumber(v string) bool {
	v = strings.TrimPrefix(v, "-")
	mant, exp, hasExp := strings.Cut(strings.ToLower(v), "e")
	whole, frac, _ := strings.Cut(mant, ".")
	if whole == "" && frac == "" || !allDigits(whole) || !allDigits(frac) {
		return false
	}
	if !hasExp {
		return true
	}
	if exp != "" && (exp[0] == '+' || exp[0] == '-') {
		exp = exp[1:]
	}
	return exp != "" && allDigits(exp)
}

func allDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// exportFilename keeps a table name to filename-safe bytes.
func exportFilename(table string) string {
	var b strings.Builder
	for _, r := range table {
		if r < 0x80 && (r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "export"
	}
	return b.String()
}

// namedRows reads the named rows' fields through the scoped CRUD handler
// with read hooks, in primary-key order: a name the caller may not read
// drops out, and so does one the Decider refuses by id, as GET
// <api>/<id> would. More than InRequestCap names, repeats counted, is
// refused.
func (u *UI) namedRows(ctx context.Context, m *meta, named, fields []string) ([]map[string]any, error) {
	if len(named) > InRequestCap {
		return nil, refuse(http.StatusUnprocessableEntity, reasonBulkOverCap, map[string]any{"cap": InRequestCap})
	}
	ids := dedupeIDs(named)
	rows, err := m.listByIDs(crud.WithReadHooks(ctx), ids, crud.ListOptions{
		Fields: fields,
		Sorts:  m.keySorts(),
	})
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(rows, func(row map[string]any) bool {
		return !canReadRecord(ctx, m.ch, m.rowID(row))
	}), nil
}
