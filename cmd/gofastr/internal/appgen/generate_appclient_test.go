package appgen

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestAddRebuildsClient(t *testing.T) {
	dir, bp := clitest.AddSetup(t, addTestAppSpec("example.com/addtest"))
	clitest.CovT_capStdout(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: bp}) })
	frag := filepath.Join(dir, "fragment.yml")
	if err := os.WriteFile(frag, []byte(addFragmentAppSpec()), 0o600); err != nil {
		t.Fatal(err)
	}
	out := clitest.CovT_capStdout(t, func() { GenerateFromAppSpec(GenerateOptions{Spec: frag, add: true}) })
	client := clitest.ReadFile(t, dir, "entities/client/client.go")
	if !strings.Contains(client, "ListComments(") || !strings.Contains(client, "ListPosts(") {
		t.Fatalf("client after --add must cover old and new entities:\n%s", out)
	}
}

// An unknown flag is bad usage, exit 2, the same status the rest of the
// generate family answers; `generate client` used to exit 1.
func TestGenerateClientUnknownFlagExitsTwo(t *testing.T) {
	code := clitest.CovT_capExit(t, func() {
		clitest.CovT_capStdout(t, func() { RunGenerateClient([]string{"--zzz-unknown-flag=1"}) })
	})
	if code != 2 {
		t.Fatalf("generate client --zzz exited %d, want 2", code)
	}
}
