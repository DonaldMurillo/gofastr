package appspec

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// Drift gate (b): every relation field in the README app spec must
// target a declared entity. loadAppSpec runs full app spec
// validation, and the explicit loop keeps the gate meaningful even if
// that validation ever loosens.
//
//gofastr:test-reads framework/docs/content/tutorial-app-spec.md
func TestReadmeAppSpecRelationsResolve(t *testing.T) {
	yamlBlock, _, initBlock := clitest.ReadmeQuickstartBlocks(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, yamlBlock)
	bp, err := LoadAppSpec(path)
	if err != nil {
		t.Fatalf("tutorial app spec does not validate: %v", err)
	}
	if module := clitest.QuickstartModule(t, initBlock); bp.App.Module != module {
		t.Fatalf("tutorial app spec module %q != quickstart `go mod init %s`", bp.App.Module, module)
	}
	declared := map[string]bool{}
	for _, decl := range bp.Entities {
		declared[decl.Name] = true
	}
	for _, decl := range bp.Entities {
		for _, field := range decl.Fields {
			if !strings.EqualFold(field.Type, "relation") {
				continue
			}
			if !declared[field.To] {
				t.Fatalf("README app spec: %s.%s relates to undeclared entity %q", decl.Name, field.Name, field.To)
			}
		}
	}
}

// Drift gate (c): the tutorial app spec must pass the repo's own
// validator end to end: full app spec validation (loadAppSpec) plus
// the unscoped-PII lint that `gofastr validate` enforces. The tutorial
// can never again ship a quickstart that the validator rejects.
func TestReadmeAppSpecPassesValidator(t *testing.T) {
	yamlBlock, _, _ := clitest.ReadmeQuickstartBlocks(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, yamlBlock)
	bp, err := LoadAppSpec(path)
	if err != nil {
		t.Fatalf("tutorial quickstart app spec fails validateAppSpec: %v", err)
	}
	for _, f := range LintUnscopedPII(bp) {
		t.Errorf("tutorial quickstart app spec fails `gofastr validate` (unscoped-pii): %s", f.Message())
	}
}
