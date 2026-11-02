package appgen

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/core/settings"
	"github.com/DonaldMurillo/gofastr/framework"
)

// Endpoints declared in gofastr.settings.yml link a typed handler in the
// entities package. The generator reads the handler's input and output
// types from the Go source, statically: the project is never built or run
// to generate it. The input struct's tags say where each value travels,
// exactly as handler.Bind reads them: `path:"id"` from the route, `query`
// from the query string, the rest from the JSON body.

const handlerImportPath = "github.com/DonaldMurillo/gofastr/core/handler"

// AppEndpoint is one settings endpoint resolved against the entities
// package source.
type AppEndpoint struct {
	settings.Endpoint
	// input is the handler's input type and output its result type, as Go
	// source in package entities.
	input, output string
	// returns is framework.Endpoint.Returns: the entity the call answers
	// with, "[]" and it for a list, "" for struct{}.
	returns string
	params  []endpointParam
}

// endpointParam is one input value of an endpoint.
type endpointParam struct {
	// name is the wire name: the path, query or json tag.
	name     string
	in       string // "path", "query" or "body"
	typ      schema.FieldType
	required bool
}

// endpointQueryMethods carry their input in the query string; the others
// carry it as a JSON body.
func endpointQueryMethods(method string) bool { return method == "GET" || method == "DELETE" }

