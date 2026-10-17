//go:build windows

// Package filepermtest checks file permissions using the host's permission model.
package filepermtest

import (
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

// AssertOwnerOnly requires the given Unix mode, or an owner-only Windows DACL.
func AssertOwnerOnly(t testing.TB, path string, _ os.FileMode) {
	t.Helper()
	assertOwnerACL(t, path, true)
}

// AssertOwnerAccess permits inheritance from an owner-only parent, as SQLite sidecars do.
func AssertOwnerAccess(t testing.TB, path string, _ os.FileMode) {
	t.Helper()
	assertOwnerACL(t, path, false)
}

func assertOwnerACL(t testing.TB, path string, protected bool) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	if protected && control&windows.SE_DACL_PROTECTED == 0 {
		t.Fatalf("%s inherits permissions, want protected owner-only DACL", path)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	acl, _, err := sd.DACL()
	if err != nil || acl == nil || acl.AceCount == 0 {
		t.Fatalf("%s has no owner ACL: ACL=%v, error=%v", path, acl, err)
	}
	var granted windows.ACCESS_MASK
	for i := uint32(0); i < uint32(acl.AceCount); i++ {
		var ace *windows.ACCESS_ALLOWED_ACE
		if err := windows.GetAce(acl, i, &ace); err != nil {
			t.Fatal(err)
		}
		sid := (*windows.SID)(unsafe.Pointer(&ace.SidStart))
		if ace.Header.AceType != windows.ACCESS_ALLOWED_ACE_TYPE || !sid.Equals(owner) {
			t.Fatalf("%s must grant access only to its owner: %s", path, sd.String())
		}
		// Windows may split a directory's owner grant into a current-object
		// ACE and an inherit-only ACE for future children. Check both SIDs,
		// but only count rights that apply to this object.
		if ace.Header.AceFlags&windows.INHERIT_ONLY_ACE == 0 {
			granted |= ace.Mask
		}
	}
	const fullControl = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff
	if granted&fullControl != fullControl && granted&windows.GENERIC_ALL == 0 {
		t.Fatalf("%s must grant the owner full control: mask=%#x", path, granted)
	}
}

// MakeUnreadable removes read access for a test and restores permissions at cleanup.
func MakeUnreadable(t testing.TB, path string) {
	t.Helper()
	denyAccess(t, path, windows.FILE_READ_DATA)
}

// MakeUnwritable blocks writes and returns a function that restores access.
func MakeUnwritable(t testing.TB, path string) func() {
	t.Helper()
	return denyAccess(t, path, windows.FILE_WRITE_DATA|windows.FILE_APPEND_DATA)
}

func denyAccess(t testing.TB, path string, denied windows.ACCESS_MASK) func() {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.OWNER_SECURITY_INFORMATION|windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	owner, _, err := sd.Owner()
	if err != nil {
		t.Fatal(err)
	}
	original, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	control, _, err := sd.Control()
	if err != nil {
		t.Fatal(err)
	}
	const fullControl = windows.STANDARD_RIGHTS_REQUIRED | windows.SYNCHRONIZE | 0x1ff
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: fullControl &^ denied,
		AccessMode:        windows.GRANT_ACCESS,
		Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID,
			TrusteeValue: windows.TrusteeValueFromSID(owner)},
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	restore := func() {
		flags := windows.SECURITY_INFORMATION(windows.DACL_SECURITY_INFORMATION | windows.UNPROTECTED_DACL_SECURITY_INFORMATION)
		if control&windows.SE_DACL_PROTECTED != 0 {
			flags = windows.DACL_SECURITY_INFORMATION | windows.PROTECTED_DACL_SECURITY_INFORMATION
		}
		if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT, flags, nil, nil, original, nil); err != nil {
			t.Errorf("restore DACL: %v", err)
		}
	}
	t.Cleanup(restore)
	if err := windows.SetNamedSecurityInfo(path, windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
	return restore
}
