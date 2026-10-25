package clitest

import (
	"os"
	"path/filepath"
	"testing"
)

func WriteSettings(t *testing.T, dir, body string) {
	t.Helper()
	//gofastr:allow(worldreadable) test fixture in the calling test's temp dir
	if err := os.WriteFile(filepath.Join(dir, "gofastr.settings.yml"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
