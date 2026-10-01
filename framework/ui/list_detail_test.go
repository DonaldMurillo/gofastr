package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// TestListDetailViewportResetKeysContentRow pins the viewport detail-cell
// reset's selector: it must key on the content row's public --viewport
// modifier (the element ui.ContentRow renders). A sheet that names any
// other frame's class instead styles nothing — the group layer's content
// cell keeps the padding the pane is supposed to own.
func TestListDetailViewportResetKeysContentRow(t *testing.T) {
	css := listDetailCSS(style.Theme{})
	if !strings.Contains(css, ".fui-content-row--viewport .fui-list-detail__detail > .layout-content") {
		t.Fatalf("the viewport detail-cell reset must key on .fui-content-row--viewport:\n%s", css)
	}
}

func TestListDetailBackLinkInDetailPane(t *testing.T) {
	out := string(ListDetail(ListDetailConfig{
		ListLabel: "Issues", MobileSinglePane: true,
		BackHref: "/projects/core", BackLabel: "Back to issues",
		List: render.Text("rows"), Detail: render.Text("THE-DETAIL"),
	}))
	pane := out[strings.Index(out, `class="fui-list-detail__detail"`):]
	back := strings.Index(pane, `class="fui-list-detail__back"`)
	if back < 0 {
		t.Fatalf("no back link in the detail pane:\n%s", out)
	}
	if back > strings.Index(pane, "THE-DETAIL") {
		t.Errorf("back link must sit above the detail content:\n%s", pane)
	}
	for _, want := range []string{`href="/projects/core"`, "Back to issues"} {
		if !strings.Contains(pane, want) {
			t.Errorf("back link missing %s:\n%s", want, pane)
		}
	}
}

func TestListDetailBackLabelDefaultsToBack(t *testing.T) {
	out := string(ListDetail(ListDetailConfig{
		ListLabel: "Issues", MobileSinglePane: true, BackHref: "/issues",
	}))
	if !strings.Contains(out, ">Back<") {
		t.Errorf("default label is not Back:\n%s", out)
	}
}

func TestListDetailNoBackHrefNoLink(t *testing.T) {
	out := string(ListDetail(ListDetailConfig{ListLabel: "Issues", MobileSinglePane: true}))
	if strings.Contains(out, "fui-list-detail__back") {
		t.Errorf("back link rendered without BackHref:\n%s", out)
	}
}

func TestListDetailBackHrefNeedsSinglePane(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("BackHref without MobileSinglePane was accepted")
		}
		if msg, _ := r.(string); !strings.Contains(msg, "MobileSinglePane") {
			t.Errorf("panic does not name MobileSinglePane: %v", r)
		}
	}()
	ListDetail(ListDetailConfig{ListLabel: "Issues", BackHref: "/issues"})
}
