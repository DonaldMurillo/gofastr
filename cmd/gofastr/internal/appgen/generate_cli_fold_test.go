package appgen

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// A CLI selection name matches an entity by an ASCII-only fold: the
// KELVIN SIGN spelling of "kiosks" is not the kiosks entity.
func TestCLIEntityMatchesFoldsASCIIOnly(t *testing.T) {
	decl := framework.EntityDeclaration{Name: "Kiosk", Table: "kiosk_items"}
	for _, name := range []string{"KIOSK", "Kiosk_Items", " kiosk-items "} {
		if !cliEntityMatches(decl, name) {
			t.Errorf("%q no longer selects the entity", name)
		}
	}
	for _, name := range []string{"Kiosk", "Kiosk_items", "Kiosk-items"} {
		if cliEntityMatches(decl, name) {
			t.Errorf("%q selected the kiosk entity", name)
		}
	}
}
