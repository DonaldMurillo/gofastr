package docs

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// The docs name Go identifiers in inline code spans: `crud.WithReadHooks`,
// `i18nui.TableSortedBy`, `Layout.WrapCtx`. A rename or a removal ships
// without a shim (no legacy, no deprecation), so every such name the docs
// still carry is a reader's compile error. The round-8 hunt found seven of
// them and the 2026-10-27 bug bash six more, each caught by hand. This gate
// resolves them all against the tree.

// goIndex is the repo's exported surface, read with go/parser.
type goIndex struct {
	// pkgs maps a package clause name to its exported top-level names.
	pkgs map[string]map[string]bool
	// types maps an exported type name to the exported methods and fields
	// declared on it (union across every package declaring that name).
	types map[string]map[string]bool
	// promote maps a type name to the type names whose members it gains:
	// embedded fields, embedded interfaces, alias and defined-type targets.
	promote map[string][]string
	// members is every exported method or field name anywhere, so a
	// receiver variable (`app.Router()`, `r.Context()`) is not read as a
	// package that lacks the name.
	members map[string]bool
	// fieldTypes maps an exported field name to the type names it holds,
	// so `Scope.OwnerField` resolves through EntityConfig's Scope field.
	fieldTypes map[string][]string
	// repoPkgs are the clause names this module declares (the stdlib
	// walk adds to pkgs, not here).
	repoPkgs map[string]bool
	// stdPkgs are the standard library's clause names.
	stdPkgs map[string]bool
	// varNames are the names this module gives local values: receivers,
	// parameters, `:=` and var declarations. Only such a name may read
	// as a value in a doc (`app.Router`, `q.Enqueue`); nothing is named
	// i18nui, so `i18nui.<Name>` must be a package reference.
	varNames map[string]bool
	// pkgMembers maps a package clause name to the exported methods and
	// fields of the types it declares: `ratelimit.Middleware` is loose
	// prose for (*Limiter).Middleware, and still names a real method.
	pkgMembers map[string]map[string]bool
}

func typeExprName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.Ident:
		return t.Name
	case *ast.StarExpr:
		return typeExprName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.IndexExpr:
		return typeExprName(t.X)
	case *ast.IndexListExpr:
		return typeExprName(t.X)
	}
	return ""
}

var (
	goIndexOnce sync.Once
	goIndexMemo *goIndex
)

// sharedGoIndex builds the index once per test binary: the three gates
// read the same tree.
func sharedGoIndex(t *testing.T, root string) *goIndex {
	t.Helper()
	goIndexOnce.Do(func() { goIndexMemo = buildGoIndex(t, root) })
	if goIndexMemo == nil {
		t.Fatal("go index build failed in an earlier test")
	}
	return goIndexMemo
}

