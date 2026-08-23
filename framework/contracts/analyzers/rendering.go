package analyzers

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"

	"github.com/DonaldMurillo/gofastr/core-ui/check"
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
	out = append(out, checkInlineScripts(p)...)
	out = append(out, checkStyleTokens(p)...)
	return out, nil
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
		clean := stripCSSComments(string(body))
		cleaned[f.Rel] = clean
		for _, m := range reCSSCustomProp.FindAllStringSubmatch(clean, -1) {
			declared[m[1]] = true
		}
	}
	known := make(map[string]bool)
	names := style.TokenNames()
	for _, n := range names {
		known[n] = true
	}

	var out []contracts.Diagnostic
	for _, f := range files {
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

// reVarRef matches a var() reference; the captured group is the
// referenced name without the leading dashes.
var reVarRef = regexp.MustCompile(`var\(\s*--([A-Za-z0-9_-]+)`)

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

// stripCSSComments removes `/* */` comments from a stylesheet,
// preserving the line structure so offsets still map to original line
// numbers. Unlike the Go stripper it does NOT treat `//` as a comment:
// that would truncate `url(https://…)` and every protocol-relative URL
// mid-line, hiding real references after it.
func stripCSSComments(src string) string {
	var b strings.Builder
	b.Grow(len(src))
	inBlock := false
	for _, line := range strings.Split(src, "\n") {
		out := line
		if inBlock {
			if i := strings.Index(out, "*/"); i >= 0 {
				out, inBlock = out[i+2:], false
			} else {
				b.WriteByte('\n')
				continue
			}
		}
		for {
			bs := strings.Index(out, "/*")
			if bs < 0 {
				break
			}
			if e := strings.Index(out[bs+2:], "*/"); e >= 0 {
				out = out[:bs] + out[bs+2+e+2:]
				continue
			}
			out, inBlock = out[:bs], true
			break
		}
		b.WriteString(out)
		b.WriteByte('\n')
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
		if d := levenshtein(name, cand); d < bestDist {
			best, bestDist = cand, d
		}
	}
	return best, best != ""
}

// levenshtein returns the edit distance between a and b: the smallest
// number of single-character insertions, deletions, and substitutions
// that turns one into the other.
func levenshtein(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(curr[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func hasPrefixAny(rel string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(rel, p) || strings.Contains(rel, "/"+p) {
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
