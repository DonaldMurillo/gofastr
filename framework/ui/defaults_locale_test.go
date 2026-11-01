package ui

import (
	"context"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/i18nui"
)

// frenchWords carries a French translator holding the given phrases.
func frenchWords(words map[i18n.Phrase]string) context.Context {
	cat := i18n.NewMapCatalog()
	for p, text := range words {
		cat.Set("fr", p.Key(), i18n.Message{Text: text})
	}
	ctx := i18n.WithContext(context.Background(), i18n.Locale{Tag: "fr"})
	return i18n.WithTranslator(ctx, i18n.NewTranslator(cat, "en"))
}

// TestDefaultLabelsSpeakCtx: each component's default words come from
// its Ctx, and a nil Ctx keeps the English.
func TestDefaultLabelsSpeakCtx(t *testing.T) {
	cases := []struct {
		name    string
		phrase  i18n.Phrase
		french  string
		english string
		render  func(ctx context.Context) render.HTML
	}{
		// 2026-10-27 U10, U21: the code blocks inside Markdown and
		// CodeTabs said their words in English on every locale.
		{"markdown code block", i18nui.CodeBlockCopy, "copier", "copy", func(ctx context.Context) render.HTML {
			return Markdown(MarkdownConfig{Ctx: ctx, Source: "```go\nx := 1\n```\n"})
		}},
		{"code tabs", i18nui.CodeBlockCopy, "copier", "copy", func(ctx context.Context) render.HTML {
			return CodeTabs(CodeTabsConfig{Ctx: ctx, Name: "t"}, CodeSample{Label: "Go", Language: "go", Code: "x := 1"})
		}},
		{"avatar group", i18nui.AvatarGroupLabel, "Avatars du groupe", "Avatars", func(ctx context.Context) render.HTML {
			return AvatarGroup(AvatarGroupConfig{Ctx: ctx, Avatars: []AvatarConfig{{Name: "Ada"}}})
		}},
		{"avatar overflow", i18nui.AvatarGroupMore, "{{count}} de plus", "1 more", func(ctx context.Context) render.HTML {
			return AvatarGroup(AvatarGroupConfig{Ctx: ctx, Max: 1, Avatars: []AvatarConfig{{Name: "Ada"}, {Name: "Grace"}}})
		}},
		{"content row sidebar", i18nui.ContentRowSidebar, "Barre latérale", "Sidebar", func(ctx context.Context) render.HTML {
			return ContentRow(ContentRowConfig{Ctx: ctx, Sidebar: render.Text("nav")}, render.Text("main"))
		}},
		{"content row aside", i18nui.ContentRowAside, "Contexte", "Context", func(ctx context.Context) render.HTML {
			return ContentRow(ContentRowConfig{Ctx: ctx, Aside: render.Text("aside")}, render.Text("main"))
		}},
		{"gallery", i18nui.GalleryLabel, "Galerie d'images", "Image gallery", func(ctx context.Context) render.HTML {
			return Gallery(GalleryConfig{Ctx: ctx, Items: []GalleryItem{{Src: "/a.png", Alt: "A"}}})
		}},
		{"list detail back", i18nui.ListDetailBack, "Retour", "Back", func(ctx context.Context) render.HTML {
			return ListDetail(ListDetailConfig{Ctx: ctx, ListLabel: "Inbox", MobileSinglePane: true, BackHref: "/inbox"})
		}},
		{"network retry", i18nui.NetworkRetryTitle, "Connexion perdue", "Connection lost", func(ctx context.Context) render.HTML {
			return NetworkRetryBanner(NetworkRetryBannerConfig{Ctx: ctx, HealthEndpoint: "/healthz"})
		}},
		{"step rail", i18nui.StepRailLabel, "Étapes de la page", "Page steps", func(ctx context.Context) render.HTML {
			return StepRail(StepRailConfig{Ctx: ctx, Items: []StepRailItem{{Number: "1", Anchor: "a", Label: "One"}}})
		}},
		{"toc sheet", i18nui.HuiTableOfContentsLabel, "Sur cette page", "On this page", func(ctx context.Context) render.HTML {
			return TableOfContents(TOCConfig{Ctx: ctx, PhoneSheet: true, Items: []TOCItem{{ID: "a", Label: "A", Level: 2}}})
		}},
		{"sidebar trigger", i18nui.SidebarOpenDrawer, "Ouvrir la navigation", "Open navigation", func(ctx context.Context) render.HTML {
			return SidebarDrawerTrigger(SidebarConfig{Ctx: ctx, DrawerName: "nav-drawer"})
		}},
		// 2026-11-01 R7-015: these handed headless StringsFor(nil), so
		// their kit words read English on every locale.
		{"network retry tone", i18nui.ToneWarning, "Attention", "Warning", func(ctx context.Context) render.HTML {
			return NetworkRetryBanner(NetworkRetryBannerConfig{Ctx: ctx, HealthEndpoint: "/healthz"})
		}},
		{"rating choice", i18nui.HuiRatingChoice, "{{n}} sur {{max}}", "3 out of 5", func(ctx context.Context) render.HTML {
			return RatingInput(RatingConfig{Ctx: ctx, Name: "r", Label: "Note", Max: 5})
		}},
		{"tag input remove", i18nui.TagRemoveLabelled, "Retirer {{label}}", "Remove go", func(ctx context.Context) render.HTML {
			return TagInput(TagInputConfig{Ctx: ctx, Name: "tags", Label: "Tags", Values: []string{"go"}})
		}},
		{"range slider thumb", i18nui.HuiRangeLow, "{{label}} minimum", "Minimum Prix", func(ctx context.Context) render.HTML {
			return RangeSlider(RangeSliderConfig{Ctx: ctx, Name: "p", Label: "Prix"})
		}},
		{"back to top", i18nui.HuiBackToTop, "Retour en haut", "Back to top", func(ctx context.Context) render.HTML {
			return BackToTop(BackToTopConfig{Ctx: ctx})
		}},
		// The presence dot named itself with the raw status value
		// ("online") on every locale.
		{"avatar status", i18nui.AvatarStatusOnline, "En ligne", `aria-label="Online"`, func(ctx context.Context) render.HTML {
			return Avatar(AvatarConfig{Ctx: ctx, Name: "Ada", Status: AvatarOnline})
		}},
		{"avatar group status", i18nui.AvatarStatusBusy, "Occupé", `aria-label="Busy"`, func(ctx context.Context) render.HTML {
			return AvatarGroup(AvatarGroupConfig{Ctx: ctx, Avatars: []AvatarConfig{{Name: "Ada", Status: AvatarBusy}}})
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			fr := string(c.render(frenchWords(map[i18n.Phrase]string{c.phrase: c.french})))
			want := strings.NewReplacer("{{count}}", "1", "{{n}}", "3", "{{max}}", "5", "{{label}}", "Prix").Replace(c.french)
			if c.phrase == i18nui.TagRemoveLabelled {
				want = strings.ReplaceAll(c.french, "{{label}}", "go")
			}
			if !strings.Contains(fr, render.Escape(want)) {
				t.Errorf("French ctx: %q missing from\n%s", want, fr)
			}
			en := string(c.render(nil))
			if !strings.Contains(en, c.english) {
				t.Errorf("nil ctx: %q missing from\n%s", c.english, en)
			}
		})
	}
}

