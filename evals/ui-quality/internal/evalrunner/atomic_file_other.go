//go:build !windows

package evalrunner

import "os"

func replaceFileAtomic(source, destination string) error {
	return os.Rename(source, destination)
}
