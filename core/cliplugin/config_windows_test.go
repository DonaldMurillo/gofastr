//go:build windows

package cliplugin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestLoadUserConfigRefusesWorldWritableWindowsDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "plugins.yml")
	if err := SaveUserConfig(path, UserConfig{Version: ConfigVersion}); err != nil {
		t.Fatalf("SaveUserConfig: %v", err)
	}
	if _, err := LoadUserConfig(path); err != nil {
		t.Fatalf("owner-only plugins.yml should load cleanly: %v", err)
	}

	everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatalf("CreateWellKnownSid: %v", err)
	}
	tu, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatalf("GetTokenUser: %v", err)
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{
		{
			AccessPermissions: windows.GENERIC_ALL,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.NO_INHERITANCE,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_USER,
				TrusteeValue: windows.TrusteeValueFromSID(tu.User.Sid),
			},
		},
		{
			AccessPermissions: windows.FILE_WRITE_DATA,
			AccessMode:        windows.GRANT_ACCESS,
			Inheritance:       windows.NO_INHERITANCE,
			Trustee: windows.TRUSTEE{
				TrusteeForm:  windows.TRUSTEE_IS_SID,
				TrusteeType:  windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
				TrusteeValue: windows.TrusteeValueFromSID(everyone),
			},
		},
	}, nil)
	if err != nil {
		t.Fatalf("ACLFromEntries: %v", err)
	}
	if err := windows.SetNamedSecurityInfo(
		path,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil,
	); err != nil {
		t.Fatalf("SetNamedSecurityInfo: %v", err)
	}

	if _, err := LoadUserConfig(path); err == nil || !strings.Contains(err.Error(), "writable by untrusted SID") {
		t.Fatalf("SECURITY: world-writable Windows DACL accepted: %v", err)
	}

	// Ensure cleanup can remove the file regardless of ACL state.
	_ = os.Remove(path)
}
