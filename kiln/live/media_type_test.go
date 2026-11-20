package live

import (
	"net/http/httptest"
	"testing"
)

func TestWantsHTMLMixedCase(t *testing.T) {
	for _, accept := range []string{"Text/HTML; charset=utf-8", "text/html", "", "application/json"} {
		r := httptest.NewRequest("GET", "/", nil)
		r.Header.Set("Accept", accept)
		want := accept != "application/json"
		if got := wantsHTML(r); got != want {
			t.Errorf("Accept %q: got %v, want %v", accept, got, want)
		}
	}
}
