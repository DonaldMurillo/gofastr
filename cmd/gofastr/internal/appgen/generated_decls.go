package appgen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"strings"
)

// refuseDuplicateDecls fails generation when two declared names meet in
// one generated Go identifier. Method and wrapper names join an entity
// name to a verb or move key (Client.<Key><Entity>, run<Entity><Key>), so
// entity orders with move mark_paid and entity paid_orders with move mark
// both mint Client.MarkPaidOrders. The compiler would reject the result;
// this names the identifier at generation time instead. It reads the
// rendered files, so it sees exactly what the emitters wrote: top-level
// funcs, types, vars and consts per package directory, and methods per
// receiver type. Files that are not Go are skipped.
func refuseDuplicateDecls(files []GeneratedFile) error {
	type declKey struct{ dir, name string }
	seen := map[declKey]string{}
	fset := token.NewFileSet()
	for _, f := range files {
		if !strings.HasSuffix(f.Name, ".go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, f.Name, f.Content, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("generated %s does not parse: %w", f.Name, err)
		}
		dir := path.Dir(f.Name)
		for _, name := range topLevelNames(parsed) {
			key := declKey{dir, name}
			if prev, dup := seen[key]; dup {
				return fmt.Errorf("generated code declares %s twice (%s and %s): two declared names (entities, fields, enum values, move keys or endpoint names) meet in one identifier; rename one of them", name, prev, f.Name)
			}
			seen[key] = f.Name
		}
	}
	return nil
}

// topLevelNames lists a file's package-level declarations; a method is
// Recv.Name. The blank identifier and init are left out.
func topLevelNames(f *ast.File) []string {
	var out []string
	for _, d := range f.Decls {
		switch d := d.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil {
				if d.Name.Name != "init" {
					out = append(out, d.Name.Name)
				}
				continue
			}
			out = append(out, RecvTypeName(d.Recv.List[0].Type)+"."+d.Name.Name)
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					out = append(out, s.Name.Name)
					// A struct's fields share its method namespace: a
					// field unmarshal_JSON is Notes.UnmarshalJSON, the
					// method a relation decode declares on Notes.
					if st, ok := s.Type.(*ast.StructType); ok {
						for _, fld := range st.Fields.List {
							for _, n := range fieldNames(fld) {
								out = append(out, s.Name.Name+"."+n)
							}
						}
					}
				case *ast.ValueSpec:
					for _, n := range s.Names {
						if n.Name != "_" {
							out = append(out, n.Name)
						}
					}
				}
			}
		}
	}
	return slices.Clip(out)
}

// fieldNames lists the names a struct field declares: its own, or for an
// embedded field the type it embeds (T, *T, pkg.T, T[P]).
func fieldNames(f *ast.Field) []string {
	if len(f.Names) > 0 {
		out := make([]string, 0, len(f.Names))
		for _, n := range f.Names {
			if n.Name != "_" {
				out = append(out, n.Name)
			}
		}
		return out
	}
	t := f.Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	switch x := t.(type) {
	case *ast.IndexExpr:
		t = x.X
	case *ast.IndexListExpr:
		t = x.X
	}
	switch x := t.(type) {
	case *ast.Ident:
		return []string{x.Name}
	case *ast.SelectorExpr:
		return []string{x.Sel.Name}
	}
	return nil
}
