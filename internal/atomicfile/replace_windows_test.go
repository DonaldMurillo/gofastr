package atomicfile

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReplaceWaitsForWindowsReader(t *testing.T) {
	dir := t.TempDir()
	source, target := filepath.Join(dir, "next"), filepath.Join(dir, "current")
	for path, content := range map[string]string{source: "new", target: "old"} {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	closed := make(chan struct{})
	go func() {
		time.Sleep(25 * time.Millisecond)
		f.Close()
		close(closed)
	}()
	err = Replace(source, target)
	<-closed
	if err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(target)
	if err != nil || string(body) != "new" {
		t.Fatalf("replacement = %q, %v", body, err)
	}
	if _, err := os.Stat(source); !os.IsNotExist(err) {
		t.Fatalf("replacement left source behind: %v", err)
	}
}

func TestReplaceFailureKeepsBothFiles(t *testing.T) {
	dir := t.TempDir()
	source, target := filepath.Join(dir, "next"), filepath.Join(dir, "current")
	if err := os.WriteFile(source, []byte("new"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(target)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	started := time.Now()
	if err := Replace(source, target); err == nil {
		t.Fatal("replaced a file whose reader denies delete sharing")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("replacement did not bound its wait: %s", elapsed)
	}
	for path, want := range map[string]string{source: "new", target: "old"} {
		body, err := os.ReadFile(path)
		if err != nil || string(body) != want {
			t.Fatalf("failed replacement changed %s: %q, %v", path, body, err)
		}
	}
}
