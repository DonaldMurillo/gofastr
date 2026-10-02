package upgrade

import (
	"embed"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"

	coreyaml "github.com/DonaldMurillo/gofastr/core/yaml"
)

// registryFS is the migration registry: registry.yml (through and the
// marker sinks) plus one releases/<version>.yml per release that
// carries migration-relevant changes, maintained alongside CHANGELOG.md
// (a release PR with a BREAKING change adds its file in the same PR).
// `gofastr upgrade` reads it to guide a project from its current
// version to a target. The format is documented for contributors in
// framework/docs/content/cli.md, under "The migration registry".
//
//go:embed registry.yml releases/*.yml
var registryFS embed.FS

// Load parses the registry embedded in the binary.
func Load() (*Registry, error) { return ParseFS(registryFS) }

// ParseFS parses a split registry: registry.yml at the root of fsys and
// every releases/*.yml beside it. Each release file is named after its
// version; releases come back sorted oldest first. Errors name the file
// and line (releases/v0.86.0.yml:12: ...).
func ParseFS(fsys fs.FS) (*Registry, error) {
	const top = "registry.yml"
	src, err := fs.ReadFile(fsys, top)
	if err != nil {
		return nil, err
	}
	root, err := parseDoc(src)
	if err != nil {
		return nil, inFile(top, err)
	}
	reg, err := parseHeader(root, "through", "marker_sinks")
	if err != nil {
		return nil, inFile(top, err)
	}
	names, err := fs.Glob(fsys, "releases/*.yml")
	if err != nil {
		return nil, err
	}
	for _, name := range names {
		src, err := fs.ReadFile(fsys, name)
		if err != nil {
			return nil, err
		}
		n, err := parseDoc(src)
		if err != nil {
			return nil, inFile(name, err)
		}
		rel, err := parseRelease(n, name)
		if err != nil {
			return nil, inFile(name, err)
		}
		if want := rel.Version + ".yml"; path.Base(name) != want {
			return nil, inFile(name, errAt(n.Map["version"].Line, "file holds release %s: name it releases/%s", rel.Version, want))
		}
		reg.Releases = append(reg.Releases, rel)
	}
	slices.SortFunc(reg.Releases, func(a, b Release) int {
		switch {
		case SemverLess(a.Version, b.Version):
			return -1
		case SemverLess(b.Version, a.Version):
			return 1
		}
		return 0
	})
	return reg, nil
}

// Parse parses a registry written as one document: through, the
// optional marker_sinks, and an inline releases list. Tests use it to
// feed small registries; the shipped registry is split (ParseFS).
// Validation is strict — unknown keys, regexes that do not compile,
// malformed symbols, contradictory note fields are refused — because a
// registry typo must fail the build's embedded-file test, not a user's
// upgrade. Errors read registry:<line>: ....
func Parse(src string) (*Registry, error) {
	const name = "registry"
	root, err := parseDoc([]byte(src))
	if err != nil {
		return nil, inFile(name, err)
	}
	reg, err := parseHeader(root, "through", "marker_sinks", "releases")
	if err != nil {
		return nil, inFile(name, err)
	}
	list := root.Map["releases"]
	if list == nil || list.Kind != coreyaml.List {
		return nil, inFile(name, errAt(root.Line, "missing releases list"))
	}
	for _, item := range list.List {
		rel, err := parseRelease(item, name)
		if err != nil {
			return nil, inFile(name, err)
		}
		reg.Releases = append(reg.Releases, rel)
	}
	return reg, nil
}

func parseDoc(src []byte) (*coreyaml.Node, error) {
	root, err := coreyaml.Parse(string(src))
	if err != nil {
		return nil, err
	}
	if root.Kind != coreyaml.Map {
		return nil, errAt(root.Line, "document must be a mapping")
	}
	return root, nil
}

