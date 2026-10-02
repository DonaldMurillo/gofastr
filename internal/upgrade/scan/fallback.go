package scan

import (
	"go/token"
	"go/types"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/tools/go/packages"
)

// fallbackPackage maps a broken package's compile errors to hits: a
// message naming a uses symbol (pkgname.Name, plus the member as a whole
// word) or an imports path lands at the error's position. Whether an
// unmatched error is unexplained is decided once every matcher has run
// (see result): a typed Go hit on the error's line explains it too. An
// error with no position (a go.mod the go tool refuses) still counts.
func (e *engine) fallbackPackage(p *packages.Package) {
	aliases := importAliases(p)
	typeAliases := typeAliasesOf(p)
	var errs []errAt
	for _, te := range p.TypeErrors {
		errs = append(errs, errAt{p.Fset.Position(te.Pos), te.Msg})
	}
	for _, pe := range p.Errors {
		if echoed, ok := e.compilerEcho(pe.Msg); ok {
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

// explainedByLine reports whether a typed Go hit (uses, imports, fields)
// sits on the error's line: a changed method signature still resolves,
// so the uses matcher finds the call while go/types writes "not enough
// arguments in call to l.WithHeader", which names no package.
func (e *engine) explainedByLine(h Hit) bool {
	return h.File != "" && e.goHitLines[lineKey{h.File, h.Line}]
}

// compilerEcho splits the go command's echo of a failed build ("# pkg"
// then one "file:line:col: msg" line per error, paths relative to the
// module root the load ran in) into positioned errors, which then dedupe
// against the type checker's copies. Lines that carry no position ("too
// many errors") are dropped.
func (e *engine) compilerEcho(msg string) ([]errAt, bool) {
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
			pos.Filename = filepath.Join(e.moduleRoot, pos.Filename)
		}
		out = append(out, errAt{pos, text})
	}
	return out, true
}

type errAt struct {
	pos token.Position
	msg string
}

// importAliases maps each file of a package to the local names it
// imports packages under, by import path: go/types writes an undefined
// selector with the name the source used ("undefined: kitui.SiteHeader"),
// not the package's declared name.
func importAliases(p *packages.Package) map[string]map[string][]string {
	out := map[string]map[string][]string{}
	for i, f := range p.Syntax {
		if i >= len(p.CompiledGoFiles) {
			break
		}
		byPath := map[string][]string{}
		for _, spec := range f.Imports {
			if spec.Name == nil || spec.Name.Name == "_" || spec.Name.Name == "." {
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
// that the package's direct imports declare: go/types names the type
// the source wrote, so an unknown field in a framework.EntityConfig
// literal reads "framework.EntityConfig" while the note names the
// declaring framework/entity.EntityConfig.
func typeAliasesOf(p *packages.Package) map[typeName][]typeName {
	out := map[typeName][]typeName{}
	for _, imp := range p.Imports {
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
			if fm.Key != "" || fm.Value != nil {
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
// package's declared name and under each local import name.
func (e *engine) qualified(t typeName, aliases map[string][]string) []string {
	out := []string{pkgNameFor(t.pkg, e.pkgNames) + "." + t.name}
	for _, local := range aliases[t.pkg] {
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
