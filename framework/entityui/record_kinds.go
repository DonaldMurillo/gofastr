package entityui

import (
	"context"
	"strconv"

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

// builtinKind is what a built-in kind name draws. email, url and color
// keep the storage (a String) and change the control; markdown and code
// draw the monospace text area the kit's mono variant ships; money draws
// a number input behind the currency symbol and prints its value as an
// amount in cells and read-only.
func builtinKind(name string) Kind {
	switch name {
	case "money":
		show := func(cc CellContext) render.HTML {
			if s := cell(cc.Value); s != "" {
				return render.Text(money(cc.Ctx, s))
			}
			return muted()
		}
		return Kind{Input: moneyInput, Cell: show}
	case "email", "url", "color":
		t := map[string]string{"email": "email", "url": "url", "color": "color"}[name]
		return Kind{Input: func(ic InputContext) render.HTML {
			return ui.FormField(ui.FormFieldConfig{
				Label: ic.Label, For: ic.Control.ID, Help: ic.Help, Required: ic.Control.Required,
				Input: func(c headless.FieldControl) render.HTML {
					return ui.Control(ui.ControlConfig{
						Field: c, Type: t, Name: ic.Name, Value: ic.Value, Placeholder: ic.Placeholder,
						MinLength: minLength(ic.Field.Min), MaxLength: maxLength(ic.Field.Max),
						ExtraAttrs: patternAttr(ic.Field.Pattern),
					})
				},
			})
		}}
	case "markdown", "code":
		return Kind{Input: func(ic InputContext) render.HTML {
			return ui.TextArea(ui.TextAreaConfig{
				Name: ic.Name, Label: ic.Label, ID: ic.Control.ID, Value: ic.Value,
				Rows: 8, Placeholder: ic.Placeholder, Help: ic.Help, Monospace: true,
				Required:  ic.Control.Required,
				MinLength: minLength(ic.Field.Min), MaxLength: maxLength(ic.Field.Max),
			})
		}}
	default:
		return Kind{}
	}
}

// moneyInput is the money kind's control: a number input whose step lets
// the type's precision through, with the currency symbol prepended.
func moneyInput(ic InputContext) render.HTML {
	step := map[schema.FieldType]string{schema.Int: "1", schema.Float: "any"}[ic.Field.Type]
	if step == "" {
		step = "0.01"
	}
	return ui.FormField(ui.FormFieldConfig{
		Label: ic.Label, For: ic.Control.ID, Help: ic.Help, Required: ic.Control.Required,
		Input: func(c headless.FieldControl) render.HTML {
			return ui.InputGroup(ui.InputGroupConfig{
				Prepend: render.Text(i18nui.T(ic.Ctx, i18nui.KeyEntityCurrency)),
				Input: ui.Control(ui.ControlConfig{
					Field: c, Type: "number", Name: ic.Name, Value: ic.Value, Placeholder: ic.Placeholder,
					Min: bound(ic.Field.Min), Max: bound(ic.Field.Max), Step: step,
				}),
			})
		},
	})
}

// bound is a numeric bound as attribute text, empty when unset.
func bound(b *float64) string {
	if b == nil {
		return ""
	}
	return strconv.FormatFloat(*b, 'f', -1, 64)
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
	case schema.Image, schema.File:
		if t := fb.b.ui.fileValue(f, fb.m.label(ctx, f.Name), cell(v), ui.ThumbnailMD); t != "" {
			return t
		}
		return muted()
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
	row, err := om.ch.GetOne(crud.WithReadHooks(ctx), id, nil)
	if err != nil || row == nil {
		return render.Text(id)
	}
	if t := fb.b.ui.rowTitles(ctx, om, []map[string]any{row}, 0)[0]; t != "" {
		return render.Text(t)
	}
	return render.Text(id)
}
