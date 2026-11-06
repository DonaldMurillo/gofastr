package desktopcmd

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMacBuildDropsStaleBundleFiles(t *testing.T) {
	_, code, out := buildFixtureBundle(t, "Clean", "--no-sign")
	if code != -1 {
		t.Fatalf("initial build exited %d", code)
	}
	bundle := filepath.Join(out, "Clean.app")
	stale := []string{"Contents/_CodeSignature/CodeResources", "Contents/Resources/retired.txt"}
	for _, relative := range stale {
		path := filepath.Join(bundle, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("stale"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	_, code = verbOutput(t, func() {
		runDesktopBuildForOS([]string{"--id=dev.gofastr.clean", "--name=Clean", "--pkg=.", "--no-sign", "-o", out}, "darwin")
	})
	if code != -1 {
		t.Fatalf("rebuild exited %d", code)
	}
	for _, relative := range stale {
		if _, err := os.Stat(filepath.Join(bundle, relative)); !os.IsNotExist(err) {
			t.Errorf("stale bundle file %s survived: %v", relative, err)
		}
	}
}
