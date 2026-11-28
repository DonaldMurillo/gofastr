package appgen

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
)

// ResolveAppSpecModule reconciles app.module with the go.mod enclosing anchorDir, deriving it when omitted and refusing a mismatch.
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
