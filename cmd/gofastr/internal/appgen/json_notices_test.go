package appgen

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func jsonExtension(t *testing.T, diagnostic string) string {
	t.Helper()
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	command := clitest.WriteReportExtension(t, dir, "")
	var argv []string
	if err := json.Unmarshal([]byte(command), &argv); err != nil {
		t.Fatal(err)
	}
	script := argv[len(argv)-1]
	content, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}
	if diagnostic != "" {
		content = []byte(strings.ReplaceAll(string(content), `{"files":`, `{"diagnostics":[{"severity":"`+diagnostic+`","message":"extension notice"}],"files":`))
		if err := os.WriteFile(script, content, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	clitest.WriteTestFile(t, "input.json", `{}`)
	writeCodegenConfig(t, dir, "    - name: custom/report\n      extension: report\n      source:\n        type: json_file\n        path: input.json\n  extensions:\n    - name: report\n      command: "+command+"\n")
	return dir
}

func TestGenerateNoticesKeepJSON(t *testing.T) {
	for _, severity := range []string{"warning", "info", "ERROR"} {
		t.Run(severity, func(t *testing.T) {
			jsonExtension(t, severity)
			run := func() { GenerateProject([]string{"--config=gofastr.settings.yml", "--dry-run", "--json"}) }
			if severity == "ERROR" {
				assertGenerateJSONError(t, run)
				return
			}
			output := clitest.CovT_capStdout(t, func() { clitest.CovT_capExit(t, run) })
			if !json.Valid([]byte(output)) {
				t.Fatalf("notice corrupted JSON: %s", output)
			}
		})
	}
}

func TestGenerateEditedOutputKeepsJSON(t *testing.T) {
	dir := jsonExtension(t, "")
	run := func() { GenerateProject([]string{"--config=gofastr.settings.yml", "--json"}) }
	clitest.CovT_capStdout(t, func() { clitest.CovT_capExit(t, run) })
	clitest.WriteTestFile(t, filepath.Join(dir, "gen", "report.go"), "package reports\n// edited\n")
	output := clitest.CovT_capStdout(t, func() { clitest.CovT_capExit(t, run) })
	if !json.Valid([]byte(output)) {
		t.Fatalf("edit warning corrupted JSON: %s", output)
	}
}

func TestJSONRefusesStylesBeforeWrites(t *testing.T) {
	dir := t.TempDir()
	clitest.CovT_chdir(t, dir)
	writeCodegenConfig(t, dir, "    - name: styles\n")
	assertGenerateJSONError(t, func() { GenerateProject([]string{"--json"}) })
}

func TestForcedAppEditsKeepJSON(t *testing.T) {
	clitest.CovT_chdir(t, t.TempDir())
	clitest.WriteTestFile(t, "go.mod", "module example.com/jsonnotice\n\ngo 1.26\n")
	clitest.WriteTestFile(t, "app.yml", addTestAppSpec("example.com/jsonnotice"))
	run := func() { RunGenerateApp([]string{"app.yml", "--force", "--json"}) }
	clitest.CovT_capStdout(t, func() { clitest.CovT_capExit(t, run) })
	clitest.WriteTestFile(t, "main.go", "package main\n// edited\n")
	output := clitest.CovT_capStdout(t, func() { clitest.CovT_capExit(t, run) })
	if !json.Valid([]byte(output)) {
		t.Fatalf("force warning corrupted JSON: %s", output)
	}
}

func TestSDKEditsKeepJSON(t *testing.T) {
	clitest.CovT_chdir(t, t.TempDir())
	clitest.WriteTestFile(t, "go.mod", "module example.com/jsonnotice\n\ngo 1.26\n")
	clitest.WriteTestFile(t, "app.yml", addTestAppSpec("example.com/jsonnotice"))
	clitest.CovT_capStdout(t, func() { clitest.CovT_capExit(t, func() { RunGenerateApp([]string{"app.yml", "--json"}) }) })
	run := func() { RunGenerateSDK([]string{"--out=sdk-output", "--json"}) }
	clitest.CovT_capStdout(t, func() { clitest.CovT_capExit(t, run) })
	clientPath := filepath.Join("sdk-output", "go", "client.go")
	client, err := os.ReadFile(clientPath)
	if err != nil || len(client) == 0 {
		t.Fatalf("generated SDK client missing: %v", err)
	}
	clitest.WriteTestFile(t, clientPath, string(client)+"\n// edited\n")
	output := clitest.CovT_capStdout(t, func() { clitest.CovT_capExit(t, run) })
	if !json.Valid([]byte(output)) {
		t.Fatalf("SDK edit warning corrupted JSON: %s", output)
	}
}
