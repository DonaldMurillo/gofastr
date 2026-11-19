package contracts

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
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/internal/buildoutput"
	"github.com/DonaldMurillo/gofastr/internal/sourcewalk"

	"golang.org/x/mod/modfile"
)

// SourceFile is one file the pass discovered, with its classification.
type SourceFile struct {
	// Rel is the slash-separated path from the pass root.
	Rel string
	// Abs is the absolute path on disk.
	Abs string
	// IsTest is true for _test.go files.
	IsTest bool
	// IsCSS is true for stylesheets (.css), which only StyleFiles returns.
	IsCSS bool
	// IsScript is true for JavaScript (.js, .mjs, .cjs), which only
	// ScriptFiles returns.
	IsScript bool
	// IsGenerated is true when the file carries the full generated-code
	// header; the App, Test, Style and Script accessors skip it.
	IsGenerated bool
	// Package is the Go import path of the file's directory, derived from
	// the module path. Empty when the root has no go.mod.
	Package string
}

// Pass is the shared context every analyzer runs against: discovered
// files, their sources, cached ASTs, and the resolved [Config]. It is
// safe for concurrent use.
type Pass struct {
	// Root is the absolute directory being analysed.
	Root string
	// ModulePath is the go.mod module path, empty when there is none.
	ModulePath string
	// Config is the resolved configuration for this run.
	Config *Config

	files []SourceFile

	mu            sync.Mutex
	fset          *token.FileSet
	sources       map[string][]byte
	asts          map[string]*ast.File
	readFailures  map[string]string
	parseFailures map[string]string
	memo          map[string]any
}

// NewPass discovers every source file under root and returns a pass ready
// for analyzers. Discovery and source reading are eager (one walk), while
// AST parsing is lazy. Unreadable roots fail discovery; unreadable files
// are recorded by [Pass.Unparsed]. Empty and incomplete passes remain
// inspectable, but [Run] refuses to analyze them.
func NewPass(root string, cfg *Config) (*Pass, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve root %q: %w", root, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("read root %q: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("root %q is not a directory", root)
	}
	abs, err = filepath.EvalSymlinks(abs)
	if err != nil {
		return nil, fmt.Errorf("resolve root %q: %w", root, err)
	}
	if cfg == nil {
		cfg = DefaultConfig()
	}
	p := &Pass{
		Root:          abs,
		ModulePath:    ReadModulePath(abs),
		Config:        cfg,
		fset:          token.NewFileSet(),
		sources:       map[string][]byte{},
		asts:          map[string]*ast.File{},
		readFailures:  map[string]string{},
		parseFailures: map[string]string{},
		memo:          map[string]any{},
	}
	if err := p.discover(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Pass) discover() error {
	err := sourcewalk.Walk(p.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if path == p.Root {
				return fmt.Errorf("read root %q: %w", p.Root, err)
			}
			if d != nil && d.IsDir() {
				if rel, relErr := filepath.Rel(p.Root, path); relErr == nil {
					p.readFailures[filepath.ToSlash(rel)] = err.Error()
				}
				return fs.SkipDir
			}
			if strings.HasSuffix(path, ".go") || strings.HasSuffix(path, ".css") || IsScriptPath(path) {
				if rel, relErr := filepath.Rel(p.Root, path); relErr == nil {
					p.readFailures[filepath.ToSlash(rel)] = err.Error()
				}
			}
			return nil
		}
		if d.IsDir() {
			if path == p.Root {
				return nil
			}
			if buildoutput.StaticExport(os.DirFS(path), ".") {
				return fs.SkipDir
			}
			return nil
		}
		isGo := strings.HasSuffix(path, ".go")
		isCSS := strings.HasSuffix(path, ".css")
		isScript := IsScriptPath(path)
		if !isGo && !isCSS && !isScript {
			return nil
		}
		rel, relErr := filepath.Rel(p.Root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			p.readFailures[rel] = readErr.Error()
			return nil
		}
		p.sources[rel] = body
		pkg := ""
		if isGo {
			pkg = ImportPathFor(p.ModulePath, p.Root, filepath.Dir(path))
		}
		p.files = append(p.files, SourceFile{
			Rel:         rel,
			Abs:         path,
			IsTest:      isGo && strings.HasSuffix(rel, "_test.go"),
			IsCSS:       isCSS,
			IsScript:    isScript,
			IsGenerated: IsGeneratedSource(body),
			Package:     pkg,
		})
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(p.files, func(i, j int) bool { return p.files[i].Rel < p.files[j].Rel })
	return nil
}

// Files returns every discovered Go file, in path order.
func (p *Pass) Files() []SourceFile {
	var out []SourceFile
	for _, f := range p.files {
		if f.IsCSS || f.IsScript {
			continue
		}
		out = append(out, f)
	}
	return out
}

