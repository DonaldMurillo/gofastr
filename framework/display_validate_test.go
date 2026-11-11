package framework

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/entity"
)

// ValidateDisplay is the whole display boot check for a generator: the
// names entity.ValidateDisplay checks and the queries (a view's Where
// and Sort, a field's ShowWhen) only the framework can parse.
func TestValidateDisplayParsesQueries(t *testing.T) {
	if err := ValidateDisplay("invoices", EntityConfig{Fields: displayCheckFields(), Display: &entity.DisplayConfig{
		Views: []entity.ListView{{Key: "open", Where: `status = "open"`, Sort: "title desc"}},
	}}); err != nil {
		t.Fatalf("valid display refused: %v", err)
	}
	for _, tc := range []struct {
		d    *entity.DisplayConfig
		want string
	}{
		{&entity.DisplayConfig{TitleFields: []string{"nope"}}, `"nope"`},
		{&entity.DisplayConfig{Views: []entity.ListView{{Key: "open", Where: `nope = 1`}}}, `view "open" where`},
		{&entity.DisplayConfig{Views: []entity.ListView{{Key: "open", Sort: "nope ASC"}}}, `view "open" sort`},
		{&entity.DisplayConfig{Fields: map[string]entity.FieldDisplay{"title": {ShowWhen: `nope = 1`}}}, "show_when"},
	} {
		err := ValidateDisplay("invoices", EntityConfig{Fields: displayCheckFields(), Display: tc.d})
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("display %+v: got %v, want %q", tc.d, err, tc.want)
		}
		if tryDisplay(t, tc.d) == nil {
			t.Errorf("display %+v: App.Entity accepts what ValidateDisplay refuses", tc.d)
		}
	}
}
