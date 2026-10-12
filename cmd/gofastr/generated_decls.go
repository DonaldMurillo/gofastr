package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"slices"
	"strings"
)

// refuseDuplicateDecls fails generation when two entities' names meet in
// one generated Go identifier. Method and wrapper names join an entity
// name to a verb or move key (Client.<Key><Entity>, run<Entity><Key>), so
// entity orders with move mark_paid and entity paid_orders with move mark
// both mint Client.MarkPaidOrders. The compiler would reject the result;
// this names the identifier at generation time instead. It reads the
// rendered files, so it sees exactly what the emitters wrote: top-level
// funcs, types, vars and consts per package directory, and methods per
// receiver type. Files that are not Go are skipped.
func refuseDuplicateDecls(files []generatedFile) error {
	type declKey struct{ dir, name string }
	seen := map[declKey]string{}
	fset := token.NewFileSet()
	for _, f := range files {
		if !strings.HasSuffix(f.name, ".go") {
			continue
		}
		parsed, err := parser.ParseFile(fset, f.name, f.content, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("generated %s does not parse: %w", f.name, err)
		}
		dir := path.Dir(f.name)
		for _, name := range topLevelNames(parsed) {
			key := declKey{dir, name}
			if prev, dup := seen[key]; dup {
				return fmt.Errorf("generated code declares %s twice (%s and %s): two entities' names meet in one identifier; rename an entity or a move key", name, prev, f.name)
			}
			seen[key] = f.name
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
			out = append(out, recvTypeName(d.Recv.List[0].Type)+"."+d.Name.Name)
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
