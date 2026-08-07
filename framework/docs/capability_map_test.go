package docs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The backend capability map is a lookup table, and a lookup table that lies
// is worse than no lookup table: an agent that trusts a wrong symbol spends
// more context recovering than it would have spent reading the package. These
// tests are the anti-rot gate — every symbol, link, and command it names has
// to resolve against the tree it ships from.
//
// This exists because of a measured cost. The 2026-07-26 backend eval put
// GoFastr at 313,579 cold-start tokens against Gin's 72,172 — 4.35x — for 48%
// fewer lines of application code. The compression is real; finding the
// primitive is what was expensive. The eval's own sixth next-move was "give
// agents one short capability map and exact verification commands before deep
// topical docs". ui-capability-map.md already did that for the UI lane. The
// backend lane, which is where the tokens actually went, had nothing.
const backendMapTopic = "backend-capability-map"

func backendMapBody(t *testing.T) string {
	t.Helper()
	body, err := Get(backendMapTopic)
	if err != nil {
		t.Fatalf("Get(%q): %v — the backend capability map is missing", backendMapTopic, err)
	}
	return string(body)
}

// repoRoot walks up from the package directory to the module root. Returns ""
// for embedded-only consumers, where the source tree is not on disk.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

// exportedSymbolsByPackage indexes every exported top-level declaration in the
// repo, keyed by the package's clause name (`framework`, `auth`, `entity`, …)
// — which is exactly how a doc refers to them. A name declared by two
// packages maps to the union, so this catches typos and renames rather than
// resolving import ambiguity.
func exportedSymbolsByPackage(t *testing.T, root string) map[string]map[string]bool {
	t.Helper()
	index := map[string]map[string]bool{}
	fset := token.NewFileSet()

	skip := map[string]bool{".git": true, "dist": true, "node_modules": true, "testdata": true}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // unreadable dirs are not the subject of this test
		}
		if info.IsDir() {
			if skip[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if perr != nil {
			return nil // a file that does not parse is go build's problem
		}
		pkg := file.Name.Name
		if index[pkg] == nil {
			index[pkg] = map[string]bool{}
		}
		for _, decl := range file.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Recv == nil && d.Name.IsExported() {
					index[pkg][d.Name.Name] = true
				}
				// Methods are indexed under their receiver type so a doc can
				// name App.Entity or Builder.Poll.
				if d.Recv != nil && d.Name.IsExported() {
					if recv := receiverTypeName(d.Recv); recv != "" {
						index[pkg][recv+"."+d.Name.Name] = true
					}
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							index[pkg][s.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, n := range s.Names {
							if n.IsExported() {
								index[pkg][n.Name] = true
							}
						}
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return index
}

func receiverTypeName(recv *ast.FieldList) string {
	if len(recv.List) == 0 {
		return ""
	}
	switch tp := recv.List[0].Type.(type) {
	case *ast.Ident:
		return tp.Name
	case *ast.StarExpr:
		if id, ok := tp.X.(*ast.Ident); ok {
			return id.Name
		}
	}
	return ""
}

// symbolRef matches a backticked `pkg.Symbol` or `pkg.Type.Method` reference.
var symbolRef = regexp.MustCompile("`([a-z][a-z0-9]*)\\.([A-Z][A-Za-z0-9]*(?:\\.[A-Z][A-Za-z0-9]*)?)`")

// TestBackendCapabilityMapSymbolsExist fails when the map names a symbol the
// tree does not export — the failure mode that makes a lookup table
// actively harmful.
func TestBackendCapabilityMapSymbolsExist(t *testing.T) {
	body := backendMapBody(t)
	root := repoRoot(t)
	if root == "" {
		t.Skip("no source tree (embedded-only consumer)")
	}
	index := exportedSymbolsByPackage(t, root)

	matches := symbolRef.FindAllStringSubmatch(body, -1)
	if len(matches) < 10 {
		t.Fatalf("found only %d `pkg.Symbol` references — a capability map that names almost no primitives is not doing its job, or this regex has drifted", len(matches))
	}
	checked := 0
	for _, m := range matches {
		pkg, sym := m[1], m[2]
		syms, ok := index[pkg]
		if !ok {
			t.Errorf("`%s.%s`: no package named %q in the repo", pkg, sym, pkg)
			continue
		}
		checked++
		if !syms[sym] {
			t.Errorf("`%s.%s`: package %q exports no %q — the map names a symbol that does not exist", pkg, sym, pkg, sym)
		}
	}
	t.Logf("verified %d symbol references across the capability map", checked)
}

// TestBackendCapabilityMapLinksResolve keeps every doc cross-link pointing at
// a topic that ships in the binary.
func TestBackendCapabilityMapLinksResolve(t *testing.T) {
	body := backendMapBody(t)
	link := regexp.MustCompile(`\]\(([a-z0-9-]+)\.md\)`)
	matches := link.FindAllStringSubmatch(body, -1)
	if len(matches) == 0 {
		t.Fatal("the map links to no other topic — it is meant to be a routing table into the detailed docs")
	}
	for _, m := range matches {
		if _, err := Get(m[1]); err != nil {
			t.Errorf("links to %s.md, which is not an embedded topic: %v", m[1], err)
		}
	}
}

// TestBackendCapabilityMapCommandsAreReal checks every `gofastr <sub>` the map
// tells a reader to run against the CLI's actual dispatch table. A
// verification command that is not a command is the worst kind of wrong: the
// reader runs it, gets a usage error, and distrusts the whole page.
func TestBackendCapabilityMapCommandsAreReal(t *testing.T) {
	body := backendMapBody(t)
	root := repoRoot(t)
	if root == "" {
		t.Skip("no source tree (embedded-only consumer)")
	}
	main, err := os.ReadFile(filepath.Join(root, "cmd", "gofastr", "main.go"))
	if err != nil {
		t.Fatalf("read cmd/gofastr/main.go: %v", err)
	}
	valid := map[string]bool{}
	caseRe := regexp.MustCompile(`case ((?:"[a-z]+",? ?)+):`)
	for _, m := range caseRe.FindAllStringSubmatch(string(main), -1) {
		for _, name := range strings.Split(m[1], ",") {
			valid[strings.Trim(strings.TrimSpace(name), `"`)] = true
		}
	}
	if len(valid) < 5 {
		t.Fatalf("parsed only %d subcommands from main.go — the dispatch shape changed and this test cannot check anything", len(valid))
	}

	cmdRe := regexp.MustCompile(`gofastr ([a-z]+)`)
	seen := map[string]bool{}
	for _, m := range cmdRe.FindAllStringSubmatch(body, -1) {
		sub := m[1]
		if seen[sub] {
			continue
		}
		seen[sub] = true
		if !valid[sub] {
			t.Errorf("names `gofastr %s`, which is not a subcommand — valid ones come from cmd/gofastr/main.go's dispatch", sub)
		}
	}
	if len(seen) == 0 {
		t.Error("the map names no gofastr command; it is supposed to carry exact verification commands")
	}
}

// TestEmbeddedDocTablesHaveHeaderText catches an empty markdown table header
// cell — `| | Package | … |` — which renders as `<th></th>` and trips axe's
// empty-table-header rule on whatever route serves the doc.
//
// This exists because that is exactly how it was found: a table in
// sqlite-engine.md whose first header cell was blank failed the site's
// full axe crawl, which takes ~11 minutes and only runs in the browser-e2e
// job. The defect is a string in a markdown file; it does not need a browser
// to detect, and waiting eleven minutes for a headless Chrome to tell you a
// `<th>` is empty is the wrong feedback loop.
//
// Deliberately scoped to header rows. Empty cells in a table BODY are
// ordinary — "not applicable" is a legitimate value — and axe does not
// object to them.
func TestEmbeddedDocTablesHaveHeaderText(t *testing.T) {
	topics, err := List()
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, top := range topics {
		body, err := Get(top.Name)
		if err != nil {
			t.Errorf("Get(%q): %v", top.Name, err)
			continue
		}
		lines := strings.Split(string(body), "\n")
		for i, line := range lines {
			// A header row is the line immediately above the ---|--- rule.
			if i+1 >= len(lines) || !isMarkdownTableHeader(line, lines[i+1]) {
				continue
			}
			checked++
			for col, cell := range splitMarkdownRow(line) {
				if strings.TrimSpace(cell) == "" {
					t.Errorf("content/%s.md line %d: table header column %d is empty — it renders as <th></th> and fails axe's empty-table-header rule. Give the column a name.",
						top.Name, i+1, col+1)
				}
			}
		}
	}
	if checked == 0 {
		t.Fatal("found no markdown tables at all — the header-row detection has drifted and this test is vacuous")
	}
	t.Logf("checked %d table header rows across the embedded docs", checked)
}

// isMarkdownTableHeader mirrors core/markdown's atTable: a row containing a
// pipe, followed by a separator that contains a pipe and nothing but `-`, `|`,
// `:` and spaces.
//
// Deliberately NOT "the line starts and ends with |". GFM makes the outer
// pipes optional, and core/markdown accepts that form — splitTableRow trims a
// leading and trailing pipe if present rather than requiring them. An earlier
// version of this test demanded them, so
//
//	Name | Package
//	---- | -------
//
// with a blank header cell would have rendered <th></th> and slipped straight
// past the guard. Matching the parser's own rule is what keeps the two from
// drifting; anything looser or stricter checks a different language than the
// one the site actually renders.
func isMarkdownTableHeader(line, next string) bool {
	if !strings.Contains(line, "|") || !strings.Contains(next, "|") {
		return false
	}
	sep := strings.TrimSpace(next)
	if sep == "" {
		return false
	}
	for _, ch := range sep {
		if ch != '-' && ch != '|' && ch != ':' && ch != ' ' {
			return false
		}
	}
	return true
}

// splitMarkdownRow mirrors core/markdown's splitTableRow: trim one optional
// outer pipe on each side, split on the rest.
func splitMarkdownRow(line string) []string {
	t := strings.TrimSpace(line)
	t = strings.TrimPrefix(t, "|")
	t = strings.TrimSuffix(t, "|")
	parts := strings.Split(t, "|")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return parts
}
