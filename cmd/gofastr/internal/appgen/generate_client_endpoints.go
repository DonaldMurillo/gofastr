package appgen

import (
	"fmt"
	"go/token"
	"go/types"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/crud"
)

func sdkEndpoints(decl framework.EntityDeclaration) []framework.Endpoint {
	var out []framework.Endpoint
	for _, ep := range decl.Endpoints {
		if ep.SDK {
			out = append(out, ep)
		}
	}
	return out
}

func endpointInput(ep framework.Endpoint) (pathVals, rest []schema.Field, inQuery bool) {
	params := map[string]bool{}
	for _, seg := range strings.Split(ep.Path, "/") {
		if strings.HasPrefix(seg, "{") {
			params[strings.Trim(seg, "{}")] = true
		}
	}
	for _, f := range ep.InputSchema {
		if params[f.Name] {
			pathVals = append(pathVals, f)
		} else {
			rest = append(rest, f)
		}
	}
	return pathVals, rest, endpointQueryMethods(ep.Method)
}

func endpointReturnsList(ep framework.Endpoint) (entity string, list bool) {
	if after, ok := strings.CutPrefix(ep.Returns, "[]"); ok {
		return after, true
	}
	return ep.Returns, false
}

var goEndpointLocals = []string{"c", "ctx", "in", "out", "path", "q", "err", "url", "fmt", "http"}

func goEndpointArg(name string) string {
	arg := appspec.ToCamelJSON(name)
	if token.IsKeyword(arg) || types.Universe.Lookup(arg) != nil || slices.Contains(goEndpointLocals, arg) {
		arg += "Arg"
	}
	return arg
}

func goEndpointType(t schema.FieldType) string {
	switch t {
	case schema.Int:
		return "int64"
	case schema.Float:
		return "float64"
	case schema.Bool:
		return "bool"
	case schema.JSON:
		return "any"
	default:
		return "string"
	}
}

func renderClientEndpoint(ep framework.Endpoint, route string) string {
	method := appspec.ToCamelCase(ep.Name)
	_, rest, inQuery := endpointInput(ep)
	var sb strings.Builder
	request := method + "Request"
	if len(rest) > 0 {
		fmt.Fprintf(&sb, "// %s is the input of %s.\ntype %s struct {\n", request, method, request)
		for _, f := range rest {
			if f.Required {
				fmt.Fprintf(&sb, "\t%s %s `json:%q`\n", appspec.ToCamelCase(f.Name), goEndpointType(f.Type), f.Name)
			} else {
				fmt.Fprintf(&sb, "\t%s *%s `json:%q`\n", appspec.ToCamelCase(f.Name), goEndpointType(f.Type), f.Name+",omitempty")
			}
		}
		sb.WriteString("}\n\n")
	}

	args := []string{"ctx context.Context"}
	pathExpr := `"/` + route
	for _, seg := range strings.Split(ep.Path, "/") {
		if !strings.HasPrefix(seg, "{") {
			pathExpr += "/" + seg
			continue
		}
		arg := goEndpointArg(strings.Trim(seg, "{}"))
		args = append(args, arg+" string")
		pathExpr += `/"+url.PathEscape(` + arg + `)+"`
	}
	pathExpr = strings.TrimSuffix(pathExpr+`"`, `+""`)
	if len(rest) > 0 {
		args = append(args, "in "+request)
	}

	entityName, list := endpointReturnsList(ep)
	result, zero := "", ""
	if entityName != "" {
		result = appspec.ToCamelCase(entityName)
		zero = result + "{}"
		if list {
			result, zero = "[]"+result, "nil"
		}
	}

	what := "calls " + ep.Method + " /" + goCommentSafe(route) + "/" + goCommentSafe(ep.Path)
	if ep.Description != "" {
		what += ": " + goCommentSafe(ep.Description)
	}
	fmt.Fprintf(&sb, "// %s %s\n", method, what)
	if result == "" {
		fmt.Fprintf(&sb, "func (c *Client) %s(%s) error {\n", method, strings.Join(args, ", "))
	} else {
		fmt.Fprintf(&sb, "func (c *Client) %s(%s) (%s, error) {\n\tvar out %s\n", method, strings.Join(args, ", "), result, result)
	}
	fmt.Fprintf(&sb, "\tpath := %s\n", pathExpr)
	body := "nil"
	switch {
	case inQuery && len(rest) > 0:
		sb.WriteString("\tq := url.Values{}\n")
		for _, f := range rest {
			field := appspec.ToCamelCase(f.Name)
			if f.Required {
				fmt.Fprintf(&sb, "\tq.Set(%q, fmt.Sprint(in.%s))\n", f.Name, field)
			} else {
				fmt.Fprintf(&sb, "\tif in.%s != nil {\n\t\tq.Set(%q, fmt.Sprint(*in.%s))\n\t}\n", field, f.Name, field)
			}
		}
		sb.WriteString("\tif len(q) > 0 {\n\t\tpath += \"?\" + q.Encode()\n\t}\n")
	case !inQuery && len(rest) > 0:
		body = "in"
	case !inQuery:
		body = "struct{}{}"
	}
	if result == "" {
		fmt.Fprintf(&sb, "\treturn c.doJSON(ctx, %q, path, %s, nil)\n}\n\n", ep.Method, body)
		return sb.String()
	}
	fmt.Fprintf(&sb, "\tif err := c.doJSON(ctx, %q, path, %s, &out); err != nil {\n\t\treturn %s, err\n\t}\n\treturn out, nil\n}\n\n", ep.Method, body, zero)
	return sb.String()
}

