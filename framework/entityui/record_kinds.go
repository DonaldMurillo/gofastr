package entityui

import (
	"context"
	"strconv"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/headless"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
	"github.com/DonaldMurillo/gofastr/framework/ui"
)

func builtinKind(name string) Kind {
	switch name {
	case "money":
		show := func(cc CellContext) render.HTML {
			if s := cell(cc.Value); s != "" {
				return render.Text(money(cc.Ctx, s))
			}
			return muted()
		}
		text := func(cc CellContext) string {
			if s := cell(cc.Value); s != "" {
				return money(cc.Ctx, s)
			}
			return ""
		}
		return Kind{Input: moneyInput, Cell: show, Text: text}
	case "email", "tel", "url", "color":
		kindType := name
		// An address, a number or a link is typed exactly: the browser
		// offers the reader's own as a fill (the autocomplete token of
		// the same name), and nothing spell-checks or capitalizes it.
		exact := name != "color"
		return Kind{Input: func(ic InputContext) render.HTML {
			t := kindType
			if t == "color" && !simpleColor(ic.Value) {
				t = "text"
			}
			auto := ""
			if exact {
				auto = kindType
			}
			return ui.FormField(ui.FormFieldConfig{
				Label: ic.Label, For: ic.Control.ID, Help: ic.Help, Required: ic.Control.Required,
				Input: func(c headless.FieldControl) render.HTML {
					return ui.Control(ui.ControlConfig{
						Field: c, Type: t, Name: ic.Name, Value: ic.Value, Placeholder: ic.Placeholder,
						AutoComplete: auto, NoSpellCheck: exact,
						ExtraAttrs: patternAttr(ic.Field.Pattern),
					})
				},
			})
		}}
	case "markdown", "code":
		return Kind{Input: func(ic InputContext) render.HTML {
			return ui.TextArea(ui.TextAreaConfig{
				Ctx:  ic.Ctx,
				Name: ic.Name, Label: ic.Label, ID: ic.Control.ID, Value: ic.Value,
				Rows: 8, Placeholder: ic.Placeholder, Help: ic.Help, Monospace: true,
				Required: ic.Control.Required,
			})
		}}
	default:
		return Kind{}
	}
}

func simpleColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func moneyInput(ic InputContext) render.HTML {
	step := map[schema.FieldType]string{schema.Int: "1", schema.Float: "any"}[ic.Field.Type]
	if step == "" {
		step = strconv.FormatFloat(fractionStep(ic.Value, 2), 'f', -1, 64)
	}
	return ui.FormField(ui.FormFieldConfig{
		Label: ic.Label, For: ic.Control.ID, Help: ic.Help, Required: ic.Control.Required,
		Input: func(c headless.FieldControl) render.HTML {
			return ui.InputGroup(ui.InputGroupConfig{
				Prepend: render.Text(i18nui.EntityCurrency.Text(ic.Ctx)),
				Input: ui.Control(ui.ControlConfig{
					Field: c, Type: "number", Name: ic.Name, Value: ic.Value, Placeholder: ic.Placeholder,
					Min: bound(ic.Field.Min), Max: bound(ic.Field.Max), Step: step,
				}),
			})
		},
	})
}

func bound(b *float64) string {
	if b == nil {
		return ""
	}
	return strconv.FormatFloat(*b, 'f', -1, 64)
}

func (fb *formBuilder) display(ctx context.Context, f schema.Field, row map[string]any) render.HTML {
	v := fb.m.rowValue(row, f.Name)
	switch f.Type {
	case schema.Enum:
		if s := cell(v); s != "" {
			return render.Text(fb.m.valueLabel(ctx, f.Name, s))
		}
	case schema.Bool:
		switch t := v.(type) {
		case bool:
			if t {
				return render.Text(i18nui.EntityYes.Text(ctx))
			}
			return render.Text(i18nui.EntityNo.Text(ctx))
		case nil:
			return muted()
		default:
			return render.Text(boolText(t))
		}
	case schema.Date:
		if s := formatDate(ctx, v, false); s != "" {
			return render.Text(s)
		}
	case schema.Timestamp:
		if s := formatDate(ctx, v, true); s != "" {
			return render.Text(s)
		}
	case schema.Decimal, schema.Float:
		if s := cell(v); s != "" {
			return render.Text(decimal(ctx, f, s))
		}
	case schema.Int:
		if s := cell(v); s != "" {
			return render.Text(integer(ctx, s))
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
			return ui.JSONViewer(ui.JSONViewerConfig{Ctx: ctx, Value: s, OpenDepth: 1})
		}
	default:
		if s := cell(v); s != "" {
			return render.Text(s)
		}
	}
	return muted()
}

func (fb *formBuilder) relationReadable(ctx context.Context, f schema.Field, id string) bool {
	other, err := fb.b.ui.entityFor(f.To)
	if err != nil {
		return false
	}
	om, err := fb.b.ui.meta(other.GetName())
	return err == nil && canReadRelated(ctx, om.ch) && canReadRecord(ctx, om.ch, id)
}

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
	row, err := om.getRow(ctx, id)
	if err != nil || row == nil {
		return render.Text(id)
	}
	if t := fb.b.ui.rowTitles(ctx, om, []map[string]any{row}, 0)[0]; t != "" {
		return render.Text(t)
	}
	return render.Text(id)
}
