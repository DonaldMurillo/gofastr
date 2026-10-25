package gates

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/check"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// BuildA11yGateTo is buildA11yGate writing to the given streams, so
// `gofastr dev` can also show the report on its hold page.
func BuildA11yGateTo(root string, stdout, stderr io.Writer) bool {
	findings, err := AuditA11y(root)
	if err != nil {
		fmt.Fprintf(stderr, "accessibility lint: %v\n", err)
		return false
	}
	if len(findings) == 0 {
		return true
	}
	fmt.Fprint(stdout, FormatA11yReport(findings))
	return false
}

// A11yFinding is one violation reported by `gofastr audit a11y`.
type A11yFinding struct {
	File    string
	Line    int
	Message string
	Element string // "Image", "Button", … parsed from the message for guidance lookup
}

// AuditA11y statically scans every non-test, non-generated .go file
// under root for missing required accessibility fields on core-ui/html
// element configs (check.LintA11yFile). Tests call this directly.
func AuditA11y(root string) ([]A11yFinding, error) {
	var all []A11yFinding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			name := d.Name()
			switch name {
			case "vendor", ".git", "node_modules", "dist", "bin", "build", "tmp":
				return fs.SkipDir
			}
			if strings.HasPrefix(name, ".") && name != "." {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		// Generated files are the generator's responsibility, not the
		// developer's. Same policy as `gofastr audit lint`.
		if contracts.IsGeneratedSource(body) {
			return nil
		}
		res, lintErr := check.LintA11yFile(path)
		if lintErr != nil {
			// Mid-edit parse errors shouldn't kill the audit.
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel == "" {
			rel = path
		}
		for _, v := range res.Violations {
			all = append(all, A11yFinding{
				File:    rel,
				Line:    v.Line,
				Message: v.Message,
				Element: a11yElementFromMessage(v.Message),
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].File != all[j].File {
			return all[i].File < all[j].File
		}
		return all[i].Line < all[j].Line
	})
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

// a11yGuidance maps an element name to the one-paragraph fix hint shown
// under each finding. The goal is to teach the rule rather than merely flag it.
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

// FormatA11yReport renders findings with a fix hint per finding. Quiet
// (single line) when findings is empty.
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
