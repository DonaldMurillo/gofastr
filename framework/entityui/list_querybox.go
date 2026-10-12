package entityui

import (
	"context"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/filter"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// The query box: the filter typed by hand. The chips narrow what a
// reader picked; the box is where they write it. It is a field of the
// toolbar's GET form, inside the Filters dropdown, named the list's
// filter param, so it submits exactly what the chips and every other
// link read: one source of truth in the URL, and a bad text keeps the
// filter-did-not-apply warning instead of failing the page. The page
// param is dropped with the rest of the toolbar's: a new narrowing
// starts on page one.

// queryField draws the query box's field, prefilled with the active
// filter text (an open saved view's included), and under it a short
// reference: an example built from the entity's own fields (the
// placeholder too), the operators, the joining words and the queryable
// field names, each as code.
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
		ref = append(ref, ui.DetailItem{Label: i18nui.T(ctx, i18nui.KeyEntityQueryBoxExample), Value: ui.InlineCode(example)})
	}
	ref = append(ref,
		ui.DetailItem{Label: i18nui.T(ctx, i18nui.KeyEntityQueryBoxOperators), Value: codeRun(queryOperators...)},
		ui.DetailItem{Label: i18nui.T(ctx, i18nui.KeyEntityQueryBoxJoin), Value: codeRun("and", "or", "( )")},
		ui.DetailItem{Label: i18nui.T(ctx, i18nui.KeyEntityQueryBoxFields), Value: codeRun(names...)},
	)
	rows, box := filterRowsOf(s)
	return render.Join(
		filterRowsField(ctx, s, queryable, rows),
		ui.TextField(ui.TextFieldConfig{
			Name:        s.p.filter,
			ID:          "eui-" + listIDSafe(s.key, m.name) + "-filter",
			Label:       i18nui.T(ctx, i18nui.KeyEntityQueryBoxField),
			Value:       box,
			Placeholder: example,
			Help:        i18nui.T(ctx, i18nui.KeyEntityQueryBoxHelp),
		}),
		ui.DetailList(ui.DetailListConfig{Items: ref}),
	)
}

// rowOps are the filter rows' operators: the DSL's own spelling, each
// with its name.
var rowOps = []struct {
	op  string
	key i18nui.Key
}{
	{"=", i18nui.KeyEntityFilterOpEq},
	{"!=", i18nui.KeyEntityFilterOpNe},
	{"contains", i18nui.KeyEntityFilterOpLike},
	{">", i18nui.KeyEntityFilterOpGt},
	{"<", i18nui.KeyEntityFilterOpLt},
	{">=", i18nui.KeyEntityFilterOpGte},
	{"<=", i18nui.KeyEntityFilterOpLte},
}

// maxFilterRows caps the rows one submit reads.
const maxFilterRows = 10

// maxFilterRowValue caps one row's value, in runes.
const maxFilterRowValue = 200

// rowTerms are the submitted rows as DSL terms: a row whose field is not
// a queryable field of the entity, whose operator is not a row
// operator, or whose value is empty or over maxFilterRowValue is
// dropped; at most maxFilterRows are read.
func rowTerms(m *meta, fields, ops, values []string) []string {
	var out []string
	for i := 0; i < len(fields) && i < maxFilterRows; i++ {
		f, ok := m.field(fields[i])
		if !ok || f.NoQuery || i >= len(ops) || i >= len(values) {
			continue
		}
		v := strings.TrimSpace(values[i])
		if v == "" || len([]rune(v)) > maxFilterRowValue || !slices.ContainsFunc(rowOps, func(o struct {
			op  string
			key i18nui.Key
		}) bool {
			return o.op == ops[i]
		}) {
			continue
		}
		out = append(out, f.Name+" "+ops[i]+" "+dslQuote(v))
	}
	return out
}

// dslQuote quotes a value the way predicateText does, so a row's term
// round-trips through the parser.
func dslQuote(v string) string { return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"` }

// composeFilter joins the rows' terms and the box's text with and; the
// box's text is grouped when it holds an or, so the and binds the whole.
func composeFilter(terms []string, box string) string {
	if box != "" {
		if len(terms) > 0 && strings.Contains(strings.ToLower(box), " or ") {
			box = "(" + box + ")"
		}
		terms = append(terms, box)
	}
	return strings.Join(terms, " and ")
}

// filterRowsOf splits the filter into rows, one per plain top-level
// term (field, comparison, one value), and the box's text, the terms a
// row cannot say (an or group, an in list). A filter that did not parse
// stays whole in the box.
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
	var rest []string
	for _, t := range terms {
		if op, ok := rowOp(t); ok {
			rows = append(rows, ui.FilterRow{Field: t.Field, Op: op, Value: t.Value})
			continue
		}
		rest = append(rest, predicateText(t))
	}
	return rows, strings.Join(rest, " and ")
}

// rowOp is a leaf's row operator, or false for a term a row cannot say.
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

// filterRowsField draws the rows over the queryable fields.
func filterRowsField(ctx context.Context, s *listState, queryable []schema.Field, rows []ui.FilterRow) render.HTML {
	// The entity's own fields first, the system ones (id, timestamps)
	// after: a new row starts on a field a reader means.
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
		ops = append(ops, ui.SelectOption{Value: o.op, Text: i18nui.T(ctx, o.key)})
	}
	return ui.FilterRows(ui.FilterRowsConfig{
		ID:         "eui-" + listIDSafe(s.key, s.m.name) + "-rows",
		FieldName:  s.p.rowF,
		OpName:     s.p.rowO,
		ValueName:  s.p.rowV,
		Fields:     fields,
		Operators:  ops,
		Rows:       rows,
		Legend:     i18nui.T(ctx, i18nui.KeyEntityFilterRows),
		FieldLabel: i18nui.T(ctx, i18nui.KeyEntityFilterRowField),
		OpLabel:    i18nui.T(ctx, i18nui.KeyEntityFilterRowOp),
		ValueLabel: i18nui.T(ctx, i18nui.KeyEntityFilterRowValue),
		Ctx:        ctx,
	})
}

// queryOperators are the comparisons the filter parser takes.
var queryOperators = []string{"=", "!=", "<", ">", "<=", ">=", "contains", "in [a, b]"}

// codeRun is each text as inline code, separated by spaces so the run
// wraps.
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

// queryExample is a filter the parser accepts, written from the
// entity's own fields: the first two of an enum against its first
// value, a number against 100, a text field's contains and a
// boolean's true, joined with "and". It never names the id, a relation
// (its value is another record's id), a date or an enum value the
// quotes would have to escape, and is "" when nothing else is left.
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
