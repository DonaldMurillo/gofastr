package entityui

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

const statRowCap = 100000

const statGroupCap = 100

// StatValue computes one metric over an entity for a stat card: agg
// "count" (or ""), or "sum" of a numeric field, over the rows matching where
// (query DSL). format "money" prints a currency. A refused read, bad filter,
// unknown agg or failed query prints "—".
func (u *UI) StatValue(ctx context.Context, entityName, agg, field, where, format string) string {
	if agg == "" || agg == "count" {
		if n, ok := u.Count(ctx, entityName, where); ok {
			return n
		}
		return "—"
	}
	m, opts, ok := u.statRead(ctx, entityName, where)
	if !ok {
		return "—"
	}
	switch agg {
	case "sum":
		total, ok := u.statSum(ctx, m, field, opts)
		if !ok {
			return "—"
		}
		if format == "money" {
			return money(ctx, total.FloatString(2))
		}
		places := 2
		if total.IsInt() {
			places = 0
		}
		s, _ := i18n.Decimal(ctx, total.FloatString(places), i18n.MinFraction(places), i18n.MaxFraction(places))
		return s
	default:
		slog.WarnContext(ctx, "entityui: stat agg is not count or sum", "entity", entityName, "agg", agg)
		return "—"
	}
}

// CheckStat reports why StatValue could never compute this spec: an
// unknown entity or agg, a bad sum field, filter or format. It is
// CheckStatFields over the registered entity's fields.
func (u *UI) CheckStat(entityName, agg, field, where, format string) error {
	m, err := u.meta(entityName)
	if err != nil {
		return err
	}
	return CheckStatFields(entityName, m.e.GetFields(), agg, field, where, format)
}

// CheckStatFields is CheckStat's rule over a field list, for a caller that
// judges a stat before any app exists; Hidden fields are skipped.
func CheckStatFields(entityName string, fields []schema.Field, agg, field, where, format string) error {
	switch agg {
	case "", "count":
		if field != "" {
			return fmt.Errorf("entityui: a count of %s takes no field (got %q)", entityName, field)
		}
		if format != "" {
			return fmt.Errorf("entityui: a count of %s takes no format (got %q)", entityName, format)
		}
	case "sum":
		f, ok := visibleField(fields, field)
		if !ok || f.NoQuery {
			return fmt.Errorf("entityui: sum over %s needs a visible, queryable field (got %q)", entityName, field)
		}
		if f.Type != schema.Int && f.Type != schema.Float && f.Type != schema.Decimal {
			return fmt.Errorf("entityui: sum over %s.%s needs an Int, Float or Decimal field", entityName, field)
		}
		if format != "" && format != "money" {
			return fmt.Errorf("entityui: stat format %q is not \"\" or \"money\"", format)
		}
	default:
		return fmt.Errorf("entityui: stat agg %q is not count or sum", agg)
	}
	p, err := dsl.ParsePredicate(where, fields)
	if err != nil {
		return fmt.Errorf("entityui: stat filter on %s: %w", entityName, err)
	}
	if err := checkEnumValues(p, fields); err != nil {
		return fmt.Errorf("entityui: stat filter on %s: %w", entityName, err)
	}
	return nil
}

func checkEnumValues(p *filter.Predicate, fields []schema.Field) error {
	if p == nil {
		return nil
	}
	for i := range p.Children {
		if err := checkEnumValues(&p.Children[i], fields); err != nil {
			return err
		}
	}
	if len(p.Children) > 0 {
		return nil
	}
	f, ok := visibleField(fields, p.Field)
	if !ok || f.Type != schema.Enum || len(f.Values) == 0 {
		return nil
	}
	var values []string
	switch p.Op {
	case filter.OpEq, filter.OpNe:
		values = []string{p.Value}
	case filter.OpIn:
		values = p.Values
	}
	for _, v := range values {
		if !slices.Contains(f.Values, v) {
			return fmt.Errorf("%q is not one of enum field %s's values %v", v, f.Name, f.Values)
		}
	}
	return nil
}

// CheckChart reports why GroupBars, GroupSlices and LineChart could never
// draw groupBy: an unknown entity, or a groupBy that is not a visible,
// queryable, groupable column. It is CheckChartFields over the entity's fields.
func (u *UI) CheckChart(entityName, groupBy string) error {
	m, err := u.meta(entityName)
	if err != nil {
		return err
	}
	return CheckChartFields(entityName, m.e.GetFields(), groupBy)
}

// CheckChartFields is CheckChart's rule over a field list.
func CheckChartFields(entityName string, fields []schema.Field, groupBy string) error {
	f, ok := visibleField(fields, groupBy)
	if !ok {
		return fmt.Errorf("entityui: a chart of %s groups by a visible field (got %q)", entityName, groupBy)
	}
	if f.NoQuery {
		return fmt.Errorf("entityui: a chart of %s cannot group by %q, a no-query field", entityName, groupBy)
	}
	if f.Type == schema.JSON || (f.Type == schema.Relation && f.Many) {
		return fmt.Errorf("entityui: a chart of %s cannot group by %q, which the database does not group", entityName, groupBy)
	}
	return nil
}

func visibleField(fields []schema.Field, name string) (schema.Field, bool) {
	for _, f := range fields {
		if f.Name == name && !f.Hidden {
			return f, true
		}
	}
	return schema.Field{}, false
}

// Count is how many records the caller can read match where ("" for all),
// formatted with digit grouping. It reports false when the read is
// refused, the filter is bad or the query fails.
func (u *UI) Count(ctx context.Context, entityName, where string) (string, bool) {
	n, ok := u.CountInt(ctx, entityName, where)
	if !ok {
		return "", false
	}
	return i18n.Number(ctx, n), true
}

