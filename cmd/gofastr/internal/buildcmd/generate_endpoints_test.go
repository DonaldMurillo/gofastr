package buildcmd

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/codegen"
)

// gofastr build regenerates for a settings file that has endpoints and
// nothing else: no codegen section, no entities/client.
func TestBuildGeneratesForEndpointsOnly(t *testing.T) {
	dir := t.TempDir()
	if buildGenerates(false, dir) {
		t.Fatal("an empty project ran codegen")
	}
	clitest.WriteSettings(t, dir, `endpoints:
  - name: feature_post
    entity: posts
    method: POST
    path: "{id}/feature"
    handler: entities/featured.go#FeaturePost
`)
	discovery, err := codegen.DiscoverConfig(dir)
	if err != nil || discovery.Found {
		t.Fatalf("expected settings with no codegen section, got Found=%t, %v", discovery.Found, err)
	}
	if !buildGenerates(discovery.Found, dir) {
		t.Fatal("build skipped codegen for a settings file with endpoints")
	}
}
