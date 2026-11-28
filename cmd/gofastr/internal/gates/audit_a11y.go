package gates

import (
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/check"
)

// BuildA11yGateTo runs the accessibility lint over root, writes its
// report to stdout and its error to stderr, and reports whether it passed.
func BuildA11yGateTo(root string, stdout, stderr io.Writer) bool {
	return lintGate(root, stdout, stderr, "accessibility lint", AuditA11y, FormatA11yReport)
}

// A11yFinding is one violation reported by `gofastr audit a11y`.
type A11yFinding struct {
	File    string
	Line    int
	Message string
	Element string
}

// AuditA11y returns the check.LintA11yFile findings of every non-test,
// non-generated .go file under root, sorted by file and line.
func AuditA11y(root string) ([]A11yFinding, error) {
	violations, err := auditTree(root, ".go", check.LintA11yFile)
	if err != nil {
		return nil, err
	}
	all := make([]A11yFinding, 0, len(violations))
	for _, v := range violations {
		all = append(all, A11yFinding{File: v.File, Line: v.Line, Message: v.Message, Element: a11yElementFromMessage(v.Message)})
	}
	return all, nil
}

var a11yMessageElement = regexp.MustCompile(`^html\.(\w+):`)

func a11yElementFromMessage(msg string) string {
	m := a11yMessageElement.FindStringSubmatch(msg)
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

var a11yGuidance = map[string]string{
	"Image":    `every image needs Alt. Informative image → describe what it shows ("Team photo at launch"). Decorative image → explicit empty Alt: "" so screen readers skip it. Never omit the field.`,
	"Button":   `a button needs an accessible name. Visible text → Label carries it; icon-only button → Label describes the action ("Close dialog", "Copy code").`,
	"Link":     `links need a destination (Href) and text a screen reader can announce (Text). "Click here" fails out of context: name the target ("View pricing").`,
	"LinkHTML": `links need a destination (Href) and announceable Content. Avoid bare icons without a text alternative.`,
	"Nav":      `multiple <nav> landmarks are indistinguishable without names. Set Label ("Main", "Footer") or LabelledBy pointing at a heading id.`,
	"Section":  `a <section> only becomes a labelled landmark with an accessible name. Set Label or LabelledBy, or use a plain Div if it isn't a landmark.`,
	"Aside":    `complementary landmarks need names when a page has more than one. Set Label or LabelledBy.`,
	"Group":    `a Group must declare its Role (e.g. "group", "radiogroup", "toolbar") so assistive tech knows what it groups.`,
	"Form":     `declare Method explicitly: GET for reads (shareable URLs), POST for writes (CSRF-protected by the framework).`,
	"Input":    `inputs need Type (drives mobile keyboards + validation) and Name (form submission). Pair with html.Label via For.`,
	"Label":    `a label must reference its control: For = the input's id, Text = the visible caption. Placeholder text is not a label.`,
	"Select":   `selects need Name for form submission. Pair with html.Label via For.`,
	"TextArea": `textareas need Name for form submission. Pair with html.Label via For.`,
	"FieldSet": `a fieldset needs a Legend: it's announced as the group name for every control inside (essential for radio groups).`,
	"Heading":  `headings need an explicit Level. Don't pick by font size: levels form the page outline screen-reader users navigate by (one h1, no skipped levels).`,
	"Abbr":     `an abbreviation needs Title with the expansion ("WCAG" → "Web Content Accessibility Guidelines").`,
	"Time":     `set Datetime to the machine-readable value (RFC 3339) so tools can parse what the visible text says.`,
	"Source":   `media sources need Src and Type so the browser can pick a playable stream.`,
}

// FormatA11yReport renders findings with a fix hint per finding, or one
// line when there are none.
func FormatA11yReport(findings []A11yFinding) string {
	if len(findings) == 0 {
		return "No accessibility issues found.\n"
	}
	files := map[string]bool{}
	for _, f := range findings {
		files[f.File] = true
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Accessibility lint: %d issue(s) in %d file(s)\n\n", len(findings), len(files))
	for _, f := range findings {
		fmt.Fprintf(&b, "%s:%d: %s\n", f.File, f.Line, f.Message)
		if hint, ok := a11yGuidance[f.Element]; ok {
			fmt.Fprintf(&b, "    fix: %s\n", hint)
		}
		b.WriteString("\n")
	}
	b.WriteString("These rules are the static floor (WCAG name/role/value basics the\n")
	b.WriteString("type system can see). For the full runtime audit of contrast, focus,\n")
	b.WriteString("landmarks, and tap targets, run against a live server:\n")
	b.WriteString("    gofastr audit a11y --url http://localhost:8080\n")
	return b.String()
}
