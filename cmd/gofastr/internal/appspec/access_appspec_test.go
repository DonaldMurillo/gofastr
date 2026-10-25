package appspec

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestAppSpecDecodesAccess(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, clitest.AccessAppSpecYAML())
	bp, err := LoadAppSpec(path)
	if err != nil {
		t.Fatalf("loadAppSpec: %v", err)
	}
	if len(bp.Entities) != 1 {
		t.Fatalf("entities len = %d, want 1", len(bp.Entities))
	}
	want := &entity.AccessDeclaration{
		Read:   "posts:read",
		Create: "posts:write",
		Update: "posts:write",
		Delete: "posts:admin",
	}
	got := bp.Entities[0].Exposure.Access
	if got == nil || *got != *want {
		t.Fatalf("Access = %#v, want %#v", got, want)
	}
}

func TestAppSpecRejectsBadAccessKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "gofastr.yml")
	clitest.WriteTestFile(t, path, `
app:
  name: AccessDemo
entities:
  - name: posts
    access:
      raed: posts:read
    fields:
      - name: title
        type: string
`)
	_, err := LoadAppSpec(path)
	if err == nil || !strings.Contains(err.Error(), `unknown key "raed"`) {
		t.Fatalf("loadAppSpec err = %v", err)
	}
}
