//go:build windows

package fileperm

import (
	"fmt"
	"os"
	"runtime"

	"golang.org/x/sys/windows"
)

var reopenFile = windows.NewLazySystemDLL("kernel32.dll").NewProc("ReOpenFile")

// Restrict replaces a file or directory DACL with an owner-only full-control
// ACE and protects it from inheriting broader permissions. Windows ignores
// POSIX mode bits, so this is the equivalent of the repository's 0600/0700
// policy for sensitive local artifacts.
func Restrict(path string, directory bool) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read owner ACL for %q: %w", path, err)
	}
	acl, err := ownerOnlyACL(sd, directory)
	if err != nil {
		return fmt.Errorf("build owner ACL for %q: %w", path, err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil,
		nil,
		acl,
		nil,
	); err != nil {
		return fmt.Errorf("set owner ACL for %q: %w", path, err)
	}
	return nil
}

// RestrictOpenFile makes an already open file owner-only without reopening its path.
func RestrictOpenFile(f *os.File) error {
	if err := f.Chmod(0o600); err != nil {
		return err
	}
	defer runtime.KeepAlive(f)
	// Go's file handle need not have WRITE_DAC. ReOpenFile obtains that
	// access to the same object, even if its name changes after the open.
	h, _, err := reopenFile.Call(f.Fd(), windows.READ_CONTROL|windows.WRITE_DAC,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, 0)
	if windows.Handle(h) == windows.InvalidHandle || h == 0 {
		return fmt.Errorf("reopen file for owner ACL: %w", err)
	}
	handle := windows.Handle(h)
	defer windows.CloseHandle(handle)
	sd, err := windows.GetSecurityInfo(handle, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read open file owner: %w", err)
	}
	acl, err := ownerOnlyACL(sd, false)
	if err != nil {
		return err
	}
	if err := windows.SetSecurityInfo(handle, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil); err != nil {
		return fmt.Errorf("set open file owner ACL: %w", err)
	}
	return nil
}

func ownerOnlyACL(sd *windows.SECURITY_DESCRIPTOR, directory bool) (*windows.ACL, error) {
	owner, _, err := sd.Owner()
	if err != nil {
		return nil, err
	}
	inheritance := uint32(windows.NO_INHERITANCE)
	if directory {
		inheritance = windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT
	}
	return windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       inheritance,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(owner),
		},
	}}, nil)
}
