package appspec

import (
	"fmt"
	"go/ast"
	"go/token"
	"maps"
	"slices"
	"strconv"

	"github.com/DonaldMurillo/gofastr/framework"
	fwentity "github.com/DonaldMurillo/gofastr/framework/entity"
)

func AstString(e ast.Expr) string {
	if lit, ok := e.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		if s, err := strconv.Unquote(lit.Value); err == nil {
			return s
		}
	}
	return ""
}

func astAccessPerm(e ast.Expr) string {
	if sel, ok := e.(*ast.SelectorExpr); ok && sel.Sel.Name == "Anyone" {
		if pkg, ok := sel.X.(*ast.Ident); ok && (pkg.Name == "framework" || pkg.Name == "entity") {
			return fwentity.Anyone
		}
	}
	return AstString(e)
}

func AstBool(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == "true"
}

// AstSelName returns the trailing identifier of pkg.Name, or a bare ident's name.
func AstSelName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.Ident:
		return t.Name
	}
	return ""
}

func astPtrCallFloat(e ast.Expr) (float64, bool) {
	if call, ok := e.(*ast.CallExpr); ok && len(call.Args) == 1 {
		return astFloat(call.Args[0])
	}
	return 0, false
}

func astPtrCallBool(e ast.Expr) (bool, bool) {
	if call, ok := e.(*ast.CallExpr); ok && len(call.Args) == 1 {
		return AstBool(call.Args[0]), true
	}
	return false, false
}

func astFloat(e ast.Expr) (float64, bool) {
	if text, kind, ok := numberLit(e); ok && (kind == token.FLOAT || kind == token.INT) {
		f, err := strconv.ParseFloat(text, 64)
		return f, err == nil
	}
	return 0, false
}

func numberLit(e ast.Expr) (string, token.Token, bool) {
	sign := ""
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.SUB {
		sign, e = "-", u.X
	}
	lit, ok := e.(*ast.BasicLit)
	if !ok || (lit.Kind != token.INT && lit.Kind != token.FLOAT) {
		return "", token.ILLEGAL, false
	}
	return sign + lit.Value, lit.Kind, true
}

func AstStringSlice(e ast.Expr) []string {
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(cl.Elts))
	for _, el := range cl.Elts {
		out = append(out, AstString(el))
	}
	return out
}

// AstAny reads a literal as the YAML parser's types: string, int64, float64, bool, nil, map[string]any, []any.
func AstAny(e ast.Expr) any {
	if text, kind, ok := numberLit(e); ok {
		if kind == token.INT {
			if i, err := strconv.ParseInt(text, 10, 64); err == nil {
				return i
			}
		} else if f, err := strconv.ParseFloat(text, 64); err == nil {
			return f
		}
		return text
	}
	switch t := e.(type) {
	case *ast.BasicLit:
		if t.Kind == token.STRING {
			if s, err := strconv.Unquote(t.Value); err == nil {
				return s
			}
		}
		return t.Value
	case *ast.Ident:
		switch t.Name {
		case "true":
			return true
		case "false":
			return false
		case "nil":
			return nil
		}
		return t.Name
	case *ast.CompositeLit:
		isMap := false
		if _, ok := t.Type.(*ast.MapType); ok {
			isMap = true
		} else if len(t.Elts) > 0 {
			_, isMap = t.Elts[0].(*ast.KeyValueExpr)
		}
		if isMap {
			m := map[string]any{}
			for _, el := range t.Elts {
				if kv, ok := el.(*ast.KeyValueExpr); ok {
					m[AstString(kv.Key)] = AstAny(kv.Value)
				}
			}
			return m
		}
		out := make([]any, 0, len(t.Elts))
		for _, el := range t.Elts {
			out = append(out, AstAny(el))
		}
		return out
	}
	return nil
}

func unwrapBuilderCalls(e ast.Expr) (ast.Expr, map[string][]ast.Expr) {
	builders := map[string][]ast.Expr{}
	for {
		call, ok := e.(*ast.CallExpr)
		if !ok {
			return e, builders
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return e, builders
		}
		builders[sel.Sel.Name] = call.Args
		e = sel.X
	}
}

