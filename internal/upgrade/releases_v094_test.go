package upgrade_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/upgrade"
	"github.com/DonaldMurillo/gofastr/internal/upgrade/scantest"
)

// A locale file still holding a moved ui.entity refusal key is found; the
// entityui spelling is quiet.
func TestV094MovedEntityKeysNote(t *testing.T) {
	reg, err := upgrade.Load()
	if err != nil {
		t.Fatalf("upgrade.Load: %v", err)
	}
	n := scantest.Note(t, reg, "v0.94.0", 7)
	const mainGo = "package main\n\nfunc main() {}\n"
	old := `{"ui": {"entity": {"bulkOverCap": "Selecciona como máximo {cap}"}}}`
	app := scantest.App(t, map[string]string{"main.go": mainGo, "locales/es.json": old}, scantest.Options{})
	if got := scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) == 0 {
		t.Error("ui.entity.bulkOverCap: no hit")
	}
	moved := `{"entityui": {"bulk_over_cap": "Selecciona como máximo {{cap}}"}}`
	app = scantest.App(t, map[string]string{"main.go": mainGo, "locales/es.json": moved}, scantest.Options{})
	if got := scantest.Hits(scantest.Run(t, app, []*upgrade.Note{n}, upgrade.MarkerSinks{}), n); len(got) != 0 {
		t.Errorf("entityui spelling fires: %v", got)
	}
}
