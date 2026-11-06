package verifycmd

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestVetFailureRootIsAbsolute(t *testing.T) {
	directory := clitest.WriteModule(t, map[string]string{"main.go": "package main\nfunc main() { missing() }\n"})
	directory, err := filepath.EvalSymlinks(directory)
	if err != nil {
		t.Fatal(err)
	}
	t.Chdir(directory)
	for _, root := range []string{".", "./nested/.."} {
		output, code := captureVerify(t, []string{"--json", "--root", root})
		var document struct {
			Root string `json:"root"`
		}
		if err := json.Unmarshal([]byte(output), &document); err != nil || code != 1 {
			t.Fatalf("exit=%d, error=%v, stdout=%s", code, err, output)
		}
		if !filepath.IsAbs(document.Root) || document.Root != directory {
			t.Fatalf("root=%q, want %q", document.Root, directory)
		}
	}
}
