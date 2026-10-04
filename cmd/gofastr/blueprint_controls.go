package main

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
)

// The control catalog: blueprint block kinds for plain form controls,
// static tables, and lists, each emitted as a framework/ui (or core-ui/html
// list or nav) call. They exist so a screen can carry a form that is not
// bound to an entity, which is what `gofastr generate screen --from-a11y`
// maps a captured page onto. The names avoid the raw node kinds (button,
// form, select, list, table, nav), which render as uinode trees and keep
// their meaning.
var blueprintControlKinds = map[string]bool{
	"action_button": true, "custom_form": true, "text_field": true,
	"number_field": true, "checkbox": true, "switch": true,
	"select_field": true, "radio_group": true, "data_table": true,
	"item_list": true, "nav_links": true,
}

func blueprintControlKind(kind string) bool {
	return blueprintControlKinds[strings.ToLower(strings.TrimSpace(kind))]
}

// blueprintChoice is one select or radio option.
type blueprintChoice struct {
	Label, Value string
	On           bool
}

// blueprintChoices reads an options prop: a list of plain strings, or of
// {label, value, selected|checked} maps. A missing value is the label.
func blueprintChoices(b BlueprintBlock) ([]blueprintChoice, error) {
	raw, ok := b.Props["options"]
	if !ok {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("options must be a list")
	}
	out := make([]blueprintChoice, 0, len(list))
	for i, item := range list {
		var c blueprintChoice
		switch v := item.(type) {
		case string:
			c.Label = v
		case map[string]any:
			label, lok := v["label"].(string)
			value, vok := v["value"].(string)
			if _, present := v["value"]; present && !vok {
				return nil, fmt.Errorf("options[%d].value must be a string", i)
			}
			if !lok {
				return nil, fmt.Errorf("options[%d].label must be a string", i)
			}
			c.Label, c.Value = label, value
			for _, key := range []string{"selected", "checked"} {
				if on, present := v[key]; present {
					flag, ok := on.(bool)
					if !ok {
						return nil, fmt.Errorf("options[%d].%s must be true or false", i, key)
					}
					c.On = c.On || flag
				}
			}
		default:
			return nil, fmt.Errorf("options[%d] must be a string or a {label, value} map", i)
		}
		if strings.TrimSpace(c.Label) == "" {
			return nil, fmt.Errorf("options[%d] needs a label", i)
		}
		if c.Value == "" {
			c.Value = c.Label
		}
		out = append(out, c)
	}
	return out, nil
}

// blueprintStringList reads a list-of-strings prop.
func blueprintStringList(b BlueprintBlock, key string) ([]string, error) {
	raw, ok := b.Props[key]
	if !ok {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a list", key)
	}
	out := make([]string, len(list))
	for i, item := range list {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be a string", key, i)
		}
		out[i] = s
	}
	return out, nil
}

// blueprintTableRows reads data_table rows: a list of lists of strings.
func blueprintTableRows(b BlueprintBlock) ([][]string, error) {
	raw, ok := b.Props["rows"]
	if !ok {
		return nil, nil
	}
	list, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("rows must be a list of lists")
	}
	out := make([][]string, len(list))
	for i, row := range list {
		cells, err := blueprintStringList(BlueprintBlock{Props: map[string]any{"row": row}}, "row")
		if err != nil {
			return nil, fmt.Errorf("rows[%d]: %w", i, err)
		}
		out[i] = cells
	}
	return out, nil
}