// ReadAppEndpoints reads the endpoints section of dir's settings file and
// resolves each against the entities package under dir. It reads only the
// settings file in dir itself, the way the cli and sdk entries are read:
// a parent directory's file belongs to another project. nil when there is
// no file or no endpoints section.
func ReadAppEndpoints(dir string, decls []framework.EntityDeclaration) ([]AppEndpoint, error) {
	f, err := settings.Load(filepath.Join(dir, settings.FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	entries, err := f.Endpoints()
	if err != nil || len(entries) == 0 {
		return nil, err
	}
	src, err := readEntitiesSource(dir)
	if err != nil {
		return nil, err
	}
	return src.resolveAll(f.Path, entries, decls)
}

// resolveAll resolves every entry against the source. at names where the
// entries came from in errors.
func (src *entitiesSource) resolveAll(at string, entries []settings.Endpoint, decls []framework.EntityDeclaration) ([]AppEndpoint, error) {
	entities := map[string]framework.EntityDeclaration{}
	byStruct := map[string]string{}
	for _, d := range decls {
		entities[d.Name] = d
		byStruct[appspec.ToCamelCase(d.Name)] = d.Name
	}
	out := make([]AppEndpoint, 0, len(entries))
	// Distinct snake names can fold to one generated name (cache_bust1,
	// cache_bust_1: CacheBust1), and the Go client's methods share one
	// namespace across entities.
	calls := map[string]string{}
	for _, e := range entries {
		if prev, ok := calls[appspec.ToCamelCase(e.Name)]; ok {
			return nil, fmt.Errorf("%s: endpoints %s and %s both become the call %s: rename one", at, prev, e.Name, appspec.ToCamelCase(e.Name))
		}
		calls[appspec.ToCamelCase(e.Name)] = e.Name
		ep, err := src.resolve(e, entities, byStruct)
		if err != nil {
			return nil, fmt.Errorf("%s: endpoint %s: %w", at, e.Name, err)
		}
		out = append(out, ep)
	}
	return out, nil
}

// entitiesSource is the parsed entities package.
type entitiesSource struct {
	files map[string]*ast.File // by path, entities/<name>.go
	types map[string]*ast.TypeSpec
}

// readEntitiesSource parses every non-test Go file of dir/entities through
// an *os.Root, so a symlinked leaf pointing out of the project is never read.
func readEntitiesSource(dir string) (*entitiesSource, error) {
	files, err := readEntitiesFiles(dir)
	if err != nil {
		return nil, err
	}
	return parseEntitiesSource(files)
}

// readEntitiesFiles reads the Go files of dir's entities package, tests
// left out, keyed by path (entities/<name>.go).
func readEntitiesFiles(dir string) (map[string]string, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	entries, err := RootReadDirEntries(root, "entities")
	if err != nil {
		return nil, fmt.Errorf("endpoints need the entities package: %w", err)
	}
	files := map[string]string{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		rel := "entities/" + name
		data, err := root.ReadFile(rel)
		if err != nil {
			continue // a leaf outside the root: never parsed
		}
		files[rel] = string(data)
	}
	return files, nil
}

// parseEntitiesSource parses the entities package from files, keyed by
// path (entities/<name>.go). generate app resolves an app spec's endpoints
// against the package it is about to write, before any of it is on disk.
func parseEntitiesSource(files map[string]string) (*entitiesSource, error) {
	src := &entitiesSource{files: map[string]*ast.File{}, types: map[string]*ast.TypeSpec{}}
	fset := token.NewFileSet()
	for _, rel := range slices.Sorted(maps.Keys(files)) {
		file, err := parser.ParseFile(fset, rel, files[rel], parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", rel, err)
		}
		src.files[rel] = file
		for _, d := range file.Decls {
			gd, ok := d.(*ast.GenDecl)
			if !ok || gd.Tok != token.TYPE {
				continue
			}
			for _, s := range gd.Specs {
				ts := s.(*ast.TypeSpec)
				src.types[ts.Name.Name] = ts
			}
		}
	}
	return src, nil
}

// resolve checks one entry against the source and reads its types.
func (src *entitiesSource) resolve(e settings.Endpoint, entities map[string]framework.EntityDeclaration, byStruct map[string]string) (AppEndpoint, error) {
	ep := AppEndpoint{Endpoint: e}
	decl, ok := entities[e.Entity]
	if !ok {
		return ep, fmt.Errorf("entity %q is not declared in entities/", e.Entity)
	}
	if err := checkEndpointName(decl, e.Name); err != nil {
		return ep, err
	}
	file := src.files[e.HandlerFile()]
	if file == nil {
		return ep, fmt.Errorf("handler file %s does not exist in the entities package", e.HandlerFile())
	}
	in, out, err := handlerTypes(file, e.HandlerName())
	if err != nil {
		return ep, fmt.Errorf("handler %s: %w", e.Handler, err)
	}
	ep.input, ep.output = types.ExprString(in), types.ExprString(out)
	ep.returns, err = endpointReturns(out, byStruct)
	if err != nil {
		return ep, fmt.Errorf("handler %s: %w", e.Handler, err)
	}
	ep.params, err = src.inputParams(e, in)
	if err != nil {
		return ep, fmt.Errorf("handler %s: input %s: %w", e.Handler, ep.input, err)
	}
	return ep, nil
}

// handlerTypes finds name in file, a func(context.Context, I) (O, error) or
// a var of type handler.Handler[I, O], and returns I and O.
func handlerTypes(file *ast.File, name string) (in, out ast.Expr, err error) {
	imports := fileImports(file)
	for _, d := range file.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv != nil || d.Name.Name != name {
				continue
			}
			ft := d.Type
			params, results := fieldTypes(ft.Params), fieldTypes(ft.Results)
			if ft.TypeParams != nil || len(params) != 2 || len(results) != 2 ||
				!isSelector(params[0], imports, "context", "Context") || !isIdent(results[1], "error") {
				return nil, nil, fmt.Errorf("func %s must have the handler signature func(ctx context.Context, in In) (Out, error)", name)
			}
			return params[1], results[0], nil
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, s := range d.Specs {
				vs := s.(*ast.ValueSpec)
				if !slices.ContainsFunc(vs.Names, func(n *ast.Ident) bool { return n.Name == name }) {
					continue
				}
				idx, ok := vs.Type.(*ast.IndexListExpr)
				if !ok || len(idx.Indices) != 2 || !isSelector(idx.X, imports, handlerImportPath, "Handler") {
					return nil, nil, fmt.Errorf("var %s must be declared with its type: var %s handler.Handler[In, Out] = ...", name, name)
				}
				return idx.Indices[0], idx.Indices[1], nil
			}
		}
	}
	return nil, nil, fmt.Errorf("the file declares no func or var %s", name)
}

// fieldTypes lists a field list's types, one per name.
func fieldTypes(fl *ast.FieldList) []ast.Expr {
	if fl == nil {
		return nil
	}
	var out []ast.Expr
	for _, f := range fl.List {
		for range max(len(f.Names), 1) {
			out = append(out, f.Type)
		}
	}
	return out
}

// fileImports maps each import's local name to its path.
func fileImports(file *ast.File) map[string]string {
	out := map[string]string{}
	for _, imp := range file.Imports {
		p, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := path.Base(p)
		if imp.Name != nil {
			name = imp.Name.Name
		}
		out[name] = p
	}
	return out
}

