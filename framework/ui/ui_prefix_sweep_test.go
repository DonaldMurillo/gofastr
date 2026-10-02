package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// The framework/gallery prefix gate renders catalog entries, but a few
// entries carry only a note: Gallery, DataTable, CommandPalette,
// PipelineImage, ConfirmAction, NotificationBell and GlobalSearch. This
// sweep renders those components here, variants and widget slots
// included, and refuses any ui- class token in the markup.

func uiClassTokens(h string) []string {
	var bad []string
	for _, m := range tokClassAttrRe.FindAllStringSubmatch(h, -1) {
		for _, tok := range strings.Fields(m[1]) {
			if strings.HasPrefix(tok, "ui-") {
				bad = append(bad, tok)
			}
		}
	}
	return bad
}

func slotsHTML(b *widget.Builder) string {
	var sb strings.Builder
	for _, s := range b.Definition().Slots {
		sb.WriteString(string(s.Component.Render()))
	}
	return sb.String()
}

func TestNoteOnlyComponentsRenderNoUIClassTokens(t *testing.T) {
	items := []GalleryItem{
		{Src: "/a.png", Alt: "a", Caption: "cap"},
		{Src: "/b.png", Alt: "b"},
	}
	palette, paletteW := CommandPalette(CommandPaletteConfig{Name: "cp", RPCPath: "/commands/search", FallbackHref: "/search"})
	confirm, confirmW := ConfirmAction(ConfirmActionConfig{
		Name: "del", TriggerLabel: "Delete", Title: "Delete?", Body: "Permanent.", RPCPath: "/del",
	})
	bell, bellW := NotificationBell(NotificationBellConfig{Name: "bell", Label: "Notifications", Href: "/notifications", UnreadCount: 3})

	cases := map[string]render.HTML{
		"gallery grid":     Gallery(GalleryConfig{Items: items}),
		"gallery strip":    Gallery(GalleryConfig{Items: items, Variant: GalleryStrip, CaptionMode: GalleryCaptionOverlay}),
		"gallery masonry":  Gallery(GalleryConfig{Items: items, Variant: GalleryMasonry, CaptionMode: GalleryCaptionOff}),
		"gallery lightbox": Gallery(GalleryConfig{Items: items, ID: "g", Lightbox: "lb"}),
		"gallery hreffn":   Gallery(GalleryConfig{Items: items, HrefFn: func(i int, _ GalleryItem) string { return "/p" }}),
		"datatable": DataTable(DataTableConfig{
			Columns: []Column{{Key: "price", Header: "Price", Sortable: true}},
			Rows:    []Row{{Cells: map[string]render.HTML{"price": render.Text("9")}}},
		}),
		"datatable empty": DataTable(DataTableConfig{Columns: []Column{{Key: "x", Header: "X"}}}),
		"palette":         palette + render.HTML(slotsHTML(paletteW)),
		"confirm":         confirm + render.HTML(slotsHTML(confirmW)),
		"bell":            bell + render.HTML(slotsHTML(bellW)),
		"global search": GlobalSearch(GlobalSearchConfig{
			ID: "s", Name: "q", Label: "Search", RPCPath: "/s", SignalName: "x", NoScriptAction: "/s",
		}),
		"pipeline image": PipelineImage(PipelineImageConfig{
			Fallback: "/hero.jpg", Alt: "Hero", Width: 800, Height: 600,
			Sources:     []PipelineSource{{URL: "/hero.webp", Width: 800, Type: "image/webp"}},
			Placeholder: testPlaceholder, Fit: ImageFitContain, Aspect: ImageAspect16x9, Rounded: true,
		}),
	}
	for name, h := range cases {
		if !strings.Contains(string(h), `class="`) {
			t.Errorf("%s: rendered no class attribute, the sweep cannot see it:\n%s", name, h)
			continue
		}
		if bad := uiClassTokens(string(h)); len(bad) > 0 {
			t.Errorf("%s renders ui- class tokens %v; framework/ui classes are fui-:\n%s", name, bad, h)
		}
	}
}
