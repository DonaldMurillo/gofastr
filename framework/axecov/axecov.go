// Package axecov is the manifest of pages the axe harness has scanned.
package axecov

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"

	"github.com/DonaldMurillo/gofastr/internal/coverageroot"
	"github.com/DonaldMurillo/gofastr/internal/filelock"
)

// FileName is the manifest's path under the coverage root.
const FileName = ".gofastr/axe-coverage.json"

// Manifest maps every scanned URL path to the color schemes it was
// scanned under.
type Manifest struct {
	Version int                 `json:"version"`
	Pages   map[string][]string `json:"pages"`
}

var mu sync.Mutex

const lockName = ".gofastr/axe-coverage.lock"

// Record merges one scanned page, a URL or a path, into dir's manifest.
func Record(dir, path, scheme string) error {
	p := normalizePath(path)

	mu.Lock()
	defer mu.Unlock()

	file := filepath.Join(dir, FileName)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		return fmt.Errorf("axecov: create %s: %w", filepath.Dir(file), err)
	}
	lock, err := os.OpenFile(filepath.Join(dir, lockName), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return fmt.Errorf("axecov: open lock: %w", err)
	}
	defer lock.Close()
	if err := filelock.Lock(lock); err != nil {
		return fmt.Errorf("axecov: lock manifest: %w", err)
	}

	m, err := readLocked(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		m = &Manifest{Version: 1, Pages: map[string][]string{}}
	}
	if !slices.Contains(m.Pages[p], scheme) {
		m.Pages[p] = append(m.Pages[p], scheme)
		slices.Sort(m.Pages[p])
	}

	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("axecov: encode manifest: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(file), "axe-coverage-*.tmp")
	if err != nil {
		return fmt.Errorf("axecov: write manifest: %w", err)
	}
	_, werr := tmp.Write(append(data, '\n'))
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("axecov: write manifest: %w", werr)
	}
	if err := os.Rename(tmp.Name(), file); err != nil {
		os.Remove(tmp.Name())
		return fmt.Errorf("axecov: replace manifest: %w", err)
	}
	return nil
}

// Read loads dir's manifest; a missing one is fs.ErrNotExist.
func Read(dir string) (*Manifest, error) {
	mu.Lock()
	defer mu.Unlock()
	return readLocked(dir)
}

func readLocked(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("axecov: parse %s: %w", FileName, err)
	}
	if m.Pages == nil {
		m.Pages = map[string][]string{}
	}
	return &m, nil
}

func normalizePath(raw string) string {
	if u, err := url.Parse(raw); err == nil {
		if u.Path != "" {
			return u.Path
		}
		if u.Host != "" {
			return "/"
		}
	}
	if raw == "" {
		return "/"
	}
	return raw
}

// DefaultDir is GOFASTR_AXE_COVERAGE_DIR, else the nearest go.work or
// go.mod directory above the working directory, else the working directory.
func DefaultDir() string {
	return coverageroot.Resolve("GOFASTR_AXE_COVERAGE_DIR")
}
