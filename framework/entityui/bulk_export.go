package entityui

import (
	"context"
	"encoding/csv"
	"log/slog"
	"maps"
	"net/http"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/internal/fieldtext"
)

const exportKeyParam = "_list"

const exportIDParam = "_id"

// ExportHandler serves GET <api>/_export.csv: every row the list's query
// matches (at most EveryMatchCap), or the rows named by _id, as CSV read
// through the read hooks, with formula cells defused. A param that is not
// the list's own answers 400; a caller who may not read the entity gets
// the list route's refusal.
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
		if !requireRead(w, r, ctx, m) {
			return
		}
		q, err := filter.ParseQueryStrict(r.URL.RawQuery)
		if err != nil {
			text, code := i18n.ErrorText(r.Context(), err, reasonExportBadQuery)
			writeBulkJSON(w, http.StatusBadRequest, map[string]string{"error": text, "reason": code})
			return
		}
		key := q.Get(exportKeyParam)
		q.Del(exportKeyParam)
		own := &listState{m: m, key: key, p: listParamsFor(key)}
		for _, name := range slices.Sorted(maps.Keys(q)) {
			if name == own.p.q && strings.TrimSpace(q.Get(name)) == "" {
				continue
			}
			if name != exportIDParam && !own.exportParam(name) {
				writeBulkError(w, r, http.StatusBadRequest, reasonExportUnknownParam, map[string]any{"name": name})
				return
			}
		}
		cols := exportColumns(m)
		fields := append(slices.Clone(m.keys), cols...)
		var rows []map[string]any
		if named := q[exportIDParam]; len(named) > 0 {
			rows, err = u.namedRows(ctx, m, named, fields)
		} else {
			rows, err = u.matchRows(crud.WithReadHooks(ctx), m, key, q, fields)
		}
		if err != nil {
			writeBulkRefusal(ctx, w, r, m, "entityui: export read", err)
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
				rec[i] = csvCell(m, f, fieldtext.Cell(m.rowValue(row, f)))
			}
			if err := cw.Write(rec); err != nil {
				slog.Debug("entityui: export write", "error", err)
				return
			}
		}
		cw.Flush()
	})
}

func (s *listState) exportParam(name string) bool {
	switch name {
	case s.p.cols, s.p.saved, s.p.as, s.p.per:
		return true
	case s.p.q:
		return len(s.m.e.Config.SearchFields) > 0
	}
	if field, ok := strings.CutPrefix(name, param(s.key, "f_")); ok {
		if slices.Contains(s.facets(), field) {
			return true
		}
		if _, isField := s.m.byName[field]; isField {
			return false
		}
	}
	return s.ownsParam(name)
}

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

func csvCell(m *meta, name, v string) string {
	if f, ok := m.field(name); ok && (f.Type == schema.Int || f.Type == schema.Float || f.Type == schema.Decimal) && plainNumber(v) {
		return v
	}
	return csvSafe(v)
}

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

func (u *UI) namedRows(ctx context.Context, m *meta, named, fields []string) ([]map[string]any, error) {
	if len(named) > InRequestCap {
		return nil, refuse(http.StatusUnprocessableEntity, reasonBulkOverCap, map[string]any{"cap": InRequestCap})
	}
	ids := dedupeIDs(named)
	rows, err := m.listByIDs(ctx, ids, crud.ListOptions{
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