// parseHeader reads through and marker_sinks from a registry's top
// document; allowed lists the keys that document may carry.
func parseHeader(root *coreyaml.Node, allowed ...string) (*Registry, error) {
	if err := unknownKeys(root, allowed...); err != nil {
		return nil, err
	}
	reg := &Registry{}
	tn := root.Map["through"]
	if tn == nil {
		return nil, errAt(root.Line, "missing through")
	}
	through, err := optString(tn, "through")
	if err != nil {
		return nil, err
	}
	if _, err := ParseSemver(through); err != nil {
		return nil, errAt(tn.Line, "through: %v", err)
	}
	reg.Through = through
	if sn := root.Map["marker_sinks"]; sn != nil {
		if reg.MarkerSinks, err = parseMarkerSinks(sn); err != nil {
			return nil, err
		}
	}
	return reg, nil
}

func parseRelease(n *coreyaml.Node, file string) (Release, error) {
	var rel Release
	if n.Kind != coreyaml.Map {
		return rel, errAt(n.Line, "release entry must be a map")
	}
	if err := unknownKeys(n, "version", "title", "notes"); err != nil {
		return rel, err
	}
	vn := n.Map["version"]
	if vn == nil {
		return rel, errAt(n.Line, "release is missing version")
	}
	version, err := optString(vn, "release version")
	if err != nil {
		return rel, err
	}
	if _, err := ParseSemver(version); err != nil {
		return rel, errAt(vn.Line, "release version: %v", err)
	}
	rel.Version = version
	if rel.Title, err = optString(n.Map["title"], "release title"); err != nil {
		return rel, err
	}
	if notes := n.Map["notes"]; notes != nil {
		if notes.Kind != coreyaml.List {
			return rel, errAt(notes.Line, "notes must be a list")
		}
		for _, item := range notes.List {
			note, err := parseNote(item)
			if err != nil {
				return rel, err
			}
			note.Version, note.File, note.Line = version, file, item.Line
			rel.Notes = append(rel.Notes, &note)
		}
	}
	return rel, nil
}

func parseNote(n *coreyaml.Node) (Note, error) {
	var note Note
	if n.Kind != coreyaml.Map {
		return note, errAt(n.Line, "note must be a map")
	}
	if err := unknownKeys(n, "change", "breaking", "guidance", "find", "nodetect"); err != nil {
		return note, err
	}
	var err error
	if note.Change, err = optString(n.Map["change"], "change"); err != nil {
		return note, err
	}
	if note.Guidance, err = optString(n.Map["guidance"], "guidance"); err != nil {
		return note, err
	}
	if note.Nodetect, err = optString(n.Map["nodetect"], "nodetect"); err != nil {
		return note, err
	}
	if bn := n.Map["breaking"]; bn != nil {
		b, ok := bn.Value.(bool)
		if bn.Kind != coreyaml.Scalar || !ok {
			return note, errAt(bn.Line, "breaking must be true or false")
		}
		note.Breaking = b
	}
	if fn := n.Map["find"]; fn != nil {
		if note.Find, err = parseFind(fn); err != nil {
			return note, err
		}
	}
	switch hasFind, hasReason := !note.Find.Empty(), strings.TrimSpace(note.Nodetect) != ""; {
	case hasFind && hasReason:
		return note, errAt(n.Line, "find and nodetect together: pick one")
	case !hasFind && !hasReason:
		return note, errAt(n.Line, "note has neither find nor nodetect: describe the code it affects, or say why no spelling differs")
	}
	return note, nil
}

