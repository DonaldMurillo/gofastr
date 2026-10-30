package entityui

import (
	"context"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/filter"

	"github.com/DonaldMurillo/gofastr/core/i18n"
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
		ui.TextField(ui.TextFieldConfig{
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

// rowOps are the filter rows' operators: the DSL's own spelling, each
// with its name.
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

// maxFilterRows caps the rows one submit reads.
const maxFilterRows = 10

// maxFilterRowValue caps one row's value, in runes.
const maxFilterRowValue = 200

// rowValue reports whether a row keeps v as it is. rowTerms trims a
// value and drops an empty or over-long one, so filterRowsOf leaves a
// term holding any other value in the box, which Apply sends unchanged.
func rowValue(v string) bool {
	return v != "" && v == strings.TrimSpace(v) && len([]rune(v)) <= maxFilterRowValue
}

// rowTerms are the submitted rows as DSL terms: a row whose field is not
// a queryable field of the entity, whose operator is not a row
// operator, or whose trimmed value fails rowValue is dropped; at most
// maxFilterRows are read.
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
		out = append(out, f.Name+" "+ops[i]+" "+dslQuote(v))
	}
	return out
}

// dslQuote quotes a value the way predicateText does, so a row's term
// round-trips through the parser.
func dslQuote(v string) string { return `"` + strings.ReplaceAll(v, `"`, `\"`) + `"` }

// composeFilter joins the rows' terms and the box's text with and. The
// box's text is grouped whenever rows join it, so the and binds the
// whole box however its or is spelled: the parser reads `"a"or(b)`
// with no spaces around the or.
func composeFilter(terms []string, box string) string {
	if box != "" {
		if len(terms) > 0 {
			box = "(" + box + ")"
		}
		terms = append(terms, box)
	}
	return strings.Join(terms, " and ")
}

// filterRowsOf splits the filter into rows, one per plain top-level
// term (field, comparison, one value a row keeps: rowValue), and the
// box's text, the terms a row cannot say (an or group, an in list, an
// empty or space-padded value). A filter that did not parse stays whole
// in the box.
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
	// One term stands alone (composeFilter parenthesizes it beside the
	// rows); several join with and, each group in parentheses.
	if len(rest) == 1 {
		return rows, predicateText(rest[0])
	}
	texts := make([]string, 0, len(rest))
	for _, t := range rest {
		texts = append(texts, termText(t))
	}
	return rows, strings.Join(texts, " and ")
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
