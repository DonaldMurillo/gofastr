package pagination

import (
	"strings"
	"testing"
)

func TestRequiresPositiveTotal(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic with Total=0")
		}
	}()
	New(Config{Total: 0, Current: 1, HrefPattern: "/?p=%d"})
}

func TestCurrentOutOfRange(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic when Current is out of range")
		}
	}()
	New(Config{Total: 3, Current: 5, HrefPattern: "/?p=%d"})
}

func TestHrefPatternMustContainPlaceholder(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic without placeholder in HrefPattern")
		}
	}()
	New(Config{Total: 5, Current: 1, HrefPattern: "/page"})
}

func TestSmallTotalShowsAllPages(t *testing.T) {
	h := string(New(Config{Total: 5, Current: 3, HrefPattern: "/?p=%d"}))
	for i := 1; i <= 5; i++ {
		want := `>` + itoa(i) + `<`
		if !strings.Contains(h, want) {
			t.Errorf("expected page %d visible, got: %s", i, h)
		}
	}
	if strings.Contains(h, "…") {
		t.Errorf("expected no ellipsis for small total, got: %s", h)
	}
}

func TestLargeTotalUsesEllipsis(t *testing.T) {
	h := string(New(Config{Total: 50, Current: 25, HrefPattern: "/?p=%d"}))
	if !strings.Contains(h, "…") {
		t.Errorf("expected ellipsis for large total, got: %s", h)
	}
	if !strings.Contains(h, `>1<`) || !strings.Contains(h, `>50<`) {
		t.Errorf("expected first and last visible, got: %s", h)
	}
	if !strings.Contains(h, `>25<`) {
		t.Errorf("expected current page visible, got: %s", h)
	}
}

func TestCurrentMarkedAriaCurrent(t *testing.T) {
	h := string(New(Config{Total: 10, Current: 4, HrefPattern: "/?p=%d"}))
	if !strings.Contains(h, `aria-current="page"`) {
		t.Errorf("expected aria-current on current page, got: %s", h)
	}
	if strings.Count(h, `aria-current="page"`) != 1 {
		t.Errorf("expected exactly one aria-current, got %d", strings.Count(h, `aria-current="page"`))
	}
}

func TestPrevNextDisabledAtBoundaries(t *testing.T) {
	first := string(New(Config{Total: 5, Current: 1, HrefPattern: "/?p=%d"}))
	if !strings.Contains(first, "is-disabled") {
		t.Errorf("expected disabled prev on page 1, got: %s", first)
	}
	last := string(New(Config{Total: 5, Current: 5, HrefPattern: "/?p=%d"}))
	if !strings.Contains(last, "is-disabled") {
		t.Errorf("expected disabled next on last page, got: %s", last)
	}
}

func TestHrefPatternFormatted(t *testing.T) {
	h := string(New(Config{Total: 3, Current: 2, HrefPattern: "/items?page=%d"}))
	for _, want := range []string{`href="/items?page=1"`, `href="/items?page=2"`, `href="/items?page=3"`} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q in: %s", want, h)
		}
	}
}

func TestNavLabel(t *testing.T) {
	h := string(New(Config{Total: 3, Current: 1, HrefPattern: "/?p=%d"}))
	if !strings.Contains(h, `<nav aria-label="Pagination">`) {
		t.Errorf("expected default nav aria-label, got: %s", h)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
