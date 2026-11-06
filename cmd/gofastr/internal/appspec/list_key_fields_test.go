package appspec

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework"
)

func TestListRefusesMissingIDColumn(t *testing.T) {
	for _, keys := range [][]string{{"school", "pupil"}, {"school"}, nil} {
		declaration := framework.EntityDeclaration{Name: "enrollments", PrimaryKey: keys,
			Exposure: &framework.ExposureDeclaration{CRUD: new(true)},
			Fields:   []framework.FieldDeclaration{{Name: "school", Type: "string"}, {Name: "pupil", Type: "string"}}}
		specification := AppSpec{App: AppSpecApp{Name: "School", APIPrefix: "api"},
			Entities: []framework.EntityDeclaration{declaration},
			Screens: []AppSpecScreen{{Name: "enrollments", Route: "/enrollments", Body: []AppSpecBlock{
				{Type: "entity_list", Entity: "enrollments", Fields: []string{"id", "school"}},
			}}}}
		err := ValidateAppSpec(specification)
		if len(keys) == 0 {
			if err != nil {
				t.Fatalf("generated id refused: %v", err)
			}
		} else if err == nil || !strings.Contains(err.Error(), `field "id"`) {
			t.Errorf("key %v accepts missing id column: %v", keys, err)
		}
	}
}