func parseFind(n *coreyaml.Node) (Find, error) {
	var f Find
	if n.Kind != coreyaml.Map {
		return f, errAt(n.Line, "find must be a map")
	}
	if len(n.Map) == 0 {
		return f, errAt(n.Line, "find is empty: a note either describes matchers or says nodetect")
	}
	if err := unknownKeys(n, "uses", "imports", "fields", "strings", "css", "config", "gomod", "text"); err != nil {
		return f, err
	}
	var err error
	if f.Uses, err = parseSymbolList(n.Map["uses"], "uses", false); err != nil {
		return f, err
	}
	if f.Imports, err = parseStringList(n.Map["imports"], "imports", true); err != nil {
		return f, err
	}
	if n.Map["fields"] != nil {
		if f.Fields, err = parseFieldMatches(n.Map["fields"]); err != nil {
			return f, err
		}
	}
	if sn := n.Map["strings"]; sn != nil {
		if f.Strings, err = parseStringMatch(sn); err != nil {
			return f, err
		}
	}
	if cn := n.Map["css"]; cn != nil {
		if f.CSS, err = parseCSSMatch(cn); err != nil {
			return f, err
		}
	}
	if n.Map["config"] != nil {
		if f.Config, err = parseConfigMatches(n.Map["config"]); err != nil {
			return f, err
		}
	}
	if gn := n.Map["gomod"]; gn != nil {
		if f.GoMod, err = parseGoModMatch(gn); err != nil {
			return f, err
		}
	}
	if n.Map["text"] != nil {
		if f.Text, err = parseTextMatches(n.Map["text"]); err != nil {
			return f, err
		}
	}
	return f, nil
}

func parseStringMatch(n *coreyaml.Node) (StringMatch, error) {
	var m StringMatch
	if n.Kind != coreyaml.Map {
		return m, errAt(n.Line, "strings must be a map")
	}
	if err := unknownKeys(n, "classes", "attrs", "properties", "match"); err != nil {
		return m, err
	}
	var err error
	if m.Classes, err = parseStringList(n.Map["classes"], "strings.classes", false); err != nil {
		return m, err
	}
	if m.Attrs, err = parseStringList(n.Map["attrs"], "strings.attrs", false); err != nil {
		return m, err
	}
	if m.Properties, err = parseStringList(n.Map["properties"], "strings.properties", false); err != nil {
		return m, err
	}
	if mn := n.Map["match"]; mn != nil {
		src, err := optString(mn, "strings.match")
		if err != nil {
			return m, err
		}
		if m.Match, err = compileRegex(mn.Line, "strings.match", src); err != nil {
			return m, err
		}
	}
	return m, nil
}

func parseCSSMatch(n *coreyaml.Node) (CSSMatch, error) {
	var m CSSMatch
	if n.Kind != coreyaml.Map {
		return m, errAt(n.Line, "css must be a map")
	}
	if err := unknownKeys(n, "classes", "properties"); err != nil {
		return m, err
	}
	var err error
	if m.Classes, err = parseStringList(n.Map["classes"], "css.classes", false); err != nil {
		return m, err
	}
	if m.Properties, err = parseStringList(n.Map["properties"], "css.properties", false); err != nil {
		return m, err
	}
	return m, nil
}

func parseFieldMatches(n *coreyaml.Node) ([]FieldMatch, error) {
	if n.Kind != coreyaml.List {
		return nil, errAt(n.Line, "fields must be a list")
	}
	out := make([]FieldMatch, 0, len(n.List))
	for _, item := range n.List {
		if item.Kind != coreyaml.Map {
			return nil, errAt(item.Line, "fields entry must be a map")
		}
		if err := unknownKeys(item, "field", "key", "value"); err != nil {
			return nil, err
		}
		fn := item.Map["field"]
		if fn == nil {
			return nil, errAt(item.Line, "fields entry is missing field")
		}
		var fm FieldMatch
		sym, err := parseSymbol(fn)
		if err != nil {
			return nil, err
		}
		if sym.Member == "" {
			return nil, errAt(fn.Line, "field %q must name Type.Member (the type that declares the field)", sym.String())
		}
		fm.Field = sym
		if fm.Key, err = optString(item.Map["key"], "fields.key"); err != nil {
			return nil, err
		}
		if vn := item.Map["value"]; vn != nil {
			src, err := optString(vn, "fields.value")
			if err != nil {
				return nil, err
			}
			if fm.Value, err = compileRegex(vn.Line, "fields.value", src); err != nil {
				return nil, err
			}
		}
		out = append(out, fm)
	}
	return out, nil
}

