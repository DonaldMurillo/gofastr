package entityui

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	appui "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/interactive"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/dsl"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func (b *RecordBuilder) createScreen(ctx context.Context, m *meta, base string) render.HTML {
	if !canRead(ctx, m.ch) {
		return accessDenied(ctx, m.plural(ctx), m.plural(ctx), 1)
	}
	if m.hasAPI && !m.ch.CanCreateScoped(ctx) {
		return accessDenied(ctx, m.plural(ctx), m.plural(ctx), 1)
	}
	values := map[string]string{}
	for _, f := range m.fields {
		if f.Default != nil && mayCreateSet(m, f) {
			values[f.Name] = formValueText(f, f.Default)
		}
	}
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
		if !canReadRecord(ctx, m.ch, dup) {
			return m.notFound(ctx)
		}
		hooked, herr := m.getRow(ctx, dup)
		if herr != nil || hooked == nil {
			return m.notFound(ctx)
		}
		row, err := m.getRowUnhooked(ctx, dup)
		if err != nil || row == nil {
			return m.notFound(ctx)
		}
		blank := uniqueBlank(m)
		for _, f := range m.fields {
			if !mayCreateSet(m, f) || blank[f.Name] {
				continue
			}
			if cell(m.rowValue(row, f.Name)) != cell(m.rowValue(hooked, f.Name)) {
				continue
			}
			if v := formValueText(f, m.rowValue(row, f.Name)); v != "" {
				values[f.Name] = v
			}
		}
	}
	cfg := ui.PageHeaderConfig{
		Title: i18nui.EntityNew.Text(ctx, map[string]any{"entity": m.singular(ctx)}),
	}
	if !m.hasAPI {
		return render.Join(drawerBar(ctx, "", ""), ui.PageHeader(cfg), readOnlyNotice(ctx, m))
	}
	cfg.Actions = ui.Cluster(ui.ClusterConfig{Gap: ui.GapSM, Align: ui.AlignCenter},
		ui.LinkButton(ui.LinkButtonConfig{
			Label: i18nui.EntityCancel.Text(ctx), Href: base, Variant: ui.ButtonGhost,
		}),
		saveButton(ctx, m, true),
	)
	return render.Join(drawerBar(ctx, "", ""), ui.PageHeader(cfg), b.drawForm(ctx, m, nil, nil, nil, values, base))
}

func uniqueBlank(m *meta) map[string]bool {
	out := map[string]bool{}
	for _, f := range m.fields {
		if f.Unique {
			out[f.Name] = true
		}
	}
	var sets [][]string
	if len(m.keys) > 0 {
		sets = append(sets, m.keys)
	}
	for _, ix := range m.e.Config.Indices {
		if ix.Unique && ix.Expression == "" {
			sets = append(sets, ix.Columns)
		}
	}
	for _, cols := range sets {
		onlyRelations := !slices.ContainsFunc(cols, func(c string) bool {
			f, ok := m.field(c)
			return ok && f.Type != schema.Relation
		})
		for _, c := range cols {
			if f, ok := m.field(c); ok && (onlyRelations || f.Type != schema.Relation) {
				out[c] = true
			}
		}
	}
	return out
}

func readOnlyNotice(ctx context.Context, m *meta) render.HTML {
	return ui.Callout(ui.CalloutConfig{Variant: ui.StatusInfo},
		render.Text(i18nui.EntityReadOnly.Text(ctx, map[string]any{"entity": m.singular(ctx)})))
}

func mayCreateSet(m *meta, f schema.Field) bool {
	return !f.Hidden && !m.createLocked(f) && !m.omitted(f.Name)
}

func (b *RecordBuilder) editTab(ctx context.Context, m *meta, raw, hooked map[string]any, masked map[string]bool, base string) render.HTML {
	return b.drawForm(ctx, m, raw, hooked, masked, nil, base)
}

