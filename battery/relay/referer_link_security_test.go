package relay

import (
	"net/http"
	"strings"
	"testing"
)

// A browser sends the FULL page URL as Referer on a same-origin request,
// and a relayed request is same-origin: a reset or magic-link page that
// loads a vendor script through the relay put its token-bearing URL in
// front of the vendor. Sent straight to the vendor, the browser's
// default policy (strict-origin-when-cross-origin) would have sent the
// origin alone. The relay must not hand the third party more than the
// browser would.
func TestRefererReducedToOrigin(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://app.example/reset-password?token=s3cret#x", "https://app.example/"},
		{"http://app.example:8080/a/b", "http://app.example:8080/"},
		{"https://user:pw@app.example/p", "https://app.example/"},
		{"not a url", ""},
		{"/relative/only?token=s3cret", ""},
		{"android-app://com.example/path", ""},
	}
	for _, tc := range cases {
		base, cap := startRelayCap(t, defaultCfg)
		req, _ := http.NewRequest(http.MethodGet, base+"/__gofastr/t/e/sdk.js", nil)
		req.Header.Set("Referer", tc.in)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		_, _, _, _, h := cap.snapshot()
		if got := h.Get("Referer"); got != tc.want {
			t.Errorf("Referer %q reached the vendor as %q, want %q", tc.in, got, tc.want)
		}
		if strings.Contains(strings.Join(h.Values("Referer"), ","), "s3cret") {
			t.Errorf("SECURITY: the page URL's token reached the vendor through Referer (%q)", h.Values("Referer"))
		}
	}
}

// The Link strip read "://" and "<//" as the only spellings of another
// origin. A browser parses a Link target with the URL parser, which
// treats "\" as "/" in an http(s) URL and drops tabs and newlines, so
// <\\vendor.example/x>, </\vendor.example/x> and </	/vendor.example/x>
// all name https://vendor.example/x. A target with any scheme
// (mailto:, data:, https:vendor.example) is not the app's either.
func TestLinkOtherOriginSpellingsStripped(t *testing.T) {
	hostile := []string{
		`<\\vendor.example/sdk.js>; rel=preload; as=script`,
		`</\vendor.example/sdk.js>; rel=preconnect`,
		`<\/vendor.example/sdk.js>; rel=preconnect`,
		"</\t/vendor.example/sdk.js>; rel=dns-prefetch",
		`< //vendor.example/sdk.js>; rel=preconnect`,
		`<https:vendor.example>; rel=preconnect`,
		`<HTTPS://vendor.example>; rel=preconnect`,
		`</ok>; rel=next, <\\vendor.example>; rel=preconnect`,
	}
	for _, v := range hostile {
		base := startRelay(t, defaultCfg, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Link", v)
			w.WriteHeader(http.StatusOK)
		})
		res, err := http.Get(base + "/__gofastr/t/e/x")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if got := res.Header.Values("Link"); len(got) != 0 {
			t.Errorf("SECURITY: upstream Link %q reached the browser as %q: it names another origin", v, got)
		}
	}

	// Control: relative targets on the app's own origin still pass.
	for _, v := range []string{`</page/2>; rel="next"`, `<page/2>; rel="next"`, `<?page=2>; rel="next"`} {
		base := startRelay(t, defaultCfg, func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Link", v)
			w.WriteHeader(http.StatusOK)
		})
		res, err := http.Get(base + "/__gofastr/t/e/x")
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if got := res.Header.Get("Link"); got != v {
			t.Errorf("relative Link %q was dropped (got %q)", v, got)
		}
	}
}
