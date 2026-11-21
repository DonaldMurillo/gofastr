package buildcmd

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// gofastr build regenerates for a settings file that has endpoints and
// nothing else: no codegen section, no entities/client.
func TestBuildGeneratesForEndpointsOnly(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	clitest.WriteSettings(t, dir, `endpoints:
  - name: feature_post
    entity: posts
    method: POST
    path: "{id}/feature"
    handler: entities/featured.go#FeaturePost
`)
	plan, err := appgen.PlanGenerate(appgen.GenerateOptions{})
	if err != nil || plan.Discovery.Found {
		t.Fatalf("expected settings with no codegen section, got Found=%t, %v", plan.Discovery.Found, err)
	}
	if !plan.HasWork() {
		t.Fatal("build skipped codegen for a settings file with endpoints")
	}
}

func TestBuildSkipsEmptyGenerationPlan(t *testing.T) {
	covT_tinyModule(t)
	var code int
	out := clitest.CovT_capOutput(t, func() {
		code = clitest.CovT_capExit(t, func() { RunBuild([]string{"--pkg=./missing"}) })
	})
	if code != 1 || !strings.Contains(out, `Build target "./missing" is invalid`) {
		t.Fatalf("exit = %d, want build target validation failure:\n%s", code, out)
	}
	if strings.Contains(out, "Generating code") || strings.Contains(out, "Nothing to generate") {
		t.Fatalf("empty generation plan produced output:\n%s", out)
	}
}
