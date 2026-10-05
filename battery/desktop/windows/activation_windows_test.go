//go:build windows && amd64

package windows

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestProtocolURLsFiltersSchemeAndLength(t *testing.T) {
	longURL := "notes://host/" + strings.Repeat("x", maxProtocolURL)
	got := protocolURLs([]string{
		"notes://one/path",
		"NOTES://two/path",
		"other://ignored",
		"not a URL",
		longURL,
	}, "notes")
	if len(got) != 2 || got[0] != "notes://one/path" || got[1] != "NOTES://two/path" {
		t.Fatalf("protocolURLs() = %#v", got)
	}
}

func TestRegisterUserProtocolAndRefuseDifferentOwner(t *testing.T) {
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		t.Fatal(err)
	}
	token := hex.EncodeToString(suffix[:])
	scheme := "gofastr-test-" + token
	appID := "test.gofastr." + token
	classPath := `Software\Classes\` + scheme
	t.Cleanup(func() { deleteUserProtocolForTest(classPath) })

	if err := registerUserProtocol(scheme, appID, "Test App"); err != nil {
		t.Fatal(err)
	}
	root, err := registry.OpenKey(registry.CURRENT_USER, classPath, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	if got, _, err := root.GetStringValue(""); err != nil || got != "URL:Test App Protocol" {
		_ = root.Close()
		t.Fatalf("protocol description = %q, %v", got, err)
	}
	if got, _, err := root.GetStringValue("GoFastrAppID"); err != nil || got != appID {
		_ = root.Close()
		t.Fatalf("protocol owner = %q, %v", got, err)
	}
	if got, _, err := root.GetStringValue("URL Protocol"); err != nil || got != "" {
		_ = root.Close()
		t.Fatalf("URL Protocol marker = %q, %v", got, err)
	}
	if err := root.Close(); err != nil {
		t.Fatal(err)
	}

	commandKey, err := registry.OpenKey(registry.CURRENT_USER, classPath+`\shell\open\command`, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	gotCommand, _, err := commandKey.GetStringValue("")
	_ = commandKey.Close()
	if err != nil {
		t.Fatal(err)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if want := `"` + exe + `" "%1"`; gotCommand != want {
		t.Fatalf("protocol command = %q, want %q", gotCommand, want)
	}

	if err := registerUserProtocol(scheme, "other.gofastr."+token, "Other App"); err == nil {
		t.Fatal("registerUserProtocol accepted a scheme owned by another app")
	}
}

func deleteUserProtocolForTest(classPath string) {
	for _, path := range []string{
		classPath + `\shell\open\command`,
		classPath + `\shell\open`,
		classPath + `\shell`,
		classPath,
	} {
		_ = registry.DeleteKey(registry.CURRENT_USER, path)
	}
}
