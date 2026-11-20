package interactive

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/render"
)

func TestWrapSkipsSingleQuotedGT(t *testing.T) {
	got := string(OnClick(render.HTML(`<button title='1>2'>Go</button>`), Post("/api/t")))
	if !strings.HasPrefix(got, `<button title='1>2' data-cui-rpc="/api/t"`) {
		t.Fatalf("attributes landed inside a single-quoted value: %s", got)
	}
}

func TestDropdownAttrsAreSorted(t *testing.T) {
	trigger := render.HTML(`<button>Menu</button>`)
	panel := render.HTML(`<div>Items</div>`)
	want := string(Dropdown(trigger, panel))
	if !strings.Contains(want, `<button aria-expanded="false" aria-haspopup="true" data-cui-dropdown="">`) {
		t.Fatalf("trigger attributes not in sorted order: %s", want)
	}
	for range 20 {
		if got := string(Dropdown(trigger, panel)); got != want {
			t.Fatalf("Dropdown changed between renders:\n%s\n%s", got, want)
		}
	}
}

func TestToastJSONKeepsMarkupCharacters(t *testing.T) {
	got := string(ToastOnClick(render.HTML(`<button>X</button>`), Toast{Title: "a<b & c>d"}))
	want := string(render.Tag("button", map[string]string{"data-cui-toast": `{"title":"a<b & c>d"}`}, render.Text("X")))
	if got != want {
		t.Fatalf("toast JSON escaped markup characters:\n got %s\nwant %s", got, want)
	}
}

func TestBindAttrRefusesExecutingAttributes(t *testing.T) {
	el := render.HTML(`<div></div>`)
	for _, attr := range []string{"srcdoc", "style", "onclick", "OnClick", "sandbox", "data-behavior", "data-cui-rpc"} {
		if got := BindAttr(el, "s", attr); got != el {
			t.Errorf("BindAttr(%q) = %s, want the element unchanged", attr, got)
		}
	}
	for _, attr := range []string{"aria-expanded", "ARIA-Hidden", "data-active", "Disabled", "href"} {
		if got := string(BindAttr(el, "s", attr)); !strings.Contains(got, "data-cui-signal-attr=") {
			t.Errorf("BindAttr(%q) = %s, want a binding", attr, got)
		}
	}
}

func TestSectionMenuRailGroupsAreNotDisclosures(t *testing.T) {
	cfg := SectionMenuConfig{Groups: []SectionGroup{{Label: "Docs", Collapsed: true, Items: []SectionItem{{Label: "Intro", Href: "/intro"}}}}}
	rail := string(SectionMenu(cfg))
	if strings.Contains(rail, "<summary") || strings.Contains(rail, "<details") || !strings.Contains(rail, `role="group"`) {
		t.Fatalf("rail group is a disclosure: %s", rail)
	}
	sheet := string(sectionMenuDrawerSlot{cfg: cfg}.Render())
	if !strings.Contains(sheet, "<summary") {
		t.Fatalf("sheet group is not a disclosure: %s", sheet)
	}
}

func TestSectionMenuSheetSpeaksTheRequestLocale(t *testing.T) {
	cfg := SectionMenuConfig{DrawerName: "docs-nav", Groups: []SectionGroup{{Label: "Docs", Items: []SectionItem{{Label: "Intro", Href: "/intro"}}}}}
	got := string(sectionMenuDrawerSlot{cfg: cfg}.RenderCtx(frenchInteractive()))
	if !strings.Contains(got, "Sommaire") {
		t.Fatalf("a sheet mounted without a ctx did not speak the request's locale: %s", got)
	}
}

func TestToastActionNeedsAnRPC(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("NewToastAction accepted an Action with no RPC")
		}
	}()
	NewToastAction("Undo", Action{})
}
