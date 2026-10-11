package crud

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/filter"
)

// llm.md's operator table is read by agents choosing a filter, so it must
// name every suffix the filter parser accepts. Walking filter.FilterSuffixes
// keeps the two in step: a new operator fails here until it is documented.
func TestLLMMDListsEveryFilterSuffix(t *testing.T) {
	e := entity.Define("posts", entity.EntityConfig{
		Name: "posts", Table: "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	}.WithTimestamps(false))
	md := EntityLLMMD(e)
	for _, s := range filter.FilterSuffixes {
		if !strings.Contains(md, "| `"+s.Suffix+"` |") {
			t.Errorf("llm.md operator table is missing %s", s.Suffix)
		}
	}
}