// validateBlueprintControlBlock enforces the props each control's
// framework/ui component refuses to render without: a missing label or
// name would otherwise ship as a panic on the generated screen.
func validateBlueprintControlBlock(screenName, kind string, b BlueprintBlock) error {
	fail := func(format string, args ...any) error {
		return fmt.Errorf("blueprint: screen %q %s %s", screenName, kind, fmt.Sprintf(format, args...))
	}
	need := func(keys ...string) error {
		for _, k := range keys {
			if strings.TrimSpace(blueprintProp(b, k)) == "" {
				return fail("requires %s", k)
			}
		}
		return nil
	}
	oneOf := func(key string, values ...string) error {
		v := blueprintProp(b, key)
		if v == "" {
			return nil
		}
		for _, ok := range values {
			if strings.EqualFold(v, ok) {
				return nil
			}
		}
		return fail("%s %q must be one of %s", key, v, strings.Join(values, ", "))
	}
	switch kind {
	case "action_button":
		if err := need("label"); err != nil {
			return err
		}
		if err := oneOf("variant", "primary", "secondary", "ghost", "danger"); err != nil {
			return err
		}
		return oneOf("type", "button", "submit", "reset")
	case "text_field", "number_field", "checkbox", "switch":
		return need("label", "name")
	case "select_field", "radio_group":
		label := "label"
		if kind == "radio_group" {
			label = "legend"
		}
		if err := need(label, "name"); err != nil {
			return err
		}
		choices, err := blueprintChoices(b)
		if err != nil {
			return fail("%v", err)
		}
		if len(choices) == 0 {
			return fail("requires options")
		}
	case "custom_form":
		if err := need("action"); err != nil {
			return err
		}
		// The same predicate login_form/signup_form apply, so validate and
		// render cannot disagree about a safe form action.
		if action := blueprintProp(b, "action"); !urlsafe.OK(action, urlsafe.Anchor) {
			return fail("action %q is not a safe form action: use an http(s) or root-relative URL", action)
		}
		if blueprintBlocksHaveCustomForm(b.Children) {
			return fail("cannot hold another custom_form: HTML forms do not nest")
		}
		return oneOf("method", "get", "post")
	case "data_table":
		columns, err := blueprintStringList(b, "columns")
		if err != nil {
			return fail("%v", err)
		}
		if len(columns) == 0 {
			return fail("requires columns")
		}
		// ui.DataTable refuses a hidden caption that is not there.
		if blueprintBoolProp(b, "caption_hidden") && strings.TrimSpace(blueprintProp(b, "caption")) == "" {
			return fail("caption_hidden needs a caption: the caption names the table for assistive technology")
		}
		rows, err := blueprintTableRows(b)
		if err != nil {
			return fail("%v", err)
		}
		for i, r := range rows {
			if len(r) > len(columns) {
				return fail("rows[%d] has %d cells for %d columns", i, len(r), len(columns))
			}
		}
	case "nav_links":
		// html.Nav refuses an unlabelled landmark.
		return need("label")
	}
	return nil
}

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

func (f goFields) String() string { return strings.Join(f, ", ") }

