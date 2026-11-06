package datexport_test

import (
	"slices"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/datexport"
)

func TestRegisterIncludesKeyColumns(t *testing.T) {
	datexport.Reset(t)
	columns := []string{"body", "second"}
	for _, keys := range []string{"first,second", " first , second "} {
		datexport.Register(datexport.DataExporter{Name: "notes", Table: "notes", PrimaryKey: keys, Columns: columns})
		if got := datexport.All()[0].Columns; !slices.Equal(got, []string{"body", "second", "first"}) {
			t.Fatalf("archive columns = %v, want declared columns followed by missing keys", got)
		}
	}
	if !slices.Equal(columns, []string{"body", "second"}) {
		t.Fatal("registration changed the caller's columns")
	}
}