func buildGoIndex(t *testing.T, root string) *goIndex {
	t.Helper()
	ix := &goIndex{
		pkgs:       map[string]map[string]bool{},
		types:      map[string]map[string]bool{},
		promote:    map[string][]string{},
		members:    map[string]bool{},
		fieldTypes: map[string][]string{},
		repoPkgs:   map[string]bool{},
		stdPkgs:    map[string]bool{},
		varNames:   map[string]bool{},
		pkgMembers: map[string]map[string]bool{},
	}
	curPkg := ""
	addMember := func(typ, name string) {
		if typ == "" || !ast.IsExported(name) {
			return
		}
		if ix.types[typ] == nil {
			ix.types[typ] = map[string]bool{}
		}
		ix.types[typ][name] = true
		ix.members[name] = true
		if ix.pkgMembers[curPkg] == nil {
			ix.pkgMembers[curPkg] = map[string]bool{}
		}
		ix.pkgMembers[curPkg][name] = true
	}
	addFields := func(typ string, fl *ast.FieldList) {
		if fl == nil {
			return
		}
		for _, f := range fl.List {
			if len(f.Names) == 0 {
				if emb := typeExprName(f.Type); emb != "" {
					ix.promote[typ] = append(ix.promote[typ], emb)
					addMember(typ, emb) // the embedded field's own name
				}
				continue
			}
			ft := typeExprName(f.Type)
			for _, n := range f.Names {
				addMember(typ, n.Name)
				if ft != "" && ast.IsExported(n.Name) {
					ix.fieldTypes[n.Name] = append(ix.fieldTypes[n.Name], ft)
				}
			}
		}
	}
	fset := token.NewFileSet()
	walkTree := func(root string, skip map[string]bool, repo bool) error {
		return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if d.IsDir() {
				if skip[d.Name()] || path != root && (strings.HasPrefix(d.Name(), ".") || strings.HasPrefix(d.Name(), "_")) {
					return filepath.SkipDir
				}
				return nil
			}
			if !strings.HasSuffix(path, ".go") {
				return nil
			}
			file, perr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
			if perr != nil {
				return nil
			}
			// An in-package _test.go file declares what a doc may call
			// "exported in test builds"; an external _test package
			// declares nothing a reader can import.
			if strings.HasSuffix(file.Name.Name, "_test") {
				return nil
			}
			pkg := file.Name.Name
			curPkg = pkg
			if repo {
				ix.repoPkgs[pkg] = true
				ast.Inspect(file, func(n ast.Node) bool {
					switch n := n.(type) {
					case *ast.FuncType:
						for _, fl := range []*ast.FieldList{n.Params, n.Results} {
							if fl == nil {
								continue
							}
							for _, f := range fl.List {
								for _, id := range f.Names {
									ix.varNames[id.Name] = true
								}
							}
						}
					case *ast.FuncDecl:
						if n.Recv != nil {
							for _, f := range n.Recv.List {
								for _, id := range f.Names {
									ix.varNames[id.Name] = true
								}
							}
						}
					case *ast.AssignStmt:
						if n.Tok == token.DEFINE {
							for _, e := range n.Lhs {
								if id, ok := e.(*ast.Ident); ok {
									ix.varNames[id.Name] = true
								}
							}
						}
					case *ast.RangeStmt:
						for _, e := range []ast.Expr{n.Key, n.Value} {
							if id, ok := e.(*ast.Ident); ok {
								ix.varNames[id.Name] = true
							}
						}
					case *ast.ValueSpec:
						for _, id := range n.Names {
							ix.varNames[id.Name] = true
						}
					}
					return true
				})
			} else {
				ix.stdPkgs[pkg] = true
			}
			if ix.pkgs[pkg] == nil {
				ix.pkgs[pkg] = map[string]bool{}
			}
			for _, decl := range file.Decls {
				switch d := decl.(type) {
				case *ast.FuncDecl:
					if d.Recv == nil {
						if d.Name.IsExported() {
							ix.pkgs[pkg][d.Name.Name] = true
						}
						continue
					}
					if len(d.Recv.List) > 0 {
						addMember(typeExprName(d.Recv.List[0].Type), d.Name.Name)
					}
				case *ast.GenDecl:
					for _, spec := range d.Specs {
						switch s := spec.(type) {
						case *ast.TypeSpec:
							if s.Name.IsExported() {
								ix.pkgs[pkg][s.Name.Name] = true
							}
							name := s.Name.Name
							switch tt := s.Type.(type) {
							case *ast.StructType:
								addFields(name, tt.Fields)
							case *ast.InterfaceType:
								addFields(name, tt.Methods)
							default:
								if target := typeExprName(tt); target != "" {
									ix.promote[name] = append(ix.promote[name], target)
								}
							}
						case *ast.ValueSpec:
							for _, n := range s.Names {
								if n.IsExported() {
									ix.pkgs[pkg][n.Name] = true
								}
							}
						}
					}
				}
			}
			return nil
		})
	}
	if err := walkTree(root, map[string]bool{"dist": true, "node_modules": true, "testdata": true}, true); err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	// The standard library, so `context.Canceled`, `log.Printf` and
	// `db.SetMaxOpenConns` resolve where a repo package shares the name.
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		t.Fatalf("go env GOROOT: %v", err)
	}
	goroot := filepath.Join(strings.TrimSpace(string(out)), "src")
	if err := walkTree(goroot, map[string]bool{"testdata": true, "cmd": true, "vendor": true, "internal": true}, false); err != nil {
		t.Fatalf("walk %s: %v", goroot, err)
	}
	return ix
}

