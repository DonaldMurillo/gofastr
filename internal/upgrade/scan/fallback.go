package scan

import (
	"go/token"
	"go/types"
	"maps"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"golang.org/x/tools/go/packages"
)

// fallbackPackage maps a broken package's compile errors to hits: a
// message naming a uses symbol (pkgname.Name, plus the member as a whole
// word) or an imports path lands at the error's position. Whether an
// unmatched error is unexplained is decided once every matcher has run
// (see result): a hit on the error's line explains it when the error
// names what the hit matched, and so does a shapes symbol on that line
// whose resolved shape the regex refused (explainedByShapeSite). An
// error with no position (a go.mod the go tool refuses) still counts.
func (e *engine) fallbackPackage(p *packages.Package) {
	aliases := importAliases(p)
	// go/types writes the erroring package's own names bare, as if its
	// files dot-imported it: a local alias (type LocalConfig =
	// html.DetailsConfig) appears as "of type LocalConfig".
	for _, byPath := range aliases {
		byPath[p.PkgPath] = append(byPath[p.PkgPath], ".")
	}
	typeAliases := typeAliasesOf(p)
	var errs []errAt
	for _, te := range p.TypeErrors {
		errs = append(errs, errAt{p.Fset.Position(te.Pos), te.Msg})
	}
	for _, pe := range p.Errors {
		if echoed, ok := compilerEcho(pe.Msg, e.loadDir(p)); ok {
			errs = append(errs, echoed...)
			continue
		}
		pos, _ := parsePos(pe.Pos)
		errs = append(errs, errAt{pos, pe.Msg})
	}
	for _, er := range errs {
		rel := ""
		if er.pos.Line > 0 {
			var ok bool
			if rel, ok = e.relToRoot(er.pos.Filename); !ok {
				continue
			}
		}
		if !e.matchErrAt(er.pos, rel, er.msg, aliases[er.pos.Filename], typeAliases) {
			e.pendingErrs = append(e.pendingErrs,
				Hit{File: rel, Line: er.pos.Line, Col: er.pos.Column, Why: er.msg})
		}
	}
}

// explainedByLine reports whether a hit on the error's line is named by
// the error: a changed method signature still resolves, so the uses
// matcher finds the call while go/types writes "not enough arguments in
// call to l.WithHeader", which names the member but no package. An
// error naming nothing the line's hits name (undefined: helper beside a
// SiteHeader call) is not explained.
func (e *engine) explainedByLine(h Hit) bool {
	if h.File == "" || !e.goHitLines[lineKey{h.File, h.Line}] {
		return false
	}
	for n, hs := range e.hits {
		for _, hit := range hs {
			if hit.File == h.File && hit.Line == h.Line && e.hitNamesError(n, hit, h.Why) {
				return true
			}
		}
	}
	return false
}

// hitNamesError reports whether the compile error msg names what hit
// matched: the imports path of an import hit, or the uses, shapes or
// fields symbol behind hit.Why.
func (e *engine) hitNamesError(n *upgrade.Note, hit Hit, msg string) bool {
	if path, ok := strings.CutPrefix(hit.Why, "import "); ok {
		return errNamesImport(msg, path)
	}
	for _, sm := range n.Find.Shapes {
		tag := sm.Symbol.String() + " shape"
		if hit.Why != tag && !strings.HasPrefix(hit.Why, tag+" ") {
			continue
		}
		if e.errNamesHitSymbol(msg, sm.Symbol) {
			return true
		}
	}
	if rest, ok := strings.CutPrefix(hit.Why, "field "); ok {
		for _, fm := range n.Find.Fields {
			if rest != fm.Field.String() && !strings.HasPrefix(rest, fm.Field.String()+" ") {
				continue
			}
			if e.errNamesHitSymbol(msg, fm.Field) {
				return true
			}
		}
		return false
	}
	for _, sym := range n.Find.Uses {
		if sym.String() == hit.Why {
			return e.errNamesHitSymbol(msg, sym)
		}
	}
	return false
}