func parseConfigMatches(n *coreyaml.Node) ([]ConfigMatch, error) {
	if n.Kind != coreyaml.List {
		return nil, errAt(n.Line, "config must be a list")
	}
	out := make([]ConfigMatch, 0, len(n.List))
	for _, item := range n.List {
		if item.Kind != coreyaml.Map {
			return nil, errAt(item.Line, "config entry must be a map")
		}
		if err := unknownKeys(item, "key", "value"); err != nil {
			return nil, err
		}
		kn := item.Map["key"]
		if kn == nil {
			return nil, errAt(item.Line, "config entry is missing key")
		}
		var cm ConfigMatch
		key, err := optString(kn, "config.key")
		if err != nil {
			return nil, err
		}
		if key == "" {
			return nil, errAt(kn.Line, "config key is empty")
		}
		for _, part := range strings.Split(key, ".") {
			if part == "" {
				return nil, errAt(kn.Line, "config key %q has an empty path element", key)
			}
		}
		cm.Key = key
		if vn := item.Map["value"]; vn != nil {
			src, err := optString(vn, "config.value")
			if err != nil {
				return nil, err
			}
			if cm.Value, err = compileRegex(vn.Line, "config.value", src); err != nil {
				return nil, err
			}
		}
		out = append(out, cm)
	}
	return out, nil
}

func parseGoModMatch(n *coreyaml.Node) (*GoModMatch, error) {
	if n.Kind != coreyaml.Map {
		return nil, errAt(n.Line, "gomod must be a map")
	}
	if err := unknownKeys(n, "go_below"); err != nil {
		return nil, err
	}
	gn := n.Map["go_below"]
	if gn == nil {
		return nil, errAt(n.Line, "gomod is missing go_below")
	}
	below, err := optString(gn, "gomod.go_below")
	if err != nil {
		return nil, err
	}
	if !isGoVersion(below) {
		return nil, errAt(gn.Line, "go_below %q is not a Go version (e.g. \"1.27\")", below)
	}
	return &GoModMatch{GoBelow: below}, nil
}

func parseTextMatches(n *coreyaml.Node) ([]TextMatch, error) {
	if n.Kind != coreyaml.List {
		return nil, errAt(n.Line, "text must be a list")
	}
	out := make([]TextMatch, 0, len(n.List))
	for _, item := range n.List {
		if item.Kind != coreyaml.Map {
			return nil, errAt(item.Line, "text entry must be a map")
		}
		if err := unknownKeys(item, "glob", "match"); err != nil {
			return nil, err
		}
		gn := item.Map["glob"]
		if gn == nil {
			return nil, errAt(item.Line, "text entry is missing glob")
		}
		glob, err := optString(gn, "text.glob")
		if err != nil {
			return nil, err
		}
		if glob == "" {
			return nil, errAt(gn.Line, "text glob is empty")
		}
		if ext, structural := globStructuralExt(glob); structural {
			return nil, errAt(gn.Line, "text glob %q targets .%s files, which the %s structural matcher owns — never a text regex", glob, ext, ext)
		}
		var tm TextMatch
		tm.Glob = glob
		mn := item.Map["match"]
		if mn == nil {
			return nil, errAt(item.Line, "text entry is missing match")
		}
		src, err := optString(mn, "text.match")
		if err != nil {
			return nil, err
		}
		if tm.Match, err = compileRegex(mn.Line, "text.match", src); err != nil {
			return nil, err
		}
		out = append(out, tm)
	}
	return out, nil
}

