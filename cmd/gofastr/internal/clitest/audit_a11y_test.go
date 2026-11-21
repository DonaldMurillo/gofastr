package clitest

import (
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteA11yTree(t *testing.T) {
	t.Run("nil map", func(t *testing.T) {
		dir := WriteA11yTree(t, nil)
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Errorf("ReadDir = %v, %v, want an empty directory", entries, err)
		}
	})
	t.Run("unchanged map", func(t *testing.T) {
		const name = "screens/example.ui.go"
		files := map[string]string{name: BadScreen}
		dir := WriteA11yTree(t, files)
		if !maps.Equal(files, map[string]string{name: BadScreen}) {
			t.Errorf("WriteA11yTree changed input map to %v", files)
		}
		if got := ReadFile(t, dir, name); got != BadScreen {
			t.Errorf("fixture content = %q, want %q", got, BadScreen)
		}
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("go.mod stat error = %v, want file not exist", err)
		}
	})
}
