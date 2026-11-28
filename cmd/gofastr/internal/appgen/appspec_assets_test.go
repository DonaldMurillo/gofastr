package appgen

import (
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/appspec"
)

func TestFontFetchChecksThePathNotTheQuery(t *testing.T) {
	for _, u := range []string{"https://fonts.gstatic.com/evil?x=.woff2", "https://fonts.gstatic.com/evil#.woff2"} {
		if appSpecIsGstaticWoff2(u) {
			t.Errorf("%s accepted: the .woff2 suffix must be on the path", u)
		}
	}
}

type fontRoundTrip func(*http.Request) (*http.Response, error)

func (f fontRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestFontFetchRefusesARedirectOffTheHost(t *testing.T) {
	var fetched []string
	old := appSpecFontHTTPClient.Transport
	t.Cleanup(func() { appSpecFontHTTPClient.Transport = old })
	appSpecFontHTTPClient.Transport = fontRoundTrip(func(r *http.Request) (*http.Response, error) {
		fetched = append(fetched, r.URL.String())
		resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("woff2")), Request: r}
		if r.URL.Host == "fonts.gstatic.com" && r.URL.Path != "/s/moved.woff2" {
			resp.StatusCode = http.StatusFound
			resp.Header.Set("Location", r.URL.Query().Get("to"))
		}
		return resp, nil
	})
	for _, to := range []string{"https://attacker.test/x.woff2", "http://fonts.gstatic.com/s/moved.woff2"} {
		fetched = nil
		if _, err := appSpecFontHTTPGet("https://fonts.gstatic.com/s/x.woff2?to=" + url.QueryEscape(to)); err == nil {
			t.Errorf("a redirect to %s was followed", to)
		}
		if len(fetched) != 1 {
			t.Errorf("redirect to %s: requests made %v, want only the first", to, fetched)
		}
	}
	data, err := appSpecFontHTTPGet("https://fonts.gstatic.com/s/x.woff2?to=" + url.QueryEscape("https://fonts.gstatic.com/s/moved.woff2"))
	if err != nil || string(data) != "woff2" {
		t.Errorf("a redirect within fonts.gstatic.com over https: %q, %v", data, err)
	}
}

func TestPWAAppGetsAStaticDirAndItsMountsDenied(t *testing.T) {
	bp := appspec.AppSpec{App: appspec.AppSpecApp{Name: "Demo", APIPrefix: "v1", PWA: appspec.AppSpecPWA{Enabled: true}}}
	if got := appSpecEffectiveStaticDir(bp); got != "static" {
		t.Errorf("a PWA app needs a static dir for its icons, got %q", got)
	}
	if lit := appSpecPWAOptionLiteral(bp); !strings.Contains(lit, `"/v1"`) {
		t.Errorf("a custom api_prefix must join the PWA deny list:\n%s", lit)
	}
}
