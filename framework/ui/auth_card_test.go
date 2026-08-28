package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestAuthCardRendersTitleAndFooter(t *testing.T) {
	h := AuthCard(AuthCardConfig{
		Title:  "Sign in",
		Footer: render.Text("No account?"),
	})
	for _, want := range []string{
		`data-fui-comp="ui-auth-card"`,
		"ui-auth-card__title",
		"Sign in",
		"No account?",
	} {
		mustContain(t, h, want)
	}
}

func TestAuthCardExtraAttrsOnRoot(t *testing.T) {
	h := AuthCard(AuthCardConfig{
		Title:      "Sign in",
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("root missing data-test:\n%s", root)
	}
}
