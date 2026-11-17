package entityui

import (
	"context"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/filter"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func (b *ListBuilder) queryField(ctx context.Context, s *listState) render.HTML {
	if !b.queryBox {
		return ""
	}
	m := s.m
	queryable := make([]schema.Field, 0, len(m.fields))
	names := make([]string, 0, len(m.fields))
	for _, f := range m.fields {
		if f.NoQuery {
			continue
		}
		queryable = append(queryable, f)
		names = append(names, f.Name)
	}
	example := queryExample(queryable)
	var ref []ui.DetailItem
	if example != "" {
		ref = append(ref, ui.DetailItem{Label: i18nui.EntityQueryBoxExample.Text(ctx), Value: ui.InlineCode(example)})
	}
	ref = append(ref,
		ui.DetailItem{Label: i18nui.EntityQueryBoxOperators.Text(ctx), Value: codeRun(queryOperators...)},
		ui.DetailItem{Label: i18nui.EntityQueryBoxJoin.Text(ctx), Value: codeRun("and", "or", "( )")},
		ui.DetailItem{Label: i18nui.EntityQueryBoxFields.Text(ctx), Value: codeRun(names...)},
	)
	rows, box := filterRowsOf(s)
	return render.Join(
		filterRowsField(ctx, s, queryable, rows),
		ui.TextField(ui.TextFieldConfig{Ctx: ctx,
			Name:        s.p.filter,
			ID:          "eui-" + listIDSafe(s.key, m.name) + "-filter",
			Label:       i18nui.EntityQueryBoxField.Text(ctx),
			Value:       box,
			Placeholder: example,
			Help:        i18nui.EntityQueryBoxHelp.Text(ctx),
		}),
		ui.DetailList(ui.DetailListConfig{Items: ref}),
	)
}

var rowOps = []struct {
	op  string
	key i18n.Phrase
}{
	{"=", i18nui.EntityFilterOpEq},
	{"!=", i18nui.EntityFilterOpNe},
	{"contains", i18nui.EntityFilterOpLike},
	{">", i18nui.EntityFilterOpGt},
	{"<", i18nui.EntityFilterOpLt},
	{">=", i18nui.EntityFilterOpGte},
	{"<=", i18nui.EntityFilterOpLte},
}

const maxFilterRows = 10

const maxFilterRowValue = 200

func rowValue(v string) bool {
	return v != "" && v == strings.TrimSpace(v) && len([]rune(v)) <= maxFilterRowValue
}

func rowTerms(m *meta, fields, ops, values []string) []string {
	var out []string
	for i := 0; i < len(fields) && i < maxFilterRows; i++ {
		f, ok := m.field(fields[i])
		if !ok || f.NoQuery || i >= len(ops) || i >= len(values) {
			continue
		}
		v := strings.TrimSpace(values[i])
		if !rowValue(v) || !slices.ContainsFunc(rowOps, func(o struct {
			op  string
			key i18n.Phrase
		}) bool {
			return o.op == ops[i]
		}) {
			continue
		}
		out = append(out, f.Name+" "+ops[i]+" "+dsl.Quote(v))
	}
	return out
}

func composeFilter(terms []string, box string) string {
	if box != "" {
		if len(terms) > 0 {
			box = "(" + box + ")"
		}
		terms = append(terms, box)
	}
	return strings.Join(terms, " and ")
}

func filterRowsOf(s *listState) (rows []ui.FilterRow, box string) {
	if s.filterPred == nil {
		return nil, s.filterText
	}
	terms := []*filter.Predicate{s.filterPred}
	if len(s.filterPred.Children) > 0 && !s.filterPred.Or {
		terms = terms[:0]
		for i := range s.filterPred.Children {
			terms = append(terms, &s.filterPred.Children[i])
		}
	}
	var rest []*filter.Predicate
	for _, t := range terms {
		if op, ok := rowOp(t); ok && rowValue(t.Value) {
			rows = append(rows, ui.FilterRow{Field: t.Field, Op: op, Value: t.Value})
			continue
		}
		rest = append(rest, t)
	}
	if len(rest) == 1 {
		return rows, predicateText(rest[0])
	}
	texts := make([]string, 0, len(rest))
	for _, t := range rest {
		texts = append(texts, termText(t))
	}
	return rows, strings.Join(texts, " and ")
}

func rowOp(p *filter.Predicate) (string, bool) {
	if len(p.Children) > 0 {
		return "", false
	}
	op, ok := map[filter.FilterOp]string{
		filter.OpEq: "=", filter.OpNe: "!=", filter.OpLike: "contains",
		filter.OpGt: ">", filter.OpLt: "<", filter.OpGte: ">=", filter.OpLte: "<=",
	}[p.Op]
	return op, ok
}

func filterRowsField(ctx context.Context, s *listState, queryable []schema.Field, rows []ui.FilterRow) render.HTML {
	fields := make([]ui.SelectOption, 0, len(queryable))
	var system []ui.SelectOption
	for _, f := range queryable {
		o := ui.SelectOption{Value: f.Name, Text: s.m.label(ctx, f.Name)}
		if s.m.system(f) {
			system = append(system, o)
			continue
		}
		fields = append(fields, o)
	}
	fields = append(fields, system...)
	ops := make([]ui.SelectOption, 0, len(rowOps))
	for _, o := range rowOps {
		ops = append(ops, ui.SelectOption{Value: o.op, Text: o.key.Text(ctx)})
	}
	return ui.FilterRows(ui.FilterRowsConfig{
		ID:         "eui-" + listIDSafe(s.key, s.m.name) + "-rows",
		FieldName:  s.p.rowF,
		OpName:     s.p.rowO,
		ValueName:  s.p.rowV,
		Fields:     fields,
		Operators:  ops,
		Rows:       rows,
		Legend:     i18nui.EntityFilterRows.Text(ctx),
		FieldLabel: i18nui.EntityFilterRowField.Text(ctx),
		OpLabel:    i18nui.EntityFilterRowOp.Text(ctx),
		ValueLabel: i18nui.EntityFilterRowValue.Text(ctx),
		Ctx:        ctx,
	})
}

var queryOperators = []string{"=", "!=", "<", ">", "<=", ">=", "contains", "in [a, b]"}

func codeRun(texts ...string) render.HTML {
	parts := make([]render.HTML, 0, 2*len(texts))
	for i, t := range texts {
		if i > 0 {
			parts = append(parts, render.Text(" "))
		}
		parts = append(parts, ui.InlineCode(t))
	}
	return render.Join(parts...)
}

func queryExample(fields []schema.Field) string {
	var enum, number, text, flag string
	for _, f := range fields {
		switch {
		case f.Name == "id":
		case f.Type == schema.Enum:
			if enum == "" && len(f.Values) > 0 && !strings.ContainsAny(f.Values[0], `"\`) {
				enum = f.Name + ` = "` + f.Values[0] + `"`
			}
		case f.Type == schema.Int, f.Type == schema.Float, f.Type == schema.Decimal:
			if number == "" {
				number = f.Name + " > 100"
			}
		case f.Type == schema.String, f.Type == schema.Text:
			if text == "" {
				text = f.Name + ` contains "a"`
			}
		case f.Type == schema.Bool:
			if flag == "" {
				flag = f.Name + " = true"
			}
		}
	}
	parts := make([]string, 0, 2)
	for _, p := range []string{enum, number, text, flag} {
		if p != "" && len(parts) < 2 {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, " and ")
}
