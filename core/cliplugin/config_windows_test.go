//go:build windows

package cliplugin

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

func TestIsTrustedWindowsSIDAcceptsTrustedInstaller(t *testing.T) {
	ti, err := windows.StringToSid(windowsTrustedInstallerSID)
	if err != nil {
		t.Fatalf("StringToSid(TrustedInstaller): %v", err)
	}
	if !isTrustedWindowsSID(ti, nil) {
		t.Fatal("TrustedInstaller SID must be trusted for Windows volume roots/ancestors")
	}
	everyone, err := windows.CreateWellKnownSid(windows.WinWorldSid)
	if err != nil {
		t.Fatalf("CreateWellKnownSid: %v", err)
	}
	if isTrustedWindowsSID(everyone, nil) {
		t.Fatal("SECURITY: Everyone SID must never be trusted")
	}
}

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

	// Refuse a writable ancestor directory even when the immediate config dir
	// and file have owner-only DACLs.
	shared := filepath.Join(t.TempDir(), "shared")
	sub := filepath.Join(shared, "gofastr")
	if err := os.MkdirAll(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(sub, "plugins.yml")
	if err := SaveUserConfig(nested, UserConfig{Version: ConfigVersion}); err != nil {
		t.Fatalf("SaveUserConfig nested: %v", err)
	}
	if err := windows.SetNamedSecurityInfo(
		shared,
		windows.SE_FILE_OBJECT,
		windows.DACL_SECURITY_INFORMATION|windows.PROTECTED_DACL_SECURITY_INFORMATION,
		nil, nil, acl, nil,
	); err != nil {
		t.Fatalf("SetNamedSecurityInfo ancestor: %v", err)
	}
	if _, err := LoadUserConfig(nested); err == nil || !strings.Contains(err.Error(), "ancestor") {
		t.Fatalf("SECURITY: world-writable Windows ancestor accepted on load: %v", err)
	}
	if err := SaveUserConfig(nested, UserConfig{Version: ConfigVersion}); err == nil || !strings.Contains(err.Error(), "ancestor") {
		t.Fatalf("SECURITY: world-writable Windows ancestor accepted on save: %v", err)
	}
}

func makeJunction(t *testing.T, link, target string) {
	t.Helper()
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
		t.Fatalf("mklink /J %s -> %s: %v\n%s", link, target, err, out)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
}

func TestWindowsRefusesJunctionsAndReparsePoints(t *testing.T) {
	base := t.TempDir()
	targetDir := filepath.Join(base, "target")
	if err := os.MkdirAll(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveUserConfig(filepath.Join(targetDir, "plugins.yml"), UserConfig{Version: ConfigVersion}); err != nil {
		t.Fatalf("SaveUserConfig target: %v", err)
	}

	// 1. Immediate config directory is a junction.
	dirJunction := filepath.Join(base, "dir-junction")
	makeJunction(t, dirJunction, targetDir)
	viaDirJunction := filepath.Join(dirJunction, "plugins.yml")
	if _, err := LoadUserConfig(viaDirJunction); err == nil || !strings.Contains(err.Error(), "junction") {
		t.Fatalf("SECURITY: junction config dir accepted on load: %v", err)
	}
	if err := SaveUserConfig(viaDirJunction, UserConfig{Version: ConfigVersion}); err == nil || !strings.Contains(err.Error(), "junction") {
		t.Fatalf("SECURITY: junction config dir accepted on save: %v", err)
	}

	// 2. Config file path itself is a junction.
	fileDir := filepath.Join(base, "file-case")
	if err := os.MkdirAll(fileDir, 0o700); err != nil {
		t.Fatal(err)
	}
	cfgJunction := filepath.Join(fileDir, "plugins.yml")
	makeJunction(t, cfgJunction, targetDir)
	if _, err := LoadUserConfig(cfgJunction); err == nil || !strings.Contains(err.Error(), "reparse") {
		t.Fatalf("SECURITY: junction config file accepted on load: %v", err)
	}
	if err := SaveUserConfig(cfgJunction, UserConfig{Version: ConfigVersion}); err == nil || !strings.Contains(err.Error(), "reparse") {
		t.Fatalf("SECURITY: junction config file accepted on save: %v", err)
	}

	// 3. Lockfile path is a junction.
	lockDir := filepath.Join(base, "lock-case")
	if err := os.MkdirAll(lockDir, 0o700); err != nil {
		t.Fatal(err)
	}
	lockJunction := filepath.Join(lockDir, "plugins.yml.lock")
	makeJunction(t, lockJunction, targetDir)
	if err := SaveUserConfig(filepath.Join(lockDir, "plugins.yml"), UserConfig{Version: ConfigVersion}); err == nil || !strings.Contains(err.Error(), "reparse") {
		t.Fatalf("SECURITY: junction lockfile accepted on save: %v", err)
	}
}
