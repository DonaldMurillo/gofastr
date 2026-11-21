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

	lock, err := coverageroot.LockManifest(filepath.Join(dir, lockName), "axecov")
	if err != nil {
		return err
	}
	defer lock.Close()

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

	return coverageroot.WriteManifest(filepath.Join(dir, FileName), "axecov", m, nil)
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
	return coverageroot.Resolve(coverageroot.AxeDirEnv)
}
