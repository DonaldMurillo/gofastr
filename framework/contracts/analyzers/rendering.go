package analyzers

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/codegen"
	"github.com/DonaldMurillo/gofastr/core/fuzzy"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
	"github.com/DonaldMurillo/gofastr/internal/bom"

	"github.com/DonaldMurillo/gofastr/core-ui/check"
	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/core-ui/style"
)

func init() {
	contracts.Register(&contracts.Analyzer{
		Name: "rendering",
		Doc:  "The one-styling-surface, no-hard-navigation, one-SSE-bus contract.",
		Rules: []string{
			contracts.RuleBespokeCSS,
			contracts.RuleHardNavigation,
			contracts.RuleBespokeEventSource,
			contracts.RuleInlineStyle,
			contracts.RuleInlineScript,
			contracts.RuleUnknownThemeToken,
			contracts.RuleHardcodedTokenValue,
			contracts.RuleFallbackDrift,
			contracts.RuleOwnerlessStylesheet,
			contracts.RuleKitClassSelector,
			contracts.RuleImportant,
			contracts.RuleRawMediaWidth,
			contracts.RuleAnimationNoReduced,
			contracts.RuleStaleStyleSource,
			contracts.RuleUpstreamCandidate,
			contracts.RuleDuplicateStyleName,
			contracts.RuleKitRootStyle,
			contracts.RuleOwnedHandleLeak,
			contracts.RuleAppSheetSelector,
			contracts.RuleTokenCustomProperty,
			contracts.RuleOwnedStatementAtRule,
			contracts.RuleDuplicateTokenValue,
			contracts.RuleRepeatedLiteral,
			contracts.RuleBareThemeLiteral, contracts.RulePhysicalSide,
			contracts.RuleFlexReverse,
		},
		Run: runRendering,
	})
}

var (
	cssHyphenProps = strings.Join([]string{
		"background-color", "background-image", "background-size", "background-position",
		"border-radius", "border-color", "border-width", "border-top", "border-bottom",
		"margin-top", "margin-bottom", "margin-left", "margin-right", "margin-inline",
		"padding-top", "padding-bottom", "padding-left", "padding-right", "padding-inline",
		"font-size", "font-family", "font-weight", "font-style", "font-display",
		"line-height", "letter-spacing", "text-align", "text-decoration", "text-transform",
		"flex-direction", "flex-wrap", "flex-grow", "flex-shrink", "align-items",
		"justify-content", "align-self", "grid-template-columns", "grid-template-rows",
		"grid-column", "grid-row", "min-width", "max-width", "min-height", "max-height",
		"box-shadow", "box-sizing", "white-space", "pointer-events", "z-index",
		"overflow-x", "overflow-y", "list-style", "object-fit", "backdrop-filter",
	}, "|")
	cssPlainProps = strings.Join([]string{
		"color", "background", "margin", "padding", "border", "display", "position",
		"top", "right", "bottom", "left", "width", "height", "font", "flex", "gap",
		"opacity", "overflow", "cursor", "transition", "transform", "content", "src",
		"fill", "stroke", "outline", "inset", "appearance", "visibility",
	}, "|")
	cssValueShape = `(?:-?[0-9.]+(?:px|rem|em|%|vh|vw|vmin|vmax|ch|fr|deg|ms|s)\b|#[0-9a-fA-F]{3,8}\b|var\(--)`
)

const notPropChar = `(?:^|[^-\w])`

var (
	reCSSHyphenRule = regexp.MustCompile(`(?i)` + notPropChar + `(?:` + cssHyphenProps + `)\s*:\s*[^;{}]+[;}]`)
	// RE2 has no lookahead, so looksLikeCSS rejects a match holding `:=`.
	reCSSValueRule = regexp.MustCompile(notPropChar + `(?:` + cssPlainProps + `)\s*:\s*[^;{}]*` + cssValueShape)
	reCSSAtRule    = regexp.MustCompile(`(?i)@(?:font-face|media|keyframes|supports|layer)\b`)
	reStyleTag     = regexp.MustCompile(`(?i)<style[\s>]`)
	reHardNav      = regexp.MustCompile(navLead + navQualChain + `*location\.(?:href|search|pathname)\s*[+]?=(?:[^=]|$)` +
		`|` + navLead + navQualChain + `+location\s*[+]?=(?:[^=]|$)` +
		`|\blocation\s*=\s*["'\x60]|\blocation\.(?:reload|assign|replace)\s*\(`)
	reHardNavBare = regexp.MustCompile(navLead + `location\s*[+]?=(?:[^=]|$)`)
	reEventSource = regexp.MustCompile(`\bnew\s+EventSource\s*\(`)
)