func (b *RecordBuilder) drawForm(ctx context.Context, m *meta, raw, hooked map[string]any, masked map[string]bool, createValues map[string]string, base string) render.HTML {
	editable := b.editableFields(m)
	form := b.form
	if form == nil {
		form = m.d.Form
	}
	if masked == nil {
		masked = map[string]bool{}
	}
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
		return slotFailed(ctx)
	}
	if !fb.create {
		if d := fb.details(ctx); d != "" {
			side = append(side, d)
		}
		side = append(side, fb.sidePanels(ctx)...)
	}
	var body render.HTML
	if len(side) > 0 {
		body = ui.FormFrame(ui.FormFrameConfig{Main: main, Side: side, SidePanel: true, SideWidth: ui.FormFrameSideWide})
	} else {
		body = render.Join(main...)
	}
	if !writable {
		return body
	}

	action := m.api
	rpc := interactive.Post(m.api)
	toast := i18nui.EntityCreated.Text(ctx, map[string]any{"entity": m.singular(ctx)})
	dest := base
	if o := appui.OverlayOriginFromContext(ctx); o != "" {
		dest = o
	}
	if !fb.create {
		action = m.api + "/" + m.idPath(b.id)
		rpc = interactive.Put(action)
		toast = i18nui.EntitySaved.Text(ctx)
		dest = under(base, m.idPath(b.id))
	}
	native := action
	if !fb.create {
		native = b.ui.saveAction(action, dest)
	}
	attrs := rpc.
		OnSuccessToast(toast).
		OnSuccess(interactive.Navigate(dest)).
		Attrs()
	forms := []render.HTML{ui.Form(ui.FormConfig{
		Action:           native,
		Method:           "POST",
		ID:               recordFormID(m),
		Ctx:              ctx,
		HideSubmit:       true,
		Multipart:        true,
		Wide:             len(side) > 0,
		ExtraAttrs:       attrs,
		LeaveGuard:       i18nui.EntityLeaveGuard.Text(ctx),
		LeaveGuardTitle:  i18nui.EntityLeaveGuardTitle.Text(ctx),
		LeaveGuardAccept: i18nui.EntityLeaveGuardAccept.Text(ctx),
	}, body)}
	for _, name := range fb.replace {
		forms = append(forms, ui.Form(ui.FormConfig{
			Action:     native,
			Method:     "POST",
			ID:         fb.replaceFormID(name),
			Ctx:        ctx,
			HideSubmit: true,
			Multipart:  true,
			ExtraAttrs: interactive.Put(action).
				OnSuccessToast(i18nui.EntitySaved.Text(ctx)).
				OnSuccess(interactive.Navigate(dest)).
				Attrs(),
		}))
	}
	return render.Join(forms...)
}

func (b *RecordBuilder) editableFields(m *meta) []schema.Field {
	var out []schema.Field
	for _, f := range m.fields {
		if m.system(f) || m.serverOwned(f.Name) || m.omitted(f.Name) || m.hint(f.Name).Omit {
			continue
		}
		out = append(out, f)
	}
	return out
}

func submitLabel(ctx context.Context, m *meta, create bool) string {
	if create {
		return i18nui.EntityCreate.Text(ctx, map[string]any{"entity": m.singular(ctx)})
	}
	return i18nui.EntitySave.Text(ctx)
}

type formBuilder struct {
	b          *RecordBuilder
	m          *meta
	row        map[string]any // the raw row: what the inputs prefill from
	displayRow map[string]any
	masked     map[string]bool
	locked     map[string]bool
	create     bool
	values     map[string]string
	placed     map[string]bool
	err        error
	replace    []string
	compact    bool
}

func (fb *formBuilder) replaceFormID(field string) string {
	return "eui-" + fb.m.name + "-replace-" + field
}

