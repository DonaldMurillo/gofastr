package sdkdocs

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestQuickstartOmitsUnavailableSort(t *testing.T) {
	for _, fields := range [][]schema.Field{
		{{Name: "title", Type: schema.String}},
		{{Name: "created_at", Type: schema.Timestamp, NoQuery: true}},
		{{Name: "created_at", Type: schema.Timestamp, Hidden: true}},
	} {
		model := entity.Define("notes", entity.EntityConfig{Fields: fields})
		site := &site{cfg: Config{BaseURL: "https://example.test"}}
		for _, output := range []string{site.quickstartTabs(model).String(), (&entityScreen{}).listParamsNotes(model.Config).String()} {
			if strings.Contains(output, "-created_at") {
				t.Error("SDK example sorts by absent or unavailable created_at")
			}
		}
	}
}
