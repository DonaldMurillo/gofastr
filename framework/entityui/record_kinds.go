package entityui

import (
	"context"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

// This file holds the field kinds the record draws itself — the
// built-ins a FieldDisplay.Input may name — and the value rendering a
// read-only field uses. An app kind registered under the same name in
// Extensions.Kinds replaces the built-in.

// builtinKind is the input a built-in kind name draws. email, url and
// color keep the storage (a String) and change the control; markdown
// and code draw the monospace text area the kit's mono variant ships.
func builtinKind(name string) Kind {
	switch name {
	case "email", "url", "color":
		t := map[string]string{"email": "email", "url": "url", "color": "color"}[name]
		return Kind{Input: func(ic InputContext) render.HTML {
			return ui.FormField(ui.FormFieldConfig{
				Label: kindLabel(ic), For: kindID(ic), Help: kindHelp(ic),
				Input: func(c headless.FieldControl) render.HTML {
					return ui.Control(ui.ControlConfig{
						Field: c, Type: t, Name: ic.Name, Value: ic.Value, Placeholder: ic.Placeholder,
					})
				},
			})
		}}
	case "markdown", "code":
		return Kind{Input: func(ic InputContext) render.HTML {
			return ui.TextArea(ui.TextAreaConfig{
				Name: ic.Name, Label: kindLabel(ic), ID: kindID(ic), Value: ic.Value,
				Rows: 8, Placeholder: ic.Placeholder, Help: kindHelp(ic), Monospace: true,
			})
		}}
	default:
		return Kind{}
	}
}

// kindLabel and kindHelp resolve through the InputContext's own ctx:
// the builder attaches the app's translator before calling a kind, so
// the kind sees the same catalog the built-in fields do.
func kindLabel(ic InputContext) string {
	if ic.Field.Type == schema.Relation {
		return i18nui.RelationLabel(ic.Ctx, nil, ic.Entity, ic.Field.Name, "")
	}
	return i18nui.FieldLabel(ic.Ctx, nil, ic.Entity, ic.Field.Name, "")
}

func kindHelp(ic InputContext) string {
	return i18nui.FieldHelp(ic.Ctx, nil, ic.Entity, ic.Field.Name, "")
}

// kindID is the control id a kind's field derives, the same scheme the
// form's own fields use.
func kindID(ic InputContext) string { return "eui-f-" + ic.Field.Name }

// kindControl is the wiring a kind that builds its own control copies
// from the InputContext: the label's target, the description chain and
// the required flag, precomputed the way FormField would hand them
// down.
func kindControl(ic InputContext) headless.FieldControl {
	return ic.Control
}

// display renders a field's stored value for a read-only surface: the
// value label for an enum, yes/no for a bool, the related record's
// title for a relation, a formatted date or number otherwise. The row
// is the hooked read: whatever a redaction shows is what shows here.
func (fb *formBuilder) display(ctx context.Context, f schema.Field, row map[string]any) render.HTML {
	v := rowValue(row, f.Name)
	switch f.Type {
	case schema.Enum:
		if s := cell(v); s != "" {
			return render.Text(fb.m.valueLabel(ctx, f.Name, s))
		}
	case schema.Bool:
		switch t := v.(type) {
		case bool:
			if t {
				return render.Text(i18nui.T(ctx, i18nui.KeyEntityYes))
			}
			return render.Text(i18nui.T(ctx, i18nui.KeyEntityNo))
		case nil:
			return muted()
		default:
			return render.Text(boolText(t))
		}
	case schema.Date:
		if s := formatDate(v, dateLayout); s != "" {
			return render.Text(s)
		}
	case schema.Timestamp:
		if s := formatDate(v, timestampLayout); s != "" {
			return render.Text(s)
		}
	case schema.Decimal:
		if s := cell(v); s != "" {
			return render.Text(decimal(s))
		}
	case schema.Float, schema.Int:
		if s := cell(v); s != "" {
			return render.Text(s)
		}
	case schema.Relation:
		return fb.relationDisplay(ctx, f, v)
	case schema.JSON:
		if s := cell(v); s != "" {
			return ui.JSONViewer(ui.JSONViewerConfig{Value: s, OpenDepth: 1})
		}
	default:
		if s := cell(v); s != "" {
			return render.Text(s)
		}
	}
	return muted()
}

// relationReadable reports whether the caller may read the record f's
// foreign key id points at, through the related entity's own read gate.
func (fb *formBuilder) relationReadable(ctx context.Context, f schema.Field, id string) bool {
	other, err := fb.b.ui.entityFor(f.To)
	if err != nil {
		return false
	}
	om, err := fb.b.ui.meta(other.GetName())
	return err == nil && canReadRecord(ctx, om.ch, id)
}

// relationDisplay names a foreign key by its record's title: one read
// of that one row through the related entity's own handler and read
// gate. A target the caller may not read renders muted, never its label
// and never the raw foreign key; a readable target the read did not
// return renders the id.
func (fb *formBuilder) relationDisplay(ctx context.Context, f schema.Field, v any) render.HTML {
	id := cell(v)
	if id == "" || !fb.relationReadable(ctx, f, id) {
		return muted()
	}
	other, err := fb.b.ui.entityFor(f.To)
	if err != nil {
		return muted()
	}
	om, err := fb.b.ui.meta(other.GetName())
	if err != nil {
		return muted()
	}
	fields := []string{om.pk}
	if tf := om.titleField(); tf != "" && tf != om.pk {
		fields = append(fields, tf)
	}
	row, err := om.ch.GetOne(crud.WithReadHooks(ctx), id, nil)
	if err != nil || row == nil {
		return render.Text(id)
	}
	if tf := om.titleField(); tf != "" {
		if l := cell(rowValue(row, tf)); l != "" {
			return render.Text(l)
		}
	}
	return render.Text(id)
}
