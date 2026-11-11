package docs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/crud"
	"github.com/DonaldMurillo/gofastr/framework/pagination"
)

// The page said `DefaultPageSize = 20`, but the exported constant is 25:
// 20 is crud's own list default. The page names both, and every number
// it is checked for comes from the code (crud.ListLimits for the list
// routes, the pagination constants for the helpers), so a change to
// either side fails here.
func TestCursorDocNamesBothDefaults(t *testing.T) {
	document, err := Get("cursor-pagination")
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.Join(strings.Fields(string(document)), " ")
	listDefault, listMax := crud.ListLimits(0)
	for _, want := range []string{
		fmt.Sprintf("default to %d rows", listDefault),
		fmt.Sprintf("(%d when unset", listMax),
		fmt.Sprintf("`pagination.DefaultPageSize` is %d", pagination.DefaultPageSize),
		fmt.Sprintf("`pagination.MaxPageSize = %d`", pagination.MaxPageSize),
	} {
		if !strings.Contains(doc, want) {
			t.Errorf("cursor-pagination.md does not say %q", want)
		}
	}
	if strings.Contains(doc, "DefaultPageSize = 20") {
		t.Error("cursor-pagination.md still calls DefaultPageSize 20")
	}
}
