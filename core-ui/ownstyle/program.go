package ownstyle

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

// RuleRepeatedLiteral is GOFASTR1822 (warn).
const RuleRepeatedLiteral = "GOFASTR1822"

var allowMarker = regexp.MustCompile(`^/\*\s*gofastr:allow\(([^)]*)\)\s*(.*?)\s*\*/$`)

// Suppressed reports whether a gofastr:allow(<rule>) <reason> comment in src
// waives d, the way `gofastr verify` reads the marker.
func Suppressed(src string, d Diagnostic) bool {
	toks := Tokenize(src)
	for i, t := range toks {
		if t.Type != TokenComment {
			continue
		}
		m := allowMarker.FindStringSubmatch(t.Text)
		if m == nil || m[2] == "" {
			continue
		}
		named := false
		for _, r := range strings.Split(m[1], ",") {
			if strings.EqualFold(strings.TrimSpace(r), d.Rule) {
				named = true
			}
		}
		if named && markerScope(toks, i) == d.Line {
			return true
		}
	}
	return false
}

func markerScope(toks []Token, i int) int {
	c := toks[i]
	end := c.Line + strings.Count(c.Text, "\n")
	for j := i - 1; j >= 0 && toks[j].Line == c.Line; j-- {
		if significant(toks[j]) {
			return c.Line
		}
	}
	for j := i + 1; j < len(toks); j++ {
		if !significant(toks[j]) {
			continue
		}
		if toks[j].Line == end {
			return end
		}
		return toks[j].Line
	}
	return end
}

// SheetSource is one owned sheet's path and bytes, for the program-wide
// checks.
type SheetSource struct {
	File string
	Src  string
}

// RepeatedLiteral is one GOFASTR1822 occurrence with the other sheets that
// write the same value broken out, so a caller checking sheet groups
// separately — `gofastr verify` runs the rule once per program — can merge two
// findings for a sheet two programs reach into one finding naming every other
// sheet.
type RepeatedLiteral struct {
	File   string
	Pos    Pos
	Value  string
	Cat    string
	Others []string
}

// FileDiagnostic renders the finding; merged Others produce the merged
// message.
func (r RepeatedLiteral) FileDiagnostic() FileDiagnostic {
	return FileDiagnostic{File: r.File, Diag: Diagnostic{
		Rule: RuleRepeatedLiteral, Severity: SeverityWarning,
		Line: r.Pos.Line, Col: r.Pos.Col,
		Message: fmt.Sprintf("%s is also written in %s; declare it once as a %s token in a <name>.tokens.css and read var() in each",
			r.Value, strings.Join(r.Others, ", "), r.Cat),
	}}
}

// RepeatedLiteralsIn runs GOFASTR1822 over the given sheets and returns each
// occurrence with the other sheets that write the value (see
// [RepeatedLiteral]).
func RepeatedLiteralsIn(sheets []SheetSource, tokens map[string]string) []RepeatedLiteral {
	valueIndex := buildValueIndex(tokens)
	type key struct{ cat, value string }
	type site struct {
		file  string
		value string
		pos   Pos
	}
	sites := map[key][]site{}
	for _, s := range sheets {
		sheet, _ := Parse(s.Src)
		var visit func(rules []*Rule)
		visit = func(rules []*Rule) {
			for _, r := range rules {
				for _, d := range r.Decls {
					prop := propName(d)
					cats, ok := propTokenCategories[prop]
					if !ok {
						continue
					}
					v := strings.TrimSpace(d.ValueSrc)
					lv := strings.ToLower(v)
					if v == "" || bareKeyword(v) || strings.Contains(lv, "var(") || strings.Contains(v, "{") || structuralLiteral(prop, lv) {
						continue
					}
					if tokenValueFor(valueIndex[lv], cats) {
						continue
					}
					k := key{cats[0], lv}
					sites[k] = append(sites[k], site{s.File, v, d.ValuePos})
				}
				visit(r.Nested)
			}
		}
		visit(sheet.Rules)
	}
	var out []RepeatedLiteral
	for _, k := range slices.SortedFunc(maps.Keys(sites), func(a, b key) int {
		if a.cat != b.cat {
			return strings.Compare(a.cat, b.cat)
		}
		return strings.Compare(a.value, b.value)
	}) {
		ss := sites[k]
		files := map[string]bool{}
		for _, s := range ss {
			files[s.file] = true
		}
		if len(files) < 2 {
			continue
		}
		for _, s := range ss {
			var others []string
			for _, f := range slices.Sorted(maps.Keys(files)) {
				if f != s.file {
					others = append(others, f)
				}
			}
			out = append(out, RepeatedLiteral{
				File: s.file, Pos: s.pos, Value: s.value, Cat: k.cat, Others: others,
			})
		}
	}
	return out
}

var zeroLength = regexp.MustCompile(`^[+-]?0*\.?0+([a-z]+|%)?$`)

func structuralLiteral(prop, lv string) bool {
	if lv == "100%" {
		return true
	}
	if prop == "z-index" && lv == "-1" {
		return true
	}
	parts := strings.Fields(lv)
	for _, p := range parts {
		if !zeroLength.MatchString(p) {
			return false
		}
	}
	return len(parts) > 0
}

func tokenValueFor(holders []string, cats []string) bool {
	for _, h := range holders {
		if slices.Contains(cats, tokenCategory(h)) {
			return true
		}
	}
	return false
}

// CheckTokenFiles parses every tokens file of a program and runs the checks
// that need all of them.
func CheckTokenFiles(files []SheetSource, builtins map[string]string) (parsed map[string]*TokensFile, tokens []AppTokenAt, diags []FileDiagnostic) {
	parsed = map[string]*TokensFile{}
	owner := map[string]string{}
	for _, f := range files {
		tf, ds := ParseTokens(f.Src)
		for _, d := range ds {
			diags = append(diags, FileDiagnostic{File: f.File, Diag: d})
		}
		parsed[f.File] = tf
		for _, tk := range tf.Tokens {
			at := func(format string, args ...any) {
				diags = append(diags, FileDiagnostic{File: f.File, Diag: Diagnostic{
					Rule: ruleTokensFile, Severity: SeverityError,
					Line: tk.Pos.Line, Col: tk.Pos.Col, Message: fmt.Sprintf(format, args...),
				}})
			}
			if _, builtin := builtins[tk.Key]; builtin {
				at("--%s is a built-in theme token; set its value on the theme, or give the app token a name of its own", tk.Key)
				continue
			}
			if prev, dup := owner[tk.Key]; dup {
				at("--%s is already declared in %s; a token has one home", tk.Key, prev)
				continue
			}
			owner[tk.Key] = f.File
			tokens = append(tokens, AppTokenAt{File: f.File, Token: tk})
		}
	}
	diags = append(diags, DuplicateTokenValues(tokens, builtins)...)
	return parsed, tokens, diags
}

// CheckTokens is the token map owned sheets are checked against.
func CheckTokens(builtins map[string]string, app []AppTokenAt) map[string]string {
	out := make(map[string]string, len(builtins)+len(app))
	maps.Copy(out, builtins)
	for _, a := range app {
		out[a.Token.Key] = a.Token.Light
		if a.Token.Dark != "" {
			out["dark."+a.Token.Key] = a.Token.Dark
		}
	}
	return out
}