// looksLikeCSS returns the text that matched, or "" when nothing did.
func looksLikeCSS(line string) string {
	if m := reCSSAtRule.FindString(line); m != "" {
		return m
	}
	if m := reCSSHyphenRule.FindString(line); m != "" {
		return m
	}
	if m := reCSSValueRule.FindString(line); m != "" && !strings.Contains(m, ":=") {
		return m
	}
	return ""
}

func clipTrigger(m string) string {
	m = strings.TrimSpace(m)
	r := []rune(m)
	if len(r) > 48 {
		return string(r[:45]) + "…"
	}
	return m
}

var designSystemPrefixes = []string{
	"core-ui/", "framework/ui/", "framework/uihost/", "framework/sdkdocs/",
	"framework/pluginhost/", "framework/dev/", "battery/",
}

var devOnlyPrefixes = []string{"framework/dev/", "kiln/"}

var navOwnerScriptPrefixes = []string{"core-ui/runtime/", "framework/headless/"}

const navLead = `(?:^|[^.\w$])`

const navQualChain = `(?:(?:window|document|self|top|parent|frames)\.)`

var (
	reDeclCSS  = regexp.MustCompile(`(?:^|[\s{;"` + "`" + `])(-{0,2}[A-Za-z][A-Za-z-]*)\s*:\s*([^;{}"` + "`" + `]+)`)
	reDeclPair = regexp.MustCompile(`"([A-Za-z][A-Za-z-]*)",\s*"([^"]+)"`)
)

type cssDeclaration struct {
	property, value string
	offset          int
}

func appendCSSDeclarations(out []cssDeclaration, source, delimiter string, pattern *regexp.Regexp) []cssDeclaration {
	if strings.Contains(source, delimiter) {
		for _, m := range pattern.FindAllStringSubmatchIndex(source, -1) {
			out = append(out, cssDeclaration{strings.ToLower(source[m[2]:m[3]]), source[m[4]:m[5]], m[2]})
		}
	}
	return out
}

func scanCSSDeclarations(source string) []cssDeclaration {
	decls := appendCSSDeclarations(nil, source, ":", reDeclCSS)
	return appendCSSDeclarations(decls, source, `",`, reDeclPair)
}

