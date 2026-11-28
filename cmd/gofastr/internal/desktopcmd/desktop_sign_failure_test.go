package desktopcmd

import (
	"archive/zip"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSigningFailureKeepsTheUnsignedBundle(t *testing.T) {
	fakeRunTool(t, func(string, []string) (string, error) { return "no identity found", errors.New("exit status 1") })
	output, code, out := buildFixtureBundle(t, "Unsigned", "--sign", "Example Corp")
	if code != -1 {
		t.Fatalf("a signing failure failed the build: exit %d\n%s", code, output)
	}
	if !strings.Contains(output, "stays unsigned") || !strings.Contains(output, "no identity found") {
		t.Errorf("the signing failure was not reported with the tool's output:\n%s", output)
	}
	if _, err := os.Stat(filepath.Join(out, "Unsigned.app", "Contents", "MacOS", "Unsigned")); err != nil {
		t.Errorf("the bundle is gone after a signing failure: %v", err)
	}
}

func TestNotarizedZipCarriesTheStaple(t *testing.T) {
	fakeRunTool(t, func(name string, args []string) (string, error) {
		if name == "xcrun" && args[0] == "stapler" {
			return "", os.WriteFile(filepath.Join(args[len(args)-1], "Contents", "ticket"), []byte("stapled"), 0o600)
		}
		return "", nil
	})
	output, code, out := buildFixtureBundle(t, "Stapled", "--notarize", "--sign", "Developer ID Application: Example Corp (ABCD12345)")
	if code != -1 {
		t.Fatalf("desktop build exited %d\n%s", code, output)
	}
	zr, err := zip.OpenReader(filepath.Join(out, "Stapled.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	for _, f := range zr.File {
		if f.Name == "Stapled.app/Contents/ticket" {
			return
		}
	}
	t.Fatal("the distributed zip was written before the staple")
}
