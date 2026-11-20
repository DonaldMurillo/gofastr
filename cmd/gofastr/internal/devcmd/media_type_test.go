package devcmd

import (
	"strings"
	"testing"
)

func TestDevHoldMixedCaseHTML(t *testing.T) {
	h := newTestHold(t)
	for _, accept := range []string{"Text/HTML; charset=utf-8", "text/html", "", "application/json"} {
		res, _ := holdGet(t, h.addr, accept)
		got := strings.HasPrefix(res.Header.Get("Content-Type"), "text/html")
		want := accept == "Text/HTML; charset=utf-8" || accept == "text/html"
		if got != want {
			t.Errorf("Accept %q: HTML = %v, want %v", accept, got, want)
		}
	}
}
