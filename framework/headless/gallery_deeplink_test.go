package headless

import (
	"html"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/chromedp/chromedp"
)

// galleryDeeplink renders one lightbox gallery item and returns its
// unescaped data-cui-deeplink.
func galleryDeeplink(t *testing.T, it GalleryItem) string {
	t.Helper()
	h := string(Gallery(GalleryProps{Label: "Photos", Lightbox: GalleryLightbox{Name: "viewer"},
		Items: []GalleryItem{it}}, nil))
	m := regexp.MustCompile(`data-cui-deeplink="([^"]*)"`).FindStringSubmatch(h)
	if m == nil {
		t.Fatalf("no deeplink: %s", h)
	}
	return html.UnescapeString(m[1])
}

// runtimeDeeplinkParse is the Go twin of the data-cui-open delegate's
// parse (core-ui/runtime/frag/widgets-boot.js): split on '&', first
// '=' splits key/value, each side decodeURIComponent'd (PathUnescape
// keeps '+' as a plus the way decodeURIComponent does).
func runtimeDeeplinkParse(raw string) map[string]string {
	got := map[string]string{}
	for _, pair := range strings.Split(raw, "&") {
		eq := strings.IndexByte(pair, '=')
		if eq < 0 {
			continue
		}
		k, _ := url.PathUnescape(pair[:eq])
		v, _ := url.PathUnescape(pair[eq+1:])
		got[k] = v
	}
	return got
}

// Gallery built the lightbox deeplink with url.PathEscape, which
// leaves '&' and '=' unescaped, so an Alt or Caption holding "&", or a
// Src with a multi-parameter query, was cut short and spilled junk
// keys into the viewer's signals.
func TestGalleryDeeplinkSurvivesAmpersands(t *testing.T) {
	const src = "/img/p.jpg?w=1200&fmt=webp"
	const alt = "Tom & Jerry = friends + foes"
	const caption = "50% off, a=b"
	got := runtimeDeeplinkParse(galleryDeeplink(t, GalleryItem{Src: src, Alt: alt, Caption: caption}))
	if got["src"] != src || got["alt"] != alt || got["caption"] != caption || len(got) != 4 {
		t.Fatalf("the viewer receives %v; want src=%q alt=%q caption=%q and the group", got, src, alt, caption)
	}
}

// The viewer's src passes the image-source policy the thumb does: a
// javascript: Src never reaches the viewer's signals.
func TestGalleryDeeplinkSrcIsCleaned(t *testing.T) {
	got := runtimeDeeplinkParse(galleryDeeplink(t, GalleryItem{Src: "javascript:alert(1)", Alt: "x"}))
	if got["src"] != "/__gofastr/blank.png" {
		t.Fatalf("the viewer's src is %q, want the blank stub", got["src"])
	}
}

// The browser proof: the runtime's own decodeURIComponent reads back
// every value the server meant.
func TestE2E_GalleryDeeplinkRoundTripsThroughDecodeURIComponent(t *testing.T) {
	const src = "/img/p.jpg?w=1200&fmt=webp"
	const alt = "Tom & Jerry = friends + foes"
	raw := galleryDeeplink(t, GalleryItem{Src: src, Alt: alt})
	b := startBehaviorServer(t, `<span id="dl">`+html.EscapeString(raw)+`</span>`)
	ctx := behaviorPage(t, b)
	var got map[string]string
	if err := chromedp.Run(ctx, chromedp.Evaluate(`(() => {
		const out = {};
		for (const pair of document.getElementById('dl').textContent.split('&')) {
			const eq = pair.indexOf('=');
			if (eq < 0) continue;
			out[decodeURIComponent(pair.slice(0, eq))] = decodeURIComponent(pair.slice(eq + 1));
		}
		return out;
	})()`, &got)); err != nil {
		t.Fatal(err)
	}
	if got["src"] != src || got["alt"] != alt {
		t.Fatalf("decodeURIComponent reads %v from %q; want src=%q alt=%q", got, raw, src, alt)
	}
}
