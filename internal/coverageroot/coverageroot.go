// Package coverageroot resolves the directory coverage manifests live under.
package coverageroot

import (
	"os"

	"github.com/DonaldMurillo/gofastr/internal/ancestor"
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

// FindUp returns the nearest directory at or above start holding marker within that directory.
func FindUp(start, marker string) (string, bool) {
	dir, found, _ := ancestor.Find(start, func(dir string) (bool, error) {
		root, err := os.OpenRoot(dir)
		if err != nil {
			return false, nil
		}
		defer root.Close()
		_, err = root.Stat(marker)
		return err == nil, nil
	})
	if !found {
		return "", false
	}
	return dir, true
}