func runRendering(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	var out []contracts.Diagnostic
	for _, f := range p.AppFiles() {
		body, ok := p.Source(f.Rel)
		if !ok {
			continue
		}
		lines := strings.Split(string(body), "\n")
		scan := strings.Split(stripComments(string(body)), "\n")
		ownsStyling := hasPrefixAny(f.Rel, designSystemPrefixes)
		devSurface := hasPrefixAny(f.Rel, devOnlyPrefixes)
		ownTheme := hasPrefixAny(f.Rel, ownThemePrefixes)
		var dark darkScheme
		if ownsStyling && !devSurface {
			out = append(out, checkGoPhysicalSides(p, f.Rel, body, lines)...)
		}

		for i, line := range scan {
			lineNo := i + 1

			if ownsStyling && (strings.Contains(line, ":") || strings.Contains(line, `",`)) {
				out = append(out, checkHardcodedTokenValues(f.Rel, lineNo, line, lines[i])...)
			}
			if ownsStyling {
				inDark := dark.line(line)
				if strings.Contains(line, "var(") {
					out = append(out, checkFallbackDrift(f.Rel, lineNo, line, lines[i], ownTheme, inDark)...)
				}
			}
			if ownsStyling {
				decls := scanCSSDeclarations(line)
				out = append(out, checkBareThemeLiterals(f.Rel, lineNo, decls, lines[i], devSurface)...)
				if !devSurface {
					out = append(out, checkFlexReverse(f.Rel, lineNo, decls, lines[i])...)
				}
			}

			if !strings.ContainsAny(line, ":@<") &&
				!strings.Contains(line, "location") &&
				!strings.Contains(line, "EventSource") {
				continue
			}

			if !ownsStyling {
				if trigger := looksLikeCSS(line); trigger != "" || reStyleTag.MatchString(line) {
					what := "a CSS rule"
					detail := fmt.Sprintf(": matched `%s`", clipTrigger(trigger))
					if reStyleTag.MatchString(line) {
						what = "a <style> block"
						detail = ""
					}
					out = append(out, contracts.Diagnostic{
						RuleID: contracts.RuleBespokeCSS, File: f.Rel, Line: lineNo,
						Message: fmt.Sprintf("%s is defined outside the design system%s", what, detail),
						Snippet: strings.TrimSpace(lines[i]),
					})
				}
			}
			if !devSurface && reHardNav.MatchString(line) {
				out = append(out, contracts.Diagnostic{
					RuleID: contracts.RuleHardNavigation, File: f.Rel, Line: lineNo,
					Message: "full page load used to navigate: scroll position, focus, and every open island are discarded",
					Snippet: strings.TrimSpace(lines[i]),
				})
			}

			if !devSurface && reEventSource.MatchString(line) {
				out = append(out, contracts.Diagnostic{
					RuleID: contracts.RuleBespokeEventSource, File: f.Rel, Line: lineNo,
					Message: "bespoke EventSource: server pushes belong on the shared /__gofastr/sse bus",
					Snippet: strings.TrimSpace(lines[i]),
				})
			}
		}
	}
	for _, f := range p.ScriptFiles() {
		if hasPrefixAny(f.Rel, devOnlyPrefixes) || hasPrefixAny(f.Rel, navOwnerScriptPrefixes) {
			continue
		}
		body, ok := p.Source(f.Rel)
		if !ok {
			continue
		}
		lines := strings.Split(string(body), "\n")
		for i, line := range strings.Split(stripScriptComments(string(body)), "\n") {
			if reHardNav.MatchString(line) || reHardNavBare.MatchString(line) {
				out = append(out, contracts.Diagnostic{
					RuleID: contracts.RuleHardNavigation, File: f.Rel, Line: i + 1,
					Message: "full page load used to navigate: scroll position, focus, and every open island are discarded",
					Snippet: strings.TrimSpace(lines[i]),
				})
			}
			if reEventSource.MatchString(line) {
				out = append(out, contracts.Diagnostic{
					RuleID: contracts.RuleBespokeEventSource, File: f.Rel, Line: i + 1,
					Message: "bespoke EventSource: server pushes belong on the shared /__gofastr/sse bus",
					Snippet: strings.TrimSpace(lines[i]),
				})
			}
		}
	}
	for _, f := range p.StyleFiles() {
		if slices.Contains(strings.Split(f.Rel, "/"), "testdata") {
			continue
		}
		ownsStyling := hasPrefixAny(f.Rel, designSystemPrefixes)
		if !ownsStyling && (strings.HasSuffix(f.Rel, ".style.css") || strings.HasSuffix(f.Rel, ".tokens.css")) {
			continue
		}
		body, ok := p.Source(f.Rel)
		if !ok {
			continue
		}
		clean := blankCSSCommentsAndStrings(string(body))
		if ownsStyling {
			if !hasPrefixAny(f.Rel, devOnlyPrefixes) {
				out = append(out, scanPhysicalSides(f.Rel, strings.Split(clean, "\n"), strings.Split(string(body), "\n"), nil)...)
			}
			continue
		}
		if stylesheetOnlyCustomProperties(clean) {
			continue
		}
		for i, line := range strings.Split(clean, "\n") {
			if strings.TrimSpace(line) == "" {
				continue
			}
			out = append(out, contracts.Diagnostic{
				RuleID: contracts.RuleOwnerlessStylesheet, File: f.Rel, Line: i + 1,
				Message: fmt.Sprintf("%s has no owner; rename it to <name>.style.css beside the layout, screen or component it styles (app.style.css for page-wide classes) and run gofastr gen styles",
					path.Base(f.Rel)),
			})
			break
		}
	}
	out = append(out, checkStyleGenFreshness(p)...)
	out = append(out, checkOwnedStyles(p)...)
	out = append(out, checkInlineScripts(p)...)
	out = append(out, checkInlineStyles(p)...)
	out = append(out, checkStyleTokens(p)...)
	return out, nil
}

