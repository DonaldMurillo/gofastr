package docs

import (
	"fmt"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/pagination"
)

// The page said `DefaultPageSize = 20`, but the exported constant is 25:
// 20 is crud's own list default. The page names both, and the constant's
// value comes from the code, so a change to either side fails here.
func TestCursorDocNamesBothDefaults(t *testing.T) {
	document, err := Get("cursor-pagination")
	if err != nil {
		t.Fatal(err)
	}
	doc := strings.Join(strings.Fields(string(document)), " ")
	for _, want := range []string{
		"default to 20 rows",
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
