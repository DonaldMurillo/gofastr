package entityui

import (
	"context"
	"encoding/csv"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
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
			writeBulkError(w, http.StatusNotFound, "not found")
			return
		}
		if m.tr != nil {
			ctx = i18nui.WithTranslator(ctx, m.tr)
		}
		if !canRead(ctx, m.ch) {
			writeBulkError(w, http.StatusForbidden, "access denied")
			return
		}
		q := r.URL.Query()
		key := q.Get(exportKeyParam)
		q.Del(exportKeyParam)
		cols := exportColumns(m)
		fields := append([]string{m.pk}, cols...)
		var rows []map[string]any
		if named := q[exportIDParam]; len(named) > 0 {
			rows, err = u.namedRows(ctx, m, named, fields)
		} else {
			rows, err = u.matchRows(crud.WithReadHooks(ctx), m, key, q, fields)
		}
		if err != nil {
			if ref, ok := errors.AsType[*bulkRefusal](err); ok {
				writeBulkError(w, ref.status, ref.msg)
				return
			}
			slog.ErrorContext(ctx, "entityui: export read", "entity", m.name, "error", err)
			writeBulkError(w, http.StatusInternalServerError, i18nui.T(ctx, i18nui.KeyEntityBulkFailed))
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", `attachment; filename="`+exportFilename(m.e.GetTable())+`.csv"`)
		cw := csv.NewWriter(w)
		header := make([]string, len(fields))
		header[0] = m.pk
		for i, f := range cols {
			header[i+1] = csvSafe(m.label(ctx, f))
		}
		if err := cw.Write(header); err != nil {
			slog.Debug("entityui: export write", "error", err)
			return
		}
		rec := make([]string, len(fields))
		for _, row := range rows {
			for i, f := range fields {
				rec[i] = csvSafe(cell(rowValue(row, f)))
			}
			if err := cw.Write(rec); err != nil {
				slog.Debug("entityui: export write", "error", err)
				return
			}
		}
		cw.Flush()
	})
}

// exportColumns are the fields an export carries after the primary key:
// every field the API returns that is queryable, not omitted, not JSON.
func exportColumns(m *meta) []string {
	var out []string
	for _, f := range m.fields {
		if f.Name == m.pk || f.NoQuery || m.omitted(f.Name) || f.Type == schema.JSON {
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
// drops out. More than InRequestCap names, repeats counted, is refused.
func (u *UI) namedRows(ctx context.Context, m *meta, named, fields []string) ([]map[string]any, error) {
	if len(named) > InRequestCap {
		return nil, refuse(http.StatusUnprocessableEntity, i18nui.TVars(ctx, i18nui.KeyEntityBulkOverCap, map[string]string{"cap": strconv.Itoa(InRequestCap)}))
	}
	ids := dedupeIDs(named)
	return m.ch.ListAll(crud.WithReadHooks(ctx), crud.ListOptions{
		Where:  &filter.Predicate{Field: m.pk, Op: filter.OpIn, Values: ids},
		Fields: fields,
		Sorts:  []filter.ParsedSort{{Field: m.pk}},
		Limit:  len(ids),
	})
}
