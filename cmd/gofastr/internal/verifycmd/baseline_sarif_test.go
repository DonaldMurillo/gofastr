package verifycmd

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

func TestBaselineSARIFRefusesBeforeWrites(t *testing.T) {
	for _, jsonMode := range []bool{false, true} {
		directory := unguardedModule(t)
		sarif := filepath.Join(directory, "scan.sarif")
		args := []string{"--root", directory, "--no-vet", "--fix", "--baseline-write", "--sarif", sarif}
		if jsonMode {
			args = append(args, "--json")
		}
		output, code := captureVerify(t, args)
		if code != 2 {
			t.Errorf("exit=%d, want 2: %s", code, output)
		}
		if jsonMode && !json.Valid([]byte(output)) {
			t.Errorf("invalid JSON: %s", output)
		}
		for _, path := range []string{sarif, filepath.Join(directory, contracts.BaselineFileName)} {
			if _, err := os.Stat(path); !os.IsNotExist(err) {
				t.Errorf("refused invocation wrote %s: %v", path, err)
			}
		}
	}
}
