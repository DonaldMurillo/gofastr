package testinputs

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestCommandReadsGeneratorSources(t *testing.T) {
	read := Command(t, "github.com/DonaldMurillo/gofastr/cmd/gofastr")
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range []string{"cmd/gofastr", "framework", "core/render"} {
		want := filepath.Join(root, filepath.FromSlash(dir))
		if !slices.Contains(read, want) {
			t.Errorf("did not read %s", want)
		}
	}
	embedded := false
	for _, p := range read {
		if strings.HasPrefix(p, runtime.GOROOT()) {
			t.Errorf("read a standard-library path %s", p)
		}
		if strings.HasPrefix(p, filepath.Join(root, "framework", "docs", "content")+string(filepath.Separator)) {
			embedded = true
		}
	}
	if !embedded {
		t.Error("read no embedded file under framework/docs/content")
	}
}

// fatalTB records Fatalf and stops the goroutine the way testing.T does.
type fatalTB struct {
	testing.TB
	msg string
}

func (f *fatalTB) Helper() {}

func (f *fatalTB) Fatalf(format string, args ...any) {
	f.msg = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

// A command built only from the standard library reads nothing, and an
// empty read would key the cache on nothing, so Command refuses it.
func TestCommandRefusesAnEmptyRead(t *testing.T) {
	tb := &fatalTB{TB: t}
	var read []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		read = Command(tb, "fmt")
	}()
	<-done
	if !strings.Contains(tb.msg, "found no package in the main module") {
		t.Fatalf("Command returned %q, want the empty-read refusal (fatal: %q)", read, tb.msg)
	}
}

// A file added under an embedded directory changes the command but no
// entry in the package directory's listing, so Command lists the
// directories down to each embedded file.
func TestCommandListsEmbeddedDirs(t *testing.T) {
	root := t.TempDir()
	for name, body := range map[string]string{
		"go.mod":               "module example.test\n\ngo 1.27\n",
		"app/main.go":          "package main\n\nimport \"embed\"\n\n//go:embed assets\nvar assets embed.FS\n\nfunc main() { _ = assets }\n",
		"app/assets/sub/a.txt": "a\n",
	} {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(root)
	t.Setenv("GOWORK", "off")
	read := Command(t, "example.test/app")
	for _, dir := range []string{"app/assets", "app/assets/sub"} {
		want, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(dir)))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, p := range read {
			if got, err := filepath.EvalSymlinks(p); err == nil && got == want {
				found = true
			}
		}
		if !found {
			t.Errorf("did not list %s: read %v", dir, read)
		}
	}
}
