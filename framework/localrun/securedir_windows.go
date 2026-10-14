//go:build windows

package localrun

import (
	"fmt"
	"os"

	"github.com/DonaldMurillo/gofastr/internal/fileperm"
)

// secureDir refuses a registry directory that is a reparse point or not
// a directory, and gives it an owner-only DACL.
func secureDir(dir string) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("localrun: %w", err)
	}
	if !fi.IsDir() {
		return fmt.Errorf("localrun: %s is not a directory", dir)
	}
	if err := fileperm.Restrict(dir, true); err != nil {
		return fmt.Errorf("localrun: %w", err)
	}
	return nil
}
