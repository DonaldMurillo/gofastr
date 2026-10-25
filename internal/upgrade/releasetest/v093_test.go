package releasetest

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// A comma string under an sdk or cli entry is found; the list spelling and
// a block list are quiet.
func TestV093SettingsListsNote(t *testing.T) {
	reg := scantest.Registry(t)
	n := scantest.Note(t, reg, "v0.93.0", 2)
	const mainGo = "package main\n\nfunc main() {}\n"
	const head = "codegen:\n  generators:\n    - name: sdk\n      config:\n"
	for _, body := range []string{
		"        only: posts,comments\n",
		"        exclude: \"audit_logs\"\n",
		"        targets: go\n",
		"    - name: cli\n      config:\n        verbs: list,get\n",
	} {
		app := scantest.App(t, map[string]string{"main.go": mainGo, "gofastr.settings.yml": head + body}, scantest.Options{})
		if got := scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) == 0 {
			t.Errorf("%q: no hit", body)
		}
	}
	for _, body := range []string{
		"        only: [posts, comments]\n",
		"        exclude:\n          - audit_logs\n",
		"        targets: [go]  # only: posts\n",
	} {
		app := scantest.App(t, map[string]string{"main.go": mainGo, "gofastr.settings.yml": head + body}, scantest.Options{})
		if got := scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 0 {
			t.Errorf("%q: fires on a list: %v", body, got)
		}
	}
}
