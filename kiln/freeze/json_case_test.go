package freeze_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/kiln/freeze"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

func TestFreezeJSONCaseContract(t *testing.T) {
	for _, casing := range []string{"", "camel", "camelCase", "snake", "snake_case", "invalid"} {
		t.Run(casing, func(t *testing.T) {
			w := world.New()
			w.App.JSONCase = casing
			dir := t.TempDir()
			err := freeze.Freeze(w, dir)
			if casing == "" || casing == "camel" || casing == "camelCase" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "json_case") {
				t.Fatalf("json_case %q: want actionable refusal, got %v", casing, err)
			}
			if casing != "invalid" && (!strings.Contains(err.Error(), "CaseSnake") || !strings.Contains(err.Error(), "camel")) {
				t.Fatalf("missing migration instructions: %v", err)
			}
			for _, name := range []string{"gofastr.yml", "world.json"} {
				if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
					t.Errorf("refused freeze wrote %s: %v", name, err)
				}
			}
		})
	}
}
