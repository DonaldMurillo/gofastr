package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestDetailListRendersRows(t *testing.T) {
	h := DetailList(DetailListConfig{
		Items: []DetailItem{{Label: "Name", Value: render.Text("Ada")}},
	})
	for _, want := range []string{
		`data-fui-comp="ui-detail-list"`,
		"<dt",
		"<dd",
		"Ada",
	} {
		mustContain(t, h, want)
	}
}

func TestDetailListExtraAttrsOnRoot(t *testing.T) {
	h := DetailList(DetailListConfig{
		Items:      []DetailItem{{Label: "Name", Value: render.Text("Ada")}},
		ExtraAttrs: map[string]string{"data-test": "hook"},
	})
	root := string(h)[:strings.Index(string(h), ">")+1]
	if !strings.Contains(root, `data-test="hook"`) {
		t.Errorf("dl root missing data-test:\n%s", root)
	}
}

// DetailList's label column reads the --ui-detail-list-label-track
// knob and keeps the page default without it.
func TestDetailListLabelTrackKnob(t *testing.T) {
	if !strings.Contains(detailListCSS(style.Theme{}), "grid-template-columns: var(--ui-detail-list-label-track, minmax(7rem, 13rem)) 1fr;") {
		t.Error("detail list label column does not read the knob with the old default")
	}
}
