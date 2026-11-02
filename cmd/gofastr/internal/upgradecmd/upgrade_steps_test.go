package upgradecmd

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/project"
	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scan"
)

// vendoredApp writes a module that vendors its dependencies: a go
// command run in it defaults to -mod=vendor.
func vendoredApp(t *testing.T, version string) string {
	t.Helper()
	dir := t.TempDir()
	clitest.WriteFileAll(t, filepath.Join(dir, "go.mod"), "module app\n\ngo 1.22\n\nrequire "+project.GofastrModule+" "+version+"\n")
	clitest.WriteFileAll(t, filepath.Join(dir, "vendor", "modules.txt"), "# "+project.GofastrModule+" "+version+"\n## explicit\n")
	return dir
}

// go get leaves vendor/ behind go.mod, and go build then refuses the
// mismatch, so a vendored app's plan re-vendors between tidy and build.
func TestUpgradePlanRevendorsVendoredApp(t *testing.T) {
	stubScan(t, func(string, []*upgrade.Note, upgrade.MarkerSinks) (*scan.Result, error) {
		return &scan.Result{TypeChecked: true}, nil
	})
	dir := vendoredApp(t, "v0.85.0")
	out := clitest.CovT_capStdout(t, func() { RunUpgrade([]string{dir, "--to", "v0.86.0"}) })
	tidy := strings.Index(out, "2. go mod tidy")
	vendor := strings.Index(out, "3. go mod vendor")
	build := strings.Index(out, "4. go build ./...")
	if tidy < 0 || vendor < tidy || build < vendor {
		t.Fatalf("a vendored app's plan must run go mod vendor after tidy and before build:\n%s", out)
	}
	if !strings.Contains(out, "6. go install") {
		t.Fatalf("the manual steps must number on from the plan:\n%s", out)
	}
	plain := applySteps(bumpedApp(t), "v0.86.0")
	for _, s := range plain {
		if strings.Join(s, " ") == "go mod vendor" {
			t.Fatalf("an app with no vendor/modules.txt must not vendor: %v", plain)
		}
	}
}

// The newest release is resolved from inside a vendored module (the go
// command refuses -versions under -mod=vendor) and a prerelease newer
// than it is not picked, the way go get …@latest picks.
func TestResolveLatestVersionVendoredAndPrerelease(t *testing.T) {
	proxy := t.TempDir()
	at := filepath.Join(proxy, "github.com", "!donald!murillo", "gofastr", "@v")
	clitest.WriteFileAll(t, filepath.Join(at, "list"), "v0.95.0\nv0.96.0\nv0.97.0-rc.1\n")
	// The go command reads the required release's go.mod, and the newest
	// release's for retractions.
	for _, v := range []string{"v0.95.0", "v0.96.0"} {
		clitest.WriteFileAll(t, filepath.Join(at, v+".info"), `{"Version":"`+v+`","Time":"2026-10-30T00:00:00Z"}`)
		clitest.WriteFileAll(t, filepath.Join(at, v+".mod"), "module "+project.GofastrModule+"\n")
	}
	t.Setenv("GOPROXY", "file://"+filepath.ToSlash(proxy))
	t.Setenv("GOSUMDB", "off")
	// The fake v0.96.0 must never reach the shared module cache.
	t.Setenv("GOMODCACHE", t.TempDir())
	t.Setenv("GOFLAGS", "-modcacherw")
	t.Setenv("GOWORK", "off")
	t.Setenv("GOPRIVATE", "")
	t.Setenv("GONOPROXY", "")
	t.Setenv("GOTOOLCHAIN", "local")
	clitest.CovT_chdir(t, vendoredApp(t, "v0.95.0"))
	got, err := resolveLatestVersion()
	if err != nil {
		t.Fatalf("resolve the newest release in a vendored module: %v", err)
	}
	if got != "v0.96.0" {
		t.Fatalf("latest = %s, want v0.96.0 (the newest release, not the rc)", got)
	}
}

// A target with build metadata names the same release as go.mod: no
// downgrade warning.
func TestUpgradeSameReleaseWithBuildMetadata(t *testing.T) {
	dir := bumpedApp(t)
	out := clitest.CovT_capStdout(t, func() { RunUpgrade([]string{dir, "--to", "v0.86.0+build.1"}) })
	if !strings.Contains(out, "Already on the target release") || strings.Contains(out, "OLDER") {
		t.Fatalf("v0.86.0+build.1 is v0.86.0:\n%s", out)
	}
}