func checkStyleGenFreshness(p *contracts.Pass) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, f := range p.StyleFiles() {
		base := path.Base(f.Rel)
		if slices.Contains(strings.Split(f.Rel, "/"), "testdata") {
			continue
		}
		var genName string
		switch {
		case strings.HasSuffix(base, ".style.css"):
			genName = ownstyle.GeneratedFileName(strings.TrimSuffix(base, ".style.css"))
		case strings.HasSuffix(base, ".tokens.css"):
			genName = ownstyle.GeneratedTokensFileName(strings.TrimSuffix(base, ".tokens.css"))
		default:
			continue
		}
		css, ok := p.Source(f.Rel)
		if !ok {
			continue
		}
		genRel := path.Join(path.Dir(f.Rel), genName)
		gen, hasGen := p.Source(genRel)
		if !hasGen {
			out = append(out, contracts.Diagnostic{
				RuleID: contracts.RuleStaleStyleSource,
				File:   f.Rel,
				Line:   1,
				Message: fmt.Sprintf("%s has no generated sibling %s; run gofastr gen styles",
					base, genName),
			})
			continue
		}
		cssBody := bom.Trim(codegen.StripMarker(f.Rel, string(css)))
		if got, hasHash := generatedSourceHash(gen); !hasHash || got != ownstyle.SourceHash(cssBody) {
			out = append(out, contracts.Diagnostic{
				RuleID: contracts.RuleStaleStyleSource,
				File:   f.Rel,
				Line:   1,
				Message: fmt.Sprintf("%s changed since its Go was generated; run gofastr gen styles",
					base),
			})
		}
	}
	return out
}

func generatedSourceHash(gen []byte) (hash string, ok bool) {
	for _, line := range strings.Split(string(gen), "\n") {
		if rest, isHash := strings.CutPrefix(strings.TrimSpace(line), ownstyle.SourceHashHeader); isHash {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

func stylesheetOnlyCustomProperties(clean string) bool {
	start, parens, brackets, valueBraces := 0, 0, 0, 0
	custom := false
	for i := range len(clean) {
		c := clean[i]
		switch c {
		case '(':
			parens++
		case ')':
			parens--
		case '[':
			brackets++
		case ']':
			brackets--
		}
		if parens != 0 || brackets != 0 {
			continue
		}
		switch c {
		case ':':
			if strings.HasPrefix(strings.TrimSpace(clean[start:i]), "--") {
				custom = true
			}
		case '{':
			if custom {
				valueBraces++
			} else {
				start = i + 1
			}
		case ';', '}':
			if valueBraces > 0 {
				if c == '}' {
					valueBraces--
				}
				continue
			}
			if strings.TrimSpace(clean[start:i]) != "" && !custom {
				return false
			}
			start, custom = i+1, false
		}
	}
	return strings.TrimSpace(clean[start:]) == ""
}

func checkStyleTokens(p *contracts.Pass) []contracts.Diagnostic {
	files := p.StyleFiles()
	if len(files) == 0 {
		return nil
	}
	declared := map[string]bool{}
	cleaned := make(map[string]string, len(files))
	for _, f := range files {
		body, ok := p.Source(f.Rel)
		if !ok {
			continue
		}
		clean := blankCSSCommentsAndStrings(string(body))
		cleaned[f.Rel] = clean
		collectDeclared(clean, declared)
	}
	known := make(map[string]bool)
	names := style.TokenNames()
	for _, n := range names {
		known[n] = true
	}

	var out []contracts.Diagnostic
	for _, f := range files {
		if isOwnedStylePath(f.Rel) {
			continue
		}
		clean := cleaned[f.Rel]
		for _, loc := range reVarRef.FindAllStringSubmatchIndex(clean, -1) {
			name := clean[loc[2]:loc[3]]
			if varRefHasFallback(clean, loc[3]) ||
				declared[name] ||
				runtimeOwnedPrefix(name) ||
				known[name] {
				continue
			}
			msg := fmt.Sprintf("theme emits no `--%s`", name)
			if fix, ok := closestToken(name, names); ok {
				msg += fmt.Sprintf("; did you mean `--%s`?", fix)
			}
			out = append(out, contracts.Diagnostic{
				RuleID:  contracts.RuleUnknownThemeToken,
				File:    f.Rel,
				Line:    1 + strings.Count(clean[:loc[0]], "\n"),
				Message: msg,
				Snippet: p.Line(f.Rel, 1+strings.Count(clean[:loc[0]], "\n")),
			})
		}
	}
	return out
}

func runtimeOwnedPrefix(name string) bool {
	//gofastr:allow(layerprefix) the token checker names the layer prefixes to exempt them
	return strings.HasPrefix(name, "ui-") || strings.HasPrefix(name, "fui-") ||
		strings.HasPrefix(name, "cui-") || strings.HasPrefix(name, "hui-")
}

var reCSSCustomProp = regexp.MustCompile(`--([A-Za-z0-9_-]+)\s*:`)

var reAtProperty = regexp.MustCompile(`(?i)@property\s+--([A-Za-z0-9_-]+)`)

func collectDeclared(clean string, into map[string]bool) {
	for _, m := range reAtProperty.FindAllStringSubmatch(clean, -1) {
		into[m[1]] = true
	}
	var brace, paren, pos int
	for _, m := range reCSSCustomProp.FindAllStringSubmatchIndex(clean, -1) {
		for ; pos < m[0]; pos++ {
			switch clean[pos] {
			case '{':
				brace++
			case '}':
				if brace > 0 {
					brace--
				}
			case '(':
				paren++
			case ')':
				if paren > 0 {
					paren--
				}
			}
		}
		if brace > 0 && paren == 0 {
			into[clean[m[2]:m[3]]] = true
		}
	}
}

var reVarRef = regexp.MustCompile(`(?i)var\(\s*--([A-Za-z0-9_-]+)`)

func varRefHasFallback(s string, i int) bool {
	depth := 1 // already inside the var( the name belongs to
	for ; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return false
			}
		case ',':
			if depth == 1 {
				return true
			}
		}
	}
	return false
}

