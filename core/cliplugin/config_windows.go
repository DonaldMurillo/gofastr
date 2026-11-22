//go:build windows

package cliplugin

import (
	"fmt"
	"io/fs"
	"os"
)

func checkUserConfigReadSafety(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink plugin config %s", path)
	}
	return nil
}

func checkUserConfigDirSafety(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: plugin config parent is not a directory", dir)
	}
	return nil
}

func checkOwnerFileMode(_ string, _ fs.FileMode) error {
	return nil
}

func openLockFileNoFollow(lockPath string) (*os.File, error) {
	if err := refuseSymlinkIfPresent(lockPath, "plugin config lock"); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
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
