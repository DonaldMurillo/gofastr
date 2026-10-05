package analyzers

import (
	"fmt"
	"math"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/contracts"

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
			contracts.RuleDuplicateTokenValue,
			contracts.RuleRepeatedLiteral,
			contracts.RuleBareThemeLiteral,
		},
		Run: runRendering,
	})
}

// Telling a stylesheet apart from ordinary code is most of this rule's
// work, and the obvious pattern does not do it. Matching any `selector {
// name: value; }` flags Go and TypeScript wholesale. `{name: rel("x"),
// content: string(data)}` is a Go composite literal, and Go struct fields
// collide with CSS property names constantly (content, src, top, width,
// color, gap, transform).
//
// Two signals survive that collision, because neither can occur in Go:
//
//   - a hyphenated property (`font-family`, `border-radius`, `z-index`):
//     Go identifiers cannot contain a hyphen;
//   - a CSS-shaped *value*: a number with a unit, a hex colour, or a
//     custom-property reference.
//
// Plus at-rules and `<style>`, which are unambiguous on their own.
var (
	// Properties that only exist hyphenated. One of these is proof.
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
	// Single-word properties, only trusted when the value is CSS-shaped.
	cssPlainProps = strings.Join([]string{
		"color", "background", "margin", "padding", "border", "display", "position",
		"top", "right", "bottom", "left", "width", "height", "font", "flex", "gap",
		"opacity", "overflow", "cursor", "transition", "transform", "content", "src",
		"fill", "stroke", "outline", "inset", "appearance", "visibility",
	}, "|")
	// A number with a CSS unit, a hex colour, or a custom property.
	cssValueShape = `(?:-?[0-9.]+(?:px|rem|em|%|vh|vw|vmin|vmax|ch|fr|deg|ms|s)\b|#[0-9a-fA-F]{3,8}\b|var\(--)`
)

// notPropChar guards the left edge of a property name. A plain `\b` is
// not enough for two reasons found against this repository:
//
//   - `\bwidth` matches inside `max-width`, so the design system's own
//     `ss.Media("(max-width: 900px)", …)` read as bespoke CSS: the API
//     you are supposed to use, reported as the thing to stop doing;
//   - a hyphen is a word boundary, so every hyphenated property's tail
//     was a match for its own shorter cousin.
const notPropChar = `(?:^|[^-\w])`

var (
	// Hyphenated properties stay case-insensitive: a hyphen cannot occur
	// in a Go identifier, so there is nothing to collide with.
	reCSSHyphenRule = regexp.MustCompile(`(?i)` + notPropChar + `(?:` + cssHyphenProps + `)\s*:\s*[^;{}]+[;}]`)
	// Single-word properties are matched case-SENSITIVELY, lowercase
	// only. CSS is written lowercase; Go struct fields are capitalised.
	// Without this, `&t.Colors.Background: "#15141B"` was reported as a
	// stylesheet. That is a theme token assignment, the exact thing this
	// rule exists to encourage.
	//
	// Matches containing `:=` are rejected IN CODE (looksLikeCSS), not
	// here: Go's regexp is RE2 and has no lookahead, so `:(?!=)` cannot
	// be written. The guard is needed because `fill` and `stroke` are
	// property names AND legal Go identifiers, and in `fill :=
	// "var(--color-surface)"` (issue #220) the `:` of `:=` satisfied
	// the colon, `= "` the value gap, and the token reference the value
	// shape. A stylesheet declaration never carries `=` directly after
	// its colon, so a match containing `:=` is Go, not CSS.
	// reCSSHyphenRule needs no such guard: a hyphen cannot occur in a
	// Go identifier, so no Go construct can match it.
	reCSSValueRule = regexp.MustCompile(notPropChar + `(?:` + cssPlainProps + `)\s*:\s*[^;{}]*` + cssValueShape)
	reCSSAtRule    = regexp.MustCompile(`(?i)@(?:font-face|media|keyframes|supports|layer)\b`)
	// A `<style>` block opened inside a string.
	reStyleTag = regexp.MustCompile(`(?i)<style[\s>]`)
	// A style attribute with a real declaration in it.
	//
	// The preceding-context group is OPTIONAL. notPropChar exists to stop
	// `width` matching inside `max-width`, but it needs a character to
	// consume, and the first declaration in an attribute has none, since
	// the opening quote is already matched. Requiring it made
	// `style="margin-top: 12px"`, by far the most common shape, invisible
	// while `style="color: red; margin-top: 12px"` was caught.
	inlineStyleLead   = `(?:[^"']*` + notPropChar + `)?`
	reInlineStyleAttr = regexp.MustCompile(`(?i)style\s*=\s*\\?["']\s*(?:` +
		inlineStyleLead + `(?:` + cssHyphenProps + `)\s*:|` +
		inlineStyleLead + `(?:` + cssPlainProps + `)\s*:\s*[^"']*` + cssValueShape + `)`)
	// Hard navigation: assigning location, or reloading.
	reHardNav = regexp.MustCompile(`\b(?:window\.)?location(?:\.href|\.assign|\.replace)?\s*=\s*["'\x60]|\blocation\.(?:reload|assign|replace)\s*\(`)
	// A bespoke server-sent-event stream.
	reEventSource = regexp.MustCompile(`\bnew\s+EventSource\s*\(`)
)

