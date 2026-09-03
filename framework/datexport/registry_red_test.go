//go:build red

package datexport_test

// RED TEST — open finding, 2026-09-02 adversarial pass (tests-only; no fix applied).
// Property: registry names that become filesystem path components (and report labels) are validated at registration, matching the documented safe-identifier contract (registry.go DataExporter doc: "Name ... must be a safe SQL identifier").
// Surfaces: framework/datexport/registry.go:Register, consumed at framework/export_data.go:162 and :263 as filepath.Join(dir, name+".ndjson")
// Finding: Register copies Name verbatim with no validation, and Name is the one field that never passes the SafeIdent/MustIdent SQL boundary (it is never SQL — it is the .ndjson path stem and manifest key), so a Name like "../../escape" reads/writes outside the export dir on export and import.
// Fix direction: validate Name at registration the way access.WithGrantTable does — query.MustIdent panics on an unsafe identifier at construction time (framework/access/store.go:104-113).

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/datexport"
)

func TestRegisterRedRejectsTraversalName(t *testing.T) {
	const name = "../../escape"
	// Register mutates the package-global registry. The name is chosen so no
	// real battery uses it; Unregister keeps the entry from outliving this
	// test no matter how the assertion lands.
	defer datexport.Unregister(name)

	panicked := func() (p bool) {
		defer func() { p = recover() != nil }()
		datexport.Register(datexport.DataExporter{
			Name: name, Source: "redtest", Table: "t",
			PrimaryKey: "id", Columns: []string{"id"},
		})
		return false
	}()
	if panicked {
		return // refused loudly at registration: nothing left to check
	}
	for _, e := range datexport.All() {
		if e.Name == name {
			t.Errorf("SECURITY: [datexport] Register accepted traversal Name %q verbatim; "+
				"export/import build filepath.Join(dir, name+\".ndjson\") from it and read/write "+
				"outside the export dir", name)
			return
		}
	}
	// Neither panicked nor registered: some other refusal API — acceptable.
}
