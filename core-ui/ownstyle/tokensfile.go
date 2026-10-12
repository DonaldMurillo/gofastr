package ownstyle

import (
	"fmt"
	"go/format"
	"go/token"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

// An app's own theme tokens live in <name>.tokens.css, beside the Go
// package that owns them, written as CSS @property registrations:
//
//	/* The column the header, main and footer share. */
//	@property --size-page-width { syntax: "<length>"; inherits: true; initial-value: 67.5rem; }
//	@property --color-highlight { syntax: "<color>"; inherits: true; initial-value: #0F766E; }
//	@media (--dark) { :root { --color-highlight: #5EEAD4; } }
//
// The name's prefix is the token type (style.TokenCategory) and the
// syntax descriptor must be that type's; initial-value is the token's
// value, checked by style.ParseToken, the rule ApplyTokens applies.
// `gofastr gen styles` writes <name>_tokens.gen.go from it: a typed
// token set the app passes to Theme.Extend. The file is a source, never
// served: the running theme emits the tokens.

// RuleDuplicateTokenValue is GOFASTR1821: an app token whose value is
// already another token's of the same type. Two names for one value
// drift apart the first time one of them changes.
const RuleDuplicateTokenValue = "GOFASTR1821"

// ruleTokensFile labels a tokens file the generator refuses. Like a
// parse error it carries no GOFASTR id: the Go is not written, so the
// file's generated sibling is missing or stale, which GOFASTR1814
// reports at verify time.
const ruleTokensFile = "tokens-file"

// TokensFileName returns the CSS file name of a token set: "acme" →
// "acme.tokens.css".
func TokensFileName(name string) string { return name + ".tokens.css" }

// GeneratedTokensFileName returns the Go file gen styles writes for a
// token set: "acme" → "acme_tokens.gen.go". GOFASTR1814 looks for
// exactly this sibling.
func GeneratedTokensFileName(name string) string { return name + "_tokens.gen.go" }

// TokensFile is a parsed <name>.tokens.css.
type TokensFile struct {
	Tokens []AppToken // in source order
}

// AppToken is one token a tokens file declares.
type AppToken struct {
	Key   string // the custom property without "--": "size-page-width"
	Value any    // the typed token: style.Size{Name: "page-width", …}
	Light string // initial-value as written
	Dark  string // the @media (--dark) value; "" when none (colours only)
	Doc   string // the comment above the @property rule
	Pos   Pos    // the property name in the @property prelude
}

// tokenSyntax is the @property syntax each token type registers with.
var tokenSyntax = map[string][]string{
	"color":       {"<color>"},
	"size":        {"<length>", "<length-percentage>"},
	"text":        {"<length>", "<length-percentage>"},
	"spacing":     {"<length>"},
	"radii":       {"<length>"},
	"stroke":      {"<length>"},
	"leading":     {"<number>", "<length>"},
	"tracking":    {"<length>"},
	"opacity":     {"<number>"},
	"font-weight": {"<number>", "<integer>"},
	"z":           {"<integer>"},
	"duration":    {"<time>"},
	"font":        {"*"},
	"shadow":      {"*"},
	"easing":      {"*"},
}

// ParseTokens parses and checks one tokens file. Every problem is a
// diagnostic at its position, all of them in one pass; a file with any
// error-severity diagnostic must not generate Go.
func ParseTokens(src string) (*TokensFile, []Diagnostic) {
	sheet, diags := Parse(src)
	f := &TokensFile{}
	bad := func(pos Pos, format string, args ...any) {
		diags = append(diags, Diagnostic{Rule: ruleTokensFile, Severity: SeverityError,
			Line: pos.Line, Col: pos.Col, Message: fmt.Sprintf(format, args...)})
	}
	index := map[string]int{}
	type darkDecl struct {
		name, value string
		pos         Pos
	}
	var darks []darkDecl
	for _, r := range sheet.Rules {
		switch {
		case r.At && r.Name == "property" && r.Block:
			tk, ok := parseProperty(r, bad)
			if !ok {
				continue
			}
			if _, dup := index[tk.Key]; dup {
				bad(tk.Pos, "--%s is declared twice", tk.Key)
				continue
			}
			index[tk.Key] = len(f.Tokens)
			f.Tokens = append(f.Tokens, tk)
		case r.At && r.Name == "media" && r.Block:
			if compactPrelude(r.Prel[1:]) != "(--dark)" {
				bad(r.Pos, "a tokens file's only media block is @media (--dark), for dark colour values; found %s", r.Text)
				continue
			}
			for _, d := range r.Decls {
				bad(d.Pos, "write dark values inside :root { } in the @media (--dark) block")
			}
			for _, n := range r.Nested {
				if n.At || n.Text != ":root" {
					bad(n.Pos, "write dark values inside :root { } in the @media (--dark) block; found %s", n.Text)
					continue
				}
				for _, d := range n.Decls {
					darks = append(darks, darkDecl{strings.TrimPrefix(d.Name, "--"), strings.TrimSpace(d.ValueSrc), d.Pos})
				}
			}
		default:
			bad(r.Pos, "a tokens file holds only @property rules and one @media (--dark) block; found %s", r.Text)
		}
	}
	for _, d := range darks {
		i, ok := index[d.name]
		switch {
		case !ok:
			bad(d.pos, "--%s has a dark value but is not declared by an @property rule in this file", d.name)
		case style.TokenCategory(d.name) != "color":
			bad(d.pos, "--%s: only colours take a dark value", d.name)
		case f.Tokens[i].Dark != "":
			bad(d.pos, "--%s has two dark values", d.name)
		default:
			if _, err := style.ParseToken(d.name, d.value); err != nil {
				bad(d.pos, "dark value: %v", err)
				continue
			}
			f.Tokens[i].Dark = d.value
		}
	}
	return f, diags
}

// parseProperty reads one @property rule into an AppToken, reporting
// each problem through bad.
func parseProperty(r *Rule, bad func(Pos, string, ...any)) (AppToken, bool) {
	var nameTok *Token
	for i := 1; i < len(r.Prel); i++ {
		t := r.Prel[i]
		if !significant(t) {
			continue
		}
		if nameTok != nil || t.Type != TokenIdent || !strings.HasPrefix(t.Text, "--") {
			bad(r.Pos, "@property takes one custom property name: %s", r.Text)
			return AppToken{}, false
		}
		nameTok = &r.Prel[i]
	}
	if nameTok == nil {
		bad(r.Pos, "@property takes one custom property name: %s", r.Text)
		return AppToken{}, false
	}
	key := strings.TrimPrefix(nameTok.Text, "--")
	pos := tokPos(*nameTok)
	ok := true
	var syntax, inherits, initial string
	var haveSyntax, haveInherits, haveInitial bool
	for _, d := range r.Decls {
		val := strings.TrimSpace(d.ValueSrc)
		switch d.Name {
		case "syntax":
			syntax, haveSyntax = val, true
		case "inherits":
			inherits, haveInherits = val, true
		case "initial-value":
			initial, haveInitial = val, true
		default:
			bad(d.Pos, "--%s: %q is not an @property descriptor (syntax, inherits, initial-value)", key, d.Name)
			ok = false
		}
	}
	for _, rest := range r.Nested {
		bad(rest.Pos, "--%s: an @property rule holds only its descriptors", key)
		ok = false
	}
	cat := style.TokenCategory(key)
	allowed, typed := tokenSyntax[cat]
	if !typed {
		// A kind style parses but this table lacks would otherwise
		// report the message "<nil>".
		if _, err := style.ParseToken(key, initial); err != nil {
			bad(pos, "%v", err)
		} else {
			bad(pos, "--%s: a %s token has no @property syntax registered here", key, cat)
		}
		return AppToken{}, false
	}
	typeName := tokenTypeName(cat)
	if !haveSyntax {
		bad(pos, "--%s: missing syntax; a %s token registers syntax: %q", key, typeName, allowed[0])
		ok = false
	} else if s, err := strconv.Unquote(syntax); err != nil || !slices.Contains(allowed, s) {
		bad(pos, "--%s is a %s token: its syntax is %q, not %s", key, typeName, allowed[0], syntax)
		ok = false
	}
	if !haveInherits || inherits != "true" {
		bad(pos, "--%s: a theme token is set on :root and reaches the page by inheritance; write inherits: true", key)
		ok = false
	}
	if !haveInitial {
		bad(pos, "--%s: missing initial-value, the token's value", key)
		return AppToken{}, false
	}
	value, err := style.ParseToken(key, initial)
	if err != nil {
		bad(pos, "%v", err)
		return AppToken{}, false
	}
	if !ok {
		return AppToken{}, false
	}
	return AppToken{Key: key, Value: value, Light: initial, Doc: r.Doc, Pos: pos}, true
}

// compactPrelude is a prelude's significant tokens joined with no
// whitespace: "( --dark )" and "(--dark)" read the same.
func compactPrelude(toks []Token) string {
	var b strings.Builder
	for _, t := range toks {
		if significant(t) {
			b.WriteString(t.Text)
		}
	}
	return b.String()
}

// tokenGroup is where a token type sits in the generated set: the same
// group names style.Theme uses, so an app token reads like a built-in
// (acme.Tokens.Colors.Highlight beside theme.Colors.Primary).
type tokenGroup struct {
	cat, field, goType string
}

var tokenGroups = []tokenGroup{
	{"color", "Colors", "Color"},
	{"font", "Fonts", "Font"},
	{"spacing", "Spacing", "Spacing"},
	{"radii", "Radii", "Radius"},
	{"stroke", "Strokes", "Stroke"},
	{"leading", "Leading", "LineHeight"},
	{"tracking", "Tracking", "LetterSpacing"},
	{"opacity", "Opacities", "Opacity"},
	{"shadow", "Shadows", "Shadow"},
	{"z", "ZIndex", "ZIndexValue"},
	{"duration", "Durations", "Duration"},
	{"easing", "Easings", "Easing"},
	{"text", "Typography", "FontSize"},
	{"font-weight", "FontWeights", "FontWeight"},
	{"size", "Sizes", "Size"},
}

func tokenTypeName(cat string) string {
	for _, g := range tokenGroups {
		if g.cat == cat {
			return g.goType
		}
	}
	return cat
}

// GenerateTokensFile renders <name>_tokens.gen.go: the token set as a
// typed value (Tokens, or <Name>Tokens when the package holds several
// token files), grouped like style.Theme, and a DarkTokens method when
// the file has dark values. css is the exact bytes of the tokens file
// (the Source hash line is computed from it). Identifiers are derived
// and validated; a collision is an error, never a mangled name.
func GenerateTokensFile(name, css string, f *TokensFile, pkg string, sharedPkg bool) (string, error) {
	file := TokensFileName(name)
	if !token.IsIdentifier(pkg) {
		return "", fmt.Errorf("gofastr gen styles: %s: package %q is not a Go identifier", file, pkg)
	}
	stem := camelIdent(name)
	setType := stem + "Tokens"
	varName := "Tokens"
	if sharedPkg {
		varName = camelUpper(name) + "Tokens"
	}
	if !token.IsIdentifier(setType) {
		return "", fmt.Errorf("gofastr gen styles: %s: %q becomes the invalid Go identifier %q; rename the file", file, name, setType)
	}
	if !token.IsIdentifier(varName) {
		return "", fmt.Errorf("gofastr gen styles: %s: %q becomes the invalid Go identifier %q; rename the file", file, name, varName)
	}

	type field struct {
		ident string
		tk    AppToken
	}
	groups := map[string][]field{}
	for _, tk := range f.Tokens {
		cat := style.TokenCategory(tk.Key)
		ident := camelUpper(strings.TrimPrefix(tk.Key, cat+"-"))
		if !token.IsIdentifier(ident) {
			return "", fmt.Errorf("gofastr gen styles: %s: --%s becomes the invalid Go identifier %q; rename it", file, tk.Key, ident)
		}
		for _, prev := range groups[cat] {
			if prev.ident == ident {
				return "", fmt.Errorf("gofastr gen styles: %s: --%s and --%s both become the field %s; rename one", file, prev.tk.Key, tk.Key, ident)
			}
		}
		groups[cat] = append(groups[cat], field{ident, tk})
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s%s. DO NOT EDIT.\n", GeneratedByHeader, file)
	fmt.Fprintf(&b, "%s%s\n\n", SourceHashHeader, SourceHash(css))
	fmt.Fprintf(&b, "package %s\n\n", pkg)
	if len(groups["duration"]) > 0 {
		b.WriteString("import (\n\t\"time\"\n\n\t\"github.com/DonaldMurillo/gofastr/core-ui/style\"\n)\n\n")
	} else {
		b.WriteString("import \"github.com/DonaldMurillo/gofastr/core-ui/style\"\n\n")
	}

	fmt.Fprintf(&b, "// %s is %s as typed theme tokens. Add them to the theme with\n", varName, file)
	fmt.Fprintf(&b, "// Extend, which also takes the file's dark values:\n//\n//\tsite.WithTheme(theme.Default().Extend(%s.%s))\n", pkg, varName)
	fmt.Fprintf(&b, "var %s = %s{\n", varName, setType)
	for _, g := range tokenGroups {
		fields := groups[g.cat]
		if len(fields) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\t%s: %s%s{\n", g.field, stem, g.field)
		for _, fd := range fields {
			ident := fd.ident
			if !token.IsIdentifier(ident) { // checked when the field was named; kept beside the emit
				return "", fmt.Errorf("gofastr gen styles: %s: %q is not a Go identifier", file, ident)
			}
			fmt.Fprintf(&b, "\t\t%s: %s,\n", ident, tokenLiteral(fd.tk.Value))
		}
		b.WriteString("\t},\n")
	}
	b.WriteString("}\n\n")

	fmt.Fprintf(&b, "// %s is the token set of %s, grouped like style.Theme.\n", setType, file)
	fmt.Fprintf(&b, "type %s struct {\n", setType)
	for _, g := range tokenGroups {
		if len(groups[g.cat]) > 0 {
			fmt.Fprintf(&b, "\t%s %s%s\n", g.field, stem, g.field)
		}
	}
	b.WriteString("}\n")
	for _, g := range tokenGroups {
		fields := groups[g.cat]
		if len(fields) == 0 {
			continue
		}
		fmt.Fprintf(&b, "\n// %s%s are the style.%s tokens of %s.\n", stem, g.field, g.goType, file)
		fmt.Fprintf(&b, "type %s%s struct {\n", stem, g.field)
		for i, fd := range fields {
			if i > 0 {
				b.WriteString("\n")
			}
			writeFieldDoc(&b, fd.ident, fd.tk)
			fmt.Fprintf(&b, "\t%s style.%s\n", fd.ident, g.goType)
		}
		b.WriteString("}\n")
	}

	var darkKeys []AppToken
	for _, tk := range f.Tokens {
		if tk.Dark != "" {
			darkKeys = append(darkKeys, tk)
		}
	}
	if len(darkKeys) > 0 {
		fmt.Fprintf(&b, "\n// DarkTokens returns the @media (--dark) values of %s, which\n", file)
		b.WriteString("// Theme.Extend merges into DarkColors when the theme has a dark palette.\n")
		fmt.Fprintf(&b, "func (%s) DarkTokens() map[string]string {\n\treturn map[string]string{\n", setType)
		for _, tk := range darkKeys {
			fmt.Fprintf(&b, "\t\t%q: %q,\n", strings.TrimPrefix(tk.Key, "color-"), tk.Dark)
		}
		b.WriteString("\t}\n}\n")
	}
	formatted, ferr := format.Source([]byte(b.String()))
	if ferr != nil {
		return "", fmt.Errorf("ownstyle: generate %s: emitted Go does not format: %w", file, ferr)
	}
	return string(formatted), nil
}

// writeFieldDoc writes a field's doc comment: the custom property it
// emits, then the tokens file's comment for it, one Go comment line per
// source line.
func writeFieldDoc(b *strings.Builder, ident string, tk AppToken) {
	first := fmt.Sprintf("%s is --%s.", ident, tk.Key)
	lines := strings.Split(tk.Doc, "\n")
	if tk.Doc == "" {
		lines = nil
	}
	if len(lines) > 0 {
		first += " " + lines[0]
		lines = lines[1:]
	}
	fmt.Fprintf(b, "\t// %s\n", commentSafe(first))
	for _, l := range lines {
		fmt.Fprintf(b, "\t// %s\n", commentSafe(strings.TrimSpace(l)))
	}
}

// commentSafe drops control bytes, so a comment in the CSS cannot end
// the Go comment line it is copied into.
func commentSafe(s string) string {
	return strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

// tokenLiteral is the Go composite literal for a typed token value.
func tokenLiteral(v any) string {
	switch t := v.(type) {
	case style.Color:
		return fmt.Sprintf("style.Color{Name: %q, Value: %q}", t.Name, t.Value)
	case style.Size:
		return fmt.Sprintf("style.Size{Name: %q, Value: %q}", t.Name, t.Value)
	case style.Font:
		return fmt.Sprintf("style.Font{Name: %q, Value: %q}", t.Name, t.Value)
	case style.Shadow:
		return fmt.Sprintf("style.Shadow{Name: %q, Value: %q}", t.Name, t.Value)
	case style.Easing:
		return fmt.Sprintf("style.Easing{Name: %q, Value: %q}", t.Name, t.Value)
	case style.FontSize:
		return fmt.Sprintf("style.FontSize{Name: %q, Value: %q}", t.Name, t.Value)
	case style.Spacing:
		return fmt.Sprintf("style.Spacing{Name: %q, Value: %d}", t.Name, t.Value)
	case style.Radius:
		return fmt.Sprintf("style.Radius{Name: %q, Value: %d}", t.Name, t.Value)
	case style.Stroke:
		return fmt.Sprintf("style.Stroke{Name: %q, Value: %q}", t.Name, t.Value)
	case style.LineHeight:
		return fmt.Sprintf("style.LineHeight{Name: %q, Value: %q}", t.Name, t.Value)
	case style.LetterSpacing:
		return fmt.Sprintf("style.LetterSpacing{Name: %q, Value: %q}", t.Name, t.Value)
	case style.Opacity:
		return fmt.Sprintf("style.Opacity{Name: %q, Value: %q}", t.Name, t.Value)
	case style.FontWeight:
		return fmt.Sprintf("style.FontWeight{Name: %q, Value: %d}", t.Name, t.Value)
	case style.ZIndexValue:
		return fmt.Sprintf("style.ZIndexValue{Name: %q, Value: %d}", t.Name, t.Value)
	case style.Duration:
		return fmt.Sprintf("style.Duration{Name: %q, Value: %s}", t.Name, durationLiteral(t.Value))
	}
	panic(fmt.Sprintf("ownstyle: no Go literal for %T", v))
}

// durationLiteral spells a duration the way a person would: 260 *
// time.Millisecond, 2 * time.Second, or nanoseconds when neither fits.
func durationLiteral(d time.Duration) string {
	switch {
	case d%time.Second == 0:
		return fmt.Sprintf("%d * time.Second", d/time.Second)
	case d%time.Millisecond == 0:
		return fmt.Sprintf("%d * time.Millisecond", d/time.Millisecond)
	}
	return fmt.Sprintf("time.Duration(%d)", int64(d))
}

// AppTokenAt is one app token and the tokens file declaring it, for
// the program-wide checks.
type AppTokenAt struct {
	File  string
	Token AppToken
}

// FileDiagnostic is a Diagnostic in a named file.
type FileDiagnostic struct {
	File string
	Diag Diagnostic
}

// DuplicateTokenValues runs GOFASTR1821 across a program: an app token
// whose value (case-folded) is already a token's of the same type,
// built-in first, then app tokens in the order given. The first holder
// owns the value; each later one is reported, naming it. Keyword values
// (none, auto) are skipped, the same carve-out GOFASTR1807 makes: a
// shared keyword is not a shared decision.
func DuplicateTokenValues(app []AppTokenAt, builtins map[string]string) []FileDiagnostic {
	type slot struct{ cat, value string }
	owners := map[slot]string{}
	for _, k := range slices.Sorted(maps.Keys(builtins)) {
		if strings.Contains(k, ".") || bareKeyword(builtins[k]) {
			continue
		}
		s := slot{style.TokenCategory(k), strings.ToLower(strings.TrimSpace(builtins[k]))}
		if _, taken := owners[s]; !taken {
			owners[s] = k
		}
	}
	var out []FileDiagnostic
	for _, a := range app {
		v := strings.TrimSpace(a.Token.Light)
		if bareKeyword(v) {
			continue
		}
		s := slot{style.TokenCategory(a.Token.Key), strings.ToLower(v)}
		if prev, taken := owners[s]; taken {
			out = append(out, FileDiagnostic{File: a.File, Diag: Diagnostic{
				Rule: RuleDuplicateTokenValue, Severity: SeverityError,
				Line: a.Token.Pos.Line, Col: a.Token.Pos.Col,
				Message: fmt.Sprintf("--%s has the value of --%s (%s); read var(--%s), or give --%s a value of its own",
					a.Token.Key, prev, v, prev, a.Token.Key),
			}})
			continue
		}
		owners[s] = a.Token.Key
	}
	return out
}
