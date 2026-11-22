//go:build windows

package cliplugin

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

const fileDeleteChild = 0x00000040

const (
	windowsFileWriteRiskMask windows.ACCESS_MASK = windows.FILE_WRITE_DATA |
		windows.FILE_APPEND_DATA |
		windows.FILE_WRITE_EA |
		windows.FILE_WRITE_ATTRIBUTES |
		windows.DELETE |
		windows.WRITE_DAC |
		windows.WRITE_OWNER |
		windows.GENERIC_WRITE |
		windows.GENERIC_ALL

	windowsDirWriteRiskMask = windowsFileWriteRiskMask | fileDeleteChild

	// Volume roots (e.g. C:\) grant BUILTIN\Users FILE_APPEND_DATA ("Create
	// Folders") by default on Windows, which cannot rename or replace existing
	// subtrees such as C:\Users. Still refuse destructive or takeover rights.
	windowsVolumeRootWriteRiskMask windows.ACCESS_MASK = windows.FILE_WRITE_DATA |
		windows.DELETE |
		fileDeleteChild |
		windows.WRITE_DAC |
		windows.WRITE_OWNER |
		windows.GENERIC_WRITE |
		windows.GENERIC_ALL
)

func checkUserConfigReadSafety(path string) error {
	reparse, err := isWindowsReparsePoint(path)
	if err != nil {
		return err
	}
	if reparse {
		return fmt.Errorf("refusing symlink or reparse point plugin config %s", path)
	}
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
	if err := checkWindowsPathDACL(path, windowsFileWriteRiskMask); err != nil {
		return err
	}
	return checkUserConfigDirSafety(filepath.Dir(path))
}

// checkUserConfigDirSafety verifies the registry directory AND every ancestor
// up to the volume root (along both the logical path and any reparse-point
// target chains) against untrusted reparse points, junctions, and writable DACLs.
func checkUserConfigDirSafety(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	visited := map[string]bool{}
	if err := checkWindowsDirChain(abs, false, visited); err != nil {
		return err
	}
	realDir, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return err
	}
	if realDir != abs {
		return checkWindowsDirChain(realDir, false, visited)
	}
	return nil
}

func checkWindowsDirChain(start string, allowImmediateReparse bool, visited map[string]bool) error {
	if visited[start] {
		return nil
	}
	visited[start] = true

	cur := start
	for i := 0; ; i++ {
		reparse, err := isWindowsReparsePoint(cur)
		if err != nil {
			return err
		}
		if reparse {
			if i == 0 && !allowImmediateReparse {
				return fmt.Errorf("refusing symlink or junction plugin config directory %s", cur)
			}
			// Verify the reparse point's own owner/DACL without following it,
			// and also inspect the resolved target's own ancestor chain.
			if err := checkWindowsReparsePointDACL(cur); err != nil {
				return fmt.Errorf("refusing untrusted reparse point ancestor %s: %w", cur, err)
			}
			if resolved, err := filepath.EvalSymlinks(cur); err == nil && resolved != cur {
				if err := checkWindowsDirChain(resolved, true, visited); err != nil {
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

		parent := filepath.Dir(cur)
		riskMask := windowsDirWriteRiskMask
		if i > 0 && parent == cur {
			riskMask = windowsVolumeRootWriteRiskMask
		}
		if err := checkWindowsPathDACL(cur, riskMask); err != nil {
			if i > 0 {
				return fmt.Errorf("plugin config ancestor %s: %w", cur, err)
			}
			return err
		}

		if parent == cur {
			break
		}
		cur = parent
	}
	return nil
}

func isWindowsReparsePoint(path string) (bool, error) {
	p16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false, err
	}
	attrs, err := windows.GetFileAttributes(p16)
	if err != nil {
		if err == windows.ERROR_FILE_NOT_FOUND || err == windows.ERROR_PATH_NOT_FOUND {
			return false, &os.PathError{Op: "GetFileAttributes", Path: path, Err: os.ErrNotExist}
		}
		return false, &os.PathError{Op: "GetFileAttributes", Path: path, Err: err}
	}
	return attrs&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0, nil
}

func checkOpenConfigSafety(path string, f *os.File, _ fs.FileInfo) error {
	defer runtime.KeepAlive(f)
	sd, err := windows.GetSecurityInfo(
		windows.Handle(f.Fd()),
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("inspect plugin config ACL for %s: %w", path, err)
	}
	return validateWindowsSD(path, sd, windowsFileWriteRiskMask)
}

func checkWindowsReparsePointDACL(path string) error {
	p16, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(
		p16,
		windows.READ_CONTROL,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return fmt.Errorf("open reparse point %s: %w", path, err)
	}
	defer windows.CloseHandle(h)
	sd, err := windows.GetSecurityInfo(
		h,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("inspect reparse point ACL for %s: %w", path, err)
	}
	return validateWindowsSD(path, sd, windowsDirWriteRiskMask)
}

func checkWindowsPathDACL(path string, riskMask windows.ACCESS_MASK) error {
	sd, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("inspect plugin config ACL for %s: %w", path, err)
	}
	return validateWindowsSD(path, sd, riskMask)
}

func validateWindowsSD(path string, sd *windows.SECURITY_DESCRIPTOR, riskMask windows.ACCESS_MASK) error {
	if sd == nil {
		return fmt.Errorf("refusing plugin config %s with missing security descriptor", path)
	}
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		return fmt.Errorf("resolve current Windows user SID: %w", err)
	}
	userSID := tu.User.Sid

	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("read owner SID for %s: %w", path, err)
	}
	if !isTrustedWindowsSID(owner, userSID) {
		return fmt.Errorf("refusing plugin config %s owned by untrusted SID %s", path, owner.String())
	}

	dacl, _, err := sd.DACL()
	if err != nil {
		return fmt.Errorf("read DACL for %s: %w", path, err)
	}
	if dacl == nil {
		return fmt.Errorf("refusing plugin config %s with unrestricted NULL DACL", path)
	}

	for i := uint32(0); i < uint32(dacl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(dacl, i, &ace); err != nil {
			return fmt.Errorf("read ACE %d for %s: %w", i, path, err)
		}
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE {
			continue
		}
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE != 0 {
			continue
		}
		if ace.Mask&riskMask == 0 {
			continue
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if isTrustedWindowsSID(sid, userSID) || sid.IsWellKnown(windows.WinCreatorOwnerSid) {
			continue
		}
		return fmt.Errorf("refusing plugin config %s writable by untrusted SID %s (mask 0x%x)", path, sid.String(), uint32(ace.Mask))
	}
	return nil
}

func isTrustedWindowsSID(sid, currentUser *windows.SID) bool {
	if sid == nil {
		return false
	}
	if currentUser != nil && sid.Equals(currentUser) {
		return true
	}
	return sid.IsWellKnown(windows.WinLocalSystemSid) || sid.IsWellKnown(windows.WinBuiltinAdministratorsSid)
}

func openLockFileNoFollow(lockPath string) (*os.File, error) {
	if reparse, err := isWindowsReparsePoint(lockPath); err == nil && reparse {
		return nil, fmt.Errorf("refusing symlink or reparse point plugin config lock at %s", lockPath)
	}
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
