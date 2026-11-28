package appgen

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/astbase"
)

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
			out = append(out, astbase.RecvBaseName(d)+"."+d.Name.Name)
		case *ast.GenDecl:
			for _, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					out = append(out, s.Name.Name)
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

func packageNamesUnder(writeRoot string) []string {
	root, err := os.OpenRoot(writeRoot)
	if err != nil {
		return nil
	}
	defer root.Close()
	return rootPackageNames(root)
}

func rootPackageNames(root *os.Root) []string {
	entries, _ := RootReadDirEntries(root, ".")
	var out []string
	for _, e := range entries {
		if e.IsDir() || !IsProductionGoSource(e.Name()) {
			continue
		}
		src, err := root.ReadFile(e.Name())
		if err != nil {
			continue
		}
		f, err := parser.ParseFile(token.NewFileSet(), e.Name(), src, parser.SkipObjectResolution)
		if err != nil {
			continue
		}
		out = append(out, topLevelNames(f)...)
	}
	return out
}
