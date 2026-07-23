package axecov

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestRecordMergesPathsAndSchemes(t *testing.T) {
	dir := t.TempDir()
	if err := Record(dir, "/docs", "dark"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := Record(dir, "/docs", "light"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := Record(dir, "/docs", "dark"); err != nil { // duplicate stays deduped
		t.Fatalf("record: %v", err)
	}
	if err := Record(dir, "/", "dark"); err != nil {
		t.Fatalf("record: %v", err)
	}

	m, err := Read(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if m.Version != 1 {
		t.Fatalf("version = %d, want 1", m.Version)
	}
	if got := m.Pages["/docs"]; len(got) != 2 {
		t.Fatalf("/docs schemes = %v, want [dark light]", got)
	}
	if _, ok := m.Pages["/"]; !ok {
		t.Fatalf("missing / entry: %v", m.Pages)
	}
}

func TestReadMissingManifestReturnsNotExist(t *testing.T) {
	_, err := Read(t.TempDir())
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v, want fs.ErrNotExist", err)
	}
}

func TestRecordNormalizesPath(t *testing.T) {
	dir := t.TempDir()
	// Query strings and fragments identify the same screen; empty paths
	// are the root.
	if err := Record(dir, "/pricing?tab=teams", "dark"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := Record(dir, "", "dark"); err != nil {
		t.Fatalf("record: %v", err)
	}
	m, err := Read(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if _, ok := m.Pages["/pricing"]; !ok {
		t.Fatalf("query string not stripped: %v", m.Pages)
	}
	if _, ok := m.Pages["/"]; !ok {
		t.Fatalf("empty path not normalized to /: %v", m.Pages)
	}
}

func TestRecordConcurrentLosesNothing(t *testing.T) {
	dir := t.TempDir()
	var wg sync.WaitGroup
	paths := []string{"/a", "/b", "/c", "/d", "/e", "/f", "/g", "/h"}
	for _, p := range paths {
		wg.Add(1)
		go func(p string) {
			defer wg.Done()
			if err := Record(dir, p, "dark"); err != nil {
				t.Errorf("record %s: %v", p, err)
			}
		}(p)
	}
	wg.Wait()
	m, err := Read(dir)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	for _, p := range paths {
		if _, ok := m.Pages[p]; !ok {
			t.Fatalf("lost %s under concurrency: %v", p, m.Pages)
		}
	}
}

func TestManifestLandsAtDocumentedLocation(t *testing.T) {
	dir := t.TempDir()
	if err := Record(dir, "/", "dark"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, FileName)); err != nil {
		t.Fatalf("manifest not at %s: %v", FileName, err)
	}
}