func endpointRoute(decl framework.EntityDeclaration) string {
	table := declTable(decl)
	return url.PathEscape(table)
}

var jsReservedArgs = []string{
	"arguments", "await", "break", "case", "catch", "class", "const", "continue",
	"debugger", "default", "delete", "do", "else", "enum", "eval", "export",
	"extends", "false", "finally", "for", "function", "if", "implements",
	"import", "in", "input", "instanceof", "interface", "let", "new", "null",
	"package", "private", "protected", "public", "return", "static", "super",
	"switch", "this", "throw", "true", "try", "typeof", "var", "void", "while",
	"with", "yield",
	"encodeURIComponent", "undefined",
}

func jsEndpointArg(name string) string {
	arg := appspec.ToCamelJSON(name)
	if slices.Contains(jsReservedArgs, arg) {
		arg += "Arg"
	}
	return arg
}

func jsEndpointBinding(prop string, ep framework.Endpoint) string {
	_, rest, _ := endpointInput(ep)
	var args, parts []string
	lit := ""
	for _, seg := range strings.Split(ep.Path, "/") {
		if !strings.HasPrefix(seg, "{") {
			lit += "/" + seg
			continue
		}
		arg := jsEndpointArg(strings.Trim(seg, "{}"))
		args = append(args, arg)
		parts = append(parts, strconv.Quote(lit+"/"), "encodeURIComponent("+arg+")")
		lit = ""
	}
	if lit != "" {
		parts = append(parts, strconv.Quote(lit))
	}
	input := "undefined"
	if len(rest) > 0 {
		args = append(args, "input")
		input = "input"
	}
	call := fmt.Sprintf("this[%q]._endpoint(%q, %s, %s)", prop, ep.Method, strings.Join(parts, " + "), input)
	if ep.Returns == "" {
		call += ".then(() => undefined)"
	}
	return fmt.Sprintf("    this[%q][%q] = (%s) => %s;", prop, appspec.ToCamelJSON(ep.Name), strings.Join(args, ", "), call)
}

func tsEndpointType(t schema.FieldType) string {
	switch t {
	case schema.Int, schema.Float:
		return "number"
	case schema.Bool:
		return "boolean"
	case schema.JSON:
		return "unknown"
	default:
		return "string"
	}
}

