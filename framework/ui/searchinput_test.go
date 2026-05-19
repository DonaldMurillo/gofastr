package ui

import (
	"strings"
	"testing"
)

func TestSearchInputRequiresName(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("SearchInput without Name should panic")
		}
	}()
	SearchInput(SearchInputConfig{ID: "q"})
}

func TestSearchInputRequiresID(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("SearchInput without ID should panic")
		}
	}()
	SearchInput(SearchInputConfig{Name: "q"})
}

func TestSearchInputEmitsTypeSearch(t *testing.T) {
	h := string(SearchInput(SearchInputConfig{Name: "q", ID: "q"}))
	if !strings.Contains(h, `type="search"`) {
		t.Errorf("expected type=search:\n%s", h)
	}
	if !strings.Contains(h, `name="q"`) {
		t.Errorf("expected name=q:\n%s", h)
	}
	if !strings.Contains(h, `id="q"`) {
		t.Errorf("expected id=q:\n%s", h)
	}
}

func TestSearchInputDefaultPlaceholder(t *testing.T) {
	h := string(SearchInput(SearchInputConfig{Name: "q", ID: "q"}))
	if !strings.Contains(h, `placeholder="Search..."`) {
		t.Errorf("expected default placeholder:\n%s", h)
	}
}

func TestSearchInputCustomPlaceholder(t *testing.T) {
	h := string(SearchInput(SearchInputConfig{
		Name: "q", ID: "q", Placeholder: "Find users...",
	}))
	if !strings.Contains(h, `placeholder="Find users..."`) {
		t.Errorf("expected custom placeholder:\n%s", h)
	}
}

func TestSearchInputHasAriaLabel(t *testing.T) {
	h := string(SearchInput(SearchInputConfig{Name: "q", ID: "q"}))
	if !strings.Contains(h, `aria-label="Search"`) {
		t.Errorf("expected aria-label=Search:\n%s", h)
	}
}

func TestSearchInputEmitsSearchIcon(t *testing.T) {
	h := string(SearchInput(SearchInputConfig{Name: "q", ID: "q"}))
	if !strings.Contains(h, "ui-search-input__icon") {
		t.Errorf("expected search icon:\n%s", h)
	}
}

func TestSearchInputEmitsClearButton(t *testing.T) {
	h := string(SearchInput(SearchInputConfig{Name: "q", ID: "q"}))
	if !strings.Contains(h, "ui-search-input__clear") {
		t.Errorf("expected clear button:\n%s", h)
	}
	if !strings.Contains(h, `aria-label="Clear search"`) {
		t.Errorf("expected aria-label Clear search:\n%s", h)
	}
}

func TestSearchInputWithoutActionNoForm(t *testing.T) {
	h := string(SearchInput(SearchInputConfig{Name: "q", ID: "q"}))
	if strings.Contains(h, `<form`) {
		t.Errorf("no Action should not wrap in form:\n%s", h)
	}
}

func TestSearchInputWithActionWrapsForm(t *testing.T) {
	h := string(SearchInput(SearchInputConfig{
		Name: "q", ID: "q", Action: "/search",
	}))
	if !strings.Contains(h, `<form`) {
		t.Errorf("Action set should wrap in form:\n%s", h)
	}
	if !strings.Contains(h, `role="search"`) {
		t.Errorf("expected role=search on form:\n%s", h)
	}
	if !strings.Contains(h, `action="/search"`) {
		t.Errorf("expected action=/search:\n%s", h)
	}
	if !strings.Contains(h, `method="GET"`) {
		t.Errorf("expected default method=GET:\n%s", h)
	}
}

// A-1: Clear button must be keyboard-reachable (WCAG 2.1.1).
// tabindex="-1" removes it from tab order — keyboard users can never activate it.
func TestSearchInputClearButtonKeyboardReachable(t *testing.T) {
	h := string(SearchInput(SearchInputConfig{Name: "q", ID: "q"}))
	if strings.Contains(h, `tabindex="-1"`) {
		t.Errorf("clear button must not have tabindex=-1 (keyboard unreachable):\n%s", h)
	}
}

func TestSearchInputCustomMethod(t *testing.T) {
	h := string(SearchInput(SearchInputConfig{
		Name: "q", ID: "q", Action: "/search", Method: "POST",
	}))
	if !strings.Contains(h, `method="POST"`) {
		t.Errorf("expected method=POST:\n%s", h)
	}
}

// K-1: SearchInput must validate Method — same as D-1 for Form.
func TestSearchInputPanicOnInvalidMethod(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("SearchInput with invalid Method should panic")
		}
	}()
	SearchInput(SearchInputConfig{Name: "q", ID: "q", Action: "/s", Method: "DELETE"})
}