func blueprintButtonVariantExpr(v string) string {
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

// renderBlueprintControlBlock emits a control-catalog block. children is
// the comma-joined child expressions (custom_form, item_list).
func renderBlueprintControlBlock(block BlueprintBlock, childExprs []string) (string, bool) {
	kind := strings.ToLower(strings.TrimSpace(block.Kind))
	if !blueprintControlKind(kind) {
		return "", false
	}
	var f goFields
	switch kind {
	case "action_button":
		f.str("Label", blueprintProp(block, "label"))
		f.raw("Variant", blueprintButtonVariantExpr(blueprintProp(block, "variant")))
		typ := strings.ToLower(blueprintProp(block, "type"))
		if typ == "" {
			typ = "button"
		}
		f.str("Type", typ)
		f.flag("Disabled", blueprintBoolProp(block, "disabled"))
		return "ui.Button(ui.ButtonConfig{" + f.String() + "})", true
	case "text_field", "number_field":
		f.str("Name", blueprintProp(block, "name"))
		f.str("Label", blueprintProp(block, "label"))
		f.str("Placeholder", blueprintProp(block, "placeholder"))
		f.str("Help", blueprintProp(block, "help"))
		f.flag("Required", blueprintBoolProp(block, "required"))
		f.flag("Disabled", blueprintBoolProp(block, "disabled"))
		if kind == "number_field" {
			return "ui.NumberField(ui.NumberFieldConfig{" + f.String() + "})", true
		}
		return "ui.TextField(ui.TextFieldConfig{" + f.String() + "})", true
	case "checkbox", "switch":
		f.str("Name", blueprintProp(block, "name"))
		f.str("Label", blueprintProp(block, "label"))
		f.flag("Checked", blueprintBoolProp(block, "checked"))
		f.flag("Disabled", blueprintBoolProp(block, "disabled"))
		fn := "ui.Checkbox"
		if kind == "switch" {
			fn = "ui.Switch"
		}
		return fn + "(ui.ToggleConfig{" + f.String() + "})", true
	case "select_field":
		choices, _ := blueprintChoices(block)
		opts := make([]string, len(choices))
		for i, c := range choices {
			var o goFields
			o.str("Value", c.Value)
			o.str("Text", c.Label)
			o.flag("Selected", c.On)
			opts[i] = "{" + o.String() + "}"
		}
		f.str("Name", blueprintProp(block, "name"))
		f.str("Label", blueprintProp(block, "label"))
		f.raw("Options", "[]ui.SelectOption{"+strings.Join(opts, ", ")+"}")
		f.flag("Required", blueprintBoolProp(block, "required"))
		f.flag("Disabled", blueprintBoolProp(block, "disabled"))
		return "ui.Select(ui.SelectConfig{" + f.String() + "})", true
	case "radio_group":
		choices, _ := blueprintChoices(block)
		opts := make([]string, len(choices))
		for i, c := range choices {
			var o goFields
			o.str("Value", c.Value)
			o.str("Label", c.Label)
			o.flag("Checked", c.On)
			opts[i] = "{" + o.String() + "}"
		}
		f.str("Name", blueprintProp(block, "name"))
		f.str("Legend", blueprintProp(block, "legend"))
		f.raw("Options", "[]ui.RadioGroupOption{"+strings.Join(opts, ", ")+"}")
		f.flag("Required", blueprintBoolProp(block, "required"))
		return "ui.RadioGroup(ui.RadioGroupConfig{" + f.String() + "})", true
	case "custom_form":
		f.str("Action", blueprintProp(block, "action"))
		f.str("Method", strings.ToUpper(blueprintProp(block, "method")))
		f.str("SubmitLabel", blueprintProp(block, "submit"))
		f.flag("HideSubmit", blueprintBoolProp(block, "hide_submit"))
		// Ctx lets the form stamp the CSRF token the request carries.
		f.raw("Ctx", "ctx")
		return "ui.Form(" + joinCallArgs("ui.FormConfig{"+f.String()+"}", childExprs) + ")", true
	case "data_table":
		columns, _ := blueprintStringList(block, "columns")
		rows, _ := blueprintTableRows(block)
		cols := make([]string, len(columns))
		for i, c := range columns {
			cols[i] = fmt.Sprintf("{Key: %q, Header: %q}", fmt.Sprintf("c%d", i), c)
		}
		rowExprs := make([]string, len(rows))
		for i, r := range rows {
			cells := make([]string, len(r))
			for j, cell := range r {
				cells[j] = fmt.Sprintf("%q: render.Text(%q)", fmt.Sprintf("c%d", j), cell)
			}
			rowExprs[i] = "{Cells: map[string]render.HTML{" + strings.Join(cells, ", ") + "}}"
		}
		f.str("Caption", blueprintProp(block, "caption"))
		f.flag("CaptionHidden", blueprintBoolProp(block, "caption_hidden"))
		f.str("ID", blueprintProp(block, "id"))
		f.raw("Columns", "[]ui.Column{"+strings.Join(cols, ", ")+"}")
		f.raw("Rows", "[]ui.Row{"+blueprintLines(rowExprs)+"}")
		return "ui.DataTable(ui.DataTableConfig{" + f.String() + "})", true
	case "item_list":
		items := make([]string, len(childExprs))
		for i, c := range childExprs {
			items[i] = "html.ListItem(html.ListItemConfig{}, " + c + ")"
		}
		fn := "html.UnorderedList"
		if blueprintBoolProp(block, "ordered") {
			fn = "html.OrderedList"
		}
		return fn + "(" + joinCallArgs("html.ListConfig{}", items) + ")", true
	case "nav_links":
		gap := blueprintProp(block, "gap")
		if gap == "" {
			gap = "md"
		}
		row := "ui.Cluster(" + joinCallArgs("ui.ClusterConfig{Gap: "+blueprintGapExpr(gap)+"}", childExprs) + ")"
		return fmt.Sprintf("html.Nav(html.NavConfig{Label: %q}, %s)", blueprintProp(block, "label"), row), true
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
	return first + "," + blueprintLines(rest)
}

// blueprintLines lays out list elements one per line, each with a
// trailing comma, ready for gofmt.
func blueprintLines(items []string) string {
	if len(items) == 0 {
		return ""
	}
	return "\n" + strings.Join(items, ",\n") + ",\n"
}

// blueprintBlocksHaveCustomForm reports whether a custom_form sits anywhere
// in blocks: its emitted ui.Form reads the request ctx.
func blueprintBlocksHaveCustomForm(blocks []BlueprintBlock) bool {
	for _, b := range blocks {
		if strings.EqualFold(strings.TrimSpace(b.Kind), "custom_form") || blueprintBlocksHaveCustomForm(b.Children) {
			return true
		}
	}
	return false
}
