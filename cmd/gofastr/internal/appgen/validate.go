package appgen

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
)

// ResolveAppSpecModule reconciles the app spec's app.module with the Go
// module enclosing anchorDir:
//
//   - no enclosing go.mod  → the declared module (possibly empty) stands.
//   - module omitted       → derived from go.mod (plus the relative path
//     from the module root to anchorDir), so generated imports compile.
//   - both present, equal  → fine.
//   - both present, differ → error with the remedy; generating code that
//     imports a module the enclosing go.mod does not declare cannot build.
func ResolveAppSpecModule(bp *appspec.AppSpec, anchorDir string) error {
	abs, err := filepath.Abs(anchorDir)
	if err != nil {
		return err
	}
	modulePath, moduleRoot := project.FindEnclosingGoMod(abs)
	if modulePath == "" {
		return nil
	}
	expected := modulePath
	if rel, relErr := filepath.Rel(moduleRoot, abs); relErr == nil && rel != "." {
		expected = modulePath + "/" + filepath.ToSlash(rel)
	}
	declared := strings.TrimSuffix(strings.TrimSpace(bp.App.Module), "/")
	if declared == "" {
		bp.App.Module = expected
		return nil
	}
	if declared != expected {
		return fmt.Errorf("app spec: app.module is %q but the enclosing go.mod (%s) makes this directory %q. Set `module: %s` (or remove module: to derive it automatically)", declared, filepath.Join(moduleRoot, "go.mod"), expected, expected)
	}
	return nil
}