func blankCSSCommentsAndStrings(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	const (
		stText = iota
		stComment
		stSingle
		stDouble
	)
	state := stText
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch state {
		case stText:
			switch {
			case c == '/' && i+1 < len(src) && src[i+1] == '*':
				b.WriteString("  ")
				i++
				state = stComment
			case c == '"':
				b.WriteByte(' ')
				state = stDouble
			case c == '\'':
				b.WriteByte(' ')
				state = stSingle
			default:
				b.WriteByte(c)
			}
		case stComment:
			if c == '*' && i+1 < len(src) && src[i+1] == '/' {
				b.WriteString("  ")
				i++
				state = stText
			} else if c == '\n' {
				b.WriteByte('\n')
			} else {
				b.WriteByte(' ')
			}
		default: // stSingle / stDouble
			switch {
			case c == '\\' && i+1 < len(src):
				b.WriteByte(' ')
				if src[i+1] == '\n' {
					b.WriteByte('\n')
				} else {
					b.WriteByte(' ')
				}
				i++
			case state == stDouble && c == '"', state == stSingle && c == '\'':
				b.WriteByte(' ')
				state = stText
			case c == '\n': // unescaped newline: bad-string recovery
				b.WriteByte('\n')
				state = stText
			default:
				b.WriteByte(' ')
			}
		}
	}
	return b.String()
}

func closestToken(name string, names []string) (string, bool) {
	best, bestDist := "", 3
	for _, cand := range names {
		if d := fuzzy.Levenshtein(name, cand); d < bestDist {
			best, bestDist = cand, d
		}
	}
	return best, best != ""
}

var propTokenCategories = ownstyle.PropTokenCategories

var tokenPropAlternation = func() string {
	props := make([]string, 0, len(propTokenCategories))
	for p := range propTokenCategories {
		props = append(props, p)
	}
	slices.Sort(props)
	return strings.Join(props, "|")
}()

var (
	reTokenValueCSS = regexp.MustCompile(
		notPropChar + `(` + tokenPropAlternation + `)\s*:\s*([^;{}"'` + "`" + `\n]*)`)
	reTokenValuePair = regexp.MustCompile(`"(` + tokenPropAlternation + `)"\s*,\s*"([^"]*)"`)
)

var tokenValueIndex = sync.OnceValue(func() map[string][]string {
	out := map[string][]string{}
	for k, v := range style.ThemeToTokens(style.DefaultTheme()) {
		if strings.HasPrefix(k, "dark.") || style.BareKeyword(v) {
			continue
		}
		lv := strings.ToLower(v)
		out[lv] = append(out[lv], k)
	}
	return out
})

