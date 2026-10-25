package generatecmd

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appgen"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework/file"
)

// The CLI refuses an upload it cannot send before it opens a connection:
// two flags reading stdin, a file over the server's per-file limit, and a
// path that does not exist.
func TestCLIUploadRefusals(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the generated CLI")
	}
	dir := cliModuleFromYAML(t, `app:
  name: myapp
entities:
  - name: customers
    crud: true
    fields:
      - name: name
        type: string
        required: true
      - name: logo
        type: image
      - name: contract
        type: file
`)
	appgen.RunGenerateCLI([]string{"--binary=myapp"})
	bin := clitest.TestExecutablePath(filepath.Join(dir, "myapp"))
	build := exec.Command("go", "build", "-o", bin, "./cmd/myapp")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build CLI: %v\n%s", err, out)
	}

	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusTeapot)
	}))
	defer srv.Close()

	big := filepath.Join(dir, "big.png")
	f, err := os.Create(big)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(file.MaxProcessFileSize + 1); err != nil {
		t.Fatal(err)
	}
	f.Close()

	home := t.TempDir()
	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"stdin twice": {[]string{"--logo", "-", "--contract", "-"}, "both read stdin"},
		"too big":     {[]string{"--logo", big}, "over the server's limit of 32 MiB per file"},
		"missing":     {[]string{"--logo", filepath.Join(dir, "nope.png")}, "nope.png"},
	} {
		cmd := exec.Command(bin, append([]string{"customers", "create", "--name", "Acme"}, tc.args...)...)
		cmd.Env = append(os.Environ(), "HOME="+home, "XDG_CONFIG_HOME="+home, "MYAPP_URL="+srv.URL, "MYAPP_TOKEN=t")
		cmd.Stdin = strings.NewReader("PNG")
		out, err := cmd.CombinedOutput()
		if err == nil || !strings.Contains(string(out), tc.want) {
			t.Errorf("%s: err = %v, output %q, want %q", name, err, out, tc.want)
		}
	}
	if n := hits.Load(); n != 0 {
		t.Fatalf("%d refused uploads reached the server", n)
	}
}
