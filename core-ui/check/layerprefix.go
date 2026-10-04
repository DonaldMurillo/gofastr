package check

// Layer-prefix lint: the kernel (core-ui) and the headless layer
// (framework/headless) never name the kit's vocabulary. Each layer owns
// the prefix of what it defines — data-cui-* and cui-* for the kernel,
// data-hui-* for headless, fui-* and data-fui-* for framework — and a
// lower layer reading or writing an upper layer's class or attribute is
// the drift this rule exists for: v0.86's kernel toast builder named
// fui-notification__title, its form-errors module placed messages by
// fui-field__error, and the copy module set fui-copied, so a kit that
// renamed a class broke behaviour three packages away. The fix shapes
// are a hook the lower layer declares (data-hui-field-error), a
// template the kit registers (preset.ToastTemplate), or a module the kit
// ships itself (framework/ui/searchinput.js).
//
// The rule fires on a string literal — JS or Go — that holds a class
// token `fui-<x>` or an attribute name `data-fui-*`. A `--fui-*` custom
// property is a theme token every layer may consume and stays quiet,
// as does a mention in a comment. A site that names the prefix on
// purpose — a refusal filter that bars data-fui-* from extra attrs, the
// lightbox Wiring that renders the framework module's family by
// explicit prop — carries `//gofastr:allow(layerprefix) <reason>` on
// its line or the line above; a bare marker with no reason silences
// nothing, the same discipline as every analyzer in internal/analyzers.

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

// reLayerPrefix matches the kit's class and attribute vocabulary inside
// a literal: a class-shaped token not preceded by `-` (so a `--fui-*`
// token stays quiet) and the framework attribute prefix.
//
//gofastr:allow(layerprefix) the rule's own pattern names what it hunts
var reLayerPrefix = regexp.MustCompile(`(?:^|[^-A-Za-z0-9_])fui-[a-z]|data-fui-`)

const layerPrefixMarker = "gofastr:allow(layerprefix)"

// layerPrefixAllowed reports whether line (1-based) of a source, or the
// line above it, carries the marker WITH a reason.
func layerPrefixAllowed(lines []string, line int) bool {
	for _, n := range []int{line, line - 1} {
		if n < 1 || n > len(lines) {
			continue
		}
		if i := strings.Index(lines[n-1], layerPrefixMarker); i >= 0 {
			if strings.TrimSpace(lines[n-1][i+len(layerPrefixMarker):]) != "" {
				return true
			}
		}
	}
	return false
}

func layerPrefixMessage(lit string) string {
	if len(lit) > 60 {
		lit = lit[:57] + "..."
	}
	return fmt.Sprintf("[layer-prefix] %q names the kit's fui vocabulary from a layer below it — the kernel speaks data-cui-*, headless speaks data-hui-*, and a kit class belongs to the kit's own module, sheet or registered template; declare a hook, register a template, or move the behaviour up, and mark a deliberate refusal filter //gofastr:allow(layerprefix) <reason>", lit)
}

// jsLiteral is one string literal of a JS source: its content and the
// byte offset of its opening quote.
type jsLiteral struct {
	pos  int
	text string
}

// jsStringLiterals scans the comment-blanked code view for single-,
// double- and backtick-quoted literals. A template's ${…} body is kept
// as content: a kit class spelled inside an interpolation is still a
// kit class the module names.
func jsStringLiterals(code string) []jsLiteral {
	var out []jsLiteral
	for i := 0; i < len(code); i++ {
		q := code[i]
		if q != '\'' && q != '"' && q != '`' {
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
		out = append(out, jsLiteral{pos: i, text: code[i+1 : j]})
		i = j
	}
	return out
}

// LintLayerPrefixJS lints the JavaScript under roots (a runtime package
// dir, a fixture dir, or .js files) for kit vocabulary in string
// literals. Roots are loaded the way every runtime-shape lint loads
// them, so the generated runtime.js bundle is never read.
func LintLayerPrefixJS(roots ...string) (*Result, error) {
	res := &Result{}
	files, err := loadJSSources(roots...)
	if err != nil {
		return nil, err
	}
	for _, f := range files {
		lines := strings.Split(f.Src, "\n")
		for _, lit := range jsStringLiterals(f.Code) {
			if !reLayerPrefix.MatchString(lit.text) {
				continue
			}
			line := f.lineOf(lit.pos)
			if layerPrefixAllowed(lines, line) {
				continue
			}
			res.add(f.Path, line, layerPrefixMessage(lit.text))
		}
	}
	return res, nil
}

// LintLayerPrefixGo lints the non-test Go files under roots for kit
// vocabulary in string literals. Vendor, testdata and hidden
// directories are skipped; a `_test.go` file is a rig and may spell
// whatever it probes.
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
				if path != root && (name == "vendor" || name == "node_modules" || name == "testdata" || strings.HasPrefix(name, ".")) {
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
		lines := strings.Split(string(src), "\n")
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
			if layerPrefixAllowed(lines, line) {
				return true
			}
			res.add(path, line, layerPrefixMessage(s))
			return true
		})
	}
	return res, nil
}

// LayerPrefixJSRoots returns the JavaScript the rule governs in a
// checkout: the runtime package (frag/ + src/) and every registered
// behaviour source that lives under core-ui or framework/headless. A
// framework/ui module (filedropzone, searchinput, lightbox) is the
// kit's own and names its own classes.
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
