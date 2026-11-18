// Package coverageroot resolves the directory coverage manifests live under.
package coverageroot

import (
	"os"
	"path/filepath"
)

// Resolve returns the first set variable in keys, else the go.work, go.mod or working directory.
func Resolve(keys ...string) string {
	for _, key := range keys {
		if v := os.Getenv(key); v != "" {
			return v
		}
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	for _, marker := range []string{"go.work", "go.mod"} {
		if d, ok := FindUp(wd, marker); ok {
			return d
		}
	}
	return wd
}

// FindUp returns the nearest directory at or above start holding marker.
func FindUp(start, marker string) (string, bool) {
	for d := start; ; {
		if _, err := os.Stat(filepath.Join(d, marker)); err == nil {
			return d, true
		}
		parent := filepath.Dir(d)
		if parent == d {
			return "", false
		}
		d = parent
	}
}
