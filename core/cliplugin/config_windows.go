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
	if err := checkWindowsPathDACL(path, false); err != nil {
		return err
	}
	return checkUserConfigDirSafety(filepath.Dir(path))
}

func checkUserConfigDirSafety(dir string) error {
	linfo, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if linfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("refusing symlink plugin config directory %s", dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s: plugin config parent is not a directory", dir)
	}
	return checkWindowsPathDACL(dir, true)
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
	return validateWindowsSD(path, sd, false)
}

func checkWindowsPathDACL(path string, isDir bool) error {
	sd, err := windows.GetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION,
	)
	if err != nil {
		return fmt.Errorf("inspect plugin config ACL for %s: %w", path, err)
	}
	return validateWindowsSD(path, sd, isDir)
}

func validateWindowsSD(path string, sd *windows.SECURITY_DESCRIPTOR, isDir bool) error {
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

	riskMask := windowsFileWriteRiskMask
	if isDir {
		riskMask = windowsDirWriteRiskMask
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
