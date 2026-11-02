package newcmd

import (
	"os"
	"path/filepath"
	"testing"
)

// Property: `gofastr new handler` writes <name>_handler.go inside the
// directory it scaffolds into. The directory itself was resolved
// (EvalSymlinks) but the file was not: a cloned repo carrying
// widget_handler.go -> ~/.bashrc had `new handler Widget --overwrite`
// replace the user's shell profile, and a dangling one read as "no file
// yet" and was created wherever it pointed. A link that stays in the
// project is still written through.
func TestNewHandlerStaysInsideTheProject(t *testing.T) {
	const precious = "precious user content\n"
	symlink := func(t *testing.T, target, link string) {
		t.Helper()
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	t.Run("linked out, --overwrite", func(t *testing.T) {
		dir := t.TempDir()
		victim := filepath.Join(t.TempDir(), "victim")
		if err := os.WriteFile(victim, []byte(precious), 0o644); err != nil {
			t.Fatal(err)
		}
		symlink(t, victim, filepath.Join(dir, "widget_handler.go"))
		_ = scaffoldHandler(dir, "Widget", "GET", "/widget", true)
		if got, _ := os.ReadFile(victim); string(got) != precious {
			t.Errorf("SECURITY: [newhandler-symlink-escape] new handler --overwrite rewrote a file outside the project: %.60q", got)
		}
	})
	t.Run("dangling out", func(t *testing.T) {
		dir := t.TempDir()
		target := filepath.Join(t.TempDir(), "created")
		symlink(t, target, filepath.Join(dir, "widget_handler.go"))
		_ = scaffoldHandler(dir, "Widget", "GET", "/widget", false)
		if _, err := os.Lstat(target); err == nil {
			t.Error("SECURITY: [newhandler-symlink-escape] new handler created a file outside the project through a dangling link")
		}
	})
	t.Run("in-project link is followed", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.MkdirAll(filepath.Join(dir, "handlers"), 0o755); err != nil {
			t.Fatal(err)
		}
		real := filepath.Join(dir, "handlers", "widget.go")
		if err := os.WriteFile(real, []byte("package main\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		symlink(t, filepath.Join("handlers", "widget.go"), filepath.Join(dir, "widget_handler.go"))
		if err := scaffoldHandler(dir, "Widget", "GET", "/widget", true); err != nil {
			t.Fatalf("new handler --overwrite through an in-project link: %v", err)
		}
		if got, _ := os.ReadFile(real); string(got) == "package main\n" {
			t.Error("the in-project link target was not rewritten")
		}
	})
}
