package datexport_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/datexport"
)

// Exporter names become filesystem path components
// (filepath.Join(dir, name+".ndjson") at export and import), so a name
// that is not one safe path segment of [A-Za-z0-9_-] is refused loudly
// at registration — a panic at construction, the query.MustIdent
// precedent — never accepted verbatim to read/write outside the export
// dir.
func TestRegisterRejectsTraversalName(t *testing.T) {
	const name = "../../escape"
	// Register mutates the package-global registry. The name is chosen so
	// no real battery uses it; Unregister keeps the entry from outliving
	// this test no matter how the assertion lands.
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
