package releasetest

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// Each file gofastr.settings.yml replaced is found (a hit per top-level
// key), as is a settings section left in the app spec; an app already on
// the settings file is quiet.
func TestV092SettingsNoteHitsRetiredFiles(t *testing.T) {
	reg := scantest.Registry(t)
	n := scantest.Note(t, reg, "v0.92.0", 1)
	const mainGo = "package main\n\nfunc main() {}\n"
	for _, old := range []map[string]string{
		{"gofastr.codegen.yml": "codegen:\n  output: gen\n"},
		{"gofastr.contracts.yaml": "rules:\n  GOFASTR1003: off\n"},
		{"gofastr.isolation.yml": "isolation:\n  enabled: false\n"},
		{"gofastr.yml": "app:\n  name: demo\nisolation:\n  enabled: true\n"},
	} {
		old["main.go"] = mainGo
		app := scantest.App(t, old, scantest.Options{})
		if got := scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) == 0 {
			t.Errorf("%v: no hit", old)
		}
	}
	app := scantest.App(t, map[string]string{
		"main.go":              mainGo,
		"gofastr.settings.yml": "isolation:\n  enabled: true\n",
		"gofastr.yml":          "app:\n  name: demo\n  isolation_note: \"codegen: x\"\n",
	}, scantest.Options{})
	if got := scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 0 {
		t.Fatalf("fires on a migrated app: %v", got)
	}
}
