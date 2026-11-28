package urlprefix

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPathTriesAllOrigins(t *testing.T) {
	rule := Rule{Prefix: "/es", Origins: []string{"https://shop.example", "https://shop.example.test"}}
	if got := Path("https://shop.example.test/menu", rule); got != "https://shop.example.test/es/menu" {
		t.Errorf("Path = %q", got)
	}
}

func TestHTMLPrefixesRequestAttributeOnImage(t *testing.T) {
	got := string(HTML([]byte(`<img src="/photo.png" data-cui-poll-src="/status">`), es))
	if !strings.Contains(got, `src="/photo.png"`) || !strings.Contains(got, `data-cui-poll-src="/es/status"`) {
		t.Errorf("HTML = %q", got)
	}
}

func TestWriterRecognizesHTMLMediaType(t *testing.T) {
	for _, tt := range []struct{ mediaType, want string }{
		{"Text/HTML; Charset=UTF-8", "/es/menu"},
		{"text/html-fragment", "/menu"},
	} {
		t.Run(tt.mediaType, func(t *testing.T) {
			rec := httptest.NewRecorder()
			w := &writer{ResponseWriter: rec, rule: es}
			w.Header().Set("Content-Type", tt.mediaType)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`<a href="/menu">Menu</a>`))
			w.finish()
			if !strings.Contains(rec.Body.String(), `href="`+tt.want+`"`) {
				t.Errorf("body = %q, want href %q", rec.Body.String(), tt.want)
			}
		})
	}
}

func TestHTMLKeepsUnfinishedMarkup(t *testing.T) {
	in := []byte(`<a href="/menu">Menu</a><div class="`)
	if got := HTML(in, es); string(got) != string(in) {
		t.Errorf("HTML = %q, want unreadable input unchanged", got)
	}
}
