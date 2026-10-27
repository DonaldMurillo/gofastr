//gofastr:test-reads /framework/ui/ /framework/gallery/ /examples/site/

package ui

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/ui/icons"
	"gopkg.in/yaml.v3"
)

// Keep literal icon calls, component config names, and embedded app-spec nav
// names backed by vendored artwork. The site must not need a private registry.
func TestSiteAndFrameworkIconNamesAreVendored(t *testing.T) {
	fset := token.NewFileSet()
	constantNames := map[string]string{}
	generated, err := parser.ParseFile(fset, "icons/icons_gen.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	ast.Inspect(generated, func(node ast.Node) bool {
		if spec, ok := node.(*ast.ValueSpec); ok && len(spec.Names) == 1 && len(spec.Values) == 1 {
			if value, ok := spec.Values[0].(*ast.BasicLit); ok && value.Kind == token.STRING {
				name, err := strconv.Unquote(value.Value)
				if err != nil {
					t.Fatal(err)
				}
				constantNames[spec.Names[0].Name] = name
			}
		}
		return true
	})
	checked := 0
	check := func(position token.Pos, name string) {
		if name == "" {
			return
		}
		checked++
		if err := iconUsageError(name); err != nil {
			t.Errorf("%s: %v", fset.Position(position), err)
		}
	}
	for _, root := range []string{".", "../gallery", "../../examples/site"} {
		err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == "icons" || strings.HasPrefix(entry.Name(), "_") {
					return filepath.SkipDir
				}
				return nil
			}
			if (strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml")) && root == "../../examples/site" {
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				names, err := yamlIconNames(data)
				if err != nil {
					return fmt.Errorf("%s: %w", path, err)
				}
				for _, name := range names {
					if err := iconUsageError(name); err != nil {
						t.Errorf("%s: %v", path, err)
					}
					if name != "" {
						checked++
					}
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.HasSuffix(path, ".gen.go") {
				return nil
			}
			file, err := parser.ParseFile(fset, path, nil, 0)
			if err != nil {
				return err
			}
			iconImports := map[string]bool{}
			for _, imp := range file.Imports {
				pkg, err := strconv.Unquote(imp.Path.Value)
				if err != nil {
					return err
				}
				if pkg == "github.com/DonaldMurillo/gofastr/framework/ui/icons" {
					alias := "icons"
					if imp.Name != nil {
						alias = imp.Name.Name
					}
					iconImports[alias] = true
				}
			}
			value := func(expr ast.Expr) string {
				if literal, ok := expr.(*ast.BasicLit); ok && literal.Kind == token.STRING {
					name, err := strconv.Unquote(literal.Value)
					if err != nil {
						t.Fatal(err)
					}
					return name
				}
				if selector, ok := expr.(*ast.SelectorExpr); ok {
					if qualifier, ok := selector.X.(*ast.Ident); ok && iconImports[qualifier.Name] {
						return constantNames[selector.Sel.Name]
					}
				}
				return ""
			}
			ast.Inspect(file, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.CallExpr:
					function := ""
					switch fn := n.Fun.(type) {
					case *ast.Ident:
						function = fn.Name
					case *ast.SelectorExpr:
						function = fn.Sel.Name
					}
					if function == "Icon" && len(n.Args) > 0 {
						check(n.Pos(), value(n.Args[0]))
					}
					if function == "RegisterIcon" && root == "../../examples/site" {
						t.Errorf("%s: site registers private artwork; use the vendored catalog", fset.Position(n.Pos()))
					}
				case *ast.KeyValueExpr:
					if key, ok := n.Key.(*ast.Ident); ok && key.Name == "Icon" {
						check(n.Pos(), value(n.Value))
					}
				case *ast.BasicLit:
					if n.Kind == token.STRING && root == "../../examples/site" && strings.Contains(n.Value, "<svg") {
						t.Errorf("%s: site embeds SVG artwork instead of using a sourced icon", fset.Position(n.Pos()))
					}
				}
				return true
			})
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if checked < 30 {
		t.Fatalf("only checked %d icon references; the source walk is incomplete", checked)
	}
}

func iconUsageError(name string) error {
	if name != "" {
		if body, ok := icons.Lookup(name); !ok || body == "" {
			return fmt.Errorf("icon %q has no vendored drawing", name)
		}
	}
	return nil
}

func yamlIconNames(data []byte) ([]string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	var names []string
	var walk func(*yaml.Node)
	walk = func(node *yaml.Node) {
		if node.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(node.Content); i += 2 {
				key, value := node.Content[i], node.Content[i+1]
				if key.Kind == yaml.AliasNode && key.Alias != nil {
					key = key.Alias
				}
				if value.Kind == yaml.AliasNode && value.Alias != nil {
					value = value.Alias
				}
				if key.Value == "icon" && value.Kind == yaml.ScalarNode && value.Tag != "!!null" {
					names = append(names, value.Value)
				}
			}
		}
		for _, child := range node.Content {
			walk(child)
		}
	}
	walk(&doc)
	return names, nil
}

func TestIconUsageGuardRejectsMalformedNames(t *testing.T) {
	for _, name := range []string{"chevron_up", "missing-icon", "Search", "search icon"} {
		if err := iconUsageError(name); err == nil {
			t.Errorf("guard accepted malformed or unknown icon %q", name)
		}
	}
	names, err := yamlIconNames([]byte("nav:\n  - {icon: chevron_up} # typo\n  - icon: 'search icon'\n  - icon: search\n"))
	if err != nil || len(names) != 3 {
		t.Fatalf("YAML extraction skipped icon values: %v, %v", names, err)
	}
	if iconUsageError(names[0]) == nil || iconUsageError(names[1]) == nil || iconUsageError(names[2]) != nil {
		t.Fatalf("YAML icon guard accepted a malformed icon or rejected a valid one: %v", names)
	}
	names, err = yamlIconNames([]byte("name: &bad chevron_up\nnav: [{icon: *bad}]\n"))
	if err != nil || len(names) != 1 || iconUsageError(names[0]) == nil {
		t.Fatalf("YAML alias hid a malformed icon: %v, %v", names, err)
	}
	names, err = yamlIconNames([]byte("name: &field icon\nnav: [{*field: chevron_up}]\n"))
	if err != nil || len(names) != 1 || iconUsageError(names[0]) == nil {
		t.Fatalf("YAML alias key hid a malformed icon: %v, %v", names, err)
	}
}
