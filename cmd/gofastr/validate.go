package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

// runValidate implements `gofastr validate <gofastr.yml|dir>`: parse the
// app spec, run every generate-time validation (including the app.module /
// go.mod consistency check and a full render pass), and exit 0/1. Errors are
// written for agents iterating on an app spec: each names the file (and line
// where the parser provides one), what is wrong, and the remedy.
func runValidate(args []string) {
	if hasHelpFlag(args) {
		printValidateHelp()
		return
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "--") {
		fail("Usage: gofastr validate <gofastr.yml|dir>")
		info("Parses the app spec and runs every generate-time validation. Exit 0 = valid.")
		osExit(1)
		return
	}
	path := args[0]
	bp, err := loadAppSpec(path)
	if err != nil {
		failAppSpecValidation(path, err)
		osExit(1)
		return
	}
	// Module consistency is anchored at the app spec's own directory: the
	// app spec conventionally sits at the project root next to go.mod.
	anchor := path
	if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
		anchor = filepath.Dir(path)
	}
	if err := resolveAppSpecModule(&bp, anchor); err != nil {
		failAppSpecValidation(path, err)
		osExit(1)
		return
	}
	// Render without writing: catches codegen-only failures (bad field
	// literals, unsupported defaults) that pure validation does not reach.
	if _, err := renderAppSpecCode(bp); err != nil {
		failAppSpecValidation(path, err)
		osExit(1)
		return
	}
	// Hard rule: per-user (PII-shaped) data must be scoped before auto-CRUD
	// exposure. An error here, a warning from `gofastr generate`.
	if findings := lintUnscopedPII(bp); len(findings) > 0 {
		for _, f := range findings {
			fail("%s: %s", path, f.Message())
		}
		osExit(1)
		return
	}
	// A module path that collides with the framework's own produces code that
	// cannot build anywhere outside this repo. Warned (not failed) for the same
	// reason `generate` warns: the in-repo examples legitimately declare such
	// paths. Surfaced here because validate → fix → generate is the loop the
	// CLI documents, and a finding that only appears at the last step is a
	// finding the loop is designed to skip past.
	for _, f := range lintModuleCollision(bp) {
		warn("%s: %s", path, f.Message())
	}
	// Uniqueness that spans owners on an owner-scoped entity: a warning,
	// since a value unique across all accounts can be deliberate.
	for _, f := range lintOwnerScopedUnique(bp) {
		warn("%s: %s", path, f.Message())
	}
	success("App spec %s is valid: %d entity(ies), %d screen(s), %d endpoint(s), %d hook(s)",
		path, len(bp.Entities), len(bp.Screens), len(bp.Endpoints), len(bp.Hooks))
}

func printValidateHelp() {
	fmt.Println(`gofastr validate: validate an app spec without writing files

Usage:
  gofastr validate <gofastr.yml|dir>

Checks:
  YAML decoding and unknown keys
  entity, screen, endpoint, and module consistency
  unscoped PII exposure
  a complete in-memory render of generated files

Exit status is 0 when valid and 1 when any check fails.`)
}

// failAppSpecValidation prints a validation failure prefixed with the
// app spec path. Parser/decoder errors already carry "at line N"; semantic
// errors name the entity/field/screen, since the decoded declarations do not
// retain source positions.
func failAppSpecValidation(path string, err error) {
	msg := strings.TrimPrefix(err.Error(), "app spec: ")
	fail("%s: %s", path, msg)
}

// resolveAppSpecModule reconciles the app spec's app.module with the Go
// module enclosing anchorDir:
//
//   - no enclosing go.mod  → the declared module (possibly empty) stands.
//   - module omitted       → derived from go.mod (plus the relative path
//     from the module root to anchorDir), so generated imports compile.
//   - both present, equal  → fine.
//   - both present, differ → error with the remedy; generating code that
//     imports a module the enclosing go.mod does not declare cannot build.
func resolveAppSpecModule(bp *AppSpec, anchorDir string) error {
	abs, err := filepath.Abs(anchorDir)
	if err != nil {
		return err
	}
	modulePath, moduleRoot := findEnclosingGoMod(abs)
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

// findEnclosingGoMod walks up from dir looking for a go.mod, mirroring how
// the go tool resolves the main module. Returns the declared module path and
// the directory containing go.mod, or empty strings when none is found.
func findEnclosingGoMod(dir string) (modulePath, moduleRoot string) {
	for {
		if mod := contracts.ReadModulePath(dir); mod != "" {
			return mod, dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
}