func isSelector(e ast.Expr, imports map[string]string, importPath, sel string) bool {
	s, ok := e.(*ast.SelectorExpr)
	if !ok || s.Sel.Name != sel {
		return false
	}
	x, ok := s.X.(*ast.Ident)
	return ok && imports[x.Name] == importPath
}

func isIdent(e ast.Expr, name string) bool {
	id, ok := e.(*ast.Ident)
	return ok && id.Name == name
}

// endpointReturns reads the output type: an entity struct, a pointer to
// one, a slice of either, or struct{}.
func endpointReturns(out ast.Expr, byStruct map[string]string) (string, error) {
	if st, ok := out.(*ast.StructType); ok && len(st.Fields.List) == 0 {
		return "", nil
	}
	list := ""
	if at, ok := out.(*ast.ArrayType); ok && at.Len == nil {
		list, out = "[]", at.Elt
	}
	if star, ok := out.(*ast.StarExpr); ok {
		out = star.X
	}
	if id, ok := out.(*ast.Ident); ok {
		if name, ok := byStruct[id.Name]; ok {
			return list + name, nil
		}
	}
	return "", fmt.Errorf("output must be an entity type (Orders, *Orders, []Orders) or struct{}, not %s", types.ExprString(out))
}

// inputParams reads the input struct's fields into params: path values in
// route order first, then the rest in field order.
func (src *entitiesSource) inputParams(e settings.Endpoint, in ast.Expr) ([]endpointParam, error) {
	pathNames := e.PathParams()
	var fields []*ast.Field
	switch t := in.(type) {
	case *ast.StructType:
		if len(t.Fields.List) != 0 {
			return nil, fmt.Errorf("declare the input as a named struct type in the entities package")
		}
	case *ast.Ident:
		ts := src.types[t.Name]
		st, ok := (*ast.StructType)(nil), ts != nil
		if ok {
			st, ok = ts.Type.(*ast.StructType)
		}
		if !ok || ts.TypeParams != nil || ts.Assign.IsValid() {
			return nil, fmt.Errorf("must be a struct type declared in the entities package")
		}
		fields = st.Fields.List
	default:
		return nil, fmt.Errorf("must be a struct type declared in the entities package")
	}
	query := endpointQueryMethods(e.Method)
	byPath := map[string]endpointParam{}
	var rest []endpointParam
	seen := map[string]bool{}
	for _, f := range fields {
		if len(f.Names) == 0 {
			return nil, fmt.Errorf("embedded field %s: list its fields instead", types.ExprString(f.Type))
		}
		var tag reflect.StructTag
		if f.Tag != nil {
			raw, err := strconv.Unquote(f.Tag.Value)
			if err != nil {
				return nil, fmt.Errorf("field %s: bad tag %s", f.Names[0].Name, f.Tag.Value)
			}
			tag = reflect.StructTag(raw)
		}
		for _, n := range f.Names {
			if !n.IsExported() {
				continue
			}
			p, err := src.inputParam(n.Name, f.Type, tag, pathNames, query)
			if err != nil {
				return nil, fmt.Errorf("field %s: %w", n.Name, err)
			}
			if p.name == "" {
				continue
			}
			// One namespace for every location: the MCP tool and the
			// generated clients take the values as one flat set.
			if seen[p.name] {
				return nil, fmt.Errorf("field %s: the name %q is bound by an earlier field too", n.Name, p.name)
			}
			seen[p.name] = true
			if p.in == "path" {
				byPath[p.name] = p
				continue
			}
			rest = append(rest, p)
		}
	}
	out := make([]endpointParam, 0, len(pathNames)+len(rest))
	for _, name := range pathNames {
		p, ok := byPath[name]
		if !ok {
			return nil, fmt.Errorf("the path names {%s}: give the input a field tagged `path:%q json:\"-\"`", name, name)
		}
		out = append(out, p)
	}
	out = append(out, rest...)
	if err := checkEndpointIdents(out); err != nil {
		return nil, err
	}
	return out, nil
}

