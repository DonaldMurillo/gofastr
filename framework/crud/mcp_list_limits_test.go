package crud

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
)

func TestMCPListLimitMatchesRoute(t *testing.T) {
	for _, maximum := range []int{0, 3, 500, 2000} {
		config := makeEntityConfig("items", "items", "", []schema.Field{{Name: "name", Type: schema.String}})
		config.Pagination = &entity.PaginationConfig{MaxListLimit: maximum}
		ent := entity.Define("items", config)
		props := listToolSchema(ent)["properties"].(map[string]any)
		limit := props["limit"].(map[string]any)
		_, want := ListLimits(maximum)
		if limit["maximum"] != want {
			t.Errorf("configured %d: maximum = %v, want %d", maximum, limit["maximum"], want)
		}
	}
}