// FieldVals returns the key to value expressions of a struct literal, builder calls unwrapped.
func FieldVals(e ast.Expr) map[string]ast.Expr {
	out := map[string]ast.Expr{}
	e, _ = unwrapBuilderCalls(e)
	if ptr, ok := e.(*ast.UnaryExpr); ok && ptr.Op == token.AND {
		e = ptr.X
	}
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return out
	}
	for _, el := range cl.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if id, ok := kv.Key.(*ast.Ident); ok {
				out[id.Name] = kv.Value
			}
		}
	}
	return out
}

var schemaTypeToYAML = map[string]string{
	"String": "string", "Text": "text", "Int": "int", "Float": "float",
	"Decimal": "decimal", "Bool": "bool", "Enum": "enum", "UUID": "uuid",
	"Timestamp": "timestamp", "Date": "date", "JSON": "json",
	"Relation": "relation", "Image": "image", "File": "file",
}

func relationTypeFromConstName(name string) framework.RelationType {
	for _, k := range RelationKinds {
		if k.ConstName == name {
			return k.Typ
		}
	}
	return framework.RelManyToOne
}

func packDisplayFromExpr(e ast.Expr) *fwentity.DisplayConfig {
	d := FieldVals(e)
	out := &fwentity.DisplayConfig{
		Singular:    AstString(d["Singular"]),
		Plural:      AstString(d["Plural"]),
		TitleFields: AstStringSlice(d["TitleFields"]),
		Description: AstString(d["Description"]),
		Columns:     AstStringSlice(d["Columns"]),
		Facets:      AstStringSlice(d["Facets"]),
		NoDuplicate: AstBool(d["NoDuplicate"]),
		NoBulk:      AstBool(d["NoBulk"]),
	}
	if v, ok := d["PageSizes"]; ok {
		if cl, ok := unwrapPtr(v).(*ast.CompositeLit); ok {
			for _, elt := range cl.Elts {
				if n, ok := AstInt(elt); ok {
					out.PageSizes = append(out.PageSizes, n)
				}
			}
		}
	}
	if v, ok := d["Nav"]; ok {
		n := FieldVals(v)
		nav := &fwentity.EntityNav{
			Group: AstString(n["Group"]), Icon: AstString(n["Icon"]), Hide: AstBool(n["Hide"]),
			HideCount: AstBool(n["HideCount"]),
		}
		if order, ok := AstInt(n["Order"]); ok {
			nav.Order = order
		}
		out.Nav = nav
	}
	if v, ok := d["Views"]; ok {
		if cl, ok := unwrapPtr(v).(*ast.CompositeLit); ok {
			for _, elt := range cl.Elts {
				m := FieldVals(elt)
				out.Views = append(out.Views, fwentity.ListView{
					Key: AstString(m["Key"]), Label: AstString(m["Label"]),
					Where: AstString(m["Where"]), Sort: AstString(m["Sort"]),
					As: AstString(m["As"]), Default: AstBool(m["Default"]),
				})
			}
		}
	}
	if v, ok := d["Form"]; ok {
		f := FieldVals(v)
		out.Form = &fwentity.EntityForm{
			Main: packFormItems(f["Main"]), Side: packFormItems(f["Side"]),
		}
	}
	if v, ok := d["Card"]; ok {
		c := FieldVals(v)
		out.Card = &fwentity.CardFields{
			Title: AstString(c["Title"]), Subtitle: AstString(c["Subtitle"]),
			Badge: AstString(c["Badge"]), Meta: AstStringSlice(c["Meta"]),
		}
	}
	if v, ok := d["Fields"]; ok {
		if cl, ok := unwrapPtr(v).(*ast.CompositeLit); ok {
			out.Fields = map[string]fwentity.FieldDisplay{}
			for _, elt := range cl.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				h := FieldVals(kv.Value)
				out.Fields[AstString(kv.Key)] = fwentity.FieldDisplay{
					Label: AstString(h["Label"]), Help: AstString(h["Help"]),
					Placeholder: AstString(h["Placeholder"]), Locked: AstBool(h["Locked"]),
					Omit: AstBool(h["Omit"]), ShowWhen: AstString(h["ShowWhen"]),
					Input: AstString(h["Input"]),
				}
			}
		}
	}
	if displayIsEmpty(out) {
		return nil
	}
	return out
}

