package headless

import (
	"math"
	"strings"
	"testing"
)

// A Window large enough to "show every page" overflowed the window
// arithmetic, and the current page dropped out of the pager.
func TestPaginationHugeWindowKeepsCurrentPage(t *testing.T) {
	h := string(Pagination(PaginationProps{AriaLabel: "Pages", Page: 5, Pages: 10, Window: math.MaxInt}, nil))
	if !strings.Contains(h, `aria-current="page"`) {
		t.Fatalf("Window=math.MaxInt renders no current page:\n%s", h)
	}
	for n := 1; n <= 10; n++ {
		if got := pageNumbers(10, 5, math.MaxInt); len(got) != 10 || got[n-1] != n {
			t.Fatalf("a window wider than the pages lists %v, want every page", got)
		}
	}
}