func writeDTSEndpoints(dts *strings.Builder, decl framework.EntityDeclaration, ent cliEntity) {
	eps := sdkEndpoints(decl)
	if len(eps) == 0 {
		return
	}
	for _, ep := range eps {
		_, rest, _ := endpointInput(ep)
		if len(rest) == 0 {
			continue
		}
		fmt.Fprintf(dts, "export interface %sRequest {\n", appspec.ToCamelCase(ep.Name))
		for _, f := range rest {
			opt := "?"
			if f.Required {
				opt = ""
			}
			fmt.Fprintf(dts, "  %q%s: %s;\n", f.Name, opt, tsEndpointType(f.Type))
		}
		dts.WriteString("}\n\n")
	}
	fmt.Fprintf(dts, "export interface %sEndpoints {\n", ent.Struct)
	for _, ep := range eps {
		_, rest, _ := endpointInput(ep)
		var args []string
		for _, seg := range strings.Split(ep.Path, "/") {
			if strings.HasPrefix(seg, "{") {
				args = append(args, jsEndpointArg(strings.Trim(seg, "{}"))+": string")
			}
		}
		if len(rest) > 0 {
			args = append(args, "input: "+appspec.ToCamelCase(ep.Name)+"Request")
		}
		result := "void"
		if name, list := endpointReturnsList(ep); name != "" {
			result = appspec.ToCamelCase(name)
			if list {
				result += "[]"
			}
		}
		what := ep.Method + " /" + ent.Table + "/" + ep.Path
		if ep.Description != "" {
			what += ": " + ep.Description
		}
		fmt.Fprintf(dts, "  /** %s */\n  %s(%s): Promise<%s>;\n", tsCommentSafe(what), appspec.ToCamelJSON(ep.Name), strings.Join(args, ", "), result)
	}
	dts.WriteString("}\n\n")
}

var jsObjectMembers = []string{"constructor", "hasOwnProperty", "isPrototypeOf", "propertyIsEnumerable", "toLocaleString", "toString", "valueOf"}

var jsResourceMembers = []string{"list", "listCursor", "get", "create", "update", "patch", "remove", "batchCreate", "batchUpdate", "batchDelete", "watch", "transition", "client", "table", "columns"}

var goClientFields = []string{"BaseURL", "HTTP", "Token"}

func checkEndpointName(decl framework.EntityDeclaration, name string) error {
	js := appspec.ToCamelJSON(name)
	cmd := strings.ReplaceAll(name, "_", "-")
	moves := map[string]bool{}
	for _, t := range crud.RoutableTransitions(decl.States) {
		moves[t.Key] = true
		moves[appspec.ToCamelJSON(t.Key)] = true
	}
	switch {
	case slices.Contains(jsResourceMembers, js) || slices.Contains(jsObjectMembers, js):
		return fmt.Errorf("the JS client would bind it as %s, which every resource already has; rename the endpoint", js)
	case slices.Contains(goClientFields, appspec.ToCamelCase(name)):
		return fmt.Errorf("the Go client would declare it as the method Client.%s, which is already a Client field; rename the endpoint", appspec.ToCamelCase(name))
	case moves[name] || moves[js] || moves[cmd]:
		return fmt.Errorf("a state move of %s already binds that method and CLI command; rename the endpoint", decl.Name)
	case slices.Contains(cliVerbs, cmd) || cmd == "help":
		return fmt.Errorf("the CLI command %q is a built-in verb; rename the endpoint", cmd)
	}
	return nil
}

func checkEndpointReturns(decls []framework.EntityDeclaration) error {
	selected := map[string]bool{}
	for _, d := range decls {
		selected[d.Name] = true
	}
	for _, d := range decls {
		for _, ep := range sdkEndpoints(d) {
			if name, _ := endpointReturnsList(ep); name != "" && !selected[name] {
				return fmt.Errorf("endpoint %q on %q returns %s, which the selection leaves out: add %s to it, or exclude %s", ep.Name, d.Name, name, name, d.Name)
			}
		}
	}
	return nil
}