// inputParam reads one exported field. A zero name means the field is not
// on the wire (json:"-" with no path or query tag).
func (src *entitiesSource) inputParam(field string, typ ast.Expr, tag reflect.StructTag, pathNames []string, query bool) (endpointParam, error) {
	if _, ok := tag.Lookup("header"); ok {
		return endpointParam{}, fmt.Errorf("header tags are not supported: the generated clients and the MCP tool send no custom headers")
	}
	jsonTag := tag.Get("json")
	jsonName, jsonOpts, _ := strings.Cut(jsonTag, ",")
	// encoding/json reads `json:"-,"` as the body key "-", and `,string`
	// wants a quoted number the generated clients never send. Only the
	// exact "-" hides a field, and only omitempty/omitzero may follow a
	// name.
	if jsonName == "-" && jsonTag != "-" {
		return endpointParam{}, fmt.Errorf("tag json:%q names the body key \"-\": write json:\"-\" to keep the field off the body", jsonTag)
	}
	for _, opt := range strings.Split(jsonOpts, ",") {
		if opt != "" && opt != "omitempty" && opt != "omitzero" {
			return endpointParam{}, fmt.Errorf("json option %q is not supported: the generated clients send plain JSON values", opt)
		}
	}
	pathName, isPath := tag.Lookup("path")
	queryName, isQuery := tag.Lookup("query")
	if isPath && isQuery {
		return endpointParam{}, fmt.Errorf("tagged both path and query")
	}
	if isPath || isQuery {
		in, name := "path", pathName
		if isQuery {
			in, name = "query", queryName
		}
		// handler.Bind treats an empty query value as absent; `,empty`
		// keeps it (a "" or a non-nil pointer). A path value is never
		// empty, so it takes no option.
		name, opts, hasOpts := strings.Cut(name, ",")
		if hasOpts && (isPath || opts != "empty") {
			return endpointParam{}, fmt.Errorf("%s option %q is not supported: a query tag takes only \"empty\", a path tag none", in, opts)
		}
		if !snakeOrCamelName(name) {
			return endpointParam{}, fmt.Errorf("%s name %q must be a plain name", in, name)
		}
		if jsonName != "-" {
			return endpointParam{}, fmt.Errorf("a %s value must also be tagged json:\"-\", or a JSON body key %q overwrites it", in, field)
		}
		if isPath && !slices.Contains(pathNames, name) {
			return endpointParam{}, fmt.Errorf("tagged path:%q but the route has no {%s}", name, name)
		}
		if isQuery && !query {
			return endpointParam{}, fmt.Errorf("a query value needs a GET or DELETE endpoint; the other methods take their input as a JSON body")
		}
		ft, optional, scalar, err := src.endpointFieldType(typ, 0)
		if err != nil {
			return endpointParam{}, err
		}
		if !scalar {
			return endpointParam{}, fmt.Errorf("a %s value must be a string, number or bool", in)
		}
		if isPath && optional {
			return endpointParam{}, fmt.Errorf("a path value is always present: drop the pointer")
		}
		return endpointParam{name: name, in: in, typ: ft, required: !optional}, nil
	}
	if jsonName == "-" {
		return endpointParam{}, nil
	}
	if jsonName == "" {
		return endpointParam{}, fmt.Errorf("give it a json tag naming its body key, or path/query and json:\"-\"")
	}
	if !snakeOrCamelName(jsonName) {
		return endpointParam{}, fmt.Errorf("json name %q must be a plain name", jsonName)
	}
	if query {
		return endpointParam{}, fmt.Errorf("a GET or DELETE endpoint takes no JSON body: tag the field query:%q json:\"-\"", jsonName)
	}
	ft, optional, _, err := src.endpointFieldType(typ, 0)
	if err != nil {
		return endpointParam{}, err
	}
	return endpointParam{name: jsonName, in: "body", typ: ft, required: !optional}, nil
}

