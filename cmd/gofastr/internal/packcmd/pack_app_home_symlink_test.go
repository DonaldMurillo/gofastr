package packcmd

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// packReadAppHome parses only regular files: an app.go that is a
// symlink, even to a file inside the app, is not read, the same as the
// per-screen files the directory loop filters.
func TestAppHomeSkipsSymlinkedAppGo(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	dir := t.TempDir()
	const src = `package main

func marketingHeader() {
	if u := handler.GetUser(ctx); u != nil {
		_ = ui.Link{Href: "/linked"}
	}
}
`
	if err := os.WriteFile(filepath.Join(dir, "real.txt"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.go"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := packReadAppHome(dir); got != "/linked" {
		t.Fatalf("regular app.go: home = %q, want /linked (the fixture does not parse)", got)
	}
	if err := os.Remove(filepath.Join(dir, "app.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real.txt", filepath.Join(dir, "app.go")); err != nil {
		t.Fatal(err)
	}
	if got := packReadAppHome(dir); got != "" {
		t.Fatalf("symlinked app.go parsed: home = %q", got)
	}
}