// hasMember reports whether a type with this name, or one it embeds or
// aliases, declares the member.
func (ix *goIndex) hasMember(typ, member string) bool {
	seen := map[string]bool{}
	var walk func(string) bool
	walk = func(n string) bool {
		if seen[n] {
			return false
		}
		seen[n] = true
		if ix.types[n][member] {
			return true
		}
		for _, p := range ix.promote[n] {
			if walk(p) {
				return true
			}
		}
		return false
	}
	return walk(typ)
}

// resolves reports whether `Name.Member` names a member, reading Name as a
// type or as a field whose type has the member.
func (ix *goIndex) resolves(name, member string) bool {
	if ix.hasMember(name, member) {
		return true
	}
	for _, ft := range ix.fieldTypes[name] {
		if ix.hasMember(ft, member) {
			return true
		}
	}
	return false
}

// knownType reports whether a member set is known for the name, as a type
// or a field. A name with none is third-party, or a local variable.
func (ix *goIndex) knownType(name string) bool {
	_, ok := ix.types[name]
	return ok || len(ix.fieldTypes[name]) > 0
}

var (
	inlineSpan = regexp.MustCompile("`([^`\n]+)`")
	// pkgRef is `pkg.Name` or `pkg.Type.Member`, not preceded by an
	// identifier byte, a dot, or a slash (an import path or a selector
	// chain on a value is not a package reference).
	pkgRef = regexp.MustCompile(`(?:^|[^A-Za-z0-9_./])([a-z][a-z0-9]*)\.([A-Z][A-Za-z0-9_]*)(?:\.([A-Z][A-Za-z0-9_]*))?`)
	// typeRef is `Type.Member` at the start of a span or after a
	// non-identifier byte.
	typeRef = regexp.MustCompile(`(?:^|[^A-Za-z0-9_./])([A-Z][A-Za-z0-9_]*)\.([A-Z][A-Za-z0-9_]*)`)
	fenced  = regexp.MustCompile("(?s)```.*?```")
	// goneWords mark a sentence that names an API to say it no longer
	// exists ("`ui.SiteHeader` is gone", "There is no `framework.WithEmail`").
	goneWords = regexp.MustCompile(`(?i)\b(gone|removed|renamed|retired|no longer|there is no|there's no|replaced by|was replaced|deleted|dropped|not exported)\b`)
)

// identExemptTopics name APIs that no longer exist on purpose: the
// upgrade and release notes say what a version removed or renamed.
var identExemptTopics = map[string]bool{
	"upgrading":    true,
	"release-0-86": true,
	"release-0-87": true,
	// The harness lives under framework/experimental, which this gate's
	// sweeps leave alone until the harness is picked up again.
	"harness-architecture": true,
}

