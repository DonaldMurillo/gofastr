package appspec

import (
	"strings"
	"testing"
)

func TestScreenRefusesAuthMeRoute(t *testing.T) {
	for _, basePath := range []string{"", "/account"} {
		base := basePath
		if base == "" {
			base = "/auth"
		}
		for _, suffix := range []string{"/me", "/me/", "/login", "/settings"} {
			for _, enabled := range []bool{false, true} {
				specification := AppSpec{App: AppSpecApp{Auth: AppSpecAuth{Enabled: enabled, DevMode: true, BasePath: basePath}},
					Screens: []AppSpecScreen{{Name: "page", Route: base + suffix, Body: []AppSpecBlock{{Type: "heading", Text: "Page"}}}}}
				err := ValidateAppSpec(specification)
				if enabled && strings.TrimRight(suffix, "/") == "/me" {
					if err == nil || !strings.Contains(err.Error(), "auth") {
						t.Errorf("auth screen %s accepted: %v", base+suffix, err)
					}
				} else if err != nil {
					t.Errorf("reachable screen %s refused: %v", base+suffix, err)
				}
			}
		}
	}
}