func packFormItems(e ast.Expr) []fwentity.FormItem {
	cl, ok := unwrapPtr(e).(*ast.CompositeLit)
	if !ok {
		return nil
	}
	var out []fwentity.FormItem
	for _, elt := range cl.Elts {
		m := FieldVals(elt)
		item := fwentity.FormItem{
			Field:     AstString(m["Field"]),
			Row:       AstStringSlice(m["Row"]),
			Section:   AstString(m["Section"]),
			Help:      AstString(m["Help"]),
			Collapsed: AstBool(m["Collapsed"]),
			Items:     packFormItems(m["Items"]),
		}
		out = append(out, item)
	}
	return out
}

func packStatesFromExpr(e ast.Expr) *fwentity.StatesConfig {
	s := FieldVals(e)
	out := &fwentity.StatesConfig{
		Field:    AstString(s["Field"]),
		Initial:  AstStringSlice(s["Initial"]),
		Advisory: AstBool(s["Advisory"]),
	}
	if v, ok := s["Transitions"]; ok {
		if cl, ok := unwrapPtr(v).(*ast.CompositeLit); ok {
			for _, elt := range cl.Elts {
				m := FieldVals(elt)
				out.Transitions = append(out.Transitions, fwentity.Transition{
					Key: AstString(m["Key"]), Label: AstString(m["Label"]),
					From: AstStringSlice(m["From"]), To: AstString(m["To"]),
					Stamp: AstString(m["Stamp"]), Variant: AstString(m["Variant"]),
					Permission: AstString(m["Permission"]), System: AstBool(m["System"]),
				})
			}
		}
	}
	if out.Field == "" {
		return nil
	}
	return out
}

func unwrapPtr(e ast.Expr) ast.Expr {
	if u, ok := e.(*ast.UnaryExpr); ok && u.Op == token.AND {
		return u.X
	}
	return e
}

func displayIsEmpty(d *fwentity.DisplayConfig) bool {
	return d.Singular == "" && d.Plural == "" && len(d.TitleFields) == 0 && d.Description == "" &&
		len(d.Columns) == 0 && len(d.Views) == 0 && len(d.Facets) == 0 && len(d.Fields) == 0 &&
		len(d.PageSizes) == 0 && !d.NoDuplicate && !d.NoBulk &&
		d.Nav == nil && d.Form == nil && d.Card == nil
}