// snakeOrCamelName is the wire-name grammar: it lands in generated Go, JS
// and CLI flags.
func snakeOrCamelName(s string) bool {
	if s == "" || !(s[0] >= 'a' && s[0] <= 'z' || s[0] >= 'A' && s[0] <= 'Z') {
		return false
	}
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

var endpointBasicTypes = map[string]schema.FieldType{
	"string": schema.String, "bool": schema.Bool,
	"int": schema.Int, "int8": schema.Int, "int16": schema.Int, "int32": schema.Int, "int64": schema.Int,
	"uint": schema.Int, "uint8": schema.Int, "uint16": schema.Int, "uint32": schema.Int, "uint64": schema.Int,
	"byte": schema.Int, "rune": schema.Int,
	"float32": schema.Float, "float64": schema.Float,
}

// endpointFieldType maps a field's Go type to a schema type. optional is a
// pointer; scalar is a type handler.Bind can parse from a path or query
// string.
func (src *entitiesSource) endpointFieldType(typ ast.Expr, depth int) (ft schema.FieldType, optional, scalar bool, err error) {
	if depth > 8 {
		return 0, false, false, fmt.Errorf("type %s nests too deep", types.ExprString(typ))
	}
	if star, ok := typ.(*ast.StarExpr); ok && depth == 0 {
		ft, _, scalar, err = src.endpointFieldType(star.X, depth+1)
		return ft, true, scalar, err
	}
	switch t := typ.(type) {
	case *ast.Ident:
		if bt, ok := endpointBasicTypes[t.Name]; ok {
			return bt, false, true, nil
		}
		if t.Name == "any" {
			return schema.JSON, false, false, nil
		}
		if ts := src.types[t.Name]; ts != nil && ts.TypeParams == nil {
			if _, isStruct := ts.Type.(*ast.StructType); isStruct {
				return schema.JSON, false, false, nil
			}
			ft, _, scalar, err = src.endpointFieldType(ts.Type, depth+1)
			return ft, false, scalar, err
		}
	case *ast.SelectorExpr:
		switch types.ExprString(t) {
		case "time.Time":
			return schema.Timestamp, false, false, nil
		case "json.RawMessage":
			return schema.JSON, false, false, nil
		}
	case *ast.MapType, *ast.ArrayType, *ast.InterfaceType, *ast.StructType:
		return schema.JSON, false, false, nil
	}
	return 0, false, false, fmt.Errorf("type %s is not supported: use a built-in type, time.Time, json.RawMessage, a slice, a map, or a type declared in the entities package", types.ExprString(typ))
}

// DeclsWithEndpoints returns decls with each entity's settings endpoints
// attached, as the framework.Endpoint values the generated registration
// builds. The generators that render clients read endpoints from here.
func DeclsWithEndpoints(decls []framework.EntityDeclaration, eps []AppEndpoint) []framework.EntityDeclaration {
	if len(eps) == 0 {
		return decls
	}
	out := slices.Clone(decls)
	for i := range out {
		out[i].Endpoints = nil
		for _, ep := range eps {
			if ep.Entity == out[i].Name {
				out[i].Endpoints = append(out[i].Endpoints, ep.framework())
			}
		}
	}
	return out
}

// framework is the endpoint as the registration declares it, handlers
// aside.
func (ep AppEndpoint) framework() framework.Endpoint {
	fe := framework.Endpoint{
		Name:        ep.Name,
		Method:      ep.Method,
		Path:        ep.Path,
		Description: ep.Description,
		MCP:         ep.MCP,
		SDK:         true,
		Returns:     ep.returns,
	}
	for _, p := range ep.params {
		fe.InputSchema = append(fe.InputSchema, schema.Field{Name: p.name, Type: p.typ, Required: p.required})
	}
	return fe
}

// ReadAppDecls is packReadEntities plus the settings endpoints: the view
// of the app the client, SDK and CLI generators render.
func ReadAppDecls(dir string) ([]framework.EntityDeclaration, error) {
	decls, err := PackReadEntities(dir)
	if err != nil || len(decls) == 0 {
		return decls, err
	}
	eps, err := ReadAppEndpoints(dir, decls)
	if err != nil {
		return nil, err
	}
	return DeclsWithEndpoints(decls, eps), nil
}

// checkEndpointIdents refuses two inputs that distinct wire names would
// still give the same generated name: {type} and {type_arg} both become
// the Go parameter typeArg, foo_bar and fooBar both the Go field FooBar
// and the CLI flag --foo-bar.
func checkEndpointIdents(params []endpointParam) error {
	taken := map[string]string{}
	for _, p := range params {
		var idents []string
		if p.in == "path" {
			idents = []string{"Go parameter " + goEndpointArg(p.name), "JS parameter " + jsEndpointArg(p.name)}
		} else {
			idents = []string{"Go field " + appspec.ToCamelCase(p.name), "CLI flag --" + strings.ReplaceAll(p.name, "_", "-")}
		}
		for _, id := range idents {
			if prev, ok := taken[id]; ok {
				return fmt.Errorf("%q and %q both become the %s: rename one", prev, p.name, id)
			}
			taken[id] = p.name
		}
	}
	return nil
}