// looksLikeCSS reports whether a line carries a stylesheet declaration,
// returning the text that matched so the diagnostic can name the trigger
// (empty string: nothing did). A reCSSValueRule match containing `:=` is
// rejected: see the comment above that regex.
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

// clipTrigger trims a matched fragment and caps it, so a long line
// produces a readable diagnostic rather than a wall of text.
func clipTrigger(m string) string {
	m = strings.TrimSpace(m)
	r := []rune(m)
	// Cut on runes, not bytes: a CSS value can hold any UTF-8, and
	// slicing mid-rune puts invalid bytes in the diagnostic.
	if len(r) > 48 {
		return string(r[:45]) + "…"
	}
	return m
}

// designSystemPrefixes are the trees that OWN styling. CSS there is the
// design system doing its job; the rule exists to stop it appearing
// anywhere else. Matching is on the path suffix so the rules hold in a
// host app that vendored or renamed the module.
var designSystemPrefixes = []string{
	"core-ui/", "framework/ui/", "framework/uihost/", "framework/sdkdocs/",
	"framework/pluginhost/", "framework/dev/", "battery/",
}

// devOnlyPrefixes are the surfaces exempt from the single-SSE-bus and
// hard-navigation rules. Dev tooling ships its own livereload stream and
// reloads the page on purpose, that is its entire job, and none of it
// runs in production. This is the exception class CLAUDE.md already
// carves out for livereload, applied here rather than left to every
// project's config.
var devOnlyPrefixes = []string{"framework/dev/", "kiln/"}

