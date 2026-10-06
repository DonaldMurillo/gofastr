package entityui

import (
	"context"
	"log/slog"
	"math"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// statRowCap bounds the rows a sum or a grouping reads.
const statRowCap = 100000

// StatValue computes one metric over an entity for a stat card: agg
// "count", or "sum" of a numeric field. where is filter text in the query
// DSL (`status = "active"`), checked against the entity's fields; format
// "money" prints a sum as $1,234.00, anything else as a number. A refused
// read, a bad filter or a failed query prints "—": a stat sits beside
// content the caller can see, and must not announce what it cannot.
func (u *UI) StatValue(ctx context.Context, entityName, agg, field, where, format string) string {
	m, opts, ok := u.statRead(ctx, entityName, where)
	if !ok {
		return "—"
	}
	if agg == "sum" {
		opts.Fields = []string{field}
		opts.Limit = statRowCap
		rows, err := m.ch.ListAll(ctx, opts)
		if err != nil {
			slog.WarnContext(ctx, "entityui: stat", "entity", entityName, "error", err)
			return "—"
		}
		var total float64
		for _, r := range rows {
			f, err := strconv.ParseFloat(cell(rowValue(r, field)), 64)
			if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
				continue
			}
			total += f
		}
		if format == "money" {
			return "$" + formatNumber(total, 2)
		}
		if total == math.Trunc(total) {
			return formatNumber(total, 0)
		}
		return formatNumber(total, 2)
	}
	n, err := m.ch.CountAll(ctx, opts)
	if err != nil {
		slog.WarnContext(ctx, "entityui: stat", "entity", entityName, "error", err)
		return "—"
	}
	return formatNumber(float64(n), 0)
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

// groupCounts counts rows per distinct value of groupBy, in first-seen
// order. A refused read is "no data", the same as an empty result, so a
// chart never tells a visitor which entities exist.
func (u *UI) groupCounts(ctx context.Context, entityName, groupBy string) (*meta, []groupCount) {
	m, opts, ok := u.statRead(ctx, entityName, "")
	if !ok {
		return nil, nil
	}
	opts.Fields = []string{groupBy}
	opts.Limit = statRowCap
	rows, err := m.ch.ListAll(ctx, opts)
	if err != nil {
		slog.WarnContext(ctx, "entityui: group", "entity", entityName, "error", err)
		return m, nil
	}
	idx := map[string]int{}
	var out []groupCount
	for _, r := range rows {
		k := cell(rowValue(r, groupBy))
		if i, seen := idx[k]; seen {
			out[i].n++
			continue
		}
		idx[k] = len(out)
		out = append(out, groupCount{key: k, n: 1})
	}
	return m, out
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
