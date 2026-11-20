package auth

import (
	"net/http/httptest"
	"testing"
)

func TestWantsHTMLMixedCase(t *testing.T) {
	for _, accept := range []string{"Text/HTML; charset=utf-8", "Application/XHTML+XML", "", "application/json"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept", accept)
		want := accept == "Text/HTML; charset=utf-8" || accept == "Application/XHTML+XML"
		if got := wantsHTML(r); got != want {
			t.Errorf("Accept %q: got %v, want %v", accept, got, want)
		}
	}
}
