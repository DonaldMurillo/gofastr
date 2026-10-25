package clitest

import (
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/settings"
)

func WriteUnmountedSettings(t *testing.T, dir string, entries ...settings.Unmounted) {
	t.Helper()
	if err := settings.SetUnmounted(filepath.Join(dir, settings.FileName), entries); err != nil {
		t.Fatal(err)
	}
}
