package ui

import (
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core-ui/urlsafe"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

func safeImageURL(u string) string {
	return urlsafe.Clean(u, urlsafe.ImageSource)
}

func scrubAttrs(in html.Attrs) html.Attrs {
	if len(in) == 0 {
		return in
	}
	out := make(html.Attrs, len(in))
	for k, v := range in {
		lk := strings.ToLower(k)
		if strings.HasPrefix(lk, "on") {
			continue
		}
		if textsafe.HasControlBytes(k) || textsafe.HasControlBytes(v) {
			continue
		}
		out[k] = v
	}
	return out
}
