package appgen

import (
	"errors"
	"fmt"
	"go/token"
	"io/fs"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/core/settings"
	"github.com/DonaldMurillo/gofastr/framework"
)

// An app spec's endpoints: list holds exactly what the endpoints section of
// gofastr.settings.yml holds. generate app writes the section, a typed
// handler stub for each linked handler that answers 501 until it is
// written, the endpoints registration, and a client with a typed call for
// each. From then on the settings file is where the endpoints live, and
// pack reads them back from it.

// renderAppSpecEndpoints renders what bp's endpoints add to the app: the
// handler stubs, entities/endpoints.gen.go, the settings file, and the
// client rebuilt with the endpoint calls. entityFiles is the rendered
// entities package, named relative to entities/; the returned client
// replaces the one in it.
func renderAppSpecEndpoints(bp appspec.AppSpec, decls []framework.EntityDeclaration, entityFiles []GeneratedFile) (files []GeneratedFile, client string, err error) {
	if len(bp.Endpoints) == 0 {
		return nil, "", nil
	}
	// Each value lands in generated Go and YAML; a render that skipped
	// validateAppSpec (tests, --add fragments) still refuses a bad one.
	if err := appspec.ValidateAppSpecEndpoints(bp); err != nil {
		return nil, "", err
	}
	generated := map[string]bool{}
	pkg := map[string]string{}
	for _, f := range entityFiles {
		name := filepath.ToSlash(f.Name)
		generated["entities/"+name] = true
		if !strings.Contains(name, "/") {
			pkg["entities/"+name] = f.Content
		}
	}
	byFile := map[string][]settings.Endpoint{}
	var order []string
	for _, e := range bp.Endpoints {
		file := e.HandlerFile()
		if generated[file] || file == AppEndpointsPath {
			return nil, "", fmt.Errorf("app spec: endpoint %s: handler file %s is a file generate app writes for the entities; link a file of its own, such as entities/%s_endpoints.go", e.Name, file, e.Entity)
		}
		if _, ok := byFile[file]; !ok {
			order = append(order, file)
		}
		byFile[file] = append(byFile[file], e)
	}
	entityStructs := map[string]string{}
	entityTables := map[string]string{}
	for _, d := range decls {
		entityStructs[d.Name] = appspec.ToCamelCase(d.Name)
		entityTables[d.Name] = declTable(d)
	}
	for _, file := range order {
		content, err := renderEndpointStubFile(byFile[file], entityStructs, entityTables)
		if err != nil {
			return nil, "", err
		}
		pkg[file] = content
		files = append(files, GeneratedFile{Name: filepath.FromSlash(file), Content: content})
	}
	src, err := parseEntitiesSource(pkg)
	if err != nil {
		return nil, "", fmt.Errorf("app spec: endpoint stubs: %w", err)
	}
	eps, err := src.resolveAll("app spec", bp.Endpoints, decls)
	if err != nil {
		return nil, "", err
	}
	registration, err := renderAppEndpoints(eps)
	if err != nil {
		return nil, "", err
	}
	files = append(files,
		GeneratedFile{Name: filepath.FromSlash(AppEndpointsPath), Content: registration},
		GeneratedFile{Name: settings.FileName, Content: settings.FormatEndpoints(bp.Endpoints)},
	)
	withEndpoints := DeclsWithEndpoints(decls, eps)
	if err := checkEndpointReturns(withEndpoints); err != nil {
		return nil, "", err
	}
	client, err = renderAppClient(withEndpoints)
	if err != nil {
		return nil, "", err
	}
	return files, client, nil
}