// explainedByShapeSite explains a compile error on a line where a shapes
// symbol resolved to a shape its regex did not match. Against a kit
// already past the release that is how the migrated spelling looks, so
// the typed walk withheld the hit; an error there that names the symbol
// (or the struct literal its field sits in) means the app still spells
// the old shape. The note gets the hit, carrying the error, the way a
// uses symbol named by an error does.
func (e *engine) explainedByShapeSite(h Hit) bool {
	if h.File == "" {
		return false
	}
	explained := false
	for _, s := range e.shapeSites[lineKey{h.File, h.Line}] {
		if !e.errNamesHitSymbol(h.Why, s.sym) {
			continue
		}
		e.addGo(s.n, Hit{File: h.File, Line: h.Line, Col: s.col, Why: s.sym.String() + " shape", Err: h.Why})
		explained = true
	}
	return explained
}

// errNamesHitSymbol is errNamesSymbol over one already-matched symbol,
// loosened to the spellings an error can write without the package: the
// bare name (a dot import writes undefined: SiteHeader) and the member
// (a call through a variable writes l.WithHeader). A changed field type
// names neither — go/types writes "cannot use ... as ... value in
// struct literal" — so an error about the struct literal counts as
// naming the member hit inside it.
func (e *engine) errNamesHitSymbol(msg string, sym upgrade.Symbol) bool {
	if sym.Member != "" && strings.Contains(msg, "in struct literal") {
		return true
	}
	spellings := []string{
		pkgNameFor(sym.Pkg, e.pkgNames) + "." + sym.Name,
		strconv.Quote(sym.Pkg) + "." + sym.Name,
		sym.Name,
	}
	if sym.Member != "" {
		spellings = append(spellings, sym.Member)
	}
	return errNamesSymbol(msg, spellings, sym.Member)
}

// compilerEcho splits the go command's echo of a failed build ("# pkg"
// then one "file:line:col: msg" line per error, paths relative to dir,
// the module the load ran in) into positioned errors, which then dedupe
// against the type checker's copies. Lines that carry no position ("too
// many errors") are dropped.
func compilerEcho(msg, dir string) ([]errAt, bool) {
	header, body, ok := strings.Cut(msg, "\n")
	if !ok || !strings.HasPrefix(header, "# ") {
		return nil, false
	}
	var out []errAt
	for line := range strings.SplitSeq(body, "\n") {
		loc, text, ok := strings.Cut(strings.TrimSpace(line), ": ")
		if !ok {
			continue
		}
		pos, ok := parsePos(loc)
		if !ok {
			continue
		}
		if !filepath.IsAbs(pos.Filename) {
			pos.Filename = filepath.Join(dir, pos.Filename)
		}
		out = append(out, errAt{pos, text})
	}
	return out, true
}

// loadDir is the module directory p was loaded from: a nested module's
// own load echoes paths relative to it, not to the root module.
func (e *engine) loadDir(p *packages.Package) string {
	if p.Module != nil && p.Module.Dir != "" {
		return p.Module.Dir
	}
	return e.moduleRoot
}

type errAt struct {
	pos token.Position
	msg string
}

// importAliases maps each file of a package to the local names it
// imports packages under, by import path: go/types writes an undefined
// selector with the name the source used ("undefined: kitui.SiteHeader"),
// not the package's declared name. A dot import is recorded as ".": its
// symbols appear bare in the source and in the errors about them.
func importAliases(p *packages.Package) map[string]map[string][]string {
	out := map[string]map[string][]string{}
	for i, f := range p.Syntax {
		if i >= len(p.CompiledGoFiles) {
			break
		}
		byPath := map[string][]string{}
		for _, spec := range f.Imports {
			if spec.Name == nil || spec.Name.Name == "_" {
				continue
			}
			if ipath, err := strconv.Unquote(spec.Path.Value); err == nil {
				byPath[ipath] = append(byPath[ipath], spec.Name.Name)
			}
		}
		out[p.CompiledGoFiles[i]] = byPath
	}
	return out
}

