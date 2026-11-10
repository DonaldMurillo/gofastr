//go:build red

package appgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

// bughuntRunSDKFixture renders the Go SDK for one entity, tags keyed on
// slug, into a temp module the way TestSDKRepoParity does, drops fixture
// in as a test file of the generated client package and runs the named
// test there. It returns that run's output and error.
func bughuntRunSDKFixture(t *testing.T, fixture, run string) ([]byte, error) {
	t.Helper()
	repoRoot, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	timestamps := false
	declarations := []framework.EntityDeclaration{{
		Name: "tags", PrimaryKey: []string{"slug"}, Timestamps: &timestamps,
		Fields: []framework.FieldDeclaration{{Name: "slug", Type: "string", Required: true}, {Name: "label", Type: "string"}},
	}}
	spec, err := buildSDKSpec(declarations, &SdkOptions{module: "example.com/repo", apiPrefix: "api"})
	if err != nil {
		t.Fatal(err)
	}
	files, err := renderSDKGoFiles(spec)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	for _, file := range files {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(directory, file.Name)), 0o755); err != nil {
			t.Fatal(err)
		}
		clitest.WriteTestFile(t, filepath.Join(directory, file.Name), file.Content)
	}
	clitest.WriteTestFile(t, filepath.Join(directory, "bughunt_fixture_test.go"), fixture)
	goVersion, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(directory, "go.mod"), "module example.com/repo\n\ngo "+goVersion+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n")
	if err := clitest.CopyGoSum(repoRoot, directory); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("go", "test", "-count=1", "-run", "^"+run+"$", ".")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS=-mod=mod")
	return command.CombinedOutput()
}
