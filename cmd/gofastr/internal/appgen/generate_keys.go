package appgen

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

type keyCol struct {
	Name  string
	Param string
	Field framework.FieldDeclaration
}

func declKeyCols(decl framework.EntityDeclaration) []keyCol {
	if appspec.DeclKeyIsID(decl) {
		return []keyCol{{Name: "id", Param: "id", Field: idField(decl)}}
	}
	out := make([]keyCol, len(decl.PrimaryKey))
	taken := map[string]bool{}
	for i, col := range decl.PrimaryKey {
		param := appspec.ToCamelJSON(col)
		for reservedKeyParams[param] || taken[param] {
			param += "Key"
		}
		taken[param] = true
		out[i] = keyCol{Name: col, Param: param}
		for _, f := range decl.Fields {
			if f.Name == col {
				out[i].Field = f
			}
		}
	}
	return out
}

func idField(decl framework.EntityDeclaration) framework.FieldDeclaration {
	for _, f := range decl.Fields {
		if f.Name == "id" {
			return f
		}
	}
	return framework.FieldDeclaration{Name: "id", Type: "string"}
}

func intID(decl framework.EntityDeclaration) bool {
	return appspec.DeclKeyIsID(decl) && goTypeForField(idField(decl).Type) == "int"
}

func goIDType(decl framework.EntityDeclaration) string {
	if intID(decl) {
		return "int"
	}
	return "string"
}

func goIDsType(decl framework.EntityDeclaration) string {
	return "[]" + goIDType(decl)
}

func keyStrings(decl framework.EntityDeclaration) string {
	if !intID(decl) {
		return ""
	}
	return `keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = strconv.Itoa(id)
	}
	`
}

func keysVar(decl framework.EntityDeclaration) string {
	if intID(decl) {
		return "keys"
	}
	return "ids"
}

func nestedKey(rel framework.Relation, decls []framework.EntityDeclaration) (name, typ, tag string) {
	for _, d := range decls {
		if d.Name != rel.Entity {
			continue
		}
		if cols := declKeyCols(d); !appspec.DeclKeyIsID(d) && len(cols) == 1 {
			return appspec.ToCamelCase(cols[0].Name), entityFieldGoType(appspec.ToCamelCase(d.Name), cols[0].Field), appspec.WireKey(cols[0].Name)
		}
		return "ID", goIDType(d), "id"
	}
	return "ID", "string", "id"
}

var reservedKeyParams = map[string]bool{
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
	"await": true, "catch": true, "class": true, "debugger": true, "delete": true,
	"do": true, "enum": true, "export": true, "extends": true, "false": true,
	"finally": true, "function": true, "implements": true, "in": true,
	"instanceof": true, "let": true, "new": true, "null": true, "private": true,
	"protected": true, "public": true, "static": true, "super": true, "this": true,
	"throw": true, "true": true, "try": true, "typeof": true, "void": true,
	"while": true, "with": true, "yield": true, "arguments": true, "eval": true,
	"undefined": true, "string": true, "error": true, "any": true, "nil": true,
	"framework": true, "crud": true, "entity": true, "schema": true, "filter": true,
	"file": true, "context": true, "json": true, "errors": true, "fmt": true,
	"io": true, "os": true, "strconv": true, "strings": true, "time": true,
	"bytes": true, "sync": true, "sql": true, "slices": true, "sort": true,
	"multipart": true, "textproto": true, "mime": true, "flag": true, "signal": true,
	"bufio": true, "tabwriter": true, "filepath": true, "cmp": true, "textsafe": true,
	"c": true, "ctx": true, "body": true, "out": true, "path": true, "files": true,
	"params": true, "items": true, "ids": true, "fn": true, "opts": true, "move": true,
	"includes": true, "patch": true, "r": true, "url": true, "http": true, "err": true,
}

func goKeyParams(decl framework.EntityDeclaration) string {
	if intID(decl) {
		return "id int"
	}
	cols := declKeyCols(decl)
	parts := make([]string, len(cols))
	for i, k := range cols {
		parts[i] = k.Param + " string"
	}
	return strings.Join(parts, ", ")
}

func goKeyPath(decl framework.EntityDeclaration, route string) string {
	if intID(decl) {
		return `"/` + route + `/"+strconv.Itoa(id)`
	}
	var sb strings.Builder
	sb.WriteString(`"/` + route + `/"`)
	for i, k := range declKeyCols(decl) {
		if i > 0 {
			sb.WriteString(`+"/"`)
		}
		sb.WriteString("+url.PathEscape(" + k.Param + ")")
	}
	return sb.String()
}

func keyArgs(decl framework.EntityDeclaration) string {
	cols := declKeyCols(decl)
	parts := make([]string, len(cols))
	for i, k := range cols {
		parts[i] = k.Param
	}
	return strings.Join(parts, ", ")
}

func keyDoc(decl framework.EntityDeclaration) string {
	if len(declKeyCols(decl)) == 1 {
		return goCommentSafe(declKeyCols(decl)[0].Name)
	}
	return "(" + goCommentSafe(keyColNames(decl)) + ")"
}

func batchKeyDoc(decl framework.EntityDeclaration) string {
	if appspec.DeclKeyIsID(decl) {
		return "ID"
	}
	cols := declKeyCols(decl)
	parts := make([]string, len(cols))
	for i, k := range cols {
		parts[i] = appspec.ToCamelCase(k.Name)
	}
	return strings.Join(parts, " and ")
}

func batchIDsDoc(decl framework.EntityDeclaration) string {
	if len(declKeyCols(decl)) > 1 {
		return "record ids (see " + appspec.ToCamelCase(decl.Name) + "ID)"
	}
	return "ids"
}

func goKeyJoin(decl framework.EntityDeclaration) string {
	cols := declKeyCols(decl)
	parts := make([]string, len(cols))
	for i, k := range cols {
		parts[i] = "url.PathEscape(" + k.Param + ")"
	}
	return strings.Join(parts, `+"/"+`)
}

func keyColumnNames(decl framework.EntityDeclaration) []string {
	cols := declKeyCols(decl)
	out := make([]string, len(cols))
	for i, k := range cols {
		out[i] = k.Name
	}
	return out
}

func keyColNames(decl framework.EntityDeclaration) string {
	cols := declKeyCols(decl)
	parts := make([]string, len(cols))
	for i, k := range cols {
		parts[i] = k.Name
	}
	return strings.Join(parts, ", ")
}
