package packcmd

import (
	"go/ast"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

func reverseControlBlock(call *ast.CallExpr, helpers map[string]ast.Expr) (appspec.AppSpecBlock, bool) {
	kind := map[string]string{
		"ui.Button": "action_button", "ui.Form": "custom_form",
		"ui.TextField": "text_field", "ui.NumberField": "number_field",
		"ui.Checkbox": "checkbox", "ui.Switch": "switch",
		"ui.Select": "select_field", "ui.RadioGroup": "radio_group",
		"ui.DataTable": "data_table", "html.OrderedList": "item_list",
		"html.UnorderedList": "item_list", "html.Nav": "nav_links",
	}[callSel(call)]
	if kind == "" || len(call.Args) == 0 {
		return appspec.AppSpecBlock{}, false
	}
	config := cfgOf(call, 0)
	props := map[string]any{}
	for prop, field := range map[string]string{
		"name": "Name", "label": "Label", "placeholder": "Placeholder", "help": "Help",
		"legend": "Legend", "action": "Action", "method": "Method", "submit": "SubmitLabel",
		"caption": "Caption", "id": "ID", "type": "Type",
	} {
		putStr(props, prop, appspec.AstString(config[field]))
	}
	for prop, field := range map[string]string{
		"required": "Required", "disabled": "Disabled", "checked": "Checked",
		"hide_submit": "HideSubmit", "caption_hidden": "CaptionHidden",
	} {
		putBool(props, prop, appspec.AstBool(config[field]))
	}
	result := block(kind, props)
	switch kind {
	case "action_button":
		putStr(props, "variant", buttonVariant(config["Variant"]))
		if props["variant"] == "primary" {
			delete(props, "variant")
		}
		if props["type"] == "button" {
			delete(props, "type")
		}
	case "custom_form":
		result.Children = reverseControlChildren(call, helpers)
	case "select_field", "radio_group":
		if choices := reverseControlChoices(config["Options"], kind == "radio_group"); len(choices) > 0 {
			props["options"] = choices
		}
	case "data_table":
		reverseControlTable(props, config)
	case "item_list":
		putBool(props, "ordered", callSel(call) == "html.OrderedList")
		for _, argument := range call.Args[1:] {
			if item, ok := argument.(*ast.CallExpr); ok && callSel(item) == "html.ListItem" {
				result.Children = append(result.Children, reverseControlChildren(item, helpers)...)
			}
		}
	case "nav_links":
		if len(call.Args) == 2 {
			if cluster, ok := call.Args[1].(*ast.CallExpr); ok && callSel(cluster) == "ui.Cluster" {
				putStr(props, "gap", reverseGap(appspec.AstSelName(cfgOf(cluster, 0)["Gap"])))
				if props["gap"] == "md" {
					delete(props, "gap")
				}
				result.Children = reverseControlChildren(cluster, helpers)
			}
		}
	}
	if len(props) == 0 {
		result.Props = nil
	}
	return result, true
}

func reverseControlChildren(call *ast.CallExpr, helpers map[string]ast.Expr) []appspec.AppSpecBlock {
	var children []appspec.AppSpecBlock
	for _, argument := range call.Args[min(1, len(call.Args)):] {
		if child, ok := reverseBlock(argument, helpers); ok {
			children = append(children, child)
		}
	}
	return children
}

func reverseControlChoices(expression ast.Expr, radio bool) []any {
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	var choices []any
	for _, element := range literal.Elts {
		config := appspec.FieldVals(element)
		labelKey, selectedKey, prop := "Text", "Selected", "selected"
		if radio {
			labelKey, selectedKey, prop = "Label", "Checked", "checked"
		}
		label, value := appspec.AstString(config[labelKey]), appspec.AstString(config["Value"])
		on := appspec.AstBool(config[selectedKey])
		if label == value && !on {
			choices = append(choices, label)
			continue
		}
		choice := props2("label", label, "value", value)
		putBool(choice, prop, on)
		choices = append(choices, choice)
	}
	return choices
}

func reverseControlTable(props map[string]any, config map[string]ast.Expr) {
	columns, ok := config["Columns"].(*ast.CompositeLit)
	if !ok {
		return
	}
	var keys, headers []string
	for _, column := range columns.Elts {
		fields := appspec.FieldVals(column)
		keys = append(keys, appspec.AstString(fields["Key"]))
		headers = append(headers, appspec.AstString(fields["Header"]))
	}
	putStrs(props, "columns", headers)
	rows, ok := config["Rows"].(*ast.CompositeLit)
	if !ok {
		return
	}
	var values []any
	for _, row := range rows.Elts {
		cells, ok := appspec.FieldVals(row)["Cells"].(*ast.CompositeLit)
		if !ok {
			continue
		}
		byKey := map[string]string{}
		for _, element := range cells.Elts {
			if cell, ok := element.(*ast.KeyValueExpr); ok {
				byKey[appspec.AstString(cell.Key)] = renderTextArg(cell.Value)
			}
		}
		ordered := make([]any, len(keys))
		for index, key := range keys {
			ordered[index] = byKey[key]
		}
		values = append(values, ordered)
	}
	if len(values) > 0 {
		props["rows"] = values
	}
}
