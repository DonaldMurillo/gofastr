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
