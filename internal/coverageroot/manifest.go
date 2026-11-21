package coverageroot

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/DonaldMurillo/gofastr/internal/atomicfile"
	"github.com/DonaldMurillo/gofastr/internal/filelock"
)

// LockManifest holds an exclusive lock on path until the caller closes the file.
func LockManifest(path, label string) (*os.File, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("%s: create %s: %w", label, dir, err)
	}
	lock, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("%s: open lock: %w", label, err)
	}
	if err := filelock.Lock(lock); err != nil {
		lock.Close()
		return nil, fmt.Errorf("%s: lock manifest: %w", label, err)
	}
	return lock, nil
}

// WriteManifest replaces the manifest entry with indented JSON and a trailing newline.
func WriteManifest(path, label string, manifest any, prepare func(*os.File) error) error {
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("%s: encode manifest: %w", label, err)
	}
	stage, err := atomicfile.WriteFileNoFollow(path, append(data, '\n'), prepare)
	if err != nil {
		if stage == atomicfile.WriteReplace {
			return fmt.Errorf("%s: replace manifest: %w", label, err)
		}
		return fmt.Errorf("%s: write manifest: %w", label, err)
	}
	return nil
}
