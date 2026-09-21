package ui

import (
	"strings"
	"testing"

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

// cfg.Class lands on the root beside the class map's own classes,
// appended rather than replacing them.
func TestDetailListAppliesClassOnTheRoot(t *testing.T) {
	h := string(DetailList(DetailListConfig{
		Items: []DetailItem{{Label: "Name", Value: render.Text("Ada")}},
		Class: "record-head",
	}))
	if !strings.Contains(h, `class="fui-detail-list record-head"`) {
		t.Errorf("the caller's Class did not land after the base class:\n%s", h)
	}
}