func runRendering(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	var out []contracts.Diagnostic
	for _, f := range p.AppFiles() {
		body, ok := p.Source(f.Rel)
		if !ok {
			continue
		}
		lines := strings.Split(string(body), "\n")
		// Scan with comments removed, report snippets from the original.
		// Every one of these constructs lives in a string literal, such
		// as CSS emitted into a template or a location assignment in a
		// script bundle, so a comment mentioning `@font-face` or
		// `<style>` is prose about the code, never the code.
		// stripComments preserves line numbering, so the two stay aligned.
		scan := strings.Split(stripComments(string(body)), "\n")
		ownsStyling := hasPrefixAny(f.Rel, designSystemPrefixes)
		devSurface := hasPrefixAny(f.Rel, devOnlyPrefixes)

		for i, line := range scan {
			lineNo := i + 1

			// GOFASTR1807 runs on the styling trees, which the byte
			// pre-filter below would mostly skip: a Set("gap", "8px")
			// pair line carries none of ':' '@' '<'. Its own guards are
			// strict supersets of what the two shapes can match.
			if ownsStyling && (strings.Contains(line, ":") || strings.Contains(line, `",`)) {
				out = append(out, checkHardcodedTokenValues(f.Rel, lineNo, line, lines[i])...)
			}
			// GOFASTR1808 is the same tree, the other direction: a var()
			// whose fallback restates the token at the wrong value.
			if ownsStyling && strings.Contains(line, "var(") {
				out = append(out, checkFallbackDrift(f.Rel, lineNo, line, lines[i])...)
			}
			// GOFASTR1823: the literals GOFASTR1807 cannot see because
			// they sit inside a shorthand.
			if ownsStyling && (strings.Contains(line, ":") || strings.Contains(line, `",`)) {
				out = append(out, checkBareThemeLiterals(f.Rel, lineNo, line, lines[i], devSurface)...)
			}

			// Cheap pre-filter. Every pattern below needs at least one of
			// these bytes, and the overwhelming majority of lines in a Go
			// repository contain none of them. This analyzer was three
			// times slower than any other, and the dev loop re-runs it on
			// every save. Each guard is a strict superset of what its
			// regex can match, so the finding set is unchanged:
			//   ':'          every CSS declaration and at-rule value
			//   '@'          at-rules
			//   '<'          <style> tags
			//   "location"   hard navigation
			//   "EventSource" bespoke streams
			if !strings.ContainsAny(line, ":@<") &&
				!strings.Contains(line, "location") &&
				!strings.Contains(line, "EventSource") {
				continue
			}

			if !ownsStyling {
				if trigger := looksLikeCSS(line); trigger != "" || reStyleTag.MatchString(line) {
					what := "a CSS rule"
					// Issue #220: the reporter read "a CSS rule is defined
					// outside the design system" as being about the value on
					// the line and went looking at what was assigned, when
					// the match was on the property name. Name the half of
					// the line that matched. The <style> wording is already
					// unambiguous, so it stays as it was.
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
				if reInlineStyleAttr.MatchString(line) {
					out = append(out, contracts.Diagnostic{
						RuleID: contracts.RuleInlineStyle, File: f.Rel, Line: lineNo,
						Message: "inline style attribute: it outranks every stylesheet rule and is blocked under a strict CSP",
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
	// GOFASTR1809: a stylesheet file with no owner. This loop was
	// GOFASTR1801's stylesheet arm; a stylesheet FILE is now reported
	// under 1809 and never under 1801, so one file never carries both.
	// The skips are the ones 1801 had (the design-system trees, testdata,
	// sheets that only assign custom properties) plus every *.style.css,
	// which is an owned style and never ownerless.
	for _, f := range p.StyleFiles() {
		if hasPrefixAny(f.Rel, designSystemPrefixes) || slices.Contains(strings.Split(f.Rel, "/"), "testdata") ||
			strings.HasSuffix(f.Rel, ".style.css") || strings.HasSuffix(f.Rel, ".tokens.css") {
			continue
		}
		body, ok := p.Source(f.Rel)
		if !ok {
			continue
		}
		clean := blankCSSCommentsAndStrings(string(body))
		if stylesheetOnlyCustomProperties(clean) {
			continue
		}
		// Keep the finding at the first rule, even when the first ordinary
		// declaration comes later, so a leading allow covers the sheet.
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
	out = append(out, checkStyleTokens(p)...)
	return out, nil
}

// checkStyleGenFreshness reports GOFASTR1814: every *.style.css must
// carry its generated sibling <name>_style.gen.go, and the sibling's
// `// Source hash: sha256:<hex>` line must match the CSS bytes. The
// generated file is the only link between the hand-written sheet and
// the typed class vocabulary the app calls; a stale pair silently
// serves the old CSS under the new names.
//
// testdata trees are skipped, the same convention GOFASTR1809's
// stylesheet loop uses: they hold parser fixtures, not shipped
// packages, and the generator itself refuses them (no Go package).
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
		if got, hasHash := generatedSourceHash(gen); !hasHash || got != ownstyle.SourceHash(string(css)) {
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

// generatedSourceHash reads the `// Source hash: sha256:<hex>` line
// from a generated file's bytes.
func generatedSourceHash(gen []byte) (hash string, ok bool) {
	for _, line := range strings.Split(string(gen), "\n") {
		if rest, isHash := strings.CutPrefix(strings.TrimSpace(line), ownstyle.SourceHashHeader); isHash {
			return strings.TrimSpace(rest), true
		}
	}
	return "", false
}

// stylesheetOnlyCustomProperties classifies CSS, not Go: display: flex is
// a declaration here even though the Go-string heuristic cannot distinguish
// it from a struct literal. Selectors and grouping rules do not style anything
// themselves; every declaration must assign a custom property to stay quiet.
// Comments and strings have already been blanked. Balanced function arguments
// and custom-property blocks keep value punctuation out of the statement scan.
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

// checkStyleTokens reports var(--name) references in project
// stylesheets whose name the theme does not emit (GOFASTR1806).
//
// The typed theme checks the DEFINITION (a style.Radius is
// compiler-checked), but a reference in a stylesheet is just a string,
// and nothing connects the two. An invalid var() is not a CSS error: it
// resolves to nothing, the declaration is silently dropped, and the
// only symptom is missing styling (issue #214: `--radius-lg` for
// `--radii-lg`, every rounded corner square for days).
//
// A reference is left alone when any of these hold:
//
//   - it carries a fallback, `var(--x, 8px)`: the declaration degrades
//     instead of being dropped, so the failure this rule exists to
//     catch cannot happen;
//   - the name is declared as a custom property in any scanned
//     stylesheet: the set is built across all of them, because a base
//
// sheet declaring what another sheet uses is normal;
//   - it starts with `ui-` or `fui-`: per-component override knobs and
//     runtime-owned properties, not theme tokens (see the theming doc's
//     `--ui-*` section);
//   - it is one of the names style.TokenNames() says the theme emits.
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
		// An owned style's 1806 comes from ownstyle.Check, the check gen
		// styles runs; reporting it here too would double every finding.
		if isOwnedStylePath(f.Rel) {
			continue
		}
		clean := cleaned[f.Rel]
		for _, loc := range reVarRef.FindAllStringSubmatchIndex(clean, -1) {
			name := clean[loc[2]:loc[3]]
			if varRefHasFallback(clean, loc[3]) ||
				declared[name] ||
				strings.HasPrefix(name, "ui-") || strings.HasPrefix(name, "fui-") ||
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

// reCSSCustomProp matches a custom-property declaration, `--name:`; the
// captured group is the name without the leading dashes.
var reCSSCustomProp = regexp.MustCompile(`--([A-Za-z0-9_-]+)\s*:`)

// reAtProperty matches an `@property --name` registration. It has no
// colon after the name, so reCSSCustomProp cannot see it, and a project
// that registers a property this way and then reads it would otherwise
// be told the token does not exist.
//
// Case-insensitive on the at-rule keyword for the same reason reVarRef
// is on the function name; the captured NAME keeps its case.
var reAtProperty = regexp.MustCompile(`(?i)@property\s+--([A-Za-z0-9_-]+)`)

// collectDeclared adds every custom property a stylesheet DECLARES to
// into. Position matters: `--name:` only declares inside a rule block and
// outside parentheses.
//
// The case that forced this is `@supports (--brand: red) { … }`. A feature
// query's condition asks whether the browser can PARSE that declaration;
// it does not make one. Counting it marked `--brand` declared and silenced
// every real finding for it, which is the failure mode this rule exists to
// prevent, arrived at from the other side.
//
// Depth is counted on the cleaned text, where comments and strings are
// already blanked, so a brace or paren inside either cannot skew it.
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

// reVarRef matches a var() reference; the captured group is the
// referenced name without the leading dashes.
//
// The FUNCTION name folds case, because CSS function names are ASCII
// case-insensitive: `VAR(--radii-lg)` and `Var(--radii-lg)` both resolve
// in a browser, so a case-sensitive match let an uppercase reference to a
// nonexistent token through unreported.
//
// The custom-property NAME does not fold, and must not: property names
// ARE case-sensitive. Measured in Chrome, `var(--mixed)` against a
// declared `--Mixed` resolves to nothing, so treating the two as the same
// token would call a real miss a match.
var reVarRef = regexp.MustCompile(`(?i)var\(\s*--([A-Za-z0-9_-]+)`)

// varRefHasFallback reports whether the var() reference whose name ends
// at s[i-1] carries a fallback: a comma at the TOP level of its
// arguments, before the matching close paren. `var(--a, var(--b))` has
// one; `var(--a)` does not. Parentheses nest, so depth is counted.
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

// blankCSSCommentsAndStrings blanks the bodies of `/* */` comments and
// of '…' / "…" strings, replacing their characters with spaces so every
// byte offset and newline position survives: checkStyleTokens computes
// reported line numbers from the cleaned text, and anything that moves
// would misattribute a finding.
//
// One left-to-right scan over four states (text, comment, 'string',
// "string") is the whole job, because the only ambiguity in CSS is
// which of those four a character belongs to:
//
//   - `/*` opens a comment in text ONLY; inside a string it is content
//     (the false negative: an unterminated "comment" used to swallow
//     the rest of the file, hiding real references after it);
//   - a quote opens a string in text ONLY; inside a comment it is
//     prose (the inverse: a quoted `*/` must not close a comment);
//   - a backslash inside a string escapes the next character, so
//     `"a\"b"` is one string; a backslash before a newline continues
//     the string onto the next line, which CSS allows.
//
// A RAW newline inside a string terminates it rather than continuing
// the scan to EOF: that is the spec's bad-string recovery, and it is
// the safe reading for a pre-pass, since the text after the newline
// resumes as ordinary CSS instead of being swallowed. Blanked bytes
// become spaces, newlines stay newlines.
//
// Unlike the Go stripper it does NOT treat `//` as a comment: that
// would truncate `url(https://…)` and every protocol-relative URL
// mid-line, hiding real references after it.
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
				// Escaped character: blank both without interpreting
				// the second (a quote, a newline, anything).
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

// closestToken returns the token within edit distance 2 of name, if any.
// names must be sorted, so equal distances resolve deterministically to
// the first. Distance 2 is enough for the typo class this rule exists
// for (`radius-lg` vs `radii-lg` is 2) without proposing nonsense for
// every unknown name.
func closestToken(name string, names []string) (string, bool) {
	best, bestDist := "", 3
	for _, cand := range names {
		if d := access.EditDistance(name, cand); d < bestDist {
			best, bestDist = cand, d
		}
	}
	return best, best != ""
}

// propTokenCategories maps a CSS property to the theme-token categories
// (the prefix of a token key, "text" in "text-xs") whose value can
// legally replace that property's through var(). A category no property
// can serve is absent on purpose: breakpoint-*, because a media query
// cannot read a custom property — var() is invalid inside an @media
// condition — so writing the px there is forced by CSS, not a token
// bypass.
//
// The property list doubles as the false-positive guard: only these
// names are matched, so `Value: "0.75rem"` on a theme declaration and a
// custom property like `--text-xs:` cannot fire. Properties match
// lowercase only: CSS is written lowercase and Go fields are
// capitalised, and folding case is what let `Colors.Background:`
// composite literals read as stylesheets in GOFASTR1801's first draft.
//
// The table itself is ownstyle.PropTokenCategories, the one owned sheets
// are judged by, so the two pipelines cannot drift.
var propTokenCategories = ownstyle.PropTokenCategories

// tokenPropAlternation is the property list as a regex alternation,
// sorted so the compiled pattern is byte-stable.
var tokenPropAlternation = func() string {
	props := make([]string, 0, len(propTokenCategories))
	for p := range propTokenCategories {
		props = append(props, p)
	}
	slices.Sort(props)
	return strings.Join(props, "|")
}()

var (
	// A stylesheet declaration `prop: value` in a string. The value stops
	// where a declaration ends (`;` `}`), at the quote or backtick of the
	// Go string the CSS lives in, or at a newline. notPropChar keeps the
	// left edge honest: without it `font-size` matches inside the custom
	// property `--font-size`.
	reTokenValueCSS = regexp.MustCompile(
		notPropChar + `(` + tokenPropAlternation + `)\s*:\s*([^;{}"'` + "`" + `\n]*)`)
	// The StyleSheet builder shape, where property and value are separate
	// string literals: Set("font-size", "0.75rem"), Child(sel, "gap",
	// "8px"). The property is fully quoted on both sides so a match can
	// never start mid-identifier.
	//
	// Known limit: the pair shape cannot see the CALL it sits in, so a
	// non-builder call passing the same two literals — e.g.
	// strings.ReplaceAll(s, "gap", "8px") in a design-system file — would
	// match. No such call exists in tree (the repo verify run is clean),
	// and distinguishing them needs the call's identity, which is type
	// information: if a real false positive ever lands, the fix is a
	// vet-lane analyzer (internal/analyzers), not more regex here. Until
	// then a stray match costs one reviewed //gofastr:allow line.
	reTokenValuePair = regexp.MustCompile(`"(` + tokenPropAlternation + `)"\s*,\s*"([^"]*)"`)
)

// tokenValueIndex is the theme token map inverted: value (lower case) →
// the tokens declaring it. Built once per process from the canonical
// theme, the same source GOFASTR1806 validates names against.
//
// Two exclusions, both load-bearing:
//
//   - dark.* keys: the dark palette re-declares the same property names,
//     so a literal equal to a dark-only value must not become
//     var(--color-x) — in the light scheme that reference resolves to
//     the light value and silently changes the pixels. The base theme
//     ships no dark entries today; the guard holds the day one lands.
//   - bare-keyword values: `none` is shadow-none's value, but
//     `box-shadow: none` is idiomatic CSS for "no shadow", not a token
//     bypass. A value needs a digit, hex, quote, comma, or paren before
//     it is evidence; keyword-only shapes never enter the index.
var tokenValueIndex = sync.OnceValue(func() map[string][]string {
	out := map[string][]string{}
	for k, v := range style.ThemeToTokens(style.DefaultTheme()) {
		if strings.HasPrefix(k, "dark.") || bareKeyword(v) {
			continue
		}
		lv := strings.ToLower(v)
		out[lv] = append(out[lv], k)
	}
	return out
})

// bareKeyword reports whether v is a single CSS identifier and nothing
// else: no digit, hex marker, string quote, comma, or parenthesis
// anywhere. Such a value cannot be distinctive evidence of a token
// bypass.
func bareKeyword(v string) bool {
	if v == "" {
		return true
	}
	for _, r := range v {
		switch {
		case r >= '0' && r <= '9', r == '#', r == ',', r == '(', r == '\'', r == '"':
			return false
		}
	}
	return true
}

// tokenCategory is style.TokenCategory: the longest token-type prefix
// of a key, so font-weight-bold is a weight and never a font family.
func tokenCategory(key string) string { return style.TokenCategory(key) }

// checkHardcodedTokenValues reports one GOFASTR1807 per declaration on a
// design-system line whose FULL value is exactly a theme token's value.
// The value is judged whole: a shorthand (`padding: 8px 12px`), a
// calc(), and a var() with its fallback restating the token (the
// degraded-mode copy, which is correct) all differ from every token
// value and stay quiet, as does any value no token carries — an
// off-scale value is a MISSING token, a different finding.
//
// Design-system CSS lives in two shapes no AST unifies: stylesheet
// strings and builder calls whose property and value are separate
// literals. Both are text on a line, so both regexes run on the same
// comment-stripped line the bespoke-CSS check reads. `!important` is
// trimmed before comparing: it modifies the cascade, not the value.
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
			if allowed[tokenCategory(k)] {
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
		ref := "{" + tokenCategory(first) + "." + strings.TrimPrefix(first, tokenCategory(first)+"-") + "}"
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

// checkInlineScripts reports script elements with an inline body, reusing
// the linter `cmd/check-csp` runs rather than re-deriving the detection.
// Two implementations of one check is how GOFASTR1804 came to miss the
// most common shape of inline style while a second detector caught it.
//
// It scans the pass's own files with the pass's own cached AST, rather
// than calling the recursive variant: that walked the tree, re-read every
// file, and re-parsed it, all three of which had just happened, for
// about 200ms on this repository, on every save in the dev loop.
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
				RuleID: contracts.RuleInlineScript,
				File:   f.Rel,
				Line:   v.Line,
				// Deliberately not spelling the tag out: this file would
				// then trip its own rule, and a detector that has to be
				// exempted from itself is one more thing to explain.
				Message: "script element with an inline body: a strict CSP refuses to execute it, so the page ships and the script silently never runs",
				Snippet: p.Line(f.Rel, v.Line),
			})
		}
	}
	return out
}

// driftCategories are the token categories GOFASTR1808 judges: the ones
// whose values are lengths or times, where a fallback either restates
// the token or teaches a scale the theme does not declare. Colour and
// font fallbacks stay out: `currentColor`, `inherit`, `transparent`, and
// a dark-surface hex beside a light token are degraded-mode choices an
// author made on purpose, and the rule cannot tell those from drift.
var driftCategories = map[string]bool{"spacing": true, "radii": true, "stroke": true, "leading": true, "tracking": true, "opacity": true, "text": true, "duration": true}

// declaredTokenValues is the theme's token table by name, light entries
// only, for the fallback comparison.
var declaredTokenValues = sync.OnceValue(func() map[string]string {
	out := map[string]string{}
	for k, v := range style.ThemeToTokens(style.DefaultTheme()) {
		if strings.HasPrefix(k, "dark.") {
			continue
		}
		out[k] = v
	}
	return out
})

// reVarFallback matches a var() reference carrying a simple fallback: a
// literal with no comma or parenthesis. A nested `var(--a, var(--b))` or a
// calc() fallback is not a restated token value and is left alone.
var reVarFallback = regexp.MustCompile(`var\(\s*--([A-Za-z0-9_-]+)\s*,\s*([^(),]+?)\s*\)`)

// reLengthOrTime is a bare CSS length or time literal in the four units
// the theme emits and authors write.
var reLengthOrTime = regexp.MustCompile(`^(\d*\.?\d+)(px|rem|ms|s)$`)

// sameScaleValue reports whether two CSS literals denote the same length
// or time. rem is compared to px at the browser's 16px root default, and
// s to ms, so `1rem` is the right fallback for a `16px` token and `.5s`
// for a `500ms` one. Anything the regex does not parse is compared as a
// case-folded string.
func sameScaleValue(a, b string) bool {
	a, b = strings.ToLower(strings.TrimSpace(a)), strings.ToLower(strings.TrimSpace(b))
	if a == b {
		return true
	}
	ma, mb := reLengthOrTime.FindStringSubmatch(a), reLengthOrTime.FindStringSubmatch(b)
	if ma == nil || mb == nil {
		return false
	}
	na, _ := strconv.ParseFloat(ma[1], 64)
	nb, _ := strconv.ParseFloat(mb[1], 64)
	ua, ub := ma[2], mb[2]
	switch ua {
	case "rem":
		na, ua = na*16, "px"
	case "s":
		na, ua = na*1000, "ms"
	}
	switch ub {
	case "rem":
		nb, ub = nb*16, "px"
	case "s":
		nb, ub = nb*1000, "ms"
	}
	return ua == ub && math.Abs(na-nb) < 1e-6
}

// checkFallbackDrift reports one GOFASTR1808 per var() reference on a
// design-system line whose fallback restates a length- or time-scale
// token at a value the theme does not declare.
func checkFallbackDrift(rel string, lineNo int, scan, orig string) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, m := range reVarFallback.FindAllStringSubmatch(scan, -1) {
		name, fallback := m[1], strings.TrimSpace(m[2])
		if !driftCategories[tokenCategory(name)] {
			continue
		}
		declared, ok := declaredTokenValues()[name]
		if !ok || sameScaleValue(fallback, declared) {
			continue
		}
		out = append(out, contracts.Diagnostic{
			RuleID: contracts.RuleFallbackDrift,
			File:   rel,
			Line:   lineNo,
			Message: fmt.Sprintf("var(--%s, %s): the theme declares --%s as %s, so a themed page renders %s and the fallback teaches a value that does not exist; write var(--%s, %s)",
				name, fallback, name, declared, declared, name, declared),
			Snippet: strings.TrimSpace(orig),
		})
	}
	return out
}
