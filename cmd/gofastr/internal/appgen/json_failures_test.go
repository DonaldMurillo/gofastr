package appgen

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestGenerateFailuresAreJSON(t *testing.T) {
	for _, command := range []struct {
		name string
		run  func()
	}{
		{"project", func() { GenerateProject([]string{"--json"}) }},
		{"app", func() { RunGenerateApp([]string{"missing.yml", "--json"}) }},
		{"sdk", func() { RunGenerateSDK([]string{"--json"}) }},
		{"cli", func() { RunGenerateCLI([]string{"--json"}) }},
		{"openapi", func() { RunGenerateCLI([]string{"--from-openapi=missing.json", "--json"}) }},
		{"screen", func() { GenerateScaffoldScreen([]string{"report", "--from-a11y=missing.txt", "--json"}) }},
	} {
		t.Run(command.name, func(t *testing.T) {
			clitest.CovT_chdir(t, t.TempDir())
			assertGenerateJSONError(t, command.run)
		})
	}
}

func TestGenerateWriteFailureIsJSON(t *testing.T) {
	dir := jsonExtension(t, "")
	clitest.WriteTestFile(t, filepath.Join(dir, "gen"), "not a directory")
	assertGenerateJSONError(t, func() { GenerateProject([]string{"--config=gofastr.settings.yml", "--no-clean", "--json"}) })
}

func TestGenerateRebuildFailureIsOneJSON(t *testing.T) {
	dir := jsonExtension(t, "")
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"), "module example.com/jsonfail\n\ngo 1.26\n")
	if err := os.MkdirAll(filepath.Join(dir, "entities", "client"), 0o700); err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "entities", "broken.go"), "not valid Go")
	clitest.WriteTestFile(t, filepath.Join(dir, appClientPath), "package client\n")
	assertGenerateJSONError(t, func() { GenerateProject([]string{"--config=gofastr.settings.yml", "--json"}) })
	if _, err := os.Stat(filepath.Join(dir, "gen", "report.go")); err != nil {
		t.Fatalf("engine did not reach the late rebuild: %v", err)
	}
}

func TestSDKWriteFailureIsJSON(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	clitest.WriteTestFile(t, "go.mod", "module example.com/jsonsdk\n\ngo 1.26\n")
	clitest.WriteTestFile(t, "app.yml", addTestAppSpec("example.com/jsonsdk"))
	clitest.CovT_capStdout(t, func() { clitest.CovT_capExit(t, func() { RunGenerateApp([]string{"app.yml", "--json"}) }) })
	clitest.WriteTestFile(t, "sdk-output", "not a directory")
	assertGenerateJSONError(t, func() { RunGenerateSDK([]string{"--out=sdk-output", "--json"}) })
}

func TestAppConflictIsOneJSON(t *testing.T) {
	clitest.CovT_chdir(t, t.TempDir())
	clitest.WriteTestFile(t, "go.mod", "module example.com/jsonconflict\n\ngo 1.26\n")
	clitest.WriteTestFile(t, "app.yml", addTestAppSpec("example.com/jsonconflict"))
	run := func() { RunGenerateApp([]string{"app.yml", "--json"}) }
	clitest.CovT_capStdout(t, func() { clitest.CovT_capExit(t, run) })
	assertGenerateJSONError(t, run)
}

func TestAddEndpointWriteFailureIsJSON(t *testing.T) {
	dir, spec := clitest.AddSetup(t, addTestAppSpec("example.com/addtest"))
	clitest.CovT_capStdout(t, func() {
		clitest.CovT_capExit(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: spec, json: true}) })
	})
	if err := os.Mkdir(filepath.Join(dir, AppEndpointsPath), 0o700); err != nil {
		t.Fatal(err)
	}
	fragment := filepath.Join(dir, "fragment.yml")
	clitest.WriteTestFile(t, fragment, `endpoints:
  - name: publish_post
    entity: posts
    method: POST
    path: "{id}/publish"
    handler: entities/posts_endpoints.go#PublishPost
`)
	assertGenerateJSONError(t, func() { RunGenerateApp([]string{fragment, "--add", "--json"}) })
}