// TestNumbersSpeakCtx: the slider's edge labels and the JSON viewer's
// counts are written in the digits of the Ctx locale.
func TestNumbersSpeakCtx(t *testing.T) {
	ar := i18n.WithContext(context.Background(), i18n.Locale{Tag: "ar-EG"})
	slider := string(Slider(SliderConfig{Ctx: ar, Name: "v", Label: "Volume", Min: 0, Max: 40, Value: 10, ShowEdgeLabels: true}))
	if !strings.Contains(slider, "٤٠") {
		t.Errorf("slider edge in ar-EG has no Arabic-Indic 40:\n%s", slider)
	}
	tree := string(JSONViewer(JSONViewerConfig{Ctx: ar, Value: []any{1, 2, 3}}))
	if !strings.Contains(tree, "٣") {
		t.Errorf("JSON array count in ar-EG has no Arabic-Indic 3:\n%s", tree)
	}
	// A phrase writes its numbers the way the answering catalog's
	// language does, so the hint needs an Arabic catalog entry.
	cat := i18n.NewMapCatalog()
	cat.Set("ar-EG", i18nui.DropzoneMaxSize.Key(), i18n.Message{Text: "الحد {{n}} ميغابايت."})
	arCat := i18n.WithTranslator(ar, i18n.NewTranslator(cat, "en"))
	drop := string(FileDropzone(FileDropzoneConfig{Ctx: arCat, Name: "f", Label: "Files", MaxSizeMB: 25}))
	if !strings.Contains(drop, "٢٥") {
		t.Errorf("dropzone size hint in ar-EG has no Arabic-Indic 25:\n%s", drop)
	}
	rail := string(AnchoredRail(AnchoredRailConfig{Ctx: ar, Label: "Sections", Items: []RailItem{{Anchor: "a", Text: "A", Count: 7}}}))
	if !strings.Contains(rail, "٧") {
		t.Errorf("rail count in ar-EG has no Arabic-Indic 7:\n%s", rail)
	}
	rows := string(FilterRows(FilterRowsConfig{Ctx: ar, ID: "fr", FieldName: "f", OpName: "o", ValueName: "v",
		Legend: "Filters", FieldLabel: "Field", OpLabel: "Op", ValueLabel: "Value"}))
	if !strings.Contains(rows, "Value ١") || !strings.Contains(rows, `id="fr-1-v"`) {
		t.Errorf("filter row label in ar-EG wants Arabic-Indic 1 and an ASCII id:\n%s", rows)
	}
}
