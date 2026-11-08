package settings

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestSectionWritersKeepBothSections(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("OS file locks unavailable")
	}
	p := filepath.Join(t.TempDir(), "gofastr.settings.yml")
	entered, release := make(chan struct{}), make(chan struct{})
	first := make(chan error, 1)
	go func() {
		first <- setSection(p, "endpoints", "# endpoints", "endpoints: []\n", func(File) error { close(entered); <-release; return nil })
	}()
	<-entered
	second := make(chan error, 1)
	go func() {
		second <- setSection(p, "unmounted", "# copies", "unmounted: []\n", func(File) error { return nil })
	}()
	var secondErr error
	early := false
	select {
	case secondErr = <-second:
		early = true
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatal(err)
	}
	if !early {
		secondErr = <-second
	}
	if secondErr != nil {
		t.Fatal(secondErr)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "endpoints:") || !strings.Contains(string(data), "unmounted:") {
		t.Fatalf("concurrent writer lost a section: %s", data)
	}
}

func TestSectionLockStaysInStateDir(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "gofastr.settings.yml")
	if err := setSection(p, "endpoints", "# endpoints", "endpoints: []\n", func(File) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p + ".lock"); !os.IsNotExist(err) {
		t.Errorf("lock file beside the settings file: stat err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".gofastr", "gofastr.settings.yml.lock")); err != nil {
		t.Errorf("lock file not under .gofastr/: %v", err)
	}
}
