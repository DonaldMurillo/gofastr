package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// The probe is laid over the package with go vet -overlay, so it never lands on disk here.
func TestVetChecksHelperFormats(t *testing.T) {
	dir := t.TempDir()
	probe := filepath.Join(dir, "probe.go")
	helpers := []string{"Success", "Fail", "UsageFail", "Info", "InfoString", "Warn", "Printf"}
	var src strings.Builder
	src.WriteString("package cli\n\nfunc vetProbe() {\n")
	for _, h := range helpers {
		src.WriteString("\t" + h + `("%d", "text")` + "\n")
	}
	src.WriteString("\tFprintf(nil, `%d`, \"text\")\n}\n")
	if err := os.WriteFile(probe, []byte(src.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	overlay, err := json.Marshal(map[string]map[string]string{"Replace": {filepath.Join(wd, "zz_vetprobe.go"): probe}})
	if err != nil {
		t.Fatal(err)
	}
	overlayPath := filepath.Join(dir, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0o600); err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("go", "vet", "-overlay", overlayPath, ".").CombinedOutput()
	for _, h := range append(helpers, "Fprintf") {
		if !strings.Contains(string(out), "cli."+h+" format %d has arg") {
			t.Errorf("go vet does not check %s's format against its arguments:\n%s", h, out)
		}
	}
}
