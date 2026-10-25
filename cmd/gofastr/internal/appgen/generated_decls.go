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