// identAllowed are references the index cannot resolve that are right
// anyway, keyed by topic and the reference as written. Each says why.
var identAllowed = map[string]string{
	// Generated code: names the generator derives from an entity spec.
	"endpoints:client.RefundOrderRequest": "the Go client generated for the doc's orders example",
	"states:client.MarkPaidOrders":        "the generated client method the doc derives from a move name",
	"query-dsl:app.UserEvents":            "a schema-qualified table name in a QuoteFolded example",
	"query-dsl:app.UserEvents_idx":        "an index name in a QuoteIndexName example",
	"contracts:Style.Scope":               "Style is the generated owned-style sheet var (*ownstyle.Sheet)",
	"runtime-contract:Style.Scope":        "Style is the generated owned-style sheet var (*ownstyle.Sheet)",
	// Third-party packages a repo package shares a name with.
	"image:draw.CatmullRom":             "golang.org/x/image/draw",
	"image:draw.BiLinear":               "golang.org/x/image/draw",
	"image:draw.ApproxBiLinear":         "golang.org/x/image/draw",
	"image:draw.NearestNeighbor":        "golang.org/x/image/draw",
	"observability:stdlib.GetConnector": "github.com/jackc/pgx/v5/stdlib",
	// Notation, not a Go selector.
	"widgets:RPC.Path": "a route-table placeholder: each registered RPC's path",
	// Go comments, keyed by file.
	"framework/uihost/uinoderender/renderer_audit_test.go:uinodev1.XProps":            "XProps stands for each uinodev1 props type",
	"cmd/gofastr/agents_snippets_test.go:queue.NewX":                                  "NewX stands for each queue constructor",
	"cmd/gofastr/agents_snippets_test.go:auth.NewAuthManager":                         "a past agents.md error the test was written for",
	"cmd/gofastr/internal/migratecmd/migrate_cmd_test.go:entities.RegisterMigrations": "the generated app's entities package",
	"core/query/safe.go:app.UserEvents":                                               "a schema-qualified table name in an example",
	"core/query/safe.go:app.UserEvents_idx":                                           "an index name in an example",
	"core/tracing/sql.go:stdlib.GetConnector":                                         "github.com/jackc/pgx/v5/stdlib",
	"framework/docs/truth_sweep_test.go:framework.Wildcard":                           "the wrong spelling the test pins out of access-control.md",
	"internal/upgrade/scan/fallback.go:ui.SiteHeader":                                 "an example go/types message for a removed symbol",
	"internal/upgrade/scan/symbols.go:kitx.Attrs":                                     "an example package-qualified assignment the scanner matches",

	// History: the fixture state a past benchmark ran against.
	"perf-results:Config.Public": "a past fixture's config, recorded as it was",

	// Repo markdown, keyed by its repo-relative path.
	"core-ui/ARCHITECTURE.md:Style.Scope":               "Style is the generated owned-style sheet var (*ownstyle.Sheet)",
	".claude/skills/gofastr-docs/SKILL.md:schema.Money": "a hypothetical new field type in an example",
	"battery/email/agents.md:client.SendMail":           "a host's mail client value, not this repo's client package",
	"CLAUDE.md:Date.UTC":                                "the JavaScript Date.UTC a browser lint fires on",
}

// importAlias reads `alias "github.com/DonaldMurillo/gofastr/<path>"` from a
// doc's code blocks, so `flag.UserEntry` under
// `import flag ".../core/featureflag"` resolves to package featureflag.
var importAlias = regexp.MustCompile(`(?m)^\s*(?:import\s+)?([a-z][a-z0-9_]*)\s+"github\.com/DonaldMurillo/gofastr/(?:[^"]+/)?([a-z0-9_]+)"`)

// TestDocsNameRealIdentifiers fails on a backticked Go identifier the tree
// does not declare.
//
//gofastr:test-reads *.go
func TestDocsNameRealIdentifiers(t *testing.T) {
	root := repoRoot(t)
	if root == "" {
		t.Skip("no source tree (embedded-only consumer)")
	}
	ix := sharedGoIndex(t, root)
	topics, err := List()
	if err != nil {
		t.Fatal(err)
	}
	var bad []string
	checked := 0
	for _, tp := range topics {
		if identExemptTopics[tp.Name] {
			continue
		}
		body, err := Get(tp.Name)
		if err != nil {
			t.Fatal(err)
		}
		b, n := ix.checkDoc(tp.Name, tp.Name+".md", body)
		bad = append(bad, b...)
		checked += n
	}
	reportIdents(t, bad, checked)
}

