package entityui

import (
	"context"
	"fmt"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// createScreen draws the create form: the record form, empty, posting
// a create to the entity's REST base. ?duplicate=<id> prefills from
// that record; ?prefill_<field>=<value> prefills one field.
func (b *RecordBuilder) createScreen(ctx context.Context, m *meta, base string) render.HTML {
	if !canRead(ctx, m.ch) {
		return accessDenied(ctx, m.plural(ctx))
	}
	if m.hasAPI && !m.ch.CanCreateScoped(ctx) {
		return accessDenied(ctx, m.plural(ctx))
	}
	values := map[string]string{}
	if b.prefill != nil {
		for k, v := range b.prefill {
			values[k] = v
		}
	}
	q := appui.QueryFromContext(ctx)
	for name, vs := range q {
		field, ok := strings.CutPrefix(name, "prefill_")
		if !ok || len(vs) == 0 {
			continue
		}
		if f, ok := m.field(field); ok && mayCreateSet(m, f) {
			values[f.Name] = vs[0]
		}
	}
	if dup := q.Get("duplicate"); dup != "" {
		// The hooked read comes first and must succeed: it applies the
		// caller's read scope, and a field its AfterGet rewrites is a
		// masked one whose stored value a create must not copy. A
		// failed or empty hooked read refuses the duplicate rather than
		// fall back to raw values. The prefill itself comes from the
		// raw read, since the values round-trip on submit. Fields a
		// create may not set (system, the state field, stamps, unique)
		// start blank, so the normal create hooks and scope apply.
		if !canReadRecord(ctx, m.ch, dup) {
			return m.notFound(ctx)
		}
		hooked, herr := m.ch.GetOne(crud.WithReadHooks(ctx), dup, nil)
		if herr != nil || hooked == nil {
			return m.notFound(ctx)
		}
		row, err := m.ch.GetOne(ctx, dup, nil)
		if err != nil || row == nil {
			return m.notFound(ctx)
		}
		for _, f := range m.fields {
			if !mayCreateSet(m, f) || f.Unique {
				continue
			}
			if cell(rowValue(row, f.Name)) != cell(rowValue(hooked, f.Name)) {
				continue
			}
			if v := formValueText(f, rowValue(row, f.Name)); v != "" {
				values[f.Name] = v
			}
		}
	}
	header := ui.PageHeader(ui.PageHeaderConfig{
		Title:   i18nui.TVars(ctx, i18nui.KeyEntityNew, map[string]string{"entity": m.singular(ctx)}),
		Eyebrow: m.plural(ctx),
		Actions: ui.LinkButton(ui.LinkButtonConfig{
			Label: i18nui.T(ctx, i18nui.KeyEntityCancel), Href: base, Variant: ui.ButtonGhost,
		}),
	})
	if !m.hasAPI {
		return render.Join(header, readOnlyNotice(ctx, m))
	}
	return render.Join(header, b.drawForm(ctx, m, nil, nil, nil, values, base))
}

// readOnlyNotice is what a create screen draws when the entity mounts
// no write routes: nothing to fill in, nothing to submit.
func readOnlyNotice(ctx context.Context, m *meta) render.HTML {
	return ui.Callout(ui.CalloutConfig{Variant: ui.StatusInfo},
		render.Text(i18nui.TVars(ctx, i18nui.KeyEntityReadOnly, map[string]string{"entity": m.singular(ctx)})))
}

// mayCreateSet reports whether a create may set the field: visible,
// not system, not Locked, not Omit, not guarded (the state field and
// stamps change only through a move).
func mayCreateSet(m *meta, f schema.Field) bool {
	if f.Hidden || m.system(f) || m.locked(f) || m.omitted(f.Name) {
		return false
	}
	return true
}

// editTab draws the record's Edit tab: the form, or the read-only
// field values when the entity mounts no write routes or the caller may
// not update this record. The form's
// inputs prefill from the raw row; its read-only values (the header's
// detail values) show the hooked row.
func (b *RecordBuilder) editTab(ctx context.Context, m *meta, raw, hooked map[string]any, masked map[string]bool, base string) render.HTML {
	return b.drawForm(ctx, m, raw, hooked, masked, nil, base)
}

// form assembles the create/edit form: the declared layout, the fields
// the layout leaves out appended in schema order, and the RPC contract
// (create POSTs the base, edit PUTs the record) with the leave guard.
func (b *RecordBuilder) drawForm(ctx context.Context, m *meta, raw, hooked map[string]any, masked map[string]bool, createValues map[string]string, base string) render.HTML {
	editable := b.editableFields(m)
	form := b.form
	if form == nil {
		form = m.d.Form
	}
	if masked == nil {
		masked = map[string]bool{}
	}
	// The form's field set: editable where the caller may write; on a
	// read-only mount, or for a caller the route would refuse, every
	// field draws as a locked value so nothing about it can submit.
	// A create form is drawn only after createScreen's own gates.
	writable := createValues != nil || canUpdate(ctx, m, b.id)
	locked := map[string]bool{}
	if !writable {
		for _, f := range editable {
			locked[f.Name] = true
		}
	}
	fb := formBuilder{b: b, m: m, row: raw, displayRow: hooked, masked: masked, locked: locked, create: createValues != nil, values: createValues}
	main, side, err := fb.walk(ctx, form)
	if err != nil {
		// The boot check and checkForm already refused bad layouts; a
		// Display form that names an omitted field is tolerated by
		// skipping it there.
		return slotFailed(ctx)
	}
	var body render.HTML
	if len(side) > 0 {
		body = ui.FormFrame(ui.FormFrameConfig{Main: main, Side: side})
	} else {
		body = render.Join(main...)
	}
	if !writable {
		// No write the caller may make: the tab is the frame of
		// read-only values, with no form around it — nothing to submit
		// and no action to point one at.
		return body
	}

	action := m.api
	rpc := interactive.Post(m.api)
	toast := i18nui.TVars(ctx, i18nui.KeyEntityCreated, map[string]string{"entity": m.singular(ctx)})
	dest := base
	if !fb.create {
		action = m.api + "/" + url.PathEscape(b.id)
		rpc = interactive.Put(action)
		toast = i18nui.T(ctx, i18nui.KeyEntitySaved)
		dest = base + "/" + url.PathEscape(b.id)
	}
	attrs := rpc.
		OnSuccessToast(toast).
		OnSuccess(interactive.Navigate(dest)).
		Attrs()
	forms := []render.HTML{ui.Form(ui.FormConfig{
		Action:      action,
		Method:      "POST",
		ID:          "eui-" + m.name + "-form",
		Ctx:         ctx,
		SubmitLabel: submitLabel(ctx, m, fb.create),
		ExtraAttrs:  attrs,
		LeaveGuard:  i18nui.T(ctx, i18nui.KeyEntityLeaveGuard),
	}, body)}
	// The masked fields' Replace forms: empty, their input and button
	// sit in the record form's markup and name them by the form
	// attribute. Each PUTs the one field it owns.
	for _, name := range fb.replace {
		forms = append(forms, ui.Form(ui.FormConfig{
			Action:     action,
			Method:     "POST",
			ID:         fb.replaceFormID(name),
			Ctx:        ctx,
			HideSubmit: true,
			ExtraAttrs: interactive.Put(action).
				OnSuccessToast(i18nui.T(ctx, i18nui.KeyEntitySaved)).
				OnSuccess(interactive.Navigate(dest)).
				Attrs(),
		}))
	}
	return render.Join(forms...)
}

// editableFields is the form's candidate set in schema order: visible,
// not system, not omitted. Locked fields stay: they render read-only.
func (b *RecordBuilder) editableFields(m *meta) []schema.Field {
	var out []schema.Field
	for _, f := range m.fields {
		if m.system(f) || m.omitted(f.Name) || m.hint(f.Name).Omit {
			continue
		}
		out = append(out, f)
	}
	return out
}

func submitLabel(ctx context.Context, m *meta, create bool) string {
	if create {
		return i18nui.TVars(ctx, i18nui.KeyEntityCreate, map[string]string{"entity": m.singular(ctx)})
	}
	return i18nui.T(ctx, i18nui.KeyEntitySave)
}

// formBuilder walks the declared layout and renders one field at a
// time, keeping track of what has been placed so the leftovers append
// in schema order.
type formBuilder struct {
	b   *RecordBuilder
	m   *meta
	row map[string]any // the raw row: what the inputs prefill from
	// displayRow is the hooked row: what read-only values show.
	displayRow map[string]any
	masked     map[string]bool
	locked     map[string]bool
	create     bool
	values     map[string]string
	placed     map[string]bool
	err        error
	// replace lists the masked fields drawn with their own Replace
	// form, in placement order; drawForm emits those forms after the
	// record form, which must not submit them.
	replace []string
}

// replaceFormID names the form a masked field's input and Replace
// button belong to.
func (fb *formBuilder) replaceFormID(field string) string {
	return "eui-" + fb.m.name + "-replace-" + field
}

// walk resolves the form layout: Main and Side children. A field the
// form leaves out goes at the end of Main in schema order.
func (fb *formBuilder) walk(ctx context.Context, f *entity.EntityForm) (main, side []render.HTML, err error) {
	fb.placed = map[string]bool{}
	if f == nil {
		f = &entity.EntityForm{}
	}
	main, err = fb.items(ctx, f.Main, 0)
	if err != nil {
		return nil, nil, err
	}
	side, err = fb.items(ctx, f.Side, 0)
	if err != nil {
		return nil, nil, err
	}
	for _, fl := range fb.b.editableFields(fb.m) {
		if fb.placed[fl.Name] {
			continue
		}
		if h := fb.field(ctx, fl); h != "" {
			main = append(main, h)
		}
	}
	return main, side, nil
}

// items renders one level of form items: fields, rows and sections.
func (fb *formBuilder) items(ctx context.Context, items []entity.FormItem, depth int) ([]render.HTML, error) {
	var out []render.HTML
	for _, it := range items {
		switch {
		case it.Field != "":
			f, ok := fb.m.field(it.Field)
			if !ok || fb.skipped(it.Field) {
				continue
			}
			if h := fb.field(ctx, f); h != "" {
				out = append(out, h)
			}
		case len(it.Row) > 0:
			var cells []render.HTML
			for _, name := range it.Row {
				f, ok := fb.m.field(name)
				if !ok || fb.skipped(name) {
					continue
				}
				if h := fb.field(ctx, f); h != "" {
					cells = append(cells, h)
				}
			}
			if len(cells) > 0 {
				out = append(out, ui.Grid(ui.GridConfig{}, cells...))
			}
		case it.Section != "":
			body, err := fb.section(ctx, it, depth)
			if err != nil {
				return nil, err
			}
			if body != "" {
				out = append(out, body)
			}
		}
	}
	return out, nil
}

// skipped reports whether a declared field is left out of this page's
// form: builder Omit, or the Display Omit hint.
func (fb *formBuilder) skipped(name string) bool {
	return fb.m.omitted(name) || slices.Contains(fb.b.omit, name)
}

// section renders a form section: a heading over its items, or a
// Collapsible that starts closed when declared so. A section whose
// fields are all hidden by ShowWhen draws nothing.
func (fb *formBuilder) section(ctx context.Context, it entity.FormItem, depth int) (render.HTML, error) {
	body, err := fb.items(ctx, it.Items, depth+1)
	if err != nil {
		return "", err
	}
	if len(body) == 0 {
		return "", nil
	}
	heading := i18nui.SectionLabel(ctx, fb.m.tr, fb.m.name, it.Section, "")
	if it.Collapsed {
		inner := body
		if it.Help != "" {
			inner = append([]render.HTML{html.Paragraph(html.TextConfig{}, render.Text(it.Help))}, inner...)
		}
		return ui.Collapsible(ui.CollapsibleConfig{Summary: heading, ID: "eui-sec-" + it.Section}, inner...), nil
	}
	return ui.Section(ui.SectionConfig{Heading: heading, Description: it.Help}, body...), nil
}

// value reads the field's current value as input text: the create
// prefill, else the (unhooked) stored row.
func (fb *formBuilder) value(f schema.Field) string {
	if fb.create {
		if fb.values != nil {
			return fb.values[f.Name]
		}
		return ""
	}
	if fb.row == nil {
		return ""
	}
	return formValueText(f, rowValue(fb.row, f.Name))
}

// field renders one field: read-only when locked or masked-write-only,
// wrapped in its ShowWhen condition when it has one, and the input the
// field's type (or kind) draws otherwise.
func (fb *formBuilder) field(ctx context.Context, f schema.Field) render.HTML {
	if fb.placed[f.Name] {
		return ""
	}
	fb.placed[f.Name] = true
	label := fb.m.label(ctx, f.Name)

	// A condition on the state field is decided here, against the
	// stored value: the field never submits, so the browser has no
	// controller to watch.
	if sw := fb.m.hint(f.Name).ShowWhen; sw != "" {
		cond, err := parseShowWhen(fb.m, sw)
		if err == nil && cond.field == fb.m.states.Field {
			current := ""
			if fb.row != nil {
				current = cell(rowValue(fb.row, fb.m.states.Field))
			} else if fb.create && fb.values != nil {
				current = fb.values[cond.field]
			}
			if !slices.Contains(cond.values, current) {
				return ""
			}
			return fb.control(ctx, f, label)
		}
	}

	h := fb.control(ctx, f, label)
	if sw := fb.m.hint(f.Name).ShowWhen; sw != "" && h != "" {
		if cond, err := parseShowWhen(fb.m, sw); err == nil {
			cfg := ui.ConditionalFieldConfig{WhenName: cond.field, Children: []render.HTML{h}}
			if len(cond.values) == 1 {
				cfg.WhenValue = cond.values[0]
			} else {
				cfg.WhenValues = cond.values
			}
			h = ui.ConditionalField(cfg)
		}
	}
	return h
}

// control draws the field's own row: read-only value, masked write-only
// input, or the input its type or kind picks.
func (fb *formBuilder) control(ctx context.Context, f schema.Field, label string) render.HTML {
	help := fb.m.help(ctx, f.Name)
	id := "eui-f-" + f.Name

	if fb.locked[f.Name] || fb.m.locked(f) {
		return fb.readOnly(ctx, f, label)
	}
	if fb.masked[f.Name] && !fb.create {
		return fb.maskedControl(ctx, f, label, help, id)
	}
	if kind, ok := fb.kind(f); ok {
		return fb.kindInput(ctx, f, label, help, id, kind)
	}
	return fb.typedInput(ctx, f, label, help, id)
}

// readOnly draws a locked field as a value, never a disabled input:
// nothing about it is submitted.
func (fb *formBuilder) readOnly(ctx context.Context, f schema.Field, label string) render.HTML {
	if k, ok := fb.kind(f); ok && (k.Detail != nil || k.Cell != nil) {
		if k.Detail != nil {
			return k.Detail(CellContext{Ctx: ctx, Entity: fb.m.name, Field: f, Value: fb.value(f), Row: fb.row})
		}
		return k.Cell(CellContext{Ctx: ctx, Entity: fb.m.name, Field: f, Value: fb.value(f), Row: fb.row})
	}
	var value render.HTML
	if fb.create {
		if v := fb.value(f); v != "" {
			value = render.Text(v)
		}
	} else if d := fb.displayRow; d != nil {
		value = fb.display(ctx, f, d)
	} else if fb.row != nil {
		value = fb.display(ctx, f, fb.row)
	}
	if value == "" {
		value = muted()
	}
	return ui.DetailList(ui.DetailListConfig{Items: []ui.DetailItem{{Label: label, Value: value}}})
}

// maskedControl draws a hook-masked column as write-only: the value
// never renders, a blank input keeps the stored value, and the help
// says whether one is set. A masked enum, bool or relation cannot use
// its native control (it would submit a value the user never saw), so
// it becomes a select with an explicit "unchanged" option.
func (fb *formBuilder) maskedControl(ctx context.Context, f schema.Field, label, help, id string) render.HTML {
	state := i18nui.T(ctx, i18nui.KeyEntityNotSet)
	if v := fb.value(f); v != "" {
		state = i18nui.T(ctx, i18nui.KeyEntitySet)
	}
	help = state + " · " + i18nui.T(ctx, i18nui.KeyEntityKeepValue)
	switch f.Type {
	case schema.Enum:
		return ui.Select(ui.SelectConfig{
			Name: f.Name, Label: label, ID: id, Help: help,
			Placeholder: i18nui.T(ctx, i18nui.KeyEntityUnchanged),
			Options:     enumOptions(ctx, fb.m, f, ""),
		})
	case schema.Bool:
		return ui.Select(ui.SelectConfig{
			Name: f.Name, Label: label, ID: id, Help: help,
			Placeholder: i18nui.T(ctx, i18nui.KeyEntityUnchanged),
			Options: []ui.SelectOption{
				{Value: "true", Text: i18nui.T(ctx, i18nui.KeyEntityYes)},
				{Value: "false", Text: i18nui.T(ctx, i18nui.KeyEntityNo)},
			},
		})
	case schema.Relation:
		return fb.relationSelect(ctx, f, label, help, id, "")
	default:
		// A blank text input cannot mean "keep": CRUD stores "" for
		// String and Text, so a record form that carried this input
		// would clear the column on every unrelated save. The input
		// and its Replace button belong to a form of their own (the
		// form attribute), which the record form never serializes;
		// that form writes this one field, and only a typed value.
		formID := fb.replaceFormID(f.Name)
		fb.replace = append(fb.replace, f.Name)
		help = state + " · " + i18nui.T(ctx, i18nui.KeyEntityReplaceHint)
		ph := i18nui.T(ctx, i18nui.KeyEntityNewValue)
		owner := html.Attrs{"form": formID}
		var input render.HTML
		if f.Type == schema.Text || f.Type == schema.JSON {
			input = ui.TextArea(ui.TextAreaConfig{
				Name: f.Name, Label: label, ID: id, Rows: 4, Help: help, Placeholder: ph,
				Required: true, ExtraAttrs: owner,
			})
		} else {
			input = ui.FormField(ui.FormFieldConfig{
				Label: label, For: id, Help: help, Required: true,
				Input: func(c headless.FieldControl) render.HTML {
					return ui.Control(ui.ControlConfig{
						Field: c, Type: inputType(f), Name: f.Name, Placeholder: ph,
						ExtraAttrs: owner,
					})
				},
			})
		}
		return ui.Stack(ui.StackConfig{Gap: ui.GapSM, Align: ui.AlignStart}, input,
			ui.Button(ui.ButtonConfig{
				Label: i18nui.T(ctx, i18nui.KeyEntityReplace), Type: "submit",
				Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall, ExtraAttrs: owner,
			}))
	}
}

// typedInput draws the input the field's own type picks.
func (fb *formBuilder) typedInput(ctx context.Context, f schema.Field, label, help, id string) render.HTML {
	val := fb.value(f)
	ph := fb.m.hint(f.Name).Placeholder
	required := f.Required
	switch f.Type {
	case schema.String:
		return ui.TextField(ui.TextFieldConfig{
			Name: f.Name, Label: label, ID: id, Value: val, Placeholder: ph,
			Help: help, Required: required,
		})
	case schema.Text:
		return ui.TextArea(ui.TextAreaConfig{
			Name: f.Name, Label: label, ID: id, Value: val, Rows: 4, Placeholder: ph,
			Help: help, Required: required,
		})
	case schema.JSON:
		return ui.TextArea(ui.TextAreaConfig{
			Name: f.Name, Label: label, ID: id, Value: val, Rows: 6, Placeholder: ph,
			Help: help, Required: required, Monospace: true,
		})
	case schema.Int:
		return ui.NumberInput(ui.NumberInputConfig{
			Name: f.Name, Label: label, ID: id, Value: intInputValue(val), Ctx: ctx,
			Help: help, Required: required, Min: intBound(f.Min), Max: intBound(f.Max),
		})
	case schema.Float, schema.Decimal:
		// step must let the type's precision through: a bare number
		// input steps by 1 and refuses 0.5.
		step := 0.01
		if f.Type == schema.Float {
			step = 1e-9
		}
		return ui.NumberField(ui.NumberFieldConfig{
			Name: f.Name, Label: label, ID: id, Value: val, Placeholder: ph,
			Help: help, Required: required, Min: f.Min, Max: f.Max, Step: &step,
		})
	case schema.Bool:
		// The hidden-false pair: the browser submits "on" for a
		// checked box and nothing for an unchecked one, and the
		// runtime's serializer folds exactly this pair into one
		// scalar, so false round-trips.
		return render.Join(
			html.Input(html.InputConfig{Type: "hidden", Name: f.Name, Value: "false"}),
			ui.Switch(ui.ToggleConfig{Name: f.Name, Label: label, ID: id, Value: "true", Checked: truthy(val), Help: help}),
		)
	case schema.Enum:
		if len(f.Values) <= 4 && required {
			return ui.SegmentedControl(ui.SegmentedControlConfig{
				Name: f.Name, ID: id, Label: label,
				Options:  enumSegments(ctx, fb.m, f),
				Selected: val,
			})
		}
		return ui.Select(ui.SelectConfig{
			Name: f.Name, Label: label, ID: id, Help: help,
			Placeholder: selectPlaceholder(ctx, f, required),
			Options:     enumOptions(ctx, fb.m, f, val),
			Required:    required,
		})
	case schema.Date:
		return ui.DateField(ui.DateFieldConfig{
			Name: f.Name, Label: label, ID: id, Value: dateInputValue(val),
			Help: help, Required: required,
		})
	case schema.Timestamp:
		return ui.FormField(ui.FormFieldConfig{
			Label: label, For: id, Help: help, Required: required,
			Input: func(c headless.FieldControl) render.HTML {
				return ui.Control(ui.ControlConfig{
					Field: c, Type: "datetime-local", Name: f.Name, Value: timestampInputValue(val),
				})
			},
		})
	case schema.Relation:
		return fb.relationSelect(ctx, f, label, help, id, val)
	case schema.UUID:
		if fb.create {
			return ui.TextField(ui.TextFieldConfig{
				Name: f.Name, Label: label, ID: id, Value: val, Placeholder: ph, Help: help,
			})
		}
		// A stored UUID is the server's: read-only with a copy affordance.
		return fb.readOnly(ctx, f, label)
	default:
		return ui.FormField(ui.FormFieldConfig{
			Label: label, For: id, Help: help,
			Input: func(c headless.FieldControl) render.HTML {
				return ui.Control(ui.ControlConfig{Field: c, Type: "text", Name: f.Name, Value: val, Placeholder: ph})
			},
		})
	}
}

// relationSelect draws a belongs-to picker: a Select of at most 100
// related records read through that entity's own crud handle and read
// gate, showing the pk and the title field.
func (fb *formBuilder) relationSelect(ctx context.Context, f schema.Field, label, help, id, cur string) render.HTML {
	opts := []ui.SelectOption{}
	if cur == "" {
		opts = append(opts, ui.SelectOption{Value: "", Text: i18nui.T(ctx, i18nui.KeyEntitySelect)})
	}
	related, refused := fb.relationOptions(ctx, f)
	for _, o := range related {
		opts = append(opts, ui.SelectOption{Value: o.id, Text: o.label, Selected: o.id == cur})
	}
	if cur != "" && !optionListed(opts, cur) {
		// The current value was not among the first 100 (or the picker
		// read none): offer it anyway so a submit cannot silently
		// clear the column. Its label is the id when the caller may
		// read the related entity (a legibility limit), and the em dash
		// when it may not: never the raw foreign key of a refused entity.
		text := cur
		if refused {
			text = "—"
		}
		opts = append([]ui.SelectOption{{Value: cur, Text: text, Selected: true}}, opts...)
	}
	return ui.Select(ui.SelectConfig{
		Name: f.Name, Label: label, ID: id, Options: opts, Help: help,
		Required: f.Required && !fb.masked[f.Name],
	})
}

// optionListed reports whether one option already carries value.
func optionListed(opts []ui.SelectOption, value string) bool {
	for _, o := range opts {
		if o.Value == value {
			return true
		}
	}
	return false
}

// relationOption is one row of a relation picker.
type relationOption struct {
	id    string
	label string
}

// relationOptions reads the related entity's first 100 rows through
// its own handler after its own read gate: a picker for an open entity
// must not become a window onto a gated one. The read runs with hooks
// so the labels show what a hooked read shows. refused reports that the
// related entity is unknown or its read gate refused the caller.
func (fb *formBuilder) relationOptions(ctx context.Context, f schema.Field) (opts []relationOption, refused bool) {
	other, err := fb.b.ui.entityFor(f.To)
	if err != nil {
		return nil, true
	}
	om, err := fb.b.ui.meta(other.GetName())
	if err != nil {
		return nil, true
	}
	if !canRead(ctx, om.ch) {
		return nil, true
	}
	fields := []string{om.pk}
	if tf := om.titleField(); tf != "" && tf != om.pk {
		fields = append(fields, tf)
	}
	rows, err := om.ch.ListAll(crud.WithReadHooks(ctx), crud.ListOptions{Fields: fields, Limit: 100, Sorts: []filter.ParsedSort{{Field: om.pk}}})
	if err != nil {
		return nil, false
	}
	out := make([]relationOption, 0, len(rows))
	for _, r := range rows {
		oid := cell(rowValue(r, om.pk))
		if oid == "" {
			continue
		}
		label := oid
		if tf := om.titleField(); tf != "" {
			if l := cell(rowValue(r, tf)); l != "" {
				label = l
			}
		}
		out = append(out, relationOption{id: oid, label: label})
	}
	return out, false
}

// kindInput draws the input a registered kind (or a built-in one)
// picks for the field. An app kind's Input runs inside a recover: its
// panic renders the type's default input and logs.
func (fb *formBuilder) kindInput(ctx context.Context, f schema.Field, label, help, id string, kind Kind) render.HTML {
	val := fb.value(f)
	ph := fb.m.hint(f.Name).Placeholder
	if kind.Input == nil {
		return fb.typedInput(ctx, f, label, help, id)
	}
	// The kind's ctx carries the app's translator so its labels
	// resolve through the same catalog the form's own fields read, and
	// Control carries the wiring a FormField would have handed down.
	kctx := i18nui.WithTranslator(ctx, fb.m.tr)
	return contain(kctx, fb.m.name, "input "+f.Name, func() (render.HTML, error) {
		return kind.Input(InputContext{
			Ctx: kctx, Entity: fb.m.name, Field: f, Name: f.Name, Value: val, Placeholder: ph,
			Control: headless.FieldControl{ID: id, Required: f.Required},
		}), nil
	})
}

// kind resolves the field's input kind: an app kind registered under
// the hint's name, else a built-in one. The second return is false
// when the field names no kind.
func (fb *formBuilder) kind(f schema.Field) (Kind, bool) {
	name := fb.m.hint(f.Name).Input
	if name == "" {
		return Kind{}, false
	}
	if k, ok := fb.b.ui.ext.Kinds[name]; ok {
		return k, true
	}
	if isBuiltinKind(name) {
		return builtinKind(name), true
	}
	return Kind{}, false
}

// maskedFields reports which of the entity's editable columns an
// AfterGet hook rewrites, by comparing the stored row against the
// hooked one — the password-field pattern applied to whatever the app
// decided to mask. NoQuery is not consulted: it answers the query
// surface, and an app may mask a column it never marked. A hook
// failure already failed the screen, so both reads succeeded here.
func maskedFields(m *meta, raw, hooked map[string]any) map[string]bool {
	masked := map[string]bool{}
	for _, f := range m.fields {
		if m.system(f) || m.locked(f) {
			continue
		}
		if cell(rowValue(raw, f.Name)) != cell(rowValue(hooked, f.Name)) {
			masked[f.Name] = true
		}
	}
	return masked
}

// showWhenCond is a parsed ShowWhen: the controller field and the
// values that show the target.
type showWhenCond struct {
	field  string
	values []string
}

// parseShowWhen parses the ShowWhen DSL the same shape checkShowWhen
// allowed at registration: one `field = value` or `field in [...]`
// term. Anything unexpected parses to an error and the field renders
// unconditioned rather than vanishing.
func parseShowWhen(m *meta, text string) (showWhenCond, error) {
	fields := slices.Clone(m.fields)
	for i := range fields {
		fields[i].NoQuery = false
	}
	p, err := dsl.ParsePredicate(text, fields)
	if err != nil || p == nil || (p.Op != filter.OpEq && p.Op != filter.OpIn) {
		return showWhenCond{}, fmt.Errorf("entityui: show_when %q is not one term", text)
	}
	values := p.Values
	if p.Op == filter.OpEq {
		values = []string{p.Value}
	}
	if len(values) == 0 {
		return showWhenCond{}, fmt.Errorf("entityui: show_when %q names no values", text)
	}
	return showWhenCond{field: p.Field, values: values}, nil
}

// enumOptions builds a Select's options over the field's values, the
// current one selected.
func enumOptions(ctx context.Context, m *meta, f schema.Field, cur string) []ui.SelectOption {
	opts := make([]ui.SelectOption, 0, len(f.Values))
	for _, v := range f.Values {
		opts = append(opts, ui.SelectOption{
			Value: v, Text: m.valueLabel(ctx, f.Name, v), Selected: v == cur,
		})
	}
	return opts
}

// enumSegments builds a SegmentedControl's options.
func enumSegments(ctx context.Context, m *meta, f schema.Field) []ui.SegmentedOption {
	opts := make([]ui.SegmentedOption, 0, len(f.Values))
	for _, v := range f.Values {
		opts = append(opts, ui.SegmentedOption{Value: v, Label: m.valueLabel(ctx, f.Name, v)})
	}
	return opts
}

func selectPlaceholder(ctx context.Context, f schema.Field, required bool) string {
	if required || len(f.Values) == 0 {
		return ""
	}
	return i18nui.T(ctx, i18nui.KeyEntitySelect)
}

// formValueText renders a stored value for an <input> of the field's
// type: dates as YYYY-MM-DD (an input[type=date] silently blanks
// anything else, and a round-tripped date column came back empty and
// wiped itself on save), timestamps as datetime-local, bools as
// true/false, everything else as plain text.
func formValueText(f schema.Field, v any) string {
	// The unnamed types (String, Text, UUID, Int, Float, Decimal,
	// Enum, Relation, JSON, Image, File) all take the plain-text fall
	// through below.
	switch f.Type {
	case schema.Date:
		switch t := v.(type) {
		case time.Time:
			return t.Format(time.DateOnly)
		case *time.Time:
			if t == nil {
				return ""
			}
			return t.Format(time.DateOnly)
		}
	case schema.Timestamp:
		switch t := v.(type) {
		case time.Time:
			return t.Format("2006-01-02T15:04")
		case *time.Time:
			if t == nil {
				return ""
			}
			return t.Format("2006-01-02T15:04")
		}
	case schema.Bool:
		return boolText(v)
	default:
		// String, Text, UUID, Int, Float, Decimal, Enum, Relation,
		// JSON, Image and File: the input's value is the plain text
		// form, whatever the driver handed back.
	}
	return cell(v)
}

// boolText spells a stored bool the way the form submits it.
func boolText(v any) string {
	switch t := v.(type) {
	case bool:
		if t {
			return "true"
		}
		return "false"
	case string:
		if truthy(t) {
			return "true"
		}
		return "false"
	case nil:
		return "false"
	}
	return cell(v)
}

// inputType maps a field type to a plain <input type=...>.
func inputType(f schema.Field) string {
	switch f.Type {
	case schema.Int, schema.Float, schema.Decimal:
		return "number"
	case schema.Date:
		return "date"
	case schema.Timestamp:
		return "datetime-local"
	case schema.UUID:
		return "text"
	default:
		return "text"
	}
}

func intInputValue(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// intBound narrows a float bound to the int NumberInput takes.
func intBound(b *float64) int {
	if b == nil {
		return 0
	}
	return int(*b)
}

func dateInputValue(s string) string {
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		return s[:10]
	}
	return ""
}

func timestampInputValue(s string) string {
	if len(s) >= 16 && s[4] == '-' && s[7] == '-' {
		return s[:16]
	}
	return ""
}
