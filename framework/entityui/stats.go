package entityui

import (
	"context"
	"errors"
	"log/slog"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// statRowCap bounds the rows a sum or a grouping reads when the entity's
// AfterList hooks keep the aggregate off the database (crud's
// ErrAggregateMasked). Past it the stat draws nothing and logs why; it
// never draws a partial total.
const statRowCap = 100000

// statGroupCap bounds the groups a chart draws. A field with more
// distinct values draws nothing and logs why: a chart of every customer
// is a list, not a chart.
const statGroupCap = 100

// StatValue computes one metric over an entity for a stat card: agg
// "count" (or empty), or "sum" of an Int, Float or Decimal field, both
// computed by the database over every match. where is filter text in the
// query DSL (`status = "active"`), checked against the entity's fields;
// format "money" prints a sum as $1,234.00 (the currency symbol is the
// i18nui.KeyEntityCurrency entry), anything else as a number. A
// refused read, a bad filter, an unknown agg or a failed query prints
// "—": a stat sits beside content the caller can see, and must not
// announce what it cannot.
func (u *UI) StatValue(ctx context.Context, entityName, agg, field, where, format string) string {
	m, opts, ok := u.statRead(ctx, entityName, where)
	if !ok {
		return "—"
	}
	switch agg {
	case "", "count":
		n, err := m.ch.CountAll(crud.WithReadHooks(ctx), opts)
		if err != nil {
			slog.WarnContext(ctx, "entityui: stat", "entity", entityName, "error", err)
			return "—"
		}
		return formatNumber(float64(n), 0)
	case "sum":
		total, ok := u.statSum(ctx, m, field, opts)
		if !ok {
			return "—"
		}
		places := 2
		if format != "money" && total.IsInt() {
			places = 0
		}
		s := groupDigits(total.FloatString(places))
		if format == "money" {
			if rest, ok := strings.CutPrefix(s, "-"); ok {
				return "-" + i18nui.T(ctx, i18nui.KeyEntityCurrency) + rest
			}
			return i18nui.T(ctx, i18nui.KeyEntityCurrency) + s
		}
		return s
	default:
		slog.WarnContext(ctx, "entityui: stat agg is not count or sum", "entity", entityName, "agg", agg)
		return "—"
	}
}

// LastUpdated is when the newest record the caller can read was last
// written: the greatest updated_at in the caller's scope, read through
// the entity's read hooks. It reports false when the entity has no
// readable, sortable updated_at, the read is refused, no row has one, or
// the newest value is not a time (a masked column): a stat card then
// prints no date rather than a wrong one. Rows without a value are left
// out, so a database that sorts NULL first does not hide the newest.
func (u *UI) LastUpdated(ctx context.Context, entityName string) (time.Time, bool) {
	if m, err := u.meta(entityName); err != nil {
		return time.Time{}, false
	} else if f, ok := m.field("updated_at"); !ok || f.Hidden || f.NoQuery {
		return time.Time{}, false
	}
	m, opts, ok := u.statRead(ctx, entityName, `updated_at > "1970-01-01T00:00:00Z"`)
	if !ok {
		return time.Time{}, false
	}
	opts.Fields = []string{"updated_at"}
	opts.Sorts = []filter.ParsedSort{{Field: "updated_at", Desc: true}}
	opts.Limit = 1
	rows, err := m.ch.ListAll(crud.WithReadHooks(ctx), opts)
	if err != nil {
		slog.WarnContext(ctx, "entityui: last updated", "entity", entityName, "error", err)
		return time.Time{}, false
	}
	if len(rows) == 0 {
		return time.Time{}, false
	}
	return parseTime(rowValue(rows[0], "updated_at"))
}

// statSum totals field exactly: the database's SUM, or, when AfterList
// hooks would mask what it totals, the masked rows up to statRowCap.
func (u *UI) statSum(ctx context.Context, m *meta, field string, opts crud.ListOptions) (*big.Rat, bool) {
	text, err := m.ch.SumAll(crud.WithReadHooks(ctx), field, opts)
	if err == nil {
		total, ok := new(big.Rat).SetString(text)
		if !ok {
			slog.WarnContext(ctx, "entityui: stat sum is not a number", "entity", m.name, "field", field)
		}
		return total, ok
	}
	if !errors.Is(err, crud.ErrAggregateMasked) {
		slog.WarnContext(ctx, "entityui: stat", "entity", m.name, "error", err)
		return nil, false
	}
	rows, ok := u.statRows(ctx, m, field, opts)
	if !ok {
		return nil, false
	}
	total := new(big.Rat)
	for _, r := range rows {
		// A value a hook masked to text is not a number, and adds nothing.
		if v, ok := new(big.Rat).SetString(cell(rowValue(r, field))); ok {
			total.Add(total, v)
		}
	}
	return total, true
}

// statRows reads field over every match through the read hooks, refusing
// past statRowCap rather than drawing part of the data.
func (u *UI) statRows(ctx context.Context, m *meta, field string, opts crud.ListOptions) ([]map[string]any, bool) {
	opts.Fields = []string{field}
	opts.Limit = statRowCap + 1
	rows, err := m.ch.ListAll(crud.WithReadHooks(ctx), opts)
	if err != nil {
		slog.WarnContext(ctx, "entityui: stat", "entity", m.name, "error", err)
		return nil, false
	}
	if len(rows) > statRowCap {
		slog.WarnContext(ctx, "entityui: stat over the row cap; AfterList hooks keep it off the database",
			"entity", m.name, "field", field, "cap", statRowCap)
		return nil, false
	}
	return rows, true
}

// statRead resolves the entity, checks the caller may list it, and parses
// where into the read's predicate.
func (u *UI) statRead(ctx context.Context, entityName, where string) (*meta, crud.ListOptions, bool) {
	m, err := u.meta(entityName)
	if err != nil {
		slog.WarnContext(ctx, "entityui: stat", "entity", entityName, "error", err)
		return nil, crud.ListOptions{}, false
	}
	if !canRead(ctx, m.ch) {
		return nil, crud.ListOptions{}, false
	}
	p, err := dsl.ParsePredicate(where, m.e.GetFields())
	if err != nil {
		slog.WarnContext(ctx, "entityui: stat filter", "entity", entityName, "error", err)
		return nil, crud.ListOptions{}, false
	}
	return m, crud.ListOptions{Where: p}, true
}

type groupCount struct {
	key string
	n   int
}

// groupCounts counts rows per distinct value of groupBy, computed by the
// database (or over the masked rows, as statSum falls back), ordered by
// value, an enum's in its declared order. A refused read is "no data",
// the same as an empty result, so a chart never tells a visitor which
// entities exist; so is a field with more than statGroupCap values.
func (u *UI) groupCounts(ctx context.Context, entityName, groupBy string) (*meta, []groupCount) {
	m, opts, ok := u.statRead(ctx, entityName, "")
	if !ok {
		return nil, nil
	}
	var out []groupCount
	groups, err := m.ch.GroupCountAll(crud.WithReadHooks(ctx), groupBy, opts, statGroupCap+1)
	switch {
	case err == nil:
		for _, g := range groups {
			out = append(out, groupCount{key: cell(g.Value), n: g.Count})
		}
	case errors.Is(err, crud.ErrAggregateMasked):
		rows, ok := u.statRows(ctx, m, groupBy, opts)
		if !ok {
			return m, nil
		}
		idx := map[string]int{}
		for _, r := range rows {
			k := cell(rowValue(r, groupBy))
			if i, seen := idx[k]; seen {
				out[i].n++
				continue
			}
			idx[k] = len(out)
			out = append(out, groupCount{key: k, n: 1})
		}
		slices.SortStableFunc(out, func(a, b groupCount) int { return strings.Compare(a.key, b.key) })
	default:
		slog.WarnContext(ctx, "entityui: group", "entity", entityName, "error", err)
		return m, nil
	}
	if len(out) > statGroupCap {
		slog.WarnContext(ctx, "entityui: group has more values than a chart draws", "entity", entityName, "field", groupBy, "cap", statGroupCap)
		return m, nil
	}
	if f, ok := m.field(groupBy); ok && f.Type == schema.Enum {
		slices.SortStableFunc(out, func(a, b groupCount) int { return enumRank(f, a.key) - enumRank(f, b.key) })
	}
	return m, out
}

// enumRank is v's place in f's declared values; a value the enum no
// longer declares sorts after them.
func enumRank(f schema.Field, v string) int {
	if i := slices.Index(f.Values, v); i >= 0 {
		return i
	}
	return len(f.Values)
}

// groupLabel is a group's label: the field's translated value label, or
// a dash for an empty value.
func groupLabel(ctx context.Context, m *meta, field, key string) string {
	if key == "" {
		return "—"
	}
	return m.valueLabel(ctx, field, key)
}

// GroupBars counts an entity's rows per value of groupBy, as bar chart bars.
func (u *UI) GroupBars(ctx context.Context, entityName, groupBy string) []ui.BarChartBar {
	m, counts := u.groupCounts(ctx, entityName, groupBy)
	bars := make([]ui.BarChartBar, 0, len(counts))
	for _, g := range counts {
		bars = append(bars, ui.BarChartBar{Label: groupLabel(ctx, m, groupBy, g.key), Value: float64(g.n)})
	}
	return bars
}

// GroupSlices is GroupBars as pie chart slices.
func (u *UI) GroupSlices(ctx context.Context, entityName, groupBy string) []ui.PieSlice {
	m, counts := u.groupCounts(ctx, entityName, groupBy)
	out := make([]ui.PieSlice, 0, len(counts))
	for _, g := range counts {
		out = append(out, ui.PieSlice{Label: groupLabel(ctx, m, groupBy, g.key), Value: float64(g.n)})
	}
	return out
}

// LineChart draws the grouped counts as a single-series line chart. Fewer
// than two groups draws ui.LineChart's empty state.
func (u *UI) LineChart(ctx context.Context, entityName, groupBy string) render.HTML {
	m, counts := u.groupCounts(ctx, entityName, groupBy)
	labels := make([]string, 0, len(counts))
	values := make([]float64, 0, len(counts))
	for _, g := range counts {
		labels = append(labels, groupLabel(ctx, m, groupBy, g.key))
		values = append(values, float64(g.n))
	}
	name := groupBy
	if m != nil {
		name = m.label(ctx, groupBy)
	}
	return ui.LineChart(ui.LineChartConfig{Series: []ui.LineSeries{{Name: name, Values: values}}, Labels: labels})
}
