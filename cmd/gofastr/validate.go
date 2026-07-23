package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// runValidate implements `gofastr validate <blueprint.yml|dir>`: parse the
// blueprint, run every generate-time validation (including the app.module /
// go.mod consistency check and a full render pass), and exit 0/1. Errors are
// written for agents iterating on a blueprint: each names the file (and line
// where the parser provides one), what is wrong, and the remedy.
func runValidate(args []string) {
	if hasHelpFlag(args) {
		printValidateHelp()
		return
	}
	if len(args) == 0 || strings.HasPrefix(args[0], "--") {
		fail("Usage: gofastr validate <blueprint.yml|blueprint-dir>")
		info("Parses the blueprint and runs every generate-time validation. Exit 0 = valid.")
		osExit(1)
		return
	}
	path := args[0]
	bp, err := loadBlueprint(path)
	if err != nil {
		failBlueprintValidation(path, err)
		osExit(1)
		return
	}
	// Module consistency is anchored at the blueprint's own directory — the
	// blueprint conventionally sits at the project root next to go.mod.
	anchor := path
	if info, statErr := os.Stat(path); statErr == nil && !info.IsDir() {
		anchor = filepath.Dir(path)
	}
	if err := resolveBlueprintModule(&bp, anchor); err != nil {
		failBlueprintValidation(path, err)
		osExit(1)
		return
	}
	// Render without writing: catches codegen-only failures (bad field
	// literals, unsupported defaults) that pure validation does not reach.
	if _, err := renderBlueprintFiles(bp); err != nil {
		failBlueprintValidation(path, err)
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
	success("Blueprint %s is valid: %d entity(ies), %d screen(s), %d endpoint(s)", path, len(bp.Entities), len(bp.Screens), len(bp.Endpoints))
}

func printValidateHelp() {
	fmt.Println(`gofastr validate — validate a blueprint without writing files

Usage:
  gofastr validate <blueprint.yml|blueprint-dir>

Checks:
  YAML decoding and unknown keys
  entity, screen, endpoint, and module consistency
  unscoped PII exposure
  a complete in-memory render of generated files

Exit status is 0 when valid and 1 when any check fails.`)
}

// failBlueprintValidation prints a validation failure prefixed with the
// blueprint path. Parser/decoder errors already carry "at line N"; semantic
// errors name the entity/field/screen, since the decoded declarations do not
// retain source positions.
func failBlueprintValidation(path string, err error) {
	msg := strings.TrimPrefix(err.Error(), "blueprint: ")
	fail("%s: %s", path, msg)
}

// resolveBlueprintModule reconciles the blueprint's app.module with the Go
// module enclosing anchorDir:
//
//   - no enclosing go.mod  → the declared module (possibly empty) stands.
//   - module omitted       → derived from go.mod (plus the relative path
//     from the module root to anchorDir), so generated imports compile.
//   - both present, equal  → fine.
//   - both present, differ → error with the remedy; generating code that
//     imports a module the enclosing go.mod does not declare cannot build.
func resolveBlueprintModule(bp *Blueprint, anchorDir string) error {
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
		return fmt.Errorf("blueprint: app.module is %q but the enclosing go.mod (%s) makes this directory %q — set `module: %s` (or remove module: to derive it automatically)", declared, filepath.Join(moduleRoot, "go.mod"), expected, expected)
	}
	return nil
}

// findEnclosingGoMod walks up from dir looking for a go.mod, mirroring how
// the go tool resolves the main module. Returns the declared module path and
// the directory containing go.mod, or empty strings when none is found.
func findEnclosingGoMod(dir string) (modulePath, moduleRoot string) {
	for {
		if mod := readModulePath(dir); mod != "" {
			return mod, dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", ""
		}
		dir = parent
	}
}
