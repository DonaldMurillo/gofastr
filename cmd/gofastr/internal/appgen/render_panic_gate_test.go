package appgen

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestGeneratedE2ERejectsRecoveredRenderPanic(t *testing.T) {
	root := clitest.RepoRootDir(t)
	dir := t.TempDir()
	version, err := clitest.RepoGoVersion(root)
	if err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "go.mod"), "module local/renderprobe\n\ngo "+version+"\nrequire github.com/DonaldMurillo/gofastr v0.0.0\nreplace github.com/DonaldMurillo/gofastr => "+root+"\n")
	if err := clitest.CopyGoSum(root, dir); err != nil {
		t.Fatal(err)
	}
	clitest.WriteTestFile(t, filepath.Join(dir, "e2e_test.go"), renderAppSpecE2ETest(appspec.AppSpec{}))
	clitest.WriteTestFile(t, filepath.Join(dir, "main.go"), `package main
import (
 "fmt"
 "net/http"
 "os"
 "strings"
 "github.com/DonaldMurillo/gofastr/core-ui/component"
 "github.com/DonaldMurillo/gofastr/core/render"
)
type brokenSidebar struct{}
func (brokenSidebar) Render() render.HTML { if os.Getenv("RENDER_PROBE_BROKEN") == "1" { panic("sidebar exploded") }; return "GoFastr" }
func (brokenSidebar) RenderError(error) render.HTML { return "GoFastr" }
func main() {
 http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
  html, _ := component.SafeRenderCtx(r.Context(), brokenSidebar{})
  fmt.Fprint(w, html, strings.Repeat("padding", 30))
 })
 if err := http.ListenAndServe(os.Getenv("PORT"), nil); err != nil { panic(err) }
}
`)
	for _, broken := range []string{"0", "1"} {
		cmd := exec.Command("go", "test", "-mod=mod", "-run", "^TestE2E$", "-count=1")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "RENDER_PROBE_BROKEN="+broken, "GOFLAGS=-mod=mod")
		output, err := cmd.CombinedOutput()
		if broken == "0" && err != nil {
			t.Fatalf("healthy generated suite failed: %v\n%s", err, output)
		}
		if broken == "1" && (err == nil || !strings.Contains(string(output), "brokenSidebar: sidebar exploded")) {
			t.Fatalf("generated suite accepted recovered panic or failed for wrong reason: %v\n%s", err, output)
		}
	}
}