// AppFiles returns the files analyzers care about by default: non-test,
// non-generated Go source that configuration has not exempted.
func (p *Pass) AppFiles() []SourceFile {
	var out []SourceFile
	for _, f := range p.files {
		if f.IsCSS || f.IsScript || f.IsTest || f.IsGenerated || p.Config.ExemptPath(f.Rel) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// TestFiles returns non-generated _test.go files that configuration has
// not exempted.
func (p *Pass) TestFiles() []SourceFile {
	var out []SourceFile
	for _, f := range p.files {
		if f.IsCSS || f.IsScript || !f.IsTest || f.IsGenerated || p.Config.ExemptPath(f.Rel) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// StyleFiles returns the non-generated .css files configuration has not
// exempted.
func (p *Pass) StyleFiles() []SourceFile {
	var out []SourceFile
	for _, f := range p.files {
		if !f.IsCSS || f.IsGenerated || p.Config.ExemptPath(f.Rel) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// ScriptFiles returns the JavaScript files configuration has not
// exempted, minus generated and minified ones (a .min.* name, or a line
// longer than 1000 bytes).
func (p *Pass) ScriptFiles() []SourceFile {
	var out []SourceFile
	for _, f := range p.files {
		if !f.IsScript || f.IsGenerated || p.Config.ExemptPath(f.Rel) || isMinifiedScript(f.Rel, p.sources[f.Rel]) {
			continue
		}
		out = append(out, f)
	}
	return out
}

// IsScriptPath reports whether a path names a JavaScript source file.
func IsScriptPath(path string) bool {
	return strings.HasSuffix(path, ".js") || strings.HasSuffix(path, ".mjs") || strings.HasSuffix(path, ".cjs")
}

const maxScriptLine = 1000

func isMinifiedScript(rel string, body []byte) bool {
	for _, ext := range []string{".min.js", ".min.mjs", ".min.cjs"} {
		if strings.HasSuffix(rel, ext) {
			return true
		}
	}
	for line := range strings.SplitSeq(string(body), "\n") {
		if len(line) > maxScriptLine {
			return true
		}
	}
	return false
}

// Source returns the bytes of a discovered file. The returned slice is
// the pass's own buffer: analyzers must not mutate it.
func (p *Pass) Source(rel string) ([]byte, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	b, ok := p.sources[rel]
	return b, ok
}

// Lines splits a discovered file into lines, for snippet extraction.
func (p *Pass) Lines(rel string) []string {
	b, ok := p.Source(rel)
	if !ok {
		return nil
	}
	return strings.Split(string(b), "\n")
}

// Line returns the trimmed 1-indexed source line, or "" when out of range.
func (p *Pass) Line(rel string, n int) string {
	lines := p.Lines(rel)
	if n < 1 || n > len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[n-1])
}

// FileSet is the shared position table. Every AST the pass hands out is
// positioned against it.
func (p *Pass) FileSet() *token.FileSet {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.fset
}

// AST parses a discovered Go file once, with comments, and caches the
// result. A file that fails to parse returns (nil, false) and is recorded
// in [Pass.Unparsed]; a stylesheet or script returns (nil, false) unrecorded.
func (p *Pass) AST(rel string) (*ast.File, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if f, ok := p.asts[rel]; ok {
		return f, f != nil
	}
	if strings.HasSuffix(rel, ".css") || IsScriptPath(rel) {
		p.asts[rel] = nil
		return nil, false
	}
	body, ok := p.sources[rel]
	if !ok {
		return nil, false
	}
	f, err := parser.ParseFile(p.fset, rel, body, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		p.asts[rel] = nil
		p.parseFailures[rel] = err.Error()
		return nil, false
	}
	p.asts[rel] = f
	return f, true
}

// Unparsed returns a copy of read and parse errors keyed by source path.
// Run refuses read failures and reports parse failures as incomplete analysis.
func (p *Pass) Unparsed() map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]string, len(p.readFailures)+len(p.parseFailures))
	for k, v := range p.readFailures {
		out[k] = v
	}
	for k, v := range p.parseFailures {
		out[k] = v
	}
	return out
}

func (p *Pass) discoveryError() error {
	if len(p.readFailures) == 0 {
		return nil
	}
	failures := make([]string, 0, len(p.readFailures))
	for path, reason := range p.readFailures {
		failures = append(failures, fmt.Sprintf("%q: %q", path, reason))
	}
	sort.Strings(failures)
	return fmt.Errorf("contracts: source inspection incomplete: %s", strings.Join(failures, "; "))
}

// Position resolves an AST position into the pass's line/column space.
func (p *Pass) Position(pos token.Pos) token.Position {
	return p.FileSet().Position(pos)
}

// Memo shares the first value stored for key across analyzers. Concurrent
// cache misses may compute the value more than once.
func (p *Pass) Memo(key string, compute func() any) any {
	p.mu.Lock()
	if v, ok := p.memo[key]; ok {
		p.mu.Unlock()
		return v
	}
	p.mu.Unlock()

	v := compute()

	p.mu.Lock()
	defer p.mu.Unlock()
	if existing, ok := p.memo[key]; ok {
		return existing
	}
	p.memo[key] = v
	return v
}

// Rel converts an absolute path to the pass-relative, slash-separated
// form diagnostics use.
func (p *Pass) Rel(abs string) string {
	rel, err := filepath.Rel(p.Root, abs)
	if err != nil {
		return filepath.ToSlash(abs)
	}
	return filepath.ToSlash(rel)
}

// IsGeneratedSource reports whether the first 512 bytes of body hold a
// whole line reading `// Code generated … DO NOT EDIT.`.
func IsGeneratedSource(body []byte) bool {
	head := body
	if len(head) > 512 {
		head = head[:512]
	}
	return reGeneratedHeader.Match(head)
}

var reGeneratedHeader = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$`)

// ReadModulePath returns the module path from root/go.mod, unquoted, or
// "" when the file is missing or declares no module.
func ReadModulePath(root string) string {
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	return modfile.ModulePath(body)
}

// ImportPathFor maps a directory to its Go import path given the module's
// root and module path.
func ImportPathFor(modulePath, root, dir string) string {
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return ""
	}
	rel = filepath.ToSlash(rel)
	if rel == "." {
		return modulePath
	}
	if modulePath == "" {
		return rel
	}
	return modulePath + "/" + rel
}