// CountInt is Count as a number.
func (u *UI) CountInt(ctx context.Context, entityName, where string) (int, bool) {
	m, opts, ok := u.statRead(ctx, entityName, where)
	if !ok {
		return 0, false
	}
	n, err := m.countRows(ctx, opts)
	if err != nil {
		slog.WarnContext(ctx, "entityui: count", "entity", entityName, "error", err)
		return 0, false
	}
	return n, true
}

// CountView counts the records a declared view lists for the caller. ok
// is false for an undeclared or hidden view, a refused read or a failure.
func (u *UI) CountView(ctx context.Context, entityName, view string) (int, bool) {
	m, opts, ok := u.statRead(ctx, entityName, "")
	if !ok || view == "" || !declaredAndViewable(ctx, m, view) {
		return 0, false
	}
	p, err := viewPredicate(ctx, m, view)
	if err != nil {
		slog.WarnContext(ctx, "entityui: view count", "entity", entityName, "view", view, "error", err)
		return 0, false
	}
	opts.Where = p
	n, err := m.countRows(ctx, opts)
	if err != nil {
		slog.WarnContext(ctx, "entityui: view count", "entity", entityName, "view", view, "error", err)
		return 0, false
	}
	return n, true
}

// CountUpTo counts the records the caller can read that match where, up
// to limit; more reports there are more, ok is false when refused or failed.
func (u *UI) CountUpTo(ctx context.Context, entityName, where string, limit int) (n int, more, ok bool) {
	if limit < 1 {
		return 0, false, false
	}
	m, opts, ok := u.statRead(ctx, entityName, where)
	if !ok {
		return 0, false, false
	}
	opts.Fields = m.keys
	opts.Limit = limit + 1
	rows, err := m.listRows(ctx, opts)
	if err != nil {
		slog.WarnContext(ctx, "entityui: bounded count", "entity", entityName, "error", err)
		return 0, false, false
	}
	if len(rows) > limit {
		return limit, true, true
	}
	return len(rows), false, true
}

// LastUpdated is the greatest updated_at the caller can read. It reports
// false when there is no readable, sortable updated_at, the read is
// refused, no row has one, or the newest value is not a time.
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
	rows, err := m.listRows(ctx, opts)
	if err != nil {
		slog.WarnContext(ctx, "entityui: last updated", "entity", entityName, "error", err)
		return time.Time{}, false
	}
	if len(rows) == 0 {
		return time.Time{}, false
	}
	return parseTime(m.rowValue(rows[0], "updated_at"))
}

func (u *UI) statSum(ctx context.Context, m *meta, field string, opts crud.ListOptions) (*big.Rat, bool) {
	text, err := m.sumRows(ctx, field, opts)
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
		if v, ok := new(big.Rat).SetString(cell(m.rowValue(r, field))); ok {
			total.Add(total, v)
		}
	}
	return total, true
}

func (u *UI) statRows(ctx context.Context, m *meta, field string, opts crud.ListOptions) ([]map[string]any, bool) {
	opts.Fields = []string{field}
	opts.Limit = statRowCap + 1
	rows, err := m.listRows(ctx, opts)
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

func (u *UI) groupCounts(ctx context.Context, entityName, groupBy string) (*meta, []groupCount) {
	m, opts, ok := u.statRead(ctx, entityName, "")
	if !ok {
		return nil, nil
	}
	var out []groupCount
	groups, err := m.groupRows(ctx, groupBy, opts, statGroupCap+1)
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
			k := cell(m.rowValue(r, groupBy))
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

func enumRank(f schema.Field, v string) int {
	if i := slices.Index(f.Values, v); i >= 0 {
		return i
	}
	return len(f.Values)
}

func groupLabel(ctx context.Context, m *meta, field, key string) string {
	if key == "" {
		return "—"
	}
	return m.valueLabel(ctx, field, key)
}

// groupSeries maps the grouped counts to the label/value pairs a chart
// takes, sharing one loop over groupCounts between the chart shapes.
func groupSeries[T any](ctx context.Context, m *meta, groupBy string, counts []groupCount, pair func(label string, value float64) T) []T {
	out := make([]T, 0, len(counts))
	for _, g := range counts {
		out = append(out, pair(groupLabel(ctx, m, groupBy, g.key), float64(g.n)))
	}
	return out
}

// GroupBars counts an entity's rows per value of groupBy, as bar chart bars.
func (u *UI) GroupBars(ctx context.Context, entityName, groupBy string) []ui.BarChartBar {
	m, counts := u.groupCounts(ctx, entityName, groupBy)
	return groupSeries(ctx, m, groupBy, counts, func(label string, value float64) ui.BarChartBar {
		return ui.BarChartBar{Label: label, Value: value}
	})
}

// GroupSlices is GroupBars as pie chart slices.
func (u *UI) GroupSlices(ctx context.Context, entityName, groupBy string) []ui.PieSlice {
	m, counts := u.groupCounts(ctx, entityName, groupBy)
	return groupSeries(ctx, m, groupBy, counts, func(label string, value float64) ui.PieSlice {
		return ui.PieSlice{Label: label, Value: value}
	})
}

// LineChart draws the grouped counts as a single-series line chart.
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
	return ui.LineChart(ui.LineChartConfig{Ctx: ctx, Series: []ui.LineSeries{{Name: name, Values: values}}, Labels: labels})
}
