package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestContentRowAsideLandmarkAfterMain(t *testing.T) {
	h := string(ContentRow(ContentRowConfig{Aside: render.Text("Events"), AsideLabel: "Activity", Toolbar: Toolbar(ToolbarConfig{Label: "Actions", Groups: []ToolbarGroup{{Children: []render.HTML{render.Text("Create")}}}})}, render.Tag("main", nil, render.Text("Detail"))))
	if !strings.Contains(h, `aria-label="Activity"`) || strings.Index(h, "</main>") > strings.Index(h, "<aside") {
		t.Fatalf("aside must be labelled and follow main: %s", h)
	}
	if strings.Index(h, "Create") > strings.Index(h, "<main") {
		t.Fatalf("toolbar must precede main: %s", h)
	}
}

func TestListDetailAccessibleScrollRegion(t *testing.T) {
	h := string(ListDetail(ListDetailConfig{ListLabel: "Issues", List: render.Text("List"), Detail: render.Text("Detail")}))
	if !strings.Contains(h, `aria-label="Issues"`) || !strings.Contains(h, `tabindex="0"`) {
		t.Fatalf("list must be a labelled keyboard-reachable region: %s", h)
	}
	if strings.Index(h, "List") > strings.Index(h, "Detail") {
		t.Fatalf("list must precede detail: %s", h)
	}
}

func TestListDetailOnlyCarriesLayoutTransitionWiring(t *testing.T) {
	h := string(ListDetail(ListDetailConfig{ListLabel: "Issues", ExtraAttrs: map[string]string{
		"data-fui-vt": "detail-region", "data-fui-vt-when": "(max-width: 767px)",
		"data-fui-comp": "spoof", "data-fui-open": "spoof", "DATA-FUI-RUN": "spoof",
		"class": "spoof",
	}}))
	if strings.Contains(h, "spoof") {
		t.Fatalf("unrelated wiring or owned attributes survived: %s", h)
	}
	if !strings.Contains(h, `data-fui-vt="detail-region"`) || !strings.Contains(h, `data-fui-vt-when="(max-width: 767px)"`) {
		t.Fatalf("layout transition wiring was lost: %s", h)
	}
}
