package canon

import (
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Lint checks every reference in the canon against the tree: owned
// files, docs, enforcing tests, analyzers and contract rules.
func (c *Canon) Lint() []Finding {
	l := &linter{c: c, tests: map[string]map[string]bool{}}
	owners := map[string][]*Primitive{}
	for _, p := range c.Primitives {
		l.p = p
		if parent := parentID(p.ID); parent != "" && c.byID[parent] == nil {
			l.errf(p.Line, "parent primitive %q has no %s", parent, PrimitiveFile)
		}
		if len(p.Code) == 0 {
			l.errf(p.Line, "missing code: a primitive owns at least one path")
		}
		for _, ref := range p.Code {
			files := l.expand(ref.Value)
			if len(files) == 0 {
				l.errf(ref.Line, "code %q matches no file", ref.Value)
			}
			for _, f := range files {
				if !slices.Contains(owners[f], p) {
					owners[f] = append(owners[f], p)
				}
			}
		}
		for _, ref := range p.Docs {
			if !l.isFile(ref.Value) {
				l.errf(ref.Line, "docs %q is not a file", ref.Value)
			}
		}
		l.imports(p)
		l.invariants(p)
		l.decisions(p)
	}
	for _, f := range slices.Sorted(maps.Keys(owners)) {
		ps := owners[f]
		for i, a := range ps {
			for _, b := range ps[i+1:] {
				if !related(a.ID, b.ID) {
					l.p = b
					l.errf(b.Line, "%s is claimed by both %s and %s: a file has one owner chain", f, a.ID, b.ID)
				}
			}
		}
	}
	slices.SortFunc(l.out, func(a, b Finding) int {
		if n := strings.Compare(a.File, b.File); n != 0 {
			return n
		}
		if a.Line != b.Line {
			return a.Line - b.Line
		}
		return strings.Compare(a.Msg, b.Msg)
	})
	return l.out
}

// related reports whether one id is the other or its ancestor.
func related(a, b string) bool {
	return a == b || strings.HasPrefix(a, b+".") || strings.HasPrefix(b, a+".")
}

type linter struct {
	c     *Canon
	p     *Primitive
	out   []Finding
	tests map[string]map[string]bool
}

func (l *linter) errf(line int, format string, args ...any) {
	decoder{file: l.p.File, findings: &l.out}.errf(line, format, args...)
}

func (l *linter) isFile(rel string) bool {
	info, err := fs.Stat(l.c.fsys, rel)
	return err == nil && !info.IsDir()
}

// expand lists the files a code entry covers: a trailing slash is a
// directory tree, anything else a path.Match pattern.
func (l *linter) expand(entry string) []string {
	var out []string
	if dir, ok := strings.CutSuffix(entry, "/"); ok {
		_ = fs.WalkDir(l.c.fsys, dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				out = append(out, p)
			}
			return nil
		})
		return out
	}
	matches, _ := fs.Glob(l.c.fsys, entry)
	for _, m := range matches {
		if l.isFile(m) {
			out = append(out, m)
		}
	}
	return out
}

func (l *linter) imports(p *Primitive) {
	for _, rule := range p.Imports {
		if rule.Deny != "" && !l.hasGo(rule.Deny) {
			l.errf(rule.Line, "deny %q is not a Go package directory", rule.Deny)
		}
		if len(rule.From) == 0 {
			l.errf(rule.Line, "missing from: name the packages the ban applies to")
		}
		for _, from := range rule.From {
			if !l.hasGo(from.Value) {
				l.errf(from.Line, "from %q is not a Go package directory", from.Value)
			}
		}
	}
}

func (l *linter) hasGo(dir string) bool {
	matches, _ := fs.Glob(l.c.fsys, path.Join(dir, "*.go"))
	return len(matches) > 0
}

func (l *linter) invariants(p *Primitive) {
	seen := map[string]bool{}
	for _, inv := range p.Invariants {
		if seen[inv.ID] {
			l.errf(inv.Line, "invariant %q is declared twice", inv.ID)
		}
		seen[inv.ID] = true
		if len(inv.EnforcedBy) == 0 && inv.Judge == "" {
			l.errf(inv.Line, "invariant %q has no enforced_by and no judge: nothing would notice it break", inv.ID)
		}
		if inv.Judge != "" && !strings.HasSuffix(inv.Judge, "?") {
			l.errf(inv.Line, "judge for %q must be a yes/no question where yes means a violation", inv.ID)
		}
		for _, e := range inv.EnforcedBy {
			l.enforcer(e)
		}
	}
}