// typeName is a package-level type, by import path and name.
type typeName struct{ pkg, name string }

// typeAliasesOf maps each type declared elsewhere to the aliases of it
// that the package itself and its direct imports declare: go/types
// names the type the source wrote, so an unknown field in a
// framework.EntityConfig literal reads "framework.EntityConfig" while
// the note names the declaring framework/entity.EntityConfig.
func typeAliasesOf(p *packages.Package) map[typeName][]typeName {
	out := map[typeName][]typeName{}
	scopes := make([]*packages.Package, 0, len(p.Imports)+1)
	scopes = append(scopes, p)
	for _, path := range slices.Sorted(maps.Keys(p.Imports)) {
		scopes = append(scopes, p.Imports[path])
	}
	for _, imp := range scopes {
		if imp.Types == nil {
			continue
		}
		scope := imp.Types.Scope()
		for _, name := range scope.Names() {
			tn, ok := scope.Lookup(name).(*types.TypeName)
			if !ok || !tn.IsAlias() {
				continue
			}
			named, ok := types.Unalias(tn.Type()).(*types.Named)
			if !ok || named.Obj().Pkg() == nil {
				continue
			}
			target := typeName{named.Obj().Pkg().Path(), named.Obj().Name()}
			out[target] = append(out[target], typeName{imp.PkgPath, name})
		}
	}
	return out
}

// matchErrAt runs every note's uses and imports entries against one
// compile error; false means no note explains it. aliases are the local
// import names of the error's file, by import path; typeAliases the
// alias spellings of each imported type.
func (e *engine) matchErrAt(pos token.Position, rel, msg string, aliases map[string][]string, typeAliases map[typeName][]typeName) bool {
	matched := false
	for _, n := range e.notes {
		for _, sym := range n.Find.Uses {
			spellings := e.spellings(typeName{sym.Pkg, sym.Name}, aliases, typeAliases)
			if !errNamesSymbol(msg, spellings, sym.Member) {
				continue
			}
			e.addGo(n, Hit{File: rel, Line: pos.Line, Col: pos.Column, Why: sym.String(), Err: msg})
			matched = true
		}
		// An unconditioned fields entry is a use of the field. A key or
		// value condition cannot be read off an error, so that error
		// stays for the unexplained list.
		for _, fm := range n.Find.Fields {
			if fm.Conditioned() {
				continue
			}
			spellings := e.spellings(typeName{fm.Field.Pkg, fm.Field.Name}, aliases, typeAliases)
			if !errNamesSymbol(msg, spellings, fm.Field.Member) {
				continue
			}
			e.addGo(n, Hit{File: rel, Line: pos.Line, Col: pos.Column, Why: "field " + fm.Field.String(), Err: msg})
			matched = true
		}
		for _, entry := range n.Find.Imports {
			if !errNamesImport(msg, entry) {
				continue
			}
			e.addGo(n, Hit{File: rel, Line: pos.Line, Col: pos.Column, Why: "import " + entry, Err: msg})
			matched = true
		}
	}
	return matched
}

// spellings lists every way an error can write t: qualified as declared
// and under each type alias of it.
func (e *engine) spellings(t typeName, aliases map[string][]string, typeAliases map[typeName][]typeName) []string {
	out := e.qualified(t, aliases)
	for _, a := range typeAliases[t] {
		out = append(out, e.qualified(a, aliases)...)
	}
	return out
}

// qualified spells a type the ways an error can write it: under its
// package's declared name, with the quoted import path go/types uses
// when two imports share that name, under each local import name, and
// bare when the file dot-imports the package.
func (e *engine) qualified(t typeName, aliases map[string][]string) []string {
	out := []string{
		pkgNameFor(t.pkg, e.pkgNames) + "." + t.name,
		strconv.Quote(t.pkg) + "." + t.name,
	}
	for _, local := range aliases[t.pkg] {
		if local == "." {
			out = append(out, t.name)
			continue
		}
		out = append(out, local+"."+t.name)
	}
	return out
}

