package check

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

//gofastr:allow(layerprefix) the rule's own pattern names what it hunts
var reLayerPrefix = regexp.MustCompile(`(?:^|[^-A-Za-z0-9_])fui-[a-z]|data-fui-`)

const layerPrefixMarker = "layerprefix"

func layerPrefixMessage(lit string) string {
	if len(lit) > 60 {
		lit = lit[:57] + "..."
	}
	return fmt.Sprintf("[layer-prefix] %q names the kit's fui vocabulary from a layer below it — the kernel speaks data-cui-*, headless speaks data-hui-*, and a kit class belongs to the kit's own module, sheet or registered template; declare a hook, register a template, or move the behaviour up, and mark a deliberate refusal filter //gofastr:allow(layerprefix) <reason>", lit)
}

type jsLiteral struct {
	pos  int
	text string
}

func jsStringLiterals(code string) []jsLiteral {
	var lits []jsLiteral
	var seen []byte
	for i := 0; i < len(code); i++ {
		q := code[i]
		if q != '\'' && q != '"' && q != '`' {
			if q == '/' && regexLiteralStartsAt(seen) {
				end := regexLiteralEnd(code, i)
				seen = append(seen, code[i:end+1]...)
				i = end
				continue
			}
			seen = append(seen, q)
			continue
		}
		j := i + 1
		for j < len(code) {
			c := code[j]
			if c == '\\' {
				j += 2
				continue
			}
			if c == q || (q != '`' && c == '\n') {
				break
			}
			j++
		}
		if j > len(code) {
			j = len(code)
		}
		lits = append(lits, jsLiteral{pos: i, text: code[i+1 : j]})
		seen = append(seen, code[i:min(j+1, len(code))]...)
		i = j
	}
	return lits
}

func regexLiteralEnd(s string, i int) int {
	j := i + 1
	inClass := false
	for j < len(s) && s[j] != '\n' {
		if s[j] == '\\' && j+1 < len(s) {
			j += 2
			continue
		}
		if s[j] == '[' {
			inClass = true
		} else if s[j] == ']' {
			inClass = false
		} else if s[j] == '/' && !inClass {
			return j
		}
		j++
	}
	return min(j, len(s)-1)
}

// LintLayerPrefixJS lints the JavaScript under roots (a runtime package dir, a fixture dir, or .js
// files) for kit vocabulary in string literals.
func LintLayerPrefixJS(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		allowed := allowedLines(jsComments(f), layerPrefixMarker)
		for _, lit := range jsStringLiterals(f.Code) {
			if !reLayerPrefix.MatchString(lit.text) {
				continue
			}
			line := f.lineOf(lit.pos)
			if allowed[line] {
				continue
			}
			res.add(f.Path, line, layerPrefixMessage(lit.text))
		}
	}
	return res, nil
}

// LintLayerPrefixGo lints the non-test Go files under roots for kit vocabulary in string literals.
func LintLayerPrefixGo(roots ...string) (*Result, error) {
	res := &Result{}
	var files []string
	for _, root := range roots {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			name := d.Name()
			if d.IsDir() {
				if path != root && (name == "vendor" || name == "node_modules" || name == "testdata" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")) {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			return nil, fmt.Errorf("layer-prefix lint: %w", err)
		}
	}
	sort.Strings(files)
	fset := token.NewFileSet()
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("layer-prefix lint: %w", err)
		}
		af, err := parser.ParseFile(fset, path, src, parser.ParseComments|parser.SkipObjectResolution)
		if err != nil {
			return nil, fmt.Errorf("layer-prefix lint: %w", err)
		}
		allowed := allowedLines(goComments(fset, af, src), layerPrefixMarker)
		ast.Inspect(af, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil || !reLayerPrefix.MatchString(s) {
				return true
			}
			line := fset.Position(lit.Pos()).Line
			if allowed[line] {
				return true
			}
			res.add(path, line, layerPrefixMessage(s))
			return true
		})
	}
	return res, nil
}

// LayerPrefixJSRoots returns the JavaScript the rule governs in a checkout: the runtime package
// (frag/ + src/) and every registered behaviour source that lives under core-ui or
// framework/headless.
func LayerPrefixJSRoots(repoRoot string) ([]string, error) {
	files, err := RegisteredBehaviorSources(repoRoot)
	if err != nil {
		return nil, err
	}
	roots := []string{filepath.Join(repoRoot, "core-ui", "runtime")}
	kernel := filepath.Join(repoRoot, "core-ui") + string(filepath.Separator)
	headless := filepath.Join(repoRoot, "framework", "headless") + string(filepath.Separator)
	for _, f := range files {
		if strings.HasPrefix(f, kernel) || strings.HasPrefix(f, headless) {
			roots = append(roots, f)
		}
	}
	return roots, nil
}

// LayerPrefixGoRoots returns the Go trees the rule governs.
func LayerPrefixGoRoots(repoRoot string) []string {
	return []string{filepath.Join(repoRoot, "core-ui"), filepath.Join(repoRoot, "framework", "headless")}
}