// PackEntityDeclFromCall rebuilds an EntityDeclaration from an app.Entity(name, config) call.
func PackEntityDeclFromCall(call *ast.CallExpr) (framework.EntityDeclaration, error) {
	decl := framework.EntityDeclaration{Name: AstString(call.Args[0])}
	_, builders := unwrapBuilderCalls(call.Args[1])
	cfg := FieldVals(call.Args[1])
	if v, ok := cfg["Table"]; ok {
		decl.Table = AstString(v)
	}
	if v, ok := cfg["PrimaryKey"]; ok {
		decl.PrimaryKey = AstStringSlice(v)
	}
	if v, ok := cfg["Scope"]; ok {
		s := FieldVals(v)
		scope := &framework.ScopeDeclaration{
			SoftDelete: AstBool(s["SoftDelete"]), MultiTenant: AstBool(s["MultiTenant"]),
			TenantField: AstString(s["TenantField"]), OwnerField: AstString(s["OwnerField"]),
			CrossOwnerRead: AstString(s["CrossOwnerRead"]),
		}
		if *scope != (framework.ScopeDeclaration{}) {
			decl.Scope = scope
		}
	}
	if v, ok := cfg["Pagination"]; ok {
		p := FieldVals(v)
		pagination := &framework.PaginationDeclaration{
			CursorField: AstString(p["CursorField"]), CursorFields: AstStringSlice(p["CursorFields"]),
		}
		if limit, ok := AstInt(p["MaxListLimit"]); ok {
			pagination.MaxListLimit = limit
		}
		if pagination.CursorField != "" || len(pagination.CursorFields) > 0 || pagination.MaxListLimit != 0 {
			decl.Pagination = pagination
		}
	}
	if v, ok := cfg["SearchFields"]; ok {
		decl.SearchFields = AstStringSlice(v)
	}
	if v, ok := cfg["Display"]; ok {
		if d := packDisplayFromExpr(v); d != nil {
			decl.Display = d
		}
	}
	if v, ok := cfg["States"]; ok {
		if st := packStatesFromExpr(v); st != nil {
			decl.States = st
		}
	}
	if args, ok := builders["WithTimestamps"]; ok && len(args) == 1 {
		b := AstBool(args[0])
		decl.Timestamps = &b
	} else if v, ok := cfg["Timestamps"]; ok {
		if b, ok := astPtrCallBool(v); ok {
			decl.Timestamps = &b
		}
	}
	if v, ok := cfg["Exposure"]; ok {
		x := FieldVals(v)
		exposure := &framework.ExposureDeclaration{MCP: AstBool(x["MCP"]), Public: AstBool(x["Public"])}
		if b, ok := astPtrCallBool(x["CRUD"]); ok {
			exposure.CRUD = &b
		}
		if accessExpr, ok := x["Access"]; ok {
			a := FieldVals(accessExpr)
			exposure.Access = &framework.AccessDeclaration{
				Read: astAccessPerm(a["Read"]), Create: astAccessPerm(a["Create"]),
				Update: astAccessPerm(a["Update"]), Delete: astAccessPerm(a["Delete"]),
			}
		}
		if rsExpr, ok := x["ReadScope"]; ok {
			r := FieldVals(rsExpr)
			rs := &framework.ReadScopeDeclaration{Unrestricted: AstString(r["Unrestricted"])}
			if filterExpr, ok := r["Filter"]; ok {
				inner, _ := unwrapBuilderCalls(filterExpr)
				if cl, ok := inner.(*ast.CompositeLit); ok {
					for _, item := range cl.Elts {
						p := FieldVals(item)
						rs.Filter = append(rs.Filter, framework.RowPredicateDeclaration{
							Field: AstString(p["Field"]), Op: AstString(p["Op"]),
							Value: AstString(p["Value"]), Values: AstStringSlice(p["Values"]),
						})
					}
				}
			}
			if rs.Unrestricted != "" || len(rs.Filter) > 0 {
				exposure.ReadScope = rs
			}
		}
		if exposure.CRUD != nil || exposure.MCP || exposure.Public || exposure.Access != nil || exposure.ReadScope != nil {
			decl.Exposure = exposure
		}
	}
	if v, ok := cfg["Properties"]; ok {
		if m, ok := AstAny(v).(map[string]any); ok && len(m) > 0 {
			decl.Properties = m
		}
	}
	if v, ok := cfg["Fields"]; ok {
		var err error
		if decl.Fields, err = packReadFields(v); err != nil {
			return decl, err
		}
	}
	if v, ok := cfg["Indices"]; ok {
		decl.Indices = packReadIndices(v)
	}
	if v, ok := cfg["Relations"]; ok {
		var err error
		decl.Relations, err = packReadRelations(v)
		if err != nil {
			return decl, err
		}
	}
	return decl, nil
}

var packHeldFieldKeys = map[string]bool{
	"Name": true, "Type": true, "Required": true, "Unique": true, "Default": true,
	"AutoGenerate": true, "ReadOnly": true, "Hidden": true, "NoQuery": true,
	"Max": true, "Min": true, "Pattern": true, "Values": true, "To": true, "Many": true,
}

func packZeroLiteral(e ast.Expr) bool {
	switch t := e.(type) {
	case *ast.BasicLit:
		return t.Value == `""` || t.Value == "``" || t.Value == "0"
	case *ast.Ident:
		return t.Name == "false" || t.Name == "nil"
	}
	return false
}

