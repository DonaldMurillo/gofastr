package appgen

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/framework"
)

// keyCol is one primary-key column as the generated clients name it.
type keyCol struct {
	Name  string // the column
	Param string // the Go and JS parameter naming its value
	Field framework.FieldDeclaration
}

// declKeyCols lists the entity's key columns in key order, each with the
// parameter a generated record method takes for it. A parameter that is
// a Go or JS keyword, a package the generated file imports, a name the
// method body already uses, or the parameter of an earlier key column
// gains a "Key" suffix until it is free.
func declKeyCols(decl framework.EntityDeclaration) []keyCol {
	if appspec.DeclKeyIsID(decl) {
		return []keyCol{{Name: "id", Param: "id", Field: framework.FieldDeclaration{Name: "id", Type: "string"}}}
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

// reservedKeyParams are the names a key parameter may not take: Go and JS
// keywords and predeclared names, the packages the generated files
// import, and the locals the generated record methods declare.
var reservedKeyParams = map[string]bool{
	// Go keywords.
	"break": true, "case": true, "chan": true, "const": true, "continue": true,
	"default": true, "defer": true, "else": true, "fallthrough": true, "for": true,
	"func": true, "go": true, "goto": true, "if": true, "import": true,
	"interface": true, "map": true, "package": true, "range": true, "return": true,
	"select": true, "struct": true, "switch": true, "type": true, "var": true,
	// JS reserved words the Go list misses.
	"await": true, "catch": true, "class": true, "debugger": true, "delete": true,
	"do": true, "enum": true, "export": true, "extends": true, "false": true,
	"finally": true, "function": true, "implements": true, "in": true,
	"instanceof": true, "let": true, "new": true, "null": true, "private": true,
	"protected": true, "public": true, "static": true, "super": true, "this": true,
	"throw": true, "true": true, "try": true, "typeof": true, "void": true,
	"while": true, "with": true, "yield": true, "arguments": true, "eval": true,
	"undefined": true, "string": true, "error": true, "any": true, "nil": true,
	// Packages the generated client, repository and CLI import.
	"framework": true, "crud": true, "entity": true, "schema": true, "filter": true,
	"file": true, "context": true, "json": true, "errors": true, "fmt": true,
	"io": true, "os": true, "strconv": true, "strings": true, "time": true,
	"bytes": true, "sync": true, "sql": true, "slices": true, "sort": true,
	"multipart": true, "textproto": true, "mime": true, "flag": true, "signal": true,
	"bufio": true, "tabwriter": true, "filepath": true, "cmp": true, "textsafe": true,
	// Locals and receivers in the generated methods.
	"c": true, "ctx": true, "body": true, "out": true, "path": true, "files": true,
	"params": true, "items": true, "ids": true, "fn": true, "opts": true, "move": true,
	"includes": true, "patch": true, "r": true, "url": true, "http": true, "err": true,
}

// goKeyParams is the parameter list for one record's key: "id string",
// or "studentId string, courseId string".
func goKeyParams(decl framework.EntityDeclaration) string {
	cols := declKeyCols(decl)
	parts := make([]string, len(cols))
	for i, k := range cols {
		parts[i] = k.Param + " string"
	}
	return strings.Join(parts, ", ")
}

// goKeyPath is the Go expression for a record's path under route: one
// escaped segment per key column, in key order.
func goKeyPath(decl framework.EntityDeclaration, route string) string {
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

// keyArgs lists a record's key parameters as call arguments.
func keyArgs(decl framework.EntityDeclaration) string {
	cols := declKeyCols(decl)
	parts := make([]string, len(cols))
	for i, k := range cols {
		parts[i] = k.Param
	}
	return strings.Join(parts, ", ")
}

// keyDoc names a record's key in a generated doc comment: "id", or
// "(student_id, course_id)".
func keyDoc(decl framework.EntityDeclaration) string {
	if len(declKeyCols(decl)) == 1 {
		return goCommentSafe(declKeyCols(decl)[0].Name)
	}
	return "(" + goCommentSafe(keyColNames(decl)) + ")"
}

// batchKeyDoc names the fields a batch-update item carries its key in.
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

// batchIDsDoc names what a batch delete's ids are.
func batchIDsDoc(decl framework.EntityDeclaration) string {
	if len(declKeyCols(decl)) > 1 {
		return "record ids (see " + appspec.ToCamelCase(decl.Name) + "ID)"
	}
	return "ids"
}

// goKeyJoin is the Go expression for a composite record id: each key
// value path-escaped and joined with "/", the spelling entity.JoinKey
// produces and the URL tail of the record.
func goKeyJoin(decl framework.EntityDeclaration) string {
	cols := declKeyCols(decl)
	parts := make([]string, len(cols))
	for i, k := range cols {
		parts[i] = "url.PathEscape(" + k.Param + ")"
	}
	return strings.Join(parts, `+"/"+`)
}

// keyColumnNames lists the key columns in order: ["id"] for the generated
// key.
func keyColumnNames(decl framework.EntityDeclaration) []string {
	cols := declKeyCols(decl)
	out := make([]string, len(cols))
	for i, k := range cols {
		out[i] = k.Name
	}
	return out
}

// keyColNames lists the key columns, comma-separated, for comments.
func keyColNames(decl framework.EntityDeclaration) string {
	cols := declKeyCols(decl)
	parts := make([]string, len(cols))
	for i, k := range cols {
		parts[i] = k.Name
	}
	return strings.Join(parts, ", ")
}
