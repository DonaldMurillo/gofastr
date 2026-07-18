package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPkgBuildsCommandPackageFromProjectRoot(t *testing.T) {
	dir := t.TempDir()
	devPkgWrite(t, filepath.Join(dir, "go.mod"), "module buildpkgtest\n\ngo 1.21\n")
	devPkgWrite(t, filepath.Join(dir, "internal", "message", "message.go"),
		"package message\n\nfunc Text() string { return \"ok\" }\n")
	devPkgWrite(t, filepath.Join(dir, "cmd", "example", "main.go"),
		"package main\n\nimport (\n\t\"fmt\"\n\t\"buildpkgtest/internal/message\"\n)\n\nfunc main() { fmt.Print(message.Text()) }\n")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	output := filepath.Join(dir, "example")
	code := covT_capExit(t, func() {
		runBuild([]string{"--no-generate", "--no-a11y", "--pkg", "./cmd/example", "--output=" + output})
	})
	if code != -1 {
		t.Fatalf("build --pkg exited %d", code)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("expected command binary at %s: %v", output, err)
	}
}

func TestBuildPkgAcceptsEqualsForm(t *testing.T) {
	opts, err := parseBuildOptions([]string{"--pkg=./cmd/example"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.pkg != "./cmd/example" {
		t.Fatalf("pkg = %q, want ./cmd/example", opts.pkg)
	}
}

func TestBuildPkgMissingValueIsTargetSpecific(t *testing.T) {
	_, err := parseBuildOptions([]string{"--pkg"})
	if err == nil {
		t.Fatal("expected missing --pkg value to fail")
	}
	if !strings.Contains(err.Error(), "--pkg") {
		t.Fatalf("error %q does not identify --pkg", err)
	}
}
