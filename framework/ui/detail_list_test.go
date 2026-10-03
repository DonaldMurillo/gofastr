package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// TestDetailListClassBuiltWithoutLeadingSpace pins the class attribute's
// shape: a lone Class used to produce class=" custom" (leading space)
// because the string was appended after the Inline branch left it empty.
func TestDetailListClassBuiltWithoutLeadingSpace(t *testing.T) {
	h := string(DetailList(DetailListConfig{
		Class: "custom",
		Items: []DetailItem{{Label: "L", Value: render.Text("V")}},
	}))
	root := h[:strings.Index(h, ">")+1]
	if !strings.Contains(root, `class="fui-detail-list custom"`) {
		t.Fatalf("plain Class should render one clean class list, got:\n%s", root)
	}
	h = string(DetailList(DetailListConfig{
		Inline: true,
		Class:  "custom",
		Items:  []DetailItem{{Label: "L", Value: render.Text("V")}},
	}))
	root = h[:strings.Index(h, ">")+1]
	if !strings.Contains(root, `class="fui-detail-list fui-detail-list--inline custom"`) {
		t.Fatalf("Inline+Class should render both variants space-separated, got:\n%s", root)
	}
	if strings.Contains(root, `"  `) || strings.Contains(root, `class=" `) {
		t.Fatalf("class attribute carries stray spacing:\n%s", root)
	}
}

// DetailList's label column reads the --ui-detail-list-label-track
// knob and keeps the page default without it.
func TestDetailListLabelTrackKnob(t *testing.T) {
	if !strings.Contains(detailListCSS(style.Theme{}), "grid-template-columns: var(--ui-detail-list-label-track, minmax(7rem, 13rem)) 1fr;") {
		t.Error("detail list label column does not read the knob with the old default")
	}
}
