package clitest

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWriteModuleTakesNilFiles(t *testing.T) {
	dir := WriteModule(t, nil)
	if _, err := os.Stat(filepath.Join(dir, "go.mod")); err != nil {
		t.Fatal(err)
	}
}
