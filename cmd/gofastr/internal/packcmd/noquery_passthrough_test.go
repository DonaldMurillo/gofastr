package packcmd

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

// TestNoQuerySurvivesPackRoundTrip covers the other half of the silent-drop
// class. pack.go's own header states the invariant that both the decoder and
// the serializer must learn every new construct, but the round-trip test
// compares fixtures, and no fixture uses no_query, so an omission here is
// vacuously "covered". Losing the flag on a pack/regenerate cycle turns a
// masked column back into a filterable one.
func TestNoQuerySurvivesPackRoundTrip(t *testing.T) {
	m := fieldToMap(framework.FieldDeclaration{
		Name:    "number",
		Type:    "string",
		NoQuery: true,
	})
	if v, ok := m["no_query"]; !ok || v != true {
		t.Errorf("fieldToMap dropped no_query: %+v — pack(generate(yml)) would strip the "+
			"protection and regenerating makes the column filterable again", m)
	}

	plain := fieldToMap(framework.FieldDeclaration{Name: "label", Type: "string"})
	if _, ok := plain["no_query"]; ok {
		t.Errorf("fieldToMap emits no_query for a normal field: %+v", plain)
	}
}
