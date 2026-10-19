package testinputs

import (
	"fmt"
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