var testFunc = regexp.MustCompile(`(?m)^func (Test\w+)\(`)

func (l *linter) enforcer(e Enforcer) {
	switch e.Kind {
	case KindTest:
		i := strings.LastIndex(e.Ref, ".")
		if i < 0 {
			l.errf(e.Line, "test %q must be <package dir>.<TestName>", e.Ref)
			return
		}
		dir, name := e.Ref[:i], e.Ref[i+1:]
		if !l.testsIn(dir)[name] {
			l.errf(e.Line, "test %s does not exist in %s", name, dir)
		}
	case KindAnalyzer:
		if !l.hasGo(path.Join("internal/analyzers", e.Ref)) {
			l.errf(e.Line, "analyzer %q has no package under internal/analyzers", e.Ref)
			return
		}
		suite, _ := fs.ReadFile(l.c.fsys, "internal/analyzers/suite/suite.go")
		if !strings.Contains(string(suite), "/internal/analyzers/"+e.Ref+`"`) {
			l.errf(e.Line, "analyzer %q is not registered in internal/analyzers/suite", e.Ref)
		}
	case KindContract:
		catalog, _ := fs.ReadFile(l.c.fsys, "framework/contracts/catalog.go")
		if !strings.Contains(string(catalog), `"`+e.Ref+`"`) {
			l.errf(e.Line, "contract rule %q is not in framework/contracts/catalog.go", e.Ref)
		}
	}
}

func (l *linter) testsIn(dir string) map[string]bool {
	if names, ok := l.tests[dir]; ok {
		return names
	}
	names := map[string]bool{}
	files, _ := fs.Glob(l.c.fsys, path.Join(dir, "*_test.go"))
	for _, f := range files {
		src, err := fs.ReadFile(l.c.fsys, f)
		if err != nil {
			continue
		}
		for _, m := range testFunc.FindAllSubmatch(src, -1) {
			names[string(m[1])] = true
		}
	}
	l.tests[dir] = names
	return names
}

func (l *linter) decisions(p *Primitive) {
	ids := map[string]bool{}
	for _, d := range p.Decisions {
		if ids[d.ID] {
			l.errf(d.Line, "decision %q is declared twice", d.ID)
		}
		ids[d.ID] = true
	}
	for _, d := range p.Decisions {
		if d.Date != "" {
			if _, err := time.Parse(time.DateOnly, d.Date); err != nil {
				l.errf(d.Line, "date %q for %q must be YYYY-MM-DD", d.Date, d.ID)
			}
		}
		switch d.Status {
		case StatusAccepted:
			if d.SupersededBy != "" {
				l.errf(d.Line, "decision %q is accepted but names superseded_by", d.ID)
			}
		case StatusSuperseded:
			if !ids[d.SupersededBy] || d.SupersededBy == d.ID {
				l.errf(d.Line, "decision %q is superseded: superseded_by must name another decision of this primitive", d.ID)
			}
		case "":
		default:
			l.errf(d.Line, "status %q for %q must be accepted or superseded", d.Status, d.ID)
		}
		for _, ref := range d.Upholds {
			if l.c.Invariant(p, ref.Value) == nil {
				l.errf(ref.Line, "upholds %q names no invariant (use <id>, or <primitive>/<id> for another primitive)", ref.Value)
			}
		}
	}
}

// Invariant resolves an invariant reference made from p: a bare id is
// p's own, <primitive>/<id> reaches another primitive.
func (c *Canon) Invariant(p *Primitive, ref string) *Invariant {
	if prim, id, ok := strings.Cut(ref, "/"); ok {
		p, ref = c.byID[prim], id
	}
	if p == nil {
		return nil
	}
	for i := range p.Invariants {
		if p.Invariants[i].ID == ref {
			return &p.Invariants[i]
		}
	}
	return nil
}
