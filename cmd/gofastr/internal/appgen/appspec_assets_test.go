package appgen

import (
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

func TestPWAAppGetsAStaticDirAndItsMountsDenied(t *testing.T) {
	bp := appspec.AppSpec{App: appspec.AppSpecApp{Name: "Demo", APIPrefix: "v1", PWA: appspec.AppSpecPWA{Enabled: true}}}
	if got := appSpecEffectiveStaticDir(bp); got != "static" {
		t.Errorf("a PWA app needs a static dir for its icons, got %q", got)
	}
	if lit := appSpecPWAOptionLiteral(bp); !strings.Contains(lit, `"/v1"`) {
		t.Errorf("a custom api_prefix must join the PWA deny list:\n%s", lit)
	}
}
