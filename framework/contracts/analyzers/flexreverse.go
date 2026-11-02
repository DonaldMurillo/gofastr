package analyzers

import (
	"fmt"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// GOFASTR1826 reads the same comment-stripped design-system lines as
// GOFASTR1824 and reports a flex container whose direction is reversed:
// `flex-direction: row-reverse`, `flex-direction: column-reverse`, the
// same keyword in a `flex-flow` shorthand, or either property set
// through a sheet builder's Set("flex-direction", "row-reverse") pair.
//
// Bug class: ui.RatingInput rendered its radios Max..1 so a `~` sibling
// cascade could fill the stars, then flipped them back with
// row-reverse (85c52e6d). The page looked right, but the radios' native
// arrow keys, Tab and a screen reader walked the document, so
// ArrowRight went to the lower star (WCAG 2.4.3). The fix renders the
// children in reading order and selects "every earlier sibling" with
// :has() over a later one.
//
// Quiet: `wrap-reverse` (it reverses the stacking of wrapped lines, not
// the order inside one), a keyword in prose or a comment (the scan is
// comment-stripped, and the value must be the declaration's own),
// dev tooling (framework/dev), CSS outside the design-system trees
// (GOFASTR1801 already reports any CSS there), and a line a
// //gofastr:allow(GOFASTR1826) <reason> marker covers: a decorative
// stack whose children carry no order of their own.
func checkFlexReverse(rel string, lineNo int, scan, orig string) []contracts.Diagnostic {
	// CSS property names and keywords are ASCII case-insensitive.
	scan = strings.ToLower(scan)
	if !strings.Contains(scan, "-reverse") {
		return nil
	}
	var out []contracts.Diagnostic
	report := func(prop, value string) {
		prop = strings.ToLower(prop)
		if prop != "flex-direction" && prop != "flex-flow" {
			return
		}
		for _, tok := range cssTokens(strings.ToLower(value)) {
			if tok != "row-reverse" && tok != "column-reverse" {
				continue
			}
			out = append(out, contracts.Diagnostic{
				RuleID: contracts.RuleFlexReverse,
				File:   rel,
				Line:   lineNo,
				Message: fmt.Sprintf("%s: %s paints the children against their document order, so a screen reader, Tab and a radio group's arrows move the other way from what the reader sees; render them in reading order and lay them out forward",
					prop, tok),
				Snippet: strings.TrimSpace(orig),
			})
			return
		}
	}
	if strings.Contains(scan, ":") {
		for _, m := range reBareDeclCSS.FindAllStringSubmatch(scan, -1) {
			report(m[1], m[2])
		}
	}
	if strings.Contains(scan, `",`) {
		for _, m := range reBareDeclPair.FindAllStringSubmatch(scan, -1) {
			report(m[1], m[2])
		}
	}
	return out
}
