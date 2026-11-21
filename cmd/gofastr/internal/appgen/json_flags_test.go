package appgen

import (
	"encoding/json"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestGenerateFlagsAreJSON(t *testing.T) {
	for _, command := range []struct {
		name string
		run  func([]string)
		exit int
	}{
		{"project", GenerateProject, 2},
		{"app", RunGenerateApp, 2},
		{"entity", GenerateScaffoldEntity, 2},
		{"screen", GenerateScaffoldScreen, 2},
		{"sdk", RunGenerateSDK, 2},
		{"cli", RunGenerateCLI, 2},
		{"client", RunGenerateClient, 2},
		{"endpoints", runGenerateEndpoints, 2},
		{"styles", runGenerateStyles, 2},
		{"package", runGeneratePackage, 2},
	} {
		t.Run(command.name, func(t *testing.T) {
			for _, args := range [][]string{{"--json", "--bogus"}, {"--invalid=1", "--json"}} {
				assertGenerateJSONErrorExit(t, command.exit, func() { command.run(args) })
			}
		})
	}
}

func TestGenerateMissingArgsAreJSON(t *testing.T) {
	for _, run := range []func([]string){GenerateScaffoldEntity, GenerateScaffoldScreen} {
		for _, args := range [][]string{{"--out", "--json"}, {"--json"}, {"--from-a11y=", "--json"}} {
			assertGenerateJSONErrorExit(t, 2, func() { run(args) })
		}
	}
}

func TestPackageArgErrorsAreJSON(t *testing.T) {
	for _, args := range [][]string{{"--json"}, {"siteheader", "--json"}, {"--out", "--json"}} {
		assertGenerateJSONErrorExit(t, 2, func() { runGeneratePackage(args) })
	}
}

func assertGenerateJSONError(t *testing.T, run func()) {
	t.Helper()
	assertGenerateJSONErrorExit(t, 1, run)
}

func assertGenerateJSONErrorExit(t *testing.T, wantExit int, run func()) {
	t.Helper()
	code := 0
	output := clitest.CovT_capStdout(t, func() {
		code = clitest.CovT_capExit(t, run)
	})
	var document struct {
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal([]byte(output), &document); err != nil || len(document.Errors) != 1 || document.Errors[0].Message == "" || code != wantExit {
		t.Fatalf("exit=%d (want %d), error=%v, stdout=%s", code, wantExit, err, output)
	}
}
