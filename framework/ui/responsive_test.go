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
		`data-fui-comp="ui-responsive"`,
		"DESKTOP",
		"fui-responsive__mobile",
		"MOBILE",
	} {
		if !strings.Contains(h, want) {
			t.Errorf("Responsive missing %q:\n%s", want, h)
		}
	}
}

func TestResponsivePostureClass(t *testing.T) {
	md := string(Responsive(ResponsiveConfig{}, render.Text("d"), render.Text("m")))
	if strings.Contains(md, "fui-responsive--stack-below-lg") {
		t.Errorf("the md default carries the lg modifier:\n%s", md)
	}
	lg := string(Responsive(ResponsiveConfig{Below: StackBelowLG}, render.Text("d"), render.Text("m")))
	if !strings.Contains(lg, `class="fui-responsive fui-responsive--stack-below-lg"`) {
		t.Errorf("StackBelowLG root lacks the lg modifier:\n%s", lg)
	}
}

func TestResponsiveUnknownBreakpointPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatal("an unknown Below value was accepted")
		}
	}()
	Responsive(ResponsiveConfig{Below: "xl"}, render.Text("d"), render.Text("m"))
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