// errNamesSymbol matches a go/types message against a symbol: one of its
// qualified spellings ("pkgname.Name") appears as a whole word, and for
// a member its name appears as a whole word too (undefined:
// ui.SiteHeader; l.WithHeader undefined (type *app.Layout has no field
// or method WithHeader)).
func errNamesSymbol(msg string, spellings []string, member string) bool {
	named := false
	for _, q := range spellings {
		if containsWord(msg, q) {
			named = true
			break
		}
	}
	if !named {
		return false
	}
	return member == "" || containsWord(msg, member)
}

// pkgNameFor reads a package's declared name from the load when present;
// the last import-path element is the stand-in for a package that never
// loaded.
func pkgNameFor(pkgPath string, pkgNames map[string]string) string {
	if n, ok := pkgNames[pkgPath]; ok && n != "" {
		return n
	}
	if i := strings.LastIndex(pkgPath, "/"); i >= 0 {
		return pkgPath[i+1:]
	}
	return pkgPath
}

// errNamesImport matches a load error that names the import path (or any
// path under a "/..." entry).
func errNamesImport(msg, entry string) bool {
	if prefix, ok := strings.CutSuffix(entry, "/..."); ok {
		return containsPathPrefix(msg, prefix)
	}
	return containsPathToken(msg, entry)
}

// containsWord reports whether word occurs in s delimited by
// non-word bytes on both sides.
func containsWord(s, word string) bool {
	for i := 0; i+len(word) <= len(s); i++ {
		if s[i:i+len(word)] != word {
			continue
		}
		left := i == 0 || !isWordByte(s[i-1])
		right := i+len(word) == len(s) || !isWordByte(s[i+len(word)])
		if left && right {
			return true
		}
	}
	return false
}

func isWordByte(c byte) bool {
	return c == '_' ||
		(c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// containsPathToken reports whether p occurs in s delimited by
// non-path bytes, so "pkg/tree" does not match inside "pkg/treeview".
func containsPathToken(s, p string) bool {
	for i := 0; i+len(p) <= len(s); i++ {
		if s[i:i+len(p)] != p {
			continue
		}
		left := i == 0 || !isPathByte(s[i-1])
		right := i+len(p) == len(s) || !isPathByte(s[i+len(p)])
		if left && right {
			return true
		}
	}
	return false
}

// containsPathPrefix reports whether s names a path equal to or under
// prefix.
func containsPathPrefix(s, prefix string) bool {
	for i := 0; i+len(prefix) <= len(s); i++ {
		if s[i:i+len(prefix)] != prefix {
			continue
		}
		if i > 0 && isPathByte(s[i-1]) {
			continue
		}
		if i+len(prefix) == len(s) || s[i+len(prefix)] == '/' || !isPathByte(s[i+len(prefix)]) {
			return true
		}
	}
	return false
}

func isPathByte(c byte) bool {
	return isWordByte(c) || c == '.' || c == '/' || c == '-' || c == '~' || c == '+'
}

// parsePos splits a packages.Error position ("file:line:col" or
// "file:line") from the right.
func parsePos(s string) (token.Position, bool) {
	if s == "" || s == "-" {
		return token.Position{}, false
	}
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return token.Position{Filename: s}, false
	}
	right, colErr := strconv.Atoi(s[i+1:])
	file := s[:i]
	j := strings.LastIndex(file, ":")
	if colErr == nil && j >= 0 {
		if line, err := strconv.Atoi(file[j+1:]); err == nil {
			return token.Position{Filename: file[:j], Line: line, Column: right}, true
		}
	}
	if colErr == nil {
		return token.Position{Filename: file, Line: right, Column: 1}, true
	}
	return token.Position{Filename: s}, false
}
