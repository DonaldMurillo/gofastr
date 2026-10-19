package testinputs

import (
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
