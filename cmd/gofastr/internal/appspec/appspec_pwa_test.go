package appspec

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

func TestAppSpecRejectsUnknownPWAKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yml")
	clitest.WriteTestFile(t, path, "app:\n  name: Demo\n  pwa:\n    enabled: true\n    bogus: 1\n")
	_, err := LoadAppSpec(path)
	if err == nil || !strings.Contains(err.Error(), `unknown key "bogus"`) {
		t.Fatalf("loadAppSpec err = %v", err)
	}
}

func TestAppSpecRejectsBadPWADisplay(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad.yml")
	clitest.WriteTestFile(t, path, "app:\n  name: Demo\n  pwa:\n    enabled: true\n    display: popup\n")
	_, err := LoadAppSpec(path)
	if err == nil || !strings.Contains(err.Error(), "app.pwa.display") {
		t.Fatalf("loadAppSpec err = %v", err)
	}
}