func checkHardcodedTokenValues(rel string, lineNo int, scan, orig string) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	report := func(prop, raw string) {
		val := strings.TrimSpace(raw)
		if rest, ok := strings.CutSuffix(val, "!important"); ok {
			val = strings.TrimSpace(rest)
		}
		if val == "" || strings.Contains(val, "{") ||
			strings.Contains(strings.ToLower(val), "var(") {
			return
		}
		cats := propTokenCategories[prop]
		allowed := map[string]bool{}
		for _, c := range cats {
			allowed[c] = true
		}
		var toks []string
		for _, k := range tokenValueIndex()[strings.ToLower(val)] {
			if allowed[style.TokenCategory(k)] {
				toks = append(toks, k)
			}
		}
		if len(toks) == 0 {
			return
		}
		slices.Sort(toks)
		dashed := make([]string, len(toks))
		for i, k := range toks {
			dashed[i] = "--" + k
		}
		first := toks[0]
		ref := "{" + style.TokenCategory(first) + "." + strings.TrimPrefix(first, style.TokenCategory(first)+"-") + "}"
		out = append(out, contracts.Diagnostic{
			RuleID: contracts.RuleHardcodedTokenValue,
			File:   rel,
			Line:   lineNo,
			Message: fmt.Sprintf("%s: %s is the declared value of %s; write var(--%s) (or the %s builder reference) so a token change reaches it",
				prop, val, strings.Join(dashed, " / "), first, ref),
			Snippet: strings.TrimSpace(orig),
		})
	}
	if strings.Contains(scan, ":") {
		for _, m := range reTokenValueCSS.FindAllStringSubmatch(scan, -1) {
			report(m[1], m[2])
		}
	}
	if strings.Contains(scan, `",`) {
		for _, m := range reTokenValuePair.FindAllStringSubmatch(scan, -1) {
			report(m[1], m[2])
		}
	}
	return out
}

func hasPrefixAny(rel string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(rel, p) || strings.Contains(rel, "/"+p) { //gofastr:allow(GOFASTR1006) every element of designSystemPrefixes/devOnlyPrefixes ends in "/", so the boundary is in the data
			return true
		}
	}
	return false
}

func checkInlineScripts(p *contracts.Pass) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, f := range p.AppFiles() {
		if !strings.HasSuffix(f.Rel, ".go") {
			continue
		}
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		raw, ok := p.Source(f.Rel)
		if !ok {
			continue
		}
		res := check.ScanInlineScriptsIn(p.FileSet(), file, f.Abs, raw)
		if res == nil {
			continue // file opted out with //check-csp:ignore-file
		}
		for _, v := range res.Violations {
			out = append(out, contracts.Diagnostic{
				RuleID:  contracts.RuleInlineScript,
				File:    f.Rel,
				Line:    v.Line,
				Message: "script element with an inline body: a strict CSP refuses to execute it, so the page ships and the script silently never runs",
				Snippet: p.Line(f.Rel, v.Line),
			})
		}
	}
	return out
}

func checkInlineStyles(p *contracts.Pass) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, f := range p.AppFiles() {
		if !strings.HasSuffix(f.Rel, ".go") {
			continue
		}
		file, ok := p.AST(f.Rel)
		if !ok {
			continue
		}
		raw, ok := p.Source(f.Rel)
		if !ok {
			continue
		}
		res := check.ScanInlineStylesIn(p.FileSet(), file, f.Abs, raw)
		if res == nil {
			continue // file opted out with //check-csp:ignore-file
		}
		for _, v := range res.Violations {
			out = append(out, contracts.Diagnostic{
				RuleID:  contracts.RuleInlineStyle,
				File:    f.Rel,
				Line:    v.Line,
				Message: "inline style attribute: it outranks every stylesheet rule and is blocked under a strict CSP",
			})
		}
	}
	return out
}

var driftCategories = map[string]bool{"spacing": true, "radii": true, "stroke": true, "leading": true, "tracking": true, "opacity": true, "text": true, "duration": true}

var colourCategories = map[string]bool{"color": true, "tk": true}

var ownThemePrefixes = []string{"battery/desktop/ui/"}

var colourKeywords = map[string]bool{"currentcolor": true, "transparent": true, "inherit": true, "initial": true, "unset": true, "revert": true, "revert-layer": true}

type schemeTokens struct{ light, dark map[string]string }