func (fb *formBuilder) sidePanels(ctx context.Context) []render.HTML {
	m := fb.m
	row := fb.displayRow
	if row == nil {
		row = fb.row
	}
	rec := Record{ID: fb.b.id, Values: row}
	var out []render.HTML
	for _, p := range m.ext.Side {
		body := contain(ctx, m.name, "side "+p.Key, func() (render.HTML, error) {
			cctx := asCaller(ctx)
			c, err := p.Build(RecordContext{Ctx: cctx, UI: fb.b.ui, Entity: m.name, Record: rec})
			if err != nil {
				return "", err
			}
			if c == nil {
				return "", fmt.Errorf("side panel body is nil")
			}
			return renderComponent(cctx, c), nil
		})
		title := p.Key
		if !p.Title.IsZero() {
			title = p.Title.Text(ctx)
		}
		out = append(out, ui.Section(ui.SectionConfig{Ctx: ctx, ID: "eui-side-" + p.Key, Heading: title, Overline: true, Compact: true}, body))
	}
	return out
}

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
		if fb.placed[fl.Name] || fb.m.guarded(fl.Name) {
			continue
		}
		if h := fb.field(ctx, fl); h != "" {
			main = append(main, h)
		}
	}
	return main, side, nil
}

func (fb *formBuilder) details(ctx context.Context) render.HTML {
	m, row := fb.m, fb.displayRow
	if row == nil {
		row = fb.row
	}
	if row == nil {
		return ""
	}
	var items []ui.DetailItem
	if id := cell(m.rowValue(row, m.pk)); m.pk != "" && id != "" {
		target := "eui-" + m.name + "-id"
		items = append(items, ui.DetailItem{
			Label: i18nui.EntityID.Text(ctx),
			Value: render.Join(
				html.Code(html.TextConfig{ID: target, ExtraAttrs: html.Attrs{"title": id}}, render.Text(id)),
				ui.CopyButton(ui.CopyButtonConfig{
					Target: target, IconOnly: true, Icon: "copy", Inline: true, Ctx: ctx,
					AriaLabel: i18nui.CopyToClipboard.Text(ctx),
				})),
		})
	}
	names := []string{"created_at", "updated_at"}
	if m.states != nil {
		names = append(names, m.states.Guarded()[1:]...)
	}
	for _, name := range names {
		f, ok := m.field(name)
		if !ok || f.Hidden || fb.placed[name] || fb.skipped(name) {
			continue
		}
		v := fb.display(ctx, f, row)
		if v == "" {
			v = muted()
		}
		items = append(items, ui.DetailItem{Label: m.label(ctx, name), Value: v})
	}
	if len(items) == 0 {
		return ""
	}
	return ui.Section(ui.SectionConfig{Ctx: ctx, Heading: i18nui.EntityDetails.Text(ctx), Overline: true, Compact: true},
		ui.DetailList(ui.DetailListConfig{Spread: true, Items: items}))
}

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

func (fb *formBuilder) skipped(name string) bool {
	return fb.m.omitted(name) || slices.Contains(fb.b.omit, name)
}

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
	return ui.Section(ui.SectionConfig{Ctx: ctx, Heading: heading, Description: it.Help}, body...), nil
}

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
	return formValueText(f, fb.m.rowValue(fb.row, f.Name))
}

