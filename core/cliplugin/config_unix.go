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
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s: plugin config must be a regular file", path)
	}
	if err := checkOwnerFileMode(path, info.Mode()); err != nil {
		return err
	}
	if err := checkOwnerUID(path, info); err != nil {
		return err
	}
	return checkUserConfigDirSafety(filepath.Dir(path))
}

// checkUserConfigDirSafety verifies the registry directory AND every ancestor
// up to the filesystem root (along both the logical path and any symlink
// target ancestor chains) so another user cannot replace an intermediate
// directory component or plant an untrusted symlink along the path.
func checkUserConfigDirSafety(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	euid := uint32(os.Geteuid())
	visited := map[string]bool{}
	if err := checkDirChain(abs, euid, false, visited); err != nil {
		return err
	}
	realDir, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return err
	}
	if realDir != abs {
		return checkDirChain(realDir, euid, false, visited)
	}
	return nil
}

func checkDirChain(start string, euid uint32, allowImmediateSymlink bool, visited map[string]bool) error {
	if visited[start] {
		return nil
	}
	visited[start] = true

	cur := start
	for i := 0; ; i++ {
		linfo, err := os.Lstat(cur)
		if err != nil {
			return err
		}
		if linfo.Mode()&os.ModeSymlink != 0 {
			// Refuse any symlink at the immediate registry directory or any
			// symlink not owned by root or the current user (OS system links
			// like macOS /var -> private/var are root-owned).
			if i == 0 && !allowImmediateSymlink {
				return fmt.Errorf("refusing symlink plugin config directory %s", cur)
			}
			if st, ok := linfo.Sys().(*syscall.Stat_t); ok && st.Uid != 0 && st.Uid != euid {
				return fmt.Errorf("refusing untrusted symlink ancestor %s for plugin config", cur)
			}
			// Also verify the symlink target's own ancestor chain so a trusted
			// symlink pointing under a non-sticky writable directory is refused.
			if resolved, err := filepath.EvalSymlinks(cur); err == nil && resolved != cur {
				if err := checkDirChain(resolved, euid, true, visited); err != nil {
					return err
				}
			}
		}

		info, err := os.Stat(cur)
		if err != nil {
			return err
		}
		if !info.IsDir() {
			return fmt.Errorf("%s: plugin config ancestor is not a directory", cur)
		}
		perm := info.Mode().Perm()
		if i == 0 && !allowImmediateSymlink {
			// The immediate registry directory must never be group/world-writable
			// (even if sticky) and must belong to the current user or root.
			if perm&0o022 != 0 {
				return fmt.Errorf("refusing plugin config in group/world-writable directory %s (mode %04o)", cur, perm)
			}
			if err := checkOwnerUID(cur, info); err != nil {
				return err
			}
		} else {
			// Higher ancestors may be sticky shared roots (e.g. /tmp 01777),
			// but a non-sticky group/world-writable ancestor lets another user
			// rename our subtree and substitute a forged registry.
			if perm&0o022 != 0 && info.Mode()&os.ModeSticky == 0 {
				return fmt.Errorf("refusing plugin config under non-sticky group/world-writable ancestor %s (mode %04o)", cur, perm)
			}
			if info.Mode()&os.ModeSticky == 0 {
				if err := checkOwnerUID(cur, info); err != nil {
					return err
				}
			}
		}

		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}
	return nil
}

func checkOpenConfigSafety(path string, _ *os.File, info fs.FileInfo) error {
	if err := checkOwnerFileMode(path, info.Mode()); err != nil {
		return err
	}
	return checkOwnerUID(path, info)
}

func checkOwnerFileMode(path string, mode fs.FileMode) error {
	if perm := mode.Perm(); perm&0o022 != 0 {
		return fmt.Errorf("refusing group/world-writable plugin config %s (mode %04o); run chmod 0600", path, perm)
	}
	return nil
}

func checkOwnerUID(path string, info fs.FileInfo) error {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	euid := uint32(os.Geteuid())
	if st.Uid != euid && st.Uid != 0 {
		return fmt.Errorf("refusing plugin config path %s owned by uid %d (expected %d or 0)", path, st.Uid, euid)
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