func packReadFields(e ast.Expr) ([]framework.FieldDeclaration, error) {
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil, nil
	}
	out := make([]framework.FieldDeclaration, 0, len(cl.Elts))
	for _, el := range cl.Elts {
		fv := FieldVals(el)
		name := AstString(fv["Name"])
		for _, key := range slices.Sorted(maps.Keys(fv)) {
			if !packHeldFieldKeys[key] && !packZeroLiteral(fv[key]) {
				return out, fmt.Errorf("field %q: schema.Field key %s has no app spec equivalent; pack would drop it", name, key)
			}
		}
		typeName := "String"
		if v, set := fv["Type"]; set {
			typeName = AstSelName(v)
		}
		typ, ok := schemaTypeToYAML[typeName]
		if !ok {
			return out, fmt.Errorf("field %q: Type %s has no app spec name", name, typeName)
		}
		for _, key := range []string{"Max", "Min"} {
			if v, set := fv[key]; set && !packZeroLiteral(v) {
				if _, ok := astPtrCallFloat(v); !ok {
					return out, fmt.Errorf("field %q: %s is not a literal number pack can read", name, key)
				}
			}
		}
		if v, set := fv["AutoGenerate"]; set && autoGenerateToYAML(AstSelName(v)) == "" && AstSelName(v) != "AutoNone" {
			return out, fmt.Errorf("field %q: AutoGenerate %s has no app spec name", name, AstSelName(v))
		}
		f := framework.FieldDeclaration{
			Name:     name,
			Type:     typ,
			Required: AstBool(fv["Required"]),
			Unique:   AstBool(fv["Unique"]),
			ReadOnly: AstBool(fv["ReadOnly"]),
			Hidden:   AstBool(fv["Hidden"]),
			NoQuery:  AstBool(fv["NoQuery"]),
			Pattern:  AstString(fv["Pattern"]),
			To:       AstString(fv["To"]),
			Many:     AstBool(fv["Many"]),
		}
		if v, ok := fv["Default"]; ok {
			f.Default = AstAny(v)
		}
		if v, ok := fv["Values"]; ok {
			f.Values = AstStringSlice(v)
		}
		if v, ok := fv["Max"]; ok {
			if n, ok := astPtrCallFloat(v); ok {
				f.Max = &n
			}
		}
		if v, ok := fv["Min"]; ok {
			if n, ok := astPtrCallFloat(v); ok {
				f.Min = &n
			}
		}
		if v, ok := fv["AutoGenerate"]; ok {
			f.AutoGenerate = autoGenerateToYAML(AstSelName(v))
		}
		out = append(out, f)
	}
	return out, nil
}

func autoGenerateToYAML(constName string) string {
	switch constName {
	case "AutoUUID":
		return "uuid"
	case "AutoTimestamp":
		return "timestamp"
	case "AutoIncrement":
		return "increment"
	default:
		return ""
	}
}

func packReadIndices(e ast.Expr) []framework.Index {
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil
	}
	out := make([]framework.Index, 0, len(cl.Elts))
	for _, el := range cl.Elts {
		iv := FieldVals(el)
		out = append(out, framework.Index{
			Name:       AstString(iv["Name"]),
			Columns:    AstStringSlice(iv["Columns"]),
			Unique:     AstBool(iv["Unique"]),
			Expression: AstString(iv["Expression"]),
		})
	}
	return out
}

func packReadRelations(e ast.Expr) ([]framework.Relation, error) {
	cl, ok := e.(*ast.CompositeLit)
	if !ok {
		return nil, nil
	}
	out := make([]framework.Relation, 0, len(cl.Elts))
	for _, el := range cl.Elts {
		rv := FieldVals(el)
		var onDelete framework.OnDeleteAction
		if rawOD, ok := rv["OnDelete"]; ok && rawOD != nil {
			od := AstSelName(rawOD)
			switch od {
			case "OnDeleteCascade":
				onDelete = framework.OnDeleteCascade
			case "OnDeleteSetNull":
				onDelete = framework.OnDeleteSetNull
			case "OnDeleteRestrict":
				onDelete = framework.OnDeleteRestrict
			case "OnDeleteNoAction":
				onDelete = framework.OnDeleteNoAction
			default:
				return nil, fmt.Errorf("unsupported OnDelete action %q", od)
			}
		}
		out = append(out, framework.Relation{
			Type:             relationTypeFromConstName(AstSelName(rv["Type"])),
			Name:             AstString(rv["Name"]),
			Entity:           AstString(rv["Entity"]),
			ForeignKey:       AstStringSlice(rv["ForeignKey"]),
			Through:          AstString(rv["Through"]),
			LocalKey:         AstString(rv["LocalKey"]),
			ForeignKeyTarget: AstString(rv["ForeignKeyTarget"]),
			OnDelete:         onDelete,
			CascadeWrite:     AstBool(rv["CascadeWrite"]),
			Hidden:           AstBool(rv["Hidden"]),
		})
	}
	return out, nil
}

func AstInt(e ast.Expr) (int, bool) {
	if text, kind, ok := numberLit(e); ok && kind == token.INT {
		if n, err := strconv.Atoi(text); err == nil {
			return n, true
		}
	}
	return 0, false
}
