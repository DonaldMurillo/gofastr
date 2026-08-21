package contracts

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// skipDirs are trees no analyzer ever wants: dependency caches, VCS
// metadata, and every build-output directory the project uses. Findings
// in `dist/` are noise: nobody edits a build artifact to fix them.
var skipDirs = map[string]bool{
	"vendor": true, "node_modules": true, ".git": true,
	"dist": true, "bin": true, "build": true, "tmp": true,
	"testdata": false, // kept: fixtures are real source worth linting
}

// SourceFile is one file the pass discovered, with the classification
// every analyzer needs before deciding whether to look at it.
type SourceFile struct {
	// Rel is the slash-separated path from the pass root.
	Rel string
	// Abs is the absolute path on disk.
	Abs string
	// IsTest is true for _test.go files.
	IsTest bool
	// IsGenerated is true when the file carries a `Code generated … DO NOT
	// EDIT` header. Analyzers skip these by default: the developer cannot
	// fix a finding there, only the generator can.
	IsGenerated bool
	// Package is the Go import path of the file's directory, derived from
	// the module path. Empty when the root has no go.mod.
	Package string
}

// Pass is the shared context every analyzer runs against. It owns file
// discovery, source reading, and AST parsing so a twelve-analyzer run
// parses each file once rather than twelve times, and it carries the
// resolved [Config] so analyzers can honour path exemptions without each
// re-implementing the matching.
type Pass struct {
	// Root is the absolute directory being analysed.
	Root string
	// ModulePath is the go.mod module path, empty when there is none.
	ModulePath string
	// Config is the resolved configuration for this run.
	Config *Config

	files []SourceFile

	mu      sync.Mutex
	fset    *token.FileSet
	sources map[string][]byte
	asts    map[string]*ast.File
	// unparsed records files the parser rejected, so the report can say
	// what it could not read instead of implying those files were clean.
	unparsed map[string]string
	memo     map[string]any
}

// NewPass discovers every source file under root and returns a pass ready
// for analyzers. Discovery is eager (one walk) but reading and parsing are
// lazy, so an analyzer that only cares about .yml files pays nothing for
// the Go tree.
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
	if cfg == nil {
		cfg = DefaultConfig()
	}
	p := &Pass{
		Root:       abs,
		ModulePath: readModulePath(abs),
		Config:     cfg,
		fset:       token.NewFileSet(),
		sources:    map[string][]byte{},
		asts:       map[string]*ast.File{},
		memo:       map[string]any{},
	}
	if err := p.discover(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Pass) discover() error {
	err := filepath.WalkDir(p.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// An unreadable subtree is not a reason to abandon the whole
			// analysis. Skip it and keep going.
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if path == p.Root {
				return nil
			}
			if skip, known := skipDirs[name]; known && skip {
				return fs.SkipDir
			}
			// Hidden directories are tooling state (.git, .gofastr,
			// .claude), never app source.
			if strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		rel, relErr := filepath.Rel(p.Root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		body, readErr := os.ReadFile(path)
		if readErr != nil {
			return nil
		}
		p.sources[rel] = body
		p.files = append(p.files, SourceFile{
			Rel:         rel,
			Abs:         path,
			IsTest:      strings.HasSuffix(rel, "_test.go"),
			IsGenerated: IsGeneratedSource(body),
			Package:     importPathFor(p.ModulePath, p.Root, filepath.Dir(path)),
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
func (p *Pass) Files() []SourceFile { return p.files }

// AppFiles returns the files analyzers care about by default: non-test,
// non-generated Go source that configuration has not exempted.
func (p *Pass) AppFiles() []SourceFile {
	var out []SourceFile
	for _, f := range p.files {
		if f.IsTest || f.IsGenerated || p.Config.ExemptPath(f.Rel) {
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
		if !f.IsTest || f.IsGenerated || p.Config.ExemptPath(f.Rel) {
			continue
		}
		out = append(out, f)
	}
	return out
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

// AST parses a discovered file once and caches the result. Comments are
// retained because suppression directives live in them. A file that fails
// to parse returns (nil, false) rather than an error: a project mid-edit
// should still get findings from every file that *does* parse.
func (p *Pass) AST(rel string) (*ast.File, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if f, ok := p.asts[rel]; ok {
		return f, f != nil
	}
	body, ok := p.sources[rel]
	if !ok {
		return nil, false
	}
	f, err := parser.ParseFile(p.fset, rel, body, parser.ParseComments|parser.SkipObjectResolution)
	if err != nil {
		p.asts[rel] = nil
		// Remembered, not merely skipped. Every analyzer that wanted this
		// file reports nothing for it, and without a record the run reads
		// as "these files are clean" rather than "these files could not
		// be read", the same distinction the baseline and --changed
		// counters exist to preserve.
		if p.unparsed == nil {
			p.unparsed = map[string]string{}
		}
		p.unparsed[rel] = err.Error()
		return nil, false
	}
	p.asts[rel] = f
	return f, true
}

// Unparsed returns the files whose source could not be parsed, keyed by
// path, with the parser's message. A tree mid-edit is the normal case for
// the dev loop, so this is reported rather than fatal, but it is
// reported.
func (p *Pass) Unparsed() map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make(map[string]string, len(p.unparsed))
	for k, v := range p.unparsed {
		out[k] = v
	}
	return out
}

// Position resolves an AST position into the pass's line/column space.
func (p *Pass) Position(pos token.Pos) token.Position {
	return p.FileSet().Position(pos)
}

// Memo computes a value once per pass and shares it across analyzers.
// This is how the routing analyzer's discovered route table reaches the
// testing and permissions analyzers without an ordering constraint
// between them: whoever asks first pays, everyone else reads the cache.
func (p *Pass) Memo(key string, compute func() any) any {
	p.mu.Lock()
	if v, ok := p.memo[key]; ok {
		p.mu.Unlock()
		return v
	}
	p.mu.Unlock()

	// Compute outside the lock: a memo body may itself call AST/Source,
	// which take the same mutex.
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

// IsGeneratedSource reports whether body carries the conventional
// generated-code header (https://pkg.go.dev/cmd/go#hdr-Generate_Go_files).
// Only the first 512 bytes are inspected, so a doc comment mentioning the
// phrase further down does not exempt a hand-written file.
func IsGeneratedSource(body []byte) bool {
	head := body
	if len(head) > 512 {
		head = head[:512]
	}
	return bytes.Contains(head, []byte("// Code generated")) ||
		bytes.Contains(head, []byte("DO NOT EDIT"))
}

// readModulePath returns the `module` line from root/go.mod, or "".
func readModulePath(root string) string {
	body, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "module ") {
			return strings.TrimSpace(strings.TrimPrefix(line, "module"))
		}
	}
	return ""
}

// importPathFor maps a directory to its Go import path.
func importPathFor(modulePath, root, dir string) string {
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