func parseMarkerSinks(n *coreyaml.Node) (MarkerSinks, error) {
	var ms MarkerSinks
	if n.Kind != coreyaml.Map {
		return ms, errAt(n.Line, "marker_sinks must be a map")
	}
	if err := unknownKeys(n, "calls", "fields", "attr_keys"); err != nil {
		return ms, err
	}
	var err error
	if cn := n.Map["calls"]; cn != nil {
		if cn.Kind != coreyaml.List {
			return ms, errAt(cn.Line, "marker_sinks.calls must be a list")
		}
		for _, item := range cn.List {
			if item.Kind != coreyaml.Map {
				return ms, errAt(item.Line, "calls entry must be a map")
			}
			if err := unknownKeys(item, "func", "arg"); err != nil {
				return ms, err
			}
			fn := item.Map["func"]
			if fn == nil {
				return ms, errAt(item.Line, "calls entry is missing func")
			}
			sym, err := parseSymbol(fn)
			if err != nil {
				return ms, err
			}
			an := item.Map["arg"]
			if an == nil {
				return ms, errAt(item.Line, "calls entry is missing arg")
			}
			arg, ok := an.Value.(int64)
			if an.Kind != coreyaml.Scalar || !ok {
				return ms, errAt(an.Line, "arg must be an integer (0-based call position), got %v", an.Value)
			}
			if arg < 0 {
				return ms, errAt(an.Line, "arg is negative (%d): call positions are 0-based", arg)
			}
			ms.Calls = append(ms.Calls, ParamSink{Func: sym, Arg: int(arg)})
		}
	}
	sinks, err := parseSymbolList(n.Map["fields"], "marker_sinks.fields", true)
	if err != nil {
		return ms, err
	}
	ms.Fields = sinks
	if ms.AttrKeys, err = parseStringList(n.Map["attr_keys"], "marker_sinks.attr_keys", false); err != nil {
		return ms, err
	}
	return ms, nil
}

// parseSymbol reads the registry's symbol spelling: an import path,
// then Name, or Type.Member. The import path is everything up to the
// last "/" plus the first dot-separated element after it, so
// "gofastr/framework/app.Layout.WithHeader" names pkg
// github.com/DonaldMurillo/gofastr/framework/app, type Layout, member
// WithHeader, and "database/sql.Open" names pkg database/sql, name
// Open. The gofastr/ shorthand is expanded first.
func parseSymbol(n *coreyaml.Node) (Symbol, error) {
	src, err := optString(n, "symbol")
	if err != nil {
		return Symbol{}, err
	}
	var sym Symbol
	if src == "" {
		return sym, errAt(n.Line, "symbol is empty")
	}
	if strings.ContainsAny(src, " \t") {
		return sym, errAt(n.Line, "malformed symbol %q: whitespace", src)
	}
	spelled := expandShorthand(src)
	tail := spelled
	if i := strings.LastIndex(spelled, "/"); i >= 0 {
		tail = spelled[i+1:]
	}
	parts := strings.Split(tail, ".")
	if len(parts) > 3 {
		return sym, errAt(n.Line, "malformed symbol %q: at most Type.Member after the import path", src)
	}
	for _, p := range parts {
		if p == "" {
			return sym, errAt(n.Line, "malformed symbol %q: empty element", src)
		}
	}
	sym.Pkg = spelled[:len(spelled)-len(tail)] + parts[0]
	sym.Name = parts[1]
	if len(parts) == 3 {
		sym.Member = parts[2]
	}
	return sym, nil
}

func parseSymbolList(n *coreyaml.Node, what string, memberRequired bool) ([]Symbol, error) {
	if n == nil {
		return nil, nil
	}
	if n.Kind != coreyaml.List {
		return nil, errAt(n.Line, "%s must be a list", what)
	}
	out := make([]Symbol, 0, len(n.List))
	for _, item := range n.List {
		sym, err := parseSymbol(item)
		if err != nil {
			return nil, err
		}
		if memberRequired && sym.Member == "" {
			return nil, errAt(item.Line, "%s symbol %q must name Type.Member (the type that declares it)", what, sym.String())
		}
		out = append(out, sym)
	}
	return out, nil
}

