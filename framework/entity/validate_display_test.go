package entity

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// ValidateDisplay is ValidateStates' twin for the display block: the
// name, type and key checks registration runs, for a declaration that
// never reached App.Entity.
func TestValidateDisplayMatchesBoot(t *testing.T) {
	fields := []schema.Field{
		{Name: "title", Type: schema.String, Required: true},
		{Name: "status", Type: schema.Enum, Values: []string{"a", "b"}},
		{Name: "secret", Type: schema.String, Hidden: true},
	}
	good := &DisplayConfig{TitleFields: []string{"title"}, Facets: []string{"status"}, Columns: []string{"title", "created_at"}}
	if err := ValidateDisplay("memos", EntityConfig{Fields: fields, Display: good}); err != nil {
		t.Fatalf("good display refused: %v", err)
	}
	if err := ValidateDisplay("memos", EntityConfig{Fields: fields}); err != nil {
		t.Fatalf("no display refused: %v", err)
	}
	for _, tc := range []struct {
		d    *DisplayConfig
		want string
	}{
		{&DisplayConfig{TitleFields: []string{"missing_field"}}, `title_fields[0] names field "missing_field"`},
		{&DisplayConfig{Facets: []string{"title"}}, "must be an Enum, Bool or Relation"},
		{&DisplayConfig{Columns: []string{"secret"}}, "Hidden"},
		{&DisplayConfig{Views: []ListView{{Key: "Bad.Key"}}}, "lowercase slug"},
	} {
		err := ValidateDisplay("memos", EntityConfig{Fields: fields, Display: tc.d})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("display %+v: got %v, want %q", tc.d, err, tc.want)
		}
		if Define("memos", EntityConfig{Fields: fields, Display: tc.d}).Validate() == nil {
			t.Errorf("display %+v: Validate accepts what ValidateDisplay refuses", tc.d)
		}
	}
}
