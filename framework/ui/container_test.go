package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestContainerDefaultsToDiv(t *testing.T) {
	h := string(Container(ContainerConfig{}, render.Text("body")))
	if !strings.Contains(h, "<div ") {
		t.Errorf("default As should render <div>:\n%s", h)
	}
	if !strings.Contains(h, "body") {
		t.Errorf("children should render:\n%s", h)
	}
}

func TestContainerAsTagOverride(t *testing.T) {
	h := string(Container(ContainerConfig{As: "main"}, render.Text("x")))
	if !strings.Contains(h, "<main ") {
		t.Errorf("As: main should render <main>:\n%s", h)
	}
}

func TestContainerWidthVariantClass(t *testing.T) {
	cases := map[ContainerWidth]string{
		ContainerNarrow: "ui-container--narrow",
		ContainerWide:   "ui-container--wide",
		ContainerFull:   "ui-container--full",
	}
	for w, cls := range cases {
		h := string(Container(ContainerConfig{Width: w}))
		if !strings.Contains(h, cls) {
			t.Errorf("Width=%q should emit .%s:\n%s", w, cls, h)
		}
	}
}

func TestContainerDefaultWidthEmitsNoModifier(t *testing.T) {
	h := string(Container(ContainerConfig{}))
	if strings.Contains(h, "ui-container--") {
		t.Errorf("default Width should not emit a modifier class:\n%s", h)
	}
}

func TestContainerRejectsUnknownWidth(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("Container with unknown Width should panic")
		}
	}()
	Container(ContainerConfig{Width: ContainerWidth("huge")})
}
