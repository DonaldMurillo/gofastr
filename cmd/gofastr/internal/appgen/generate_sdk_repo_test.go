package appgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework"
)

//gofastr:test-reads internal/analyzers/ cmd/vettool/
func TestSDKRepoParity(t *testing.T) {
	repoRoot, err := filepath.Abs("../../../..")
	if err != nil {
		t.Fatal(err)
	}
	timestamps := false
	crud := true
	declaration := framework.EntityDeclaration{
		Name: "notes", Table: "notes", Timestamps: &timestamps,
		Scope:    &framework.ScopeDeclaration{OwnerField: "owner_id", MultiTenant: true},
		Exposure: &framework.ExposureDeclaration{CRUD: &crud},
		Fields: []framework.FieldDeclaration{
			{Name: "title", Type: "string", Required: true},
			{Name: "body", Type: "string"},
			{Name: "score", Type: "int"},
			{Name: "owner_id", Type: "string", Hidden: true},
		},
	}
	declarations := []framework.EntityDeclaration{declaration, {
		Name: "counters", Fields: []framework.FieldDeclaration{{Name: "id", Type: "int", AutoGenerate: "increment"}},
	}, {
		Name: "memberships", PrimaryKey: []string{"repo", "http", "context"},
		Fields: []framework.FieldDeclaration{{Name: "repo", Type: "string"}, {Name: "http", Type: "string"}, {Name: "context", Type: "string"}},
	}}
	projectDir := t.TempDir()
	clitest.WriteTestFile(t, filepath.Join(projectDir, "gofastr.yml"), "app:\n  name: repo\n")
	opts := SdkOptions{module: "example.com/repo"}
	if err := applySDKAppDefaults(projectDir, &opts); err != nil {
		t.Fatal(err)
	}
	spec, err := buildSDKSpec(declarations, &opts)
	if err != nil {
		t.Fatal(err)
	}
	files, err := renderSDKGoFiles(spec)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	found := map[string]bool{}
	for _, file := range files {
		found[file.Name] = true
		if err := os.MkdirAll(filepath.Dir(filepath.Join(directory, file.Name)), 0o755); err != nil {
			t.Fatal(err)
		}
		clitest.WriteTestFile(t, filepath.Join(directory, file.Name), file.Content)
	}
	for _, name := range []string{"repositories.go", "local/repositories.go"} {
		if !found[name] {
			t.Fatalf("SDK is missing %s", name)
		}
	}
	fixture, err := os.ReadFile("testdata/repository/conformance.go.txt")
	if err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(directory, "repository_test.go"), string(fixture))
	goVersion, err := clitest.RepoGoVersion(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(directory, "go.mod"), "module example.com/repo\n\ngo "+goVersion+"\n\nrequire github.com/DonaldMurillo/gofastr v0.0.0\n\nreplace github.com/DonaldMurillo/gofastr => "+repoRoot+"\n")
	if err := clitest.CopyGoSum(repoRoot, directory); err != nil {
		t.Fatal(err)
	}
	vettool, err := clitest.RepoVettool()
	if err != nil {
		t.Fatal(err)
	}
	for _, arguments := range [][]string{{"test", "-count=1", "-v", "./..."}, {"vet", "-vettool=" + vettool, "./..."}} {
		command := exec.Command("go", arguments...)
		command.Dir = directory
		command.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS=-mod=mod")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(arguments, " "), err, output)
		}
		t.Logf("go %s:\n%s", strings.Join(arguments, " "), output)
	}
	command := exec.Command("go", "list", "-deps", "./...")
	command.Dir = directory
	command.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS=-mod=mod")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("client dependencies: %v\n%s", err, output)
	}
	if strings.Contains(string(output), "github.com/DonaldMurillo/gofastr") {
		t.Fatalf("generated SDK build links the framework:\n%s", output)
	}
}

func TestSDKRepoNames(t *testing.T) {
	for _, name := range []string{"notes_repo", "notes_remote", "new_notes_remote"} {
		declarations := []framework.EntityDeclaration{{Name: "notes"}, {Name: name}}
		spec, err := buildSDKSpec(declarations, &SdkOptions{})
		if err == nil {
			_, err = renderSDKGoFiles(spec)
		}
		if err == nil {
			t.Errorf("accepted repository name collision %q", name)
		}
	}
}
