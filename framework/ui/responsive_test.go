package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestResponsiveEmitsBothVariants(t *testing.T) {
	h := string(Responsive(ResponsiveConfig{},
		render.Text("DESKTOP"), render.Text("MOBILE")))
	for _, want := range []string{
		`data-fui-comp="ui-responsive-1024"`,
		"DESKTOP",
		"ui-responsive__mobile",
		"MOBILE",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("Responsive missing %q:\n%s", want, h)
		}
	}
}

func TestResponsiveExtraAttrsOnRoot(t *testing.T) {
	h := Responsive(ResponsiveConfig{
		ExtraAttrs: map[string]string{"data-test": "hook"},
	}, render.Text("d"), render.Text("m"))
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("Responsive root missing data-test:\n%s", root)
	}
}
