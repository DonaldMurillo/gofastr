package atomicfile

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
)

// Replace renames source over target without removing target first.
func Replace(source, target string) error {
	rename := func() error { return os.Rename(source, target) }
	sourceDir, err := filepath.Abs(filepath.Dir(source))
	if err != nil {
		return err
	}
	targetDir, err := filepath.Abs(filepath.Dir(target))
	if err != nil {
		return err
	}
	if sourceDir == targetDir {
		root, err := os.OpenRoot(targetDir)
		if err != nil {
			return err
		}
		defer root.Close()
		// Root.Rename uses Windows' POSIX replacement semantics. Unlike
		// MoveFileEx, it permits readers that granted FILE_SHARE_DELETE
		// to keep reading the previous file after replacement.
		rename = func() error { return root.Rename(filepath.Base(source), filepath.Base(target)) }
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for {
		err := rename()
		transient := errors.Is(err, syscall.ERROR_ACCESS_DENIED) ||
			errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
		if err == nil || !transient || !time.Now().Before(deadline) {
			return err
		}
		time.Sleep(5 * time.Millisecond)
	}
}
