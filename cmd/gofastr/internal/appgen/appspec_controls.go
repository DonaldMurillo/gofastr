package appgen

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// goFields renders `Key: value` pairs for a config literal, skipping the
// zero ones so the generated screen reads like hand-written code.
type goFields []string

func (f *goFields) str(key, v string) {
	if v != "" {
		*f = append(*f, fmt.Sprintf("%s: %q", key, v))
	}
}

func (f *goFields) flag(key string, on bool) {
	if on {
		*f = append(*f, key+": true")
	}
}

func (f *goFields) raw(key, expr string) { *f = append(*f, key+": "+expr) }

// text sets a reader-facing field: a phrase read through the request
// ctx when the file has a collector, the literal otherwise.
func (f *goFields) text(bp appspec.AppSpec, key, v string) {
	if v != "" {
		*f = append(*f, key+": "+appSpecTx(bp, v))
	}
}

func (f goFields) String() string { return strings.Join(f, ", ") }

func appSpecButtonVariantExpr(v string) string {
	switch strings.ToLower(v) {
	case "secondary":
		return "ui.ButtonSecondary"
	case "ghost":
		return "ui.ButtonGhost"
	case "danger":
		return "ui.ButtonDanger"
	}
	return "ui.ButtonPrimary"
}

// renderAppSpecControlBlock emits a control-catalog block. children is
// the comma-joined child expressions (custom_form, item_list).
func renderAppSpecControlBlock(bp appspec.AppSpec, block appspec.AppSpecBlock, childExprs []string) (string, bool) {
	kind := strings.ToLower(strings.TrimSpace(block.Kind))
	if !appspec.AppSpecControlKind(kind) {
		return "", false
	}
	var f goFields
	switch kind {
	case "action_button":
		f.text(bp, "Label", appspec.AppSpecProp(block, "label"))
		f.raw("Variant", appSpecButtonVariantExpr(appspec.AppSpecProp(block, "variant")))
		typ := strings.ToLower(appspec.AppSpecProp(block, "type"))
		if typ == "" {
			typ = "button"
		}
		f.str("Type", typ)
		f.flag("Disabled", appspec.AppSpecBoolProp(block, "disabled"))
		f.raw("Ctx", "ctx") // the Shortcut keycaps are said through it
		return "ui.Button(ui.ButtonConfig{" + f.String() + "})", true
	case "text_field", "number_field":
		f.str("Name", appspec.AppSpecProp(block, "name"))
		f.text(bp, "Label", appspec.AppSpecProp(block, "label"))
		f.text(bp, "Placeholder", appspec.AppSpecProp(block, "placeholder"))
		f.text(bp, "Help", appspec.AppSpecProp(block, "help"))
		f.flag("Required", appspec.AppSpecBoolProp(block, "required"))
		f.flag("Disabled", appspec.AppSpecBoolProp(block, "disabled"))
		if kind == "number_field" {
			return "ui.NumberField(ui.NumberFieldConfig{" + f.String() + "})", true
		}
		return "ui.TextField(ui.TextFieldConfig{" + f.String() + "})", true
	case "checkbox", "switch":
		f.str("Name", appspec.AppSpecProp(block, "name"))
		f.text(bp, "Label", appspec.AppSpecProp(block, "label"))
		f.flag("Checked", appspec.AppSpecBoolProp(block, "checked"))
		f.flag("Disabled", appspec.AppSpecBoolProp(block, "disabled"))
		fn := "ui.Checkbox"
		if kind == "switch" {
			fn = "ui.Switch"
		}
		return fn + "(ui.ToggleConfig{" + f.String() + "})", true
	case "select_field":
		choices, _ := appspec.AppSpecChoices(block)
		opts := make([]string, len(choices))
		for i, c := range choices {
			var o goFields
			o.str("Value", c.Value)
			o.text(bp, "Text", c.Label)
			o.flag("Selected", c.On)
			opts[i] = "{" + o.String() + "}"
		}
		f.str("Name", appspec.AppSpecProp(block, "name"))
		f.text(bp, "Label", appspec.AppSpecProp(block, "label"))
		f.raw("Options", "[]ui.SelectOption{"+strings.Join(opts, ", ")+"}")
		f.flag("Required", appspec.AppSpecBoolProp(block, "required"))
		f.flag("Disabled", appspec.AppSpecBoolProp(block, "disabled"))
		return "ui.Select(ui.SelectConfig{" + f.String() + "})", true
	case "radio_group":
		choices, _ := appspec.AppSpecChoices(block)
		opts := make([]string, len(choices))
		for i, c := range choices {
			var o goFields
			o.str("Value", c.Value)
			o.text(bp, "Label", c.Label)
			o.flag("Checked", c.On)
			opts[i] = "{" + o.String() + "}"
		}
		f.str("Name", appspec.AppSpecProp(block, "name"))
		f.text(bp, "Legend", appspec.AppSpecProp(block, "legend"))
		f.raw("Options", "[]ui.RadioGroupOption{"+strings.Join(opts, ", ")+"}")
		f.flag("Required", appspec.AppSpecBoolProp(block, "required"))
		return "ui.RadioGroup(ui.RadioGroupConfig{" + f.String() + "})", true
	case "custom_form":
		f.str("Action", appspec.AppSpecProp(block, "action"))
		f.str("Method", strings.ToUpper(appspec.AppSpecProp(block, "method")))
		f.text(bp, "SubmitLabel", appspec.AppSpecProp(block, "submit"))
		f.flag("HideSubmit", appspec.AppSpecBoolProp(block, "hide_submit"))
		// Ctx lets the form stamp the CSRF token the request carries.
		f.raw("Ctx", "ctx")
		return "ui.Form(" + joinCallArgs("ui.FormConfig{"+f.String()+"}", childExprs) + ")", true
	case "data_table":
		columns, _ := appspec.AppSpecStringList(block, "columns")
		rows, _ := appspec.AppSpecTableRows(block)
		cols := make([]string, len(columns))
		for i, c := range columns {
			cols[i] = fmt.Sprintf("{Key: %q, Header: %s}", fmt.Sprintf("c%d", i), appSpecTx(bp, c))
		}
		rowExprs := make([]string, len(rows))
		for i, r := range rows {
			cells := make([]string, len(r))
			for j, cell := range r {
				cells[j] = fmt.Sprintf("%q: render.Text(%s)", fmt.Sprintf("c%d", j), appSpecTx(bp, cell))
			}
			rowExprs[i] = "{Cells: map[string]render.HTML{" + strings.Join(cells, ", ") + "}}"
		}
		f.text(bp, "Caption", appspec.AppSpecProp(block, "caption"))
		f.flag("CaptionHidden", appspec.AppSpecBoolProp(block, "caption_hidden"))
		f.str("ID", appspec.AppSpecProp(block, "id"))
		f.raw("Columns", "[]ui.Column{"+strings.Join(cols, ", ")+"}")
		f.raw("Rows", "[]ui.Row{"+appSpecLines(rowExprs)+"}")
		f.raw("Ctx", "ctx") // sort, empty-state and pagination words
		return "ui.DataTable(ui.DataTableConfig{" + f.String() + "})", true
	case "item_list":
		items := make([]string, len(childExprs))
		for i, c := range childExprs {
			items[i] = "html.ListItem(html.ListItemConfig{}, " + c + ")"
		}
		fn := "html.UnorderedList"
		if appspec.AppSpecBoolProp(block, "ordered") {
			fn = "html.OrderedList"
		}
		return fn + "(" + joinCallArgs("html.ListConfig{}", items) + ")", true
	case "nav_links":
		gap := appspec.AppSpecProp(block, "gap")
		if gap == "" {
			gap = "md"
		}
		row := "ui.Cluster(" + joinCallArgs("ui.ClusterConfig{Gap: "+appSpecGapExpr(gap)+"}", childExprs) + ")"
		return fmt.Sprintf("html.Nav(html.NavConfig{Label: %s}, %s)", appSpecTx(bp, appspec.AppSpecProp(block, "label")), row), true
	}
	return "", false
}

// joinCallArgs lays a call's trailing arguments out one per line, which
// gofmt then indents: a form's fields read top to bottom in the owned
// screen.
func joinCallArgs(first string, rest []string) string {
	if len(rest) == 0 {
		return first
	}
	return first + "," + appSpecLines(rest)
}

// appSpecLines lays out list elements one per line, each with a
// trailing comma, ready for gofmt.
func appSpecLines(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return "\n" + strings.Join(items, ",\n") + ",\n"
}
