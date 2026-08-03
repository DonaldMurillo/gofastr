package analyzers

import (
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/check"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func init() {
	contracts.Register(&contracts.Analyzer{
		Name: "accessibility",
		Doc:  "The static WCAG floor: required accessibility fields on core-ui/html elements.",
		Rules: []string{
			contracts.RuleMissingAlt,
			contracts.RuleMissingAccessibleName,
			contracts.RuleUnnamedLandmark,
			contracts.RuleIncompleteFormControl,
			contracts.RuleImplicitHeadingLevel,
			contracts.RuleMissingElementMeta,
		},
		Run: runAccessibility,
	})
}

// elementRule maps the html element a violation is about onto the rule
// that describes the class of failure. The underlying linter reports one
// message shape per element; grouping them here is what lets a reader see
// "six missing accessible names" instead of six unrelated lines.
var elementRule = map[string]string{
	"Image":    contracts.RuleMissingAlt,
	"Button":   contracts.RuleMissingAccessibleName,
	"Link":     contracts.RuleMissingAccessibleName,
	"LinkHTML": contracts.RuleMissingAccessibleName,
	"Nav":      contracts.RuleUnnamedLandmark,
	"Section":  contracts.RuleUnnamedLandmark,
	"Aside":    contracts.RuleUnnamedLandmark,
	"Group":    contracts.RuleUnnamedLandmark,
	"Form":     contracts.RuleIncompleteFormControl,
	"Input":    contracts.RuleIncompleteFormControl,
	"Label":    contracts.RuleIncompleteFormControl,
	"Select":   contracts.RuleIncompleteFormControl,
	"TextArea": contracts.RuleIncompleteFormControl,
	"FieldSet": contracts.RuleIncompleteFormControl,
	"Heading":  contracts.RuleImplicitHeadingLevel,
}

var reA11yElement = regexp.MustCompile(`^html\.(\w+):`)

func runAccessibility(p *contracts.Pass) ([]contracts.Diagnostic, error) {
	var out []contracts.Diagnostic
	for _, f := range p.AppFiles() {
		res, err := check.LintA11yFile(f.Abs)
		if err != nil {
			// Mid-edit parse failures are the normal case for a file that
			// does not compile yet. The rest of the tree still gets checked.
			continue
		}
		for _, v := range res.Violations {
			element := ""
			if m := reA11yElement.FindStringSubmatch(v.Message); len(m) == 2 {
				element = m[1]
			}
			ruleID := elementRule[element]
			if ruleID == "" {
				ruleID = contracts.RuleMissingElementMeta
			}
			out = append(out, contracts.Diagnostic{
				RuleID:   ruleID,
				File:     f.Rel,
				Line:     v.Line,
				Message:  strings.TrimSpace(v.Message),
				Snippet:  p.Line(f.Rel, v.Line),
				Evidence: map[string]string{"element": element},
			})
		}
	}
	return out, nil
}