func reportIdents(t *testing.T, bad []string, checked int) {
	t.Helper()
	slices.Sort(bad)
	bad = slices.Compact(bad)
	for _, b := range bad {
		t.Error(b)
	}
	t.Logf("checked %d identifier references, %d unresolved", checked, len(bad))
}

// checkDoc resolves every inline-code Go reference in one markdown body.
// key names the doc in identAllowed; display prefixes each finding.
func (ix *goIndex) checkDoc(key, display string, body []byte) (bad []string, checked int) {
	aliases := map[string]string{}
	for _, m := range importAlias.FindAllStringSubmatch(string(body), -1) {
		aliases[m[1]] = m[2]
	}
	text := fenced.ReplaceAllStringFunc(string(body), func(s string) string {
		return strings.Repeat("\n", strings.Count(s, "\n"))
	})
	lines := strings.Split(text, "\n")
	for li, line := range lines {
		window := line
		if li > 0 {
			window = lines[li-1] + " " + window
		}
		if li+1 < len(lines) {
			window += " " + lines[li+1]
		}
		if goneWords.MatchString(window) {
			continue
		}
		at := display + ":" + strconv.Itoa(li+1) + ": `"
		for _, span := range inlineSpan.FindAllStringSubmatch(line, -1) {
			code := span[1]
			for _, m := range pkgRef.FindAllStringSubmatch(code, -1) {
				pkg, name, member := m[1], m[2], m[3]
				syms, ok := ix.pkgs[pkg]
				target, aliased := aliases[pkg]
				if !ok && !aliased {
					continue // third-party, or a variable
				}
				checked++
				ref := pkg + "." + name
				if member != "" {
					ref += "." + member
				}
				if _, allowed := identAllowed[key+":"+ref]; allowed {
					continue
				}
				if !syms[name] && !ix.pkgs[target][name] {
					if ix.valueRef(pkg, name) || ix.pkgMembers[target][name] {
						continue // a value's method or field: `app.Router`
					}
					bad = append(bad, at+pkg+"."+name+"`: package "+pkg+" declares no "+name)
					continue
				}
				if member != "" && ix.knownType(name) && !ix.resolves(name, member) {
					bad = append(bad, at+ref+"`: type "+name+" has no "+member)
				}
			}
			for _, m := range typeRef.FindAllStringSubmatch(code, -1) {
				typ, member := m[1], m[2]
				if !ix.knownType(typ) {
					continue
				}
				checked++
				if _, allowed := identAllowed[key+":"+typ+"."+member]; allowed {
					continue
				}
				if !ix.resolves(typ, member) {
					bad = append(bad, at+typ+"."+member+"`: type "+typ+" has no "+member)
				}
			}
		}
	}
	return bad, checked
}

// TestRepoDocsNameRealIdentifiers runs the same resolution over the
// markdown that ships beside the code: ARCHITECTURE docs, battery
// agents.md files, READMEs, skills, and the agent instructions.
//
//gofastr:test-reads *.go
//gofastr:test-reads *.md
func TestRepoDocsNameRealIdentifiers(t *testing.T) {
	root := repoRoot(t)
	if root == "" {
		t.Skip("no source tree (embedded-only consumer)")
	}
	ix := sharedGoIndex(t, root)
	var bad []string
	checked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if repoDocSkipDir(rel, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".md") || repoDocSkipFile[rel] {
			return nil
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			t.Fatalf("read %s: %v", rel, rerr)
		}
		b, n := ix.checkDoc(rel, rel, body)
		bad = append(bad, b...)
		checked += n
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	reportIdents(t, bad, checked)
}

// repoDocSkipDir leaves out the embedded corpus (the test above owns it),
// the changelog archive (history names what each release removed),
// generated and vendored trees, the experimental and kiln trees, and
// Claude Code's local worktrees and per-machine project state, which are
// gitignored and never reach a checkout.
func repoDocSkipDir(rel, name string) bool {
	if rel == "." {
		return false
	}
	if name == "testdata" || name == "node_modules" || name == "dist" || strings.HasPrefix(name, "_") {
		return true
	}
	if strings.HasPrefix(name, ".") && rel != ".claude" {
		return true
	}
	switch rel {
	case "framework/docs/content", "changelog", "kiln", "framework/experimental", ".claude/worktrees", ".claude/projects":
		return true
	}
	return false
}

