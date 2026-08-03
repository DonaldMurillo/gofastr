//go:build windows

package fileperm

import (
	"fmt"

	"golang.org/x/sys/windows"
)

// Restrict replaces a file or directory DACL with an owner-only full-control
// ACE and protects it from inheriting broader permissions. Windows ignores
// POSIX mode bits, so this is the equivalent of the repository's 0600/0700
// policy for sensitive local artifacts.
func Restrict(path string, directory bool) error {
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, windows.OWNER_SECURITY_INFORMATION)
	if err != nil {
		return fmt.Errorf("read owner ACL for %q: %w", path, err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		return fmt.Errorf("read owner for %q: %w", path, err)
	}
	inheritance := uint32(windows.NO_INHERITANCE)
	if directory {
		inheritance = windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.GENERIC_ALL,
		AccessMode:        windows.GRANT_ACCESS,
		Inheritance:       inheritance,
		Trustee: windows.TRUSTEE{
			TrusteeForm:  windows.TRUSTEE_IS_SID,
			TrusteeType:  windows.TRUSTEE_IS_USER,
			TrusteeValue: windows.TrusteeValueFromSID(owner),
		},
	}}, nil)
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
