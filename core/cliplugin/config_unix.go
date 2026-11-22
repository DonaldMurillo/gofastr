//go:build !windows

package cliplugin

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

func checkUserConfigReadSafety(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink plugin config %s", path)
	}
	if err := checkOwnerFileMode(path, info.Mode()); err != nil {
		return err
	}
	return checkUserConfigDirSafety(filepath.Dir(path))
}

func checkUserConfigDirSafety(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: plugin config parent is not a directory", dir)
	}
	if perm := info.Mode().Perm(); perm&0o022 != 0 {
		return fmt.Errorf("refusing plugin config in group/world-writable directory %s (mode %04o)", dir, perm)
	}
	return nil
}

func checkOwnerFileMode(path string, mode fs.FileMode) error {
	if perm := mode.Perm(); perm&0o022 != 0 {
		return fmt.Errorf("refusing group/world-writable plugin config %s (mode %04o); run chmod 0600", path, perm)
	}
	return nil
}

func openLockFileNoFollow(lockPath string) (*os.File, error) {
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("lockfile %s is not a regular file", lockPath)
	}
	return f, nil
}
