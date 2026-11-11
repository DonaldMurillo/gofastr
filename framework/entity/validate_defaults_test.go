package entity

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
)

// ValidateDefaults is the generators' entry point for the Default check
// registration runs: a declaration that never passed App.Entity gets the
// same refusal, so `gofastr validate` stops the bad value instead of the
// generated app panicking at boot.
func TestValidateDefaultsMatchesBoot(t *testing.T) {
	ok := []schema.Field{
		{Name: "status", Type: schema.Enum, Values: []string{"draft", "live"}, Default: "draft"},
		{Name: "n", Type: schema.Int, Default: "5"},
		{Name: "price", Type: schema.Decimal, Default: "0"},
		{Name: "on", Type: schema.Bool, Default: "true"},
	}
	if err := ValidateDefaults("things", EntityConfig{Fields: ok}); err != nil {
		t.Fatalf("good defaults refused: %v", err)
	}
	for _, tc := range []struct {
		field schema.Field
		want  string
	}{
		{schema.Field{Name: "status", Type: schema.Enum, Values: []string{"draft", "live"}, Default: "shipped"}, "must be one of"},
		{schema.Field{Name: "n", Type: schema.Int, Default: "notanumber"}, "integer"},
		{schema.Field{Name: "on", Type: schema.Bool, Default: "maybe"}, `"on"`},
		{schema.Field{Name: "name", Type: schema.String, Required: true, Default: ""}, "required"},
	} {
		err := ValidateDefaults("things", EntityConfig{Fields: []schema.Field{tc.field}})
		if err == nil || !strings.Contains(err.Error(), "invalid Default") || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s default %#v: got %v, want the boot refusal naming %q", tc.field.Name, tc.field.Default, err, tc.want)
		}
		// The boot path refuses the same declaration.
		if bootErr := Define("things", EntityConfig{Fields: []schema.Field{tc.field}}).Validate(); bootErr == nil {
			t.Errorf("%s: Validate accepts what ValidateDefaults refuses", tc.field.Name)
		}
	}
}