func parseStringList(n *coreyaml.Node, what string, expand bool) ([]string, error) {
	if n == nil {
		return nil, nil
	}
	if n.Kind != coreyaml.List {
		return nil, errAt(n.Line, "%s must be a list", what)
	}
	out := make([]string, 0, len(n.List))
	for _, item := range n.List {
		s, err := optString(item, what+" entry")
		if err != nil {
			return nil, err
		}
		if s == "" {
			return nil, errAt(item.Line, "%s entry is empty", what)
		}
		if expand {
			s = expandShorthand(s)
		}
		out = append(out, s)
	}
	return out, nil
}

// compileRegex compiles a registry regex, refusing both non-compiling
// and empty sources: the empty regex matches every line, which would
// flag an entire project as affected.
func compileRegex(line int, what, src string) (*regexp.Regexp, error) {
	if src == "" {
		return nil, errAt(line, "%s is empty", what)
	}
	re, err := regexp.Compile(src)
	if err != nil {
		return nil, errAt(line, "%s does not compile: %v", what, err)
	}
	return re, nil
}

// expandShorthand rewrites a leading "gofastr/" to the full module
// path, in symbols, imports and marker sinks alike.
func expandShorthand(s string) string {
	if rest, ok := strings.CutPrefix(s, "gofastr/"); ok {
		return ModulePath + "/" + rest
	}
	return s
}

// isGoVersion reports whether s is a Go version as the go directive
// spells one: one to three dot-separated numbers, e.g. "1", "1.27",
// "1.27.1".
func isGoVersion(s string) bool {
	if s == "" {
		return false
	}
	parts := strings.Split(s, ".")
	if len(parts) > 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// globStructuralExt reports which structural file extension a text
// glob targets, if any. Go and CSS files are never text targets: the
// uses matcher reads .go through the type checker and the css matcher
// reads .css through the CSS tokenizer, so a per-line regex over them
// would cry wolf on comments and strings.
func globStructuralExt(glob string) (ext string, structural bool) {
	seg := glob
	if i := strings.LastIndex(glob, "/"); i >= 0 {
		seg = glob[i+1:]
	}
	dot := strings.LastIndex(seg, ".")
	if dot < 0 || dot == len(seg)-1 {
		return "", false
	}
	ext = seg[dot+1:]
	if strings.ContainsAny(ext, "*?[{") {
		return "", false
	}
	if ext == "go" || ext == "css" {
		return ext, true
	}
	return "", false
}

func unknownKeys(n *coreyaml.Node, allowed ...string) error {
	for _, k := range slices.Sorted(maps.Keys(n.Map)) {
		if !slices.Contains(allowed, k) {
			return errAt(n.Map[k].Line, "unknown key %q (allowed: %s)", k, strings.Join(allowed, ", "))
		}
	}
	return nil
}

// optString reads an optional scalar. A missing node is the empty
// string; anything present must be a scalar the parser read as a
// string (or an integer, which YAML cannot distinguish from a digit
// string), and a float is refused with a pointer at quoting so
// go_below: 1.27 does not silently become 1.27-e0.
func optString(n *coreyaml.Node, what string) (string, error) {
	if n == nil {
		return "", nil
	}
	if n.Kind != coreyaml.Scalar {
		return "", errAt(n.Line, "%s must be a scalar", what)
	}
	switch v := n.Value.(type) {
	case string:
		return v, nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	default:
		return "", errAt(n.Line, "%s must be a quoted string (YAML read it as %T)", what, n.Value)
	}
}

// errAt reports a problem at a line; inFile adds the file name at the
// document boundary, giving file:line: message.
func errAt(line int, format string, args ...any) error {
	return fmt.Errorf("%d: %s", line, fmt.Sprintf(format, args...))
}

func inFile(name string, err error) error {
	return fmt.Errorf("%s:%w", name, err)
}
