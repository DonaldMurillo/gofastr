package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
	"github.com/DonaldMurillo/gofastr/framework/file"
)

// A file flag joins the mutation verbs' flag set, so it gets the checks a
// field flag gets: a usable name, not a reserved one, not a duplicate.
func TestCLIUploadFlagChecks(t *testing.T) {
	for name, tc := range map[string]struct {
		fields []framework.FieldDeclaration
		want   string
	}{
		"unusable":  {[]framework.FieldDeclaration{{Name: "9logo", Type: "image"}}, "does not derive a usable CLI flag"},
		"reserved":  {[]framework.FieldDeclaration{{Name: "json", Type: "file"}}, "which is reserved"},
		"duplicate": {[]framework.FieldDeclaration{{Name: "status", Type: "string"}, {Name: "status_ne", Type: "image"}}, "both derive CLI flag --status-ne"},
	} {
		decls := []framework.EntityDeclaration{{Name: "customers", Table: "customers", Fields: tc.fields}}
		_, err := buildCLISpec(decls, defaultCLIOptions(), "example.com/app/entities/client")
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
}

// The CLI offers a file flag for exactly the fields the client has an
// upload slot for: never a read-only, auto-generated or hidden one.
func TestCLIUploadFlagsMatchClient(t *testing.T) {
	decl := uploadDecl()
	decl.Fields = append(decl.Fields, framework.FieldDeclaration{Name: "stamp", Type: "file", AutoGenerate: "uuid"})
	spec, err := buildCLISpec([]framework.EntityDeclaration{decl}, defaultCLIOptions(), "example.com/app/entities/client")
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range spec.Entities[0].Uploads {
		got = append(got, f.Snake)
	}
	if want := []string{"logo", "contract"}; !slices.Equal(got, want) {
		t.Errorf("file flags = %v, want %v", got, want)
	}
}

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
	runGenerateCLI([]string{"--binary=myapp"})
	bin := testExecutablePath(filepath.Join(dir, "myapp"))
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