func (fb *formBuilder) field(ctx context.Context, f schema.Field) render.HTML {
	if fb.placed[f.Name] {
		return ""
	}
	fb.placed[f.Name] = true
	label := fb.m.label(ctx, f.Name)

	if sw := fb.m.hint(f.Name).ShowWhen; sw != "" {
		cond, err := parseShowWhen(fb.m, sw)
		if err == nil && fb.m.states != nil && cond.field == fb.m.states.Field {
			current := ""
			if fb.row != nil {
				current = cell(fb.m.rowValue(fb.row, fb.m.states.Field))
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
			cfg := ui.ConditionalFieldConfig{WhenName: fb.m.inputName(cond.field), Children: []render.HTML{h}}
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

func (fb *formBuilder) control(ctx context.Context, f schema.Field, label string) render.HTML {
	help := fb.m.help(ctx, f.Name)
	id := "eui-f-" + f.Name

	if fb.locked[f.Name] || (fb.create && fb.m.createLocked(f)) || (!fb.create && fb.m.locked(f)) {
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

func (fb *formBuilder) readOnly(ctx context.Context, f schema.Field, label string) render.HTML {
	if k, ok := fb.kind(f); ok && (k.Detail != nil || k.Cell != nil) {
		row, val := fb.row, fb.value(f)
		if !fb.create && fb.displayRow != nil {
			row, val = fb.displayRow, formValueText(f, fb.m.rowValue(fb.displayRow, f.Name))
		}
		if f.Type == schema.Relation && val != "" && !fb.relationReadable(ctx, f, val) {
			return ui.DetailList(ui.DetailListConfig{Stacked: true, Items: []ui.DetailItem{{Label: label, Value: muted()}}})
		}
		cc := CellContext{Ctx: asCaller(ctx), Entity: fb.m.name, Field: f, Value: val, Row: row}
		draw := k.Cell
		if k.Detail != nil {
			draw = k.Detail
		}
		return ui.DetailList(ui.DetailListConfig{Stacked: true, Items: []ui.DetailItem{{Label: label, Value: draw(cc)}}})
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
	return ui.DetailList(ui.DetailListConfig{Stacked: true, Items: []ui.DetailItem{{Label: label, Value: value}}})
}

func (fb *formBuilder) maskedControl(ctx context.Context, f schema.Field, label, help, id string) render.HTML {
	state := i18nui.EntityNotSet.Text(ctx)
	if v := fb.value(f); v != "" {
		state = i18nui.EntitySet.Text(ctx)
	}
	help = state + " · " + i18nui.EntityKeepValue.Text(ctx)
	switch f.Type {
	case schema.Enum:
		return ui.Select(ui.SelectConfig{Ctx: ctx,
			Name: fb.m.inputName(f.Name), Label: label, ID: id, Help: help,
			Placeholder: i18nui.EntityUnchanged.Text(ctx),
			Options:     enumOptions(ctx, fb.m, f, ""),
		})
	case schema.Bool:
		return ui.Select(ui.SelectConfig{Ctx: ctx,
			Name: fb.m.inputName(f.Name), Label: label, ID: id, Help: help,
			Placeholder: i18nui.EntityUnchanged.Text(ctx),
			Options: []ui.SelectOption{
				{Value: "true", Text: i18nui.EntityYes.Text(ctx)},
				{Value: "false", Text: i18nui.EntityNo.Text(ctx)},
			},
		})
	case schema.Relation:
		if !fb.m.hasAPI {
			return fb.readOnly(ctx, f, label)
		}
		formID := fb.replaceFormID(f.Name)
		fb.replace = append(fb.replace, f.Name)
		help = state + " · " + i18nui.EntityReplaceHint.Text(ctx)
		return ui.Stack(ui.StackConfig{Gap: ui.GapSM, Align: ui.AlignStart},
			fb.relationPicker(ctx, f, label, help, id, "", formID),
			replaceButton(ctx, formID))
	default:
		formID := fb.replaceFormID(f.Name)
		fb.replace = append(fb.replace, f.Name)
		help = state + " · " + i18nui.EntityReplaceHint.Text(ctx)
		ph := i18nui.EntityNewValue.Text(ctx)
		owner := html.Attrs{"form": formID}
		var input render.HTML
		if f.Type == schema.Text || f.Type == schema.JSON {
			input = ui.TextArea(ui.TextAreaConfig{
				Ctx:  ctx,
				Name: fb.m.inputName(f.Name), Label: label, ID: id, Rows: 4, Help: help, Placeholder: ph,
				Required: true, ExtraAttrs: owner,
			})
		} else {
			input = ui.FormField(ui.FormFieldConfig{
				Label: label, For: id, Help: help, Required: true,
				Input: func(c headless.FieldControl) render.HTML {
					return ui.Control(ui.ControlConfig{
						Field: c, Type: inputType(f), Name: fb.m.inputName(f.Name), Placeholder: ph,
						ExtraAttrs: owner,
					})
				},
			})
		}
		return ui.Stack(ui.StackConfig{Gap: ui.GapSM, Align: ui.AlignStart}, input,
			replaceButton(ctx, formID))
	}
}

func replaceButton(ctx context.Context, formID string) render.HTML {
	return ui.Button(ui.ButtonConfig{
		Ctx:   ctx,
		Label: i18nui.EntityReplace.Text(ctx), Type: "submit",
		Variant: ui.ButtonSecondary, Size: ui.ButtonSizeSmall, ExtraAttrs: html.Attrs{"form": formID},
	})
}

func (fb *formBuilder) typedInput(ctx context.Context, f schema.Field, label, help, id string) render.HTML {
	val := fb.value(f)
	ph := fb.m.hint(f.Name).Placeholder
	required := f.Required
	switch f.Type {
	case schema.String:
		return ui.TextField(ui.TextFieldConfig{Ctx: ctx,
			Name: fb.m.inputName(f.Name), Label: label, ID: id, Value: val, Placeholder: ph,
			Help: help, Required: required, LabelHidden: fb.compact,
			ExtraAttrs: patternAttr(f.Pattern),
		})
	case schema.Text:
		return ui.TextArea(ui.TextAreaConfig{
			Ctx:  ctx,
			Name: fb.m.inputName(f.Name), Label: label, ID: id, Value: val, Rows: 4, Placeholder: ph,
			Help: help, Required: required,
		})
	case schema.JSON:
		return ui.TextArea(ui.TextAreaConfig{
			Name: fb.m.inputName(f.Name), Label: label, ID: id, Value: val, Rows: 6, Placeholder: ph,
			Help: help, Required: required, Monospace: true, JSON: true, Ctx: ctx,
		})
	case schema.Int:
		if fb.compact || !required {
			step := 1.0
			return ui.NumberField(ui.NumberFieldConfig{Ctx: ctx,
				Name: fb.m.inputName(f.Name), Label: label, ID: id, Value: val, LabelHidden: fb.compact,
				Help: help, Required: required, Min: f.Min, Max: f.Max, Step: &step,
			})
		}
		return ui.NumberInput(ui.NumberInputConfig{
			Name: fb.m.inputName(f.Name), Label: label, ID: id, Value: intInputValue(val), Ctx: ctx,
			Help: help, Required: required, Min: intBound(f.Min), Max: intBound(f.Max),
		})
	case schema.Float, schema.Decimal:
		step := decimalStep(val)
		if f.Type == schema.Float {
			step = 1e-9
		}
		return ui.NumberField(ui.NumberFieldConfig{Ctx: ctx,
			Name: fb.m.inputName(f.Name), Label: label, ID: id, Value: val, Placeholder: ph, LabelHidden: fb.compact,
			Help: help, Required: required, Min: f.Min, Max: f.Max, Step: &step,
		})
	case schema.Bool:
		if !required && !fb.create && val == "" {
			return ui.Select(ui.SelectConfig{Ctx: ctx,
				Name: fb.m.inputName(f.Name), Label: label, ID: id, Help: help, LabelHidden: fb.compact,
				Options: []ui.SelectOption{
					{Value: "", Text: i18nui.EntityNotSet.Text(ctx), Selected: true},
					{Value: "true", Text: i18nui.EntityYes.Text(ctx)},
					{Value: "false", Text: i18nui.EntityNo.Text(ctx)},
				},
			})
		}
		return render.Join(
			html.Input(html.InputConfig{Type: "hidden", Name: fb.m.inputName(f.Name), Value: "false"}),
			ui.Switch(ui.ToggleConfig{Ctx: ctx, Name: fb.m.inputName(f.Name), Label: label, ID: id, Value: "true", Checked: truthy(val), Help: help, LabelHidden: fb.compact}),
		)
	case schema.Enum:
		stale := val != "" && !slices.Contains(f.Values, val)
		if len(f.Values) >= 2 && len(f.Values) <= 4 && required && !fb.compact && !stale {
			return ui.SegmentedControl(ui.SegmentedControlConfig{
				Name: fb.m.inputName(f.Name), ID: id, Label: label,
				Options:  enumSegments(ctx, fb.m, f),
				Selected: val,
			})
		}
		opts, ph := enumOptions(ctx, fb.m, f, val), ""
		switch {
		case stale:
			ph = i18nui.EntitySelect.Text(ctx)
		case required || len(f.Values) == 0:
		default:
			opts = append([]ui.SelectOption{{
				Value: "", Text: i18nui.EntitySelect.Text(ctx), Selected: val == "",
			}}, opts...)
		}
		return ui.Select(ui.SelectConfig{Ctx: ctx,
			Name: fb.m.inputName(f.Name), Label: label, ID: id, Help: help, LabelHidden: fb.compact,
			Placeholder: ph, Options: opts, Required: required,
		})
	case schema.Date:
		return ui.DateField(ui.DateFieldConfig{Ctx: ctx,
			Name: fb.m.inputName(f.Name), Label: label, ID: id, Value: dateInputValue(val),
			Help: help, Required: required, LabelHidden: fb.compact,
		})
	case schema.Timestamp:
		v := timestampInputValue(val)
		return ui.DateTimeField(ui.DateTimeFieldConfig{Ctx: ctx,
			Name: fb.m.inputName(f.Name), Label: label, ID: id, Value: v, Step: timestampStep(v),
			Help: help, Required: required,
		})
	case schema.Relation:
		return fb.relationPicker(ctx, f, label, help, id, val, "")
	case schema.Image, schema.File:
		if fb.m.uploads() {
			return fb.uploadInput(ctx, f, label, help, id, val, required)
		}
		field := ui.TextField(ui.TextFieldConfig{Ctx: ctx,
			Name: fb.m.inputName(f.Name), Label: label, ID: id, Value: val, Placeholder: ph,
			Help: help, Required: required,
		})
		if t := fb.b.ui.fileValue(f, label, val, ui.ThumbnailLG); t != "" {
			return ui.Stack(ui.StackConfig{Gap: ui.GapSM}, t, field)
		}
		return field
	case schema.UUID:
		if fb.create {
			return ui.TextField(ui.TextFieldConfig{Ctx: ctx,
				Name: fb.m.inputName(f.Name), Label: label, ID: id, Value: val, Placeholder: ph, Help: help,
			})
		}
		return fb.readOnly(ctx, f, label)
	default:
		return ui.FormField(ui.FormFieldConfig{
			Label: label, For: id, Help: help,
			Input: func(c headless.FieldControl) render.HTML {
				return ui.Control(ui.ControlConfig{Field: c, Type: "text", Name: fb.m.inputName(f.Name), Value: val, Placeholder: ph})
			},
		})
	}
}

func (fb *formBuilder) relationOpen(ctx context.Context, f schema.Field, cur string) render.HTML {
	if cur == "" {
		return ""
	}
	base, ok := fb.b.ui.relatedBase(ctx, fb.m, f.Name)
	if !ok || !fb.relationReadable(ctx, f, cur) {
		return ""
	}
	other, err := fb.b.ui.entityFor(f.To)
	if err != nil {
		return ""
	}
	om, err := fb.b.ui.meta(other.GetName())
	if err != nil {
		return ""
	}
	if row, err := om.getRow(ctx, cur); err != nil || row == nil {
		return ""
	}
	return ui.LinkButton(ui.LinkButtonConfig{
		Label:    i18nui.EntityOpen.Text(ctx, map[string]any{"entity": om.singular(ctx)}),
		Href:     under(base, url.PathEscape(cur)),
		Variant:  ui.ButtonSecondary,
		Icon:     "arrow-up-right",
		IconOnly: true,
	})
}

func (fb *formBuilder) kindInput(ctx context.Context, f schema.Field, label, help, id string, kind Kind) render.HTML {
	val := fb.value(f)
	ph := fb.m.hint(f.Name).Placeholder
	if kind.Input == nil {
		return fb.typedInput(ctx, f, label, help, id)
	}
	kctx := i18n.WithTranslator(asCaller(ctx), fb.m.tr)
	return contain(kctx, fb.m.name, "input "+f.Name, func() (render.HTML, error) {
		return kind.Input(InputContext{
			Ctx: kctx, Entity: fb.m.name, Field: f, Name: fb.m.inputName(f.Name), Value: val, Placeholder: ph,
			Label: label, Help: help, Control: headless.FieldControl{ID: id, Required: f.Required},
		}), nil
	})
}

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

func maskedFields(m *meta, raw, hooked map[string]any) map[string]bool {
	masked := map[string]bool{}
	for _, f := range m.fields {
		if m.system(f) || m.locked(f) {
			continue
		}
		if cell(m.rowValue(raw, f.Name)) != cell(m.rowValue(hooked, f.Name)) {
			masked[f.Name] = true
		}
	}
	return masked
}

func rewrittenFields(m *meta, raw, hooked map[string]any) map[string]bool {
	out := map[string]bool{}
	for _, f := range m.fields {
		if cell(m.rowValue(raw, f.Name)) != cell(m.rowValue(hooked, f.Name)) {
			out[f.Name] = true
		}
	}
	return out
}

type showWhenCond struct {
	field  string
	values []string
}

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

func enumOptions(ctx context.Context, m *meta, f schema.Field, cur string) []ui.SelectOption {
	opts := make([]ui.SelectOption, 0, len(f.Values))
	for _, v := range f.Values {
		opts = append(opts, ui.SelectOption{
			Value: v, Text: m.valueLabel(ctx, f.Name, v), Selected: v == cur,
		})
	}
	return opts
}

func enumSegments(ctx context.Context, m *meta, f schema.Field) []ui.SegmentedOption {
	opts := make([]ui.SegmentedOption, 0, len(f.Values))
	for _, v := range f.Values {
		opts = append(opts, ui.SegmentedOption{Value: v, Label: m.valueLabel(ctx, f.Name, v)})
	}
	return opts
}

func formValueText(f schema.Field, v any) string {
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
			return timestampInput(t)
		case *time.Time:
			if t == nil {
				return ""
			}
			return timestampInput(*t)
		}
	case schema.Bool:
		return boolText(v)
	default:
	}
	return cell(v)
}

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
		return ""
	}
	return cell(v)
}

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

func patternAttr(p string) html.Attrs {
	if p == "" || strings.Contains(strings.ReplaceAll(p, "(?:", ""), "(?") {
		return nil
	}
	return html.Attrs{"pattern": `[\s\S]*(?:` + p + `)[\s\S]*`}
}

func decimalStep(val string) float64 {
	return fractionStep(val, defaultScale)
}

func fractionStep(val string, digits int) float64 {
	if _, frac, ok := strings.Cut(strings.TrimSpace(val), "."); ok && len(frac) > digits && len(frac) <= 15 {
		if _, err := strconv.ParseUint(frac, 10, 64); err == nil {
			digits = len(frac)
		}
	}
	return math.Pow10(-digits)
}

func intInputValue(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

func intBound(b *float64) int {
	if b == nil || math.IsNaN(*b) {
		return 0
	}
	const limit = 1 << 53
	v := math.Max(-limit, math.Min(math.Trunc(*b), limit))
	if v >= math.MaxInt {
		return math.MaxInt
	}
	if v <= math.MinInt {
		return math.MinInt
	}
	return int(v)
}

func dateInputValue(s string) string {
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		return s[:10]
	}
	return ""
}

func timestampInputValue(s string) string {
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return timestampInput(t)
		}
	}
	return ""
}

func timestampInput(t time.Time) string {
	t = t.UTC().Truncate(time.Millisecond)
	switch {
	case t.Nanosecond() != 0:
		return t.Format("2006-01-02T15:04:05.000")
	case t.Second() != 0:
		return t.Format("2006-01-02T15:04:05")
	}
	return t.Format("2006-01-02T15:04")
}

func timestampStep(v string) float64 {
	switch {
	case strings.Contains(v, "."):
		return 0.001
	case len(v) > len("2006-01-02T15:04"):
		return 1
	}
	return 0
}