// repoDocSkipFile are histories: they name what a release removed.
var repoDocSkipFile = map[string]bool{
	"CHANGELOG.md": true,
}

// declares reports whether a package (by clause name) declares name, or
// the singular of a plural (`ui.MetricBands`, a prose plural of MetricBand).
func (ix *goIndex) declares(pkg, name string) bool {
	if ix.pkgs[pkg][name] {
		return true
	}
	return strings.HasSuffix(name, "s") && ix.pkgs[pkg][strings.TrimSuffix(name, "s")]
}

// TestGoCommentsNameRealIdentifiers fails on a comment that names
// `pkg.Name` for a repo package the file imports (or its own package)
// when that package declares no Name. Round 8 found comments pointing
// readers at i18nui.T and i18nui.Defaults, both long gone.
//
//gofastr:test-reads *.go
func TestGoCommentsNameRealIdentifiers(t *testing.T) {
	root := repoRoot(t)
	if root == "" {
		t.Skip("no source tree (embedded-only consumer)")
	}
	ix := sharedGoIndex(t, root)
	const modPrefix = "github.com/DonaldMurillo/gofastr/"
	fset := token.NewFileSet()
	var bad []string
	checked := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel != "framework/docs/content" && repoDocSkipDir(rel, d.Name()) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(rel, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(fset, path, nil, parser.ParseComments|parser.SkipObjectResolution)
		if perr != nil {
			return nil
		}
		// Local name → package clause name (the import path's last
		// element), for this module's packages only.
		imported := map[string]string{}
		for _, im := range f.Imports {
			p, _ := strconv.Unquote(im.Path.Value)
			if !strings.HasPrefix(p, modPrefix) {
				continue
			}
			base := p[strings.LastIndex(p, "/")+1:]
			local := base
			if im.Name != nil {
				local = im.Name.Name
			}
			imported[local] = base
		}
		for _, cg := range f.Comments {
			text := cg.Text()
			if goneWords.MatchString(text) {
				continue
			}
			for _, loc := range pkgRef.FindAllStringSubmatchIndex(text, -1) {
				pkg, name := text[loc[2]:loc[3]], text[loc[4]:loc[5]]
				if loc[5] < len(text) && text[loc[5]] == '*' {
					continue // a family: `render.Respond*`
				}
				target, ok := imported[pkg]
				if !ok {
					// A comment may name any package of this module
					// (`see crud.WithReadHooks`) without importing it;
					// a stdlib name it does not import is left alone.
					if !ix.repoPkgs[pkg] || isStdlibName(ix, pkg) {
						continue
					}
					target = pkg
				}
				checked++
				if ix.declares(target, name) || ix.valueRef(pkg, name) {
					continue
				}
				if _, allowed := identAllowed[rel+":"+pkg+"."+name]; allowed {
					continue
				}
				bad = append(bad, rel+":"+strconv.Itoa(fset.Position(cg.Pos()).Line)+": "+pkg+"."+name+": package "+target+" declares no "+name)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	reportIdents(t, bad, checked)
}

// isStdlibName reports whether a standard library package shares the
// clause name. Such a name in a comment is usually the stdlib package
// (`log.Printf`), which the union index resolves anyway; leaving it out
// keeps the gate to names only this module can own.
func isStdlibName(ix *goIndex, pkg string) bool {
	return ix.stdPkgs[pkg]
}

// valueRef reports whether `x.Name` can read as a method or field of a
// value: x is a name this module gives values, and Name is a member of
// some type, or x is a package one of whose types has the member.
func (ix *goIndex) valueRef(x, name string) bool {
	return ix.varNames[x] && ix.members[name] || ix.pkgMembers[x][name]
}
