package appgen

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestAppSpecLintsNeverBlockGenerate(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), "module "+frameworkModulePath+"/example\n")
	clitest.WriteFileAll(t, filepath.Join(dir, "bp.yml"), clitest.OwnerUniqueYML)
	var code int
	out := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, func() {
			GenerateFromAppSpec(GenerateOptions{Spec: filepath.Join(dir, "bp.yml"), outputDir: "gen", dryRun: true})
		})
	})
	if !strings.Contains(out, "uniqueness spans every account") || !strings.Contains(out, "ambiguous import") {
		t.Fatalf("the spec should trip two lints:\n%s", out)
	}
	if code != -1 || !strings.Contains(out, "Would generate") {
		t.Fatalf("a lint warning must not stop generate, got exit %d:\n%s", code, out)
	}
}

func TestAppSpecLintsStayOffJSONOutput(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), "module "+frameworkModulePath+"/example\n")
	clitest.WriteFileAll(t, filepath.Join(dir, "bp.yml"), clitest.OwnerUniqueYML)
	out := clitest.CovT_capStdout(t, func() {
		clitest.CovT_capExit(t, func() {
			GenerateFromAppSpec(GenerateOptions{Spec: filepath.Join(dir, "bp.yml"), outputDir: "gen", dryRun: true, json: true})
		})
	})
	if strings.Contains(out, "uniqueness spans every account") {
		t.Fatalf("--json output must carry no lint warning:\n%s", out)
	}
	var v any
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &v); err != nil {
		t.Fatalf("--json output is not one JSON value: %v\n%s", err, out)
	}
}