// renderEndpointStubFile renders one linked handler file: for each
// endpoint, an input struct carrying the route's path values and a
// handler that answers 501 Not Implemented. The output is the entity the
// endpoint hangs under; the developer changes either type as the handler
// takes shape, and `gofastr generate` reads the new ones.
func renderEndpointStubFile(eps []settings.Endpoint, entityStructs, entityTables map[string]string) (string, error) {
	var sb strings.Builder
	sb.WriteString("package entities\n\nimport (\n\t\"context\"\n\t\"net/http\"\n\n\t\"github.com/DonaldMurillo/gofastr/core/handler\"\n)\n")
	for _, e := range eps {
		name := e.HandlerName()
		// Validate already holds the name to an exported Go identifier;
		// the emitted declarations below must not rely on a caller having
		// run it.
		if !token.IsIdentifier(name) || !token.IsExported(name) {
			return "", fmt.Errorf("app spec: endpoint %s: handler %q is not an exported Go name", e.Name, name)
		}
		input := name + "Input"
		// The endpoint mounts under its entity's table, not the raw name.
		route := "/" + entityTables[e.Entity] + "/" + e.Path
		fmt.Fprintf(&sb, "\n// %s is what endpoint %s takes. A path value is tagged path and\n", input, e.Name)
		sb.WriteString("// json:\"-\"; every other field is a JSON body key (a query value on a\n// GET or DELETE).\n")
		fmt.Fprintf(&sb, "type %s struct {\n", input)
		fields := map[string]string{}
		for _, p := range e.PathParams() {
			field := pathFieldName(p)
			if prev, ok := fields[field]; ok {
				return "", fmt.Errorf("app spec: endpoint %s: path values {%s} and {%s} both become input field %s; rename one", e.Name, prev, p, field)
			}
			fields[field] = p
			fmt.Fprintf(&sb, "\t%s string `path:%q json:\"-\"`\n", field, p)
		}
		sb.WriteString("}\n\n")
		fmt.Fprintf(&sb, "// %s serves endpoint %s, %s %s.\n", name, e.Name, e.Method, route)
		if e.Description != "" {
			fmt.Fprintf(&sb, "// %s\n", e.Description)
		}
		sb.WriteString("// It answers 501 Not Implemented until it is written.\n")
		fmt.Fprintf(&sb, "func %s(ctx context.Context, in %s) (*%s, error) {\n", name, input, entityStructs[e.Entity])
		fmt.Fprintf(&sb, "\treturn nil, handler.Errorf(http.StatusNotImplemented, %q)\n}\n", "endpoint "+e.Name+" is not implemented yet")
	}
	return sb.String(), nil
}

// splitSettingsFile takes the settings file out of files: generate app
// merges the endpoints section into the file instead of writing it whole,
// so the rest of a settings file already there is kept.
func splitSettingsFile(files []GeneratedFile) (rest []GeneratedFile, found bool) {
	rest = slices.DeleteFunc(slices.Clone(files), func(f GeneratedFile) bool { return f.Name == settings.FileName })
	return rest, len(rest) != len(files)
}

// ExistingEndpoints reads the endpoints section of the settings file in
// dir; nil when there is no file.
func ExistingEndpoints(dir string) ([]settings.Endpoint, error) {
	f, err := settings.Load(filepath.Join(dir, settings.FileName))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return f.Endpoints()
}

// endpointsConflict reports whether writing eps into dir's settings file
// would replace a different endpoints section.
func endpointsConflict(dir string, eps []settings.Endpoint) bool {
	have, err := ExistingEndpoints(dir)
	if err != nil {
		return true
	}
	return len(have) > 0 && !slices.Equal(have, eps)
}

// mergeEndpoints adds the fragment's endpoints to the project's, the way
// --add unions entities: a name the project already has keeps the
// project's entry.
func mergeEndpoints(project, fragment []settings.Endpoint) []settings.Endpoint {
	out := slices.Clone(project)
	for _, e := range fragment {
		if !slices.ContainsFunc(project, func(p settings.Endpoint) bool { return p.Name == e.Name }) {
			out = append(out, e)
		}
	}
	return out
}

// checkAddEndpoints resolves bp's endpoints, the project's and the
// fragment's, against the entities package --add leaves behind: the files
// on disk plus the new ones in written. --add skips a file that exists, so
// a fragment's handler stub never lands in one; a handler missing from the
// result is refused here, before the settings file or any Go is written.
func checkAddEndpoints(dir string, bp appspec.AppSpec, written []GeneratedFile) error {
	files, err := readEntitiesFiles(dir)
	if err != nil {
		return err
	}
	for _, f := range written {
		name := filepath.ToSlash(f.Name)
		if path.Dir(name) == "entities" && strings.HasSuffix(name, ".go") {
			files[name] = f.Content
		}
	}
	src, err := parseEntitiesSource(files)
	if err != nil {
		return fmt.Errorf("app spec: %w", err)
	}
	if _, err := src.resolveAll("app spec", bp.Endpoints, bp.Entities); err != nil {
		return fmt.Errorf("%w; --add writes no stub into a file the project already has, so declare the handler there first or link a file of its own", err)
	}
	return nil
}

// pathFieldName names the input field for path value p, spelling an id
// word the way Go does: {id} is ID, {order_id} is OrderID.
func pathFieldName(p string) string {
	var b strings.Builder
	for _, word := range strings.Split(p, "_") {
		if word == "id" {
			b.WriteString("ID")
			continue
		}
		b.WriteString(appspec.ToCamelCase(word))
	}
	return b.String()
}
