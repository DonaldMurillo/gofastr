package uihost

import (
	"bytes"
	stdimage "image"
	"image/color"
	"image/png"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
)

// testIconPNG returns PNG bytes of a solid w×h image.
func testIconPNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := stdimage.NewNRGBA(stdimage.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.NRGBA{R: 30, G: 90, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode test icon: %v", err)
	}
	return buf.Bytes()
}

func newAppIconHost(t *testing.T, opts ...Option) *UIHost {
	t.Helper()
	a := app.NewApp("x")
	a.Register("/", &plainComp{}, nil)
	return New(a, opts...)
}

func TestWithAppIconEmitsHeadLinks(t *testing.T) {
	ds := newAppIconHost(t, WithAppIcon(testIconPNG(t, 512, 512)))
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	body := w.Body.String()
	for _, want := range []string{
		`<link rel="icon" type="image/png" sizes="32x32" href="/__gofastr/icons/icon-32.png">`,
		`<link rel="icon" type="image/png" sizes="192x192" href="/__gofastr/icons/icon-192.png">`,
		`<link rel="apple-touch-icon" sizes="180x180" href="/__gofastr/icons/icon-180.png">`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %s in head, got:\n%s", want, body)
		}
	}
}

func TestWithAppIconServesGeneratedPNGs(t *testing.T) {
	ds := newAppIconHost(t, WithAppIcon(testIconPNG(t, 512, 512)))
	for _, size := range []int{32, 180, 192, 512} {
		url := "/__gofastr/icons/icon-" + strconv.Itoa(size) + ".png"
		w := httptest.NewRecorder()
		ds.ServeHTTP(w, httptest.NewRequest("GET", url, nil))
		if w.Code != 200 {
			t.Fatalf("GET %s: expected 200, got %d", url, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); ct != "image/png" {
			t.Errorf("GET %s: content type %q", url, ct)
		}
		img, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
		if err != nil {
			t.Fatalf("GET %s: not a PNG: %v", url, err)
		}
		if b := img.Bounds(); b.Dx() != size || b.Dy() != size {
			t.Errorf("GET %s: got %dx%d, want %dx%d", url, b.Dx(), b.Dy(), size, size)
		}
	}
}

func TestWithAppIconServesFaviconICO(t *testing.T) {
	ds := newAppIconHost(t, WithAppIcon(testIconPNG(t, 512, 512)))
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/favicon.ico", nil))
	if w.Code != 200 {
		t.Fatalf("expected 200 for /favicon.ico with WithAppIcon, got %d", w.Code)
	}
	if _, err := png.Decode(bytes.NewReader(w.Body.Bytes())); err != nil {
		t.Errorf("/favicon.ico body is not a decodable PNG: %v", err)
	}
}

func TestWithAppIconSquaresNonSquareSource(t *testing.T) {
	ds := newAppIconHost(t, WithAppIcon(testIconPNG(t, 640, 320)))
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/__gofastr/icons/icon-192.png", nil))
	if w.Code != 200 {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	img, err := png.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("not a PNG: %v", err)
	}
	if b := img.Bounds(); b.Dx() != 192 || b.Dy() != 192 {
		t.Errorf("non-square source must yield square icon, got %dx%d", b.Dx(), b.Dy())
	}
}

func TestWithAppIconInvalidSourceIsSkipped(t *testing.T) {
	ds := newAppIconHost(t, WithAppIcon([]byte("not an image")))
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if strings.Contains(w.Body.String(), "/__gofastr/icons/") {
		t.Errorf("invalid source must not emit icon links:\n%s", w.Body.String())
	}
	// /favicon.ico keeps the 204 no-favicon fallback.
	w = httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/favicon.ico", nil))
	if w.Code != 204 {
		t.Errorf("expected 204 fallback for /favicon.ico, got %d", w.Code)
	}
}

func TestWithAppIconFeedsPWAManifest(t *testing.T) {
	ds := newAppIconHost(t,
		WithAppIcon(testIconPNG(t, 512, 512)),
		WithPWA(PWAConfig{}),
	)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/manifest.webmanifest", nil))
	if w.Code != 200 {
		t.Fatalf("expected 200 manifest, got %d", w.Code)
	}
	body := w.Body.String()
	for _, want := range []string{
		`/__gofastr/icons/icon-192.png`,
		`/__gofastr/icons/icon-512.png`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("manifest missing generated icon %s, got:\n%s", want, body)
		}
	}
}

func TestWithAppIconDoesNotOverrideExplicitPWAIcons(t *testing.T) {
	ds := newAppIconHost(t,
		WithPWA(PWAConfig{Icons: []PWAIcon{{Src: "/static/mine-192.png", Sizes: "192x192", Type: "image/png"}}}),
		WithAppIcon(testIconPNG(t, 512, 512)),
	)
	w := httptest.NewRecorder()
	ds.ServeHTTP(w, httptest.NewRequest("GET", "/manifest.webmanifest", nil))
	body := w.Body.String()
	if !strings.Contains(body, "/static/mine-192.png") {
		t.Errorf("explicit PWA icons must win, got:\n%s", body)
	}
	if strings.Contains(body, "/__gofastr/icons/") {
		t.Errorf("generated icons must not be injected when explicit icons exist:\n%s", body)
	}
}