var themeTokens = sync.OnceValue(func() schemeTokens {
	t := schemeTokens{light: map[string]string{}, dark: map[string]string{}}
	canon := style.DefaultTheme()
	canon.DarkColors = style.DefaultDarkColors()
	for k, v := range style.ThemeToTokens(canon) {
		if name, ok := strings.CutPrefix(k, "dark."); ok {
			t.dark[name] = v
		} else {
			t.light[name] = v
		}
	}
	return t
})

func declaredTokenValues() map[string]string { return themeTokens().light }

var reHex = regexp.MustCompile(`^#([0-9A-Fa-f]{3}|[0-9A-Fa-f]{6})$`)

func normHex(v string) string {
	if !reHex.MatchString(v) {
		return ""
	}
	v = strings.ToUpper(v)
	if len(v) == 4 {
		long := []byte{'#'}
		for _, c := range []byte(v[1:]) {
			long = append(long, c, c)
		}
		v = string(long)
	}
	return v
}

func colourFallback(name, fallback string, dark bool) (want string, fine bool) {
	t := themeTokens()
	want, declared := t.light[name]
	if !declared {
		return "", true
	}
	if d, ok := t.dark[name]; dark && ok {
		want = d
	}
	if colourKeywords[strings.ToLower(fallback)] {
		return want, true
	}
	got := normHex(fallback)
	return want, got != "" && got == normHex(want)
}

type darkScheme struct{ depth int }

var reDarkPrelude = regexp.MustCompile(`prefers-color-scheme:\s*dark|data-color-scheme="?dark|data-theme="?dark`)

func (d *darkScheme) line(s string) []bool {
	if d.depth == 0 && !strings.Contains(s, "dark") {
		return nil // the common line: nothing dark to open or close
	}
	dark := make([]bool, len(s))
	preludes := reDarkPrelude.FindAllStringIndex(s, -1)
	pending := false
	for i := 0; i < len(s); i++ {
		if d.depth == 0 && len(preludes) > 0 && i == preludes[0][0] {
			pending = true
		}
		for len(preludes) > 0 && i >= preludes[0][1]-1 {
			preludes = preludes[1:]
		}
		switch s[i] {
		case '{':
			if d.depth > 0 || pending {
				d.depth++
				pending = false
			}
		case '}':
			d.depth = max(d.depth-1, 0)
		}
		dark[i] = d.depth > 0
	}
	return dark
}

var reVarFallback = regexp.MustCompile(`var\(\s*--([A-Za-z0-9_-]+)\s*,\s*([^(),]+?)\s*\)`)

var reColourFallback = regexp.MustCompile(`var\(\s*--((?:color|tk)-[A-Za-z0-9_-]+)\s*,\s*([^()]*?(?:\([^()]*\))?)\s*\)`)

func checkFallbackDrift(rel string, lineNo int, scan, orig string, ownTheme bool, dark []bool) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	report := func(name, fallback, declared, scheme string) {
		out = append(out, contracts.Diagnostic{
			RuleID: contracts.RuleFallbackDrift,
			File:   rel,
			Line:   lineNo,
			Message: fmt.Sprintf("var(--%s, %s): the theme declares --%s as %s%s, so a themed page renders %s and the fallback teaches a value that does not exist; write var(--%s, %s)",
				name, fallback, name, declared, scheme, declared, name, declared),
			Snippet: strings.TrimSpace(orig),
		})
	}
	for _, m := range reVarFallback.FindAllStringSubmatch(scan, -1) {
		name, fallback := m[1], strings.TrimSpace(m[2])
		declared, ok := declaredTokenValues()[name]
		if !driftCategories[style.TokenCategory(name)] || !ok || ownstyle.SameScaleValue(fallback, declared) {
			continue
		}
		report(name, fallback, declared, "")
	}
	if ownTheme {
		return out
	}
	for _, ix := range reColourFallback.FindAllStringSubmatchIndex(scan, -1) {
		name, fallback := scan[ix[2]:ix[3]], strings.TrimSpace(scan[ix[4]:ix[5]])
		inDark := ix[0] < len(dark) && dark[ix[0]]
		if !colourCategories[style.TokenCategory(name)] || strings.HasPrefix(fallback, "var(") || strings.Contains(fallback, `"`) {
			continue
		}
		want, fine := colourFallback(name, fallback, inDark)
		if fine {
			continue
		}
		scheme := " in the light scheme"
		if inDark {
			scheme = " in this dark-scheme block"
		}
		report(name, fallback, want, scheme)
	}
	return out
}
