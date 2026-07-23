// Package axecov is the axe-coverage manifest shared between the axe test
// harness and strict mode. The harness (framework/testkit/axetest) calls
// [Record] after every successful scan, accumulating which URL paths the
// app's accessibility tests actually exercised; uihost strict mode reads
// the result with [Read] and refuses to serve a page route no axe test
// covers.
//
// The manifest lives at .gofastr/axe-coverage.json relative to the
// directory the axe tests run in — for a normal app that is the project
// root, the same directory the app serves from. `.gofastr/` is a local
// build-artifact directory (gitignored, wiped by `make clean`), so the
// manifest never ships: strict mode only enforces axe coverage in dev,
// where the file exists.
//
// This package is deliberately dependency-free (no chromedp) so
// production code can read manifests without linking a headless browser.
package axecov

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

// FileName is the manifest location relative to the app/test working
// directory.
const FileName = ".gofastr/axe-coverage.json"

// Manifest maps every scanned URL path to the color schemes it was
// scanned under.
type Manifest struct {
	Version int                 `json:"version"`
	Pages   map[string][]string `json:"pages"`
}

// mu serializes read-merge-write cycles within one process. Axe suites
// run their scans from a single test binary, so in-process serialization
// is the contract; two separate suites writing the same directory
// concurrently is not supported.
var mu sync.Mutex

// Record merges one scanned page into dir's manifest, creating the
// manifest (and .gofastr/) on first use. path may be a full URL or a
// bare path — the query string and fragment are stripped, because
// coverage is per screen, not per parameter combination.
func Record(dir, path, scheme string) error {
	p := normalizePath(path)

	mu.Lock()
	defer mu.Unlock()

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

	file := filepath.Join(dir, FileName)
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return fmt.Errorf("axecov: create %s: %w", filepath.Dir(file), err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("axecov: encode manifest: %w", err)
	}
	// Write-then-rename so a reader never sees a torn manifest.
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("axecov: write manifest: %w", err)
	}
	if err := os.Rename(tmp, file); err != nil {
		return fmt.Errorf("axecov: replace manifest: %w", err)
	}
	return nil
}

// Read loads dir's manifest. A missing manifest returns an error
// satisfying errors.Is(err, fs.ErrNotExist) — callers decide whether
// absence is fatal (strict dev) or fine (production).
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

// normalizePath reduces a scanned URL to its screen path: scheme, host,
// query, and fragment are dropped; empty becomes "/".
func normalizePath(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Path != "" {
		return u.Path
	}
	if raw == "" {
		return "/"
	}
	return raw
}
