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
	}{
		{"project", GenerateProject},
		{"app", RunGenerateApp},
		{"entity", GenerateScaffoldEntity},
		{"screen", GenerateScaffoldScreen},
		{"sdk", RunGenerateSDK},
		{"cli", RunGenerateCLI},
		{"client", RunGenerateClient},
		{"endpoints", runGenerateEndpoints},
		{"styles", runGenerateStyles},
	} {
		t.Run(command.name, func(t *testing.T) {
			for _, args := range [][]string{{"--json", "--bogus"}, {"--invalid=1", "--json"}} {
				assertGenerateJSONError(t, func() { command.run(args) })
			}
		})
	}
}

func TestGenerateMissingArgsAreJSON(t *testing.T) {
	for _, run := range []func([]string){GenerateScaffoldEntity, GenerateScaffoldScreen} {
		for _, args := range [][]string{{"--out", "--json"}, {"--json"}, {"--from-a11y=", "--json"}} {
			assertGenerateJSONError(t, func() { run(args) })
		}
	}
}

func assertGenerateJSONError(t *testing.T, run func()) {
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
	if err := json.Unmarshal([]byte(output), &document); err != nil || len(document.Errors) != 1 || document.Errors[0].Message == "" || code != 1 {
		t.Fatalf("exit=%d, error=%v, stdout=%s", code, err, output)
	}
}
