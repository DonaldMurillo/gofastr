package packages_test

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The local state is laid over the package with go list -overlay, so nothing is written here.
func TestFSShipsNoDotOrUnderscoreEntry(t *testing.T) {
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	stub := filepath.Join(dir, "stub")
	if err := os.WriteFile(stub, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	overlay, err := json.Marshal(map[string]map[string]string{"Replace": {
		filepath.Join(wd, ".gofastr", "state.json"): stub,
		filepath.Join(wd, "_draft", "notes.txt"):    stub,
	}})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("go", "list", "-overlay", overlayPath, "-f", `{{join .EmbedFiles "\n"}}`, ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	files := strings.Fields(string(out))
	if len(files) == 0 {
		t.Fatal("FS embeds nothing")
	}
	for _, f := range files {
		if strings.HasPrefix(f, ".") || strings.HasPrefix(f, "_") {
			t.Errorf("FS embeds %s, local state that must not ship in the binary", f)
		}
	}
}
