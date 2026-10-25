package auditcmd

import (
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

// appSpecRootCandidates are the conventional app spec file names probed
// by `gofastr audit lint` at the audited root. Arbitrary *.yml files are
// NOT decoded; a project root full of CI configs must not break the lint
// walk or masquerade as an app spec.
var appSpecRootCandidates = []string{"gofastr.yml", "gofastr.yaml", "gofastr.json"}

// lintAppSpecPIIRoot adapts lintUnscopedPII to the audit-lint surface:
// it decodes the conventional app spec file(s) at root (silently skipping
// files that do not parse; `gofastr validate` owns those errors), merges
// them so the lint sees the whole declared app, and attributes each
// finding to the file declaring the entity.
func lintAppSpecPIIRoot(root string) []LintFinding {
	var merged appspec.AppSpec
	fileOf := map[string]string{}
	found := false
	for _, name := range appSpecRootCandidates {
		bp, err := appspec.DecodeAppSpecFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		found = true
		for _, decl := range bp.Entities {
			if fileOf[decl.Name] == "" {
				fileOf[decl.Name] = name
			}
		}
		merged = appspec.MergeAppSpecs(merged, bp)
	}
	if !found {
		return nil
	}
	var out []LintFinding
	for _, f := range appspec.LintUnscopedPII(merged) {
		file := fileOf[f.Entity]
		if file == "" {
			file = appSpecRootCandidates[0]
		}
		out = append(out, LintFinding{
			File:    file,
			Line:    1,
			Rule:    "unscoped-pii",
			Message: f.Message(),
		})
	}
	return out
}
