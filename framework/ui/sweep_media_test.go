package ui

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/headless"
)

func TestSweepMediaAvatarGroupOverflowIsANamedImage(t *testing.T) {
	out := string(AvatarGroup(AvatarGroupConfig{
		Avatars: []AvatarConfig{{Name: "Ada"}, {Name: "Grace"}, {Name: "Joan"}},
		Max:     1,
	}))
	i := classTokenIndex(out, "fui-avatar-group__overflow")
	if i < 0 {
		t.Fatalf("no overflow pill:\n%s", out)
	}
	open := out[strings.LastIndex(out[:i], "<"):]
	open = open[:strings.Index(open, ">")+1]
	for _, want := range []string{`role="img"`, `aria-label="2 more"`} {
		if !strings.Contains(open, want) {
			t.Errorf("overflow pill %s lacks %s", open, want)
		}
	}
	if !strings.Contains(out, `<span aria-hidden="true">+2</span>`) {
		t.Errorf("the visible +2 is not aria-hidden:\n%s", out)
	}
}

func TestSweepMediaGalleryLightboxWinsOverHrefFn(t *testing.T) {
	out := string(Gallery(GalleryConfig{
		Items:    []GalleryItem{{Src: "/a.jpg", Alt: "A"}},
		Lightbox: "viewer",
		HrefFn:   func(int, GalleryItem) string { return "/detail/a" },
	}))
	if strings.Contains(out, "/detail/a") {
		t.Errorf("HrefFn drove the anchor although Lightbox is set:\n%s", out)
	}
	if !strings.Contains(out, `data-cui-open="viewer"`) {
		t.Errorf("the item is not a trigger for the paired lightbox:\n%s", out)
	}
}

func TestSweepMediaGalleryThumbsLoadLazily(t *testing.T) {
	out := string(Gallery(GalleryConfig{Items: []GalleryItem{{Src: "/a.jpg", Alt: "A"}}}))
	i := strings.Index(out, "<img")
	if i < 0 {
		t.Fatalf("no thumb:\n%s", out)
	}
	img := out[i : i+strings.Index(out[i:], ">")+1]
	if !strings.Contains(img, `loading="lazy"`) {
		t.Errorf("gallery thumb is not lazy: %s", img)
	}
}

func TestSweepMediaHighlightStringHidesCommentMarkers(t *testing.T) {
	h := highlightJoined(`u := "http://a" // c`, "go")
	if !strings.Contains(h, spanOf("tk-str", "&quot;http://a&quot;")) || !strings.Contains(h, spanOf("tk-com", "// c")) {
		t.Errorf("a // inside a string was read as a comment:\n%s", h)
	}
}

func TestSweepMediaHighlightUnterminatedQuoteStopsAtLineEnd(t *testing.T) {
	lines := HighlightLines("a := \"oops\nb := 1\n", "go")
	if len(lines) != 2 || !strings.Contains(string(lines[1]), spanOf("tk-num", "1")) {
		t.Errorf("an unterminated string swallowed the next line: %q", lines)
	}
}

func TestSweepMediaHighlightHashCommentNeedsWordBoundary(t *testing.T) {
	for lang, src := range map[string]string{
		"bash": "echo ${#x} a#b\n",
		"yaml": "key: a#b\n",
	} {
		if h := highlightJoined(src, lang); strings.Contains(h, "tk-com") {
			t.Errorf("%s: a # inside a word opened a comment:\n%s", lang, h)
		}
	}
}

func TestSweepMediaHighlightLanguageFamilies(t *testing.T) {
	h := highlightJoined("# note\nx = 'a' + 12\n", "python")
	for _, want := range []string{spanOf("tk-com", "# note"), spanOf("tk-str", "&#39;a&#39;"), spanOf("tk-num", "12")} {
		if !strings.Contains(h, want) {
			t.Errorf("generic family missing %s:\n%s", want, h)
		}
	}
	for _, lang := range []string{"", "text", "plain"} {
		if h := highlightJoined("x = 'a' // 1", lang); strings.Contains(h, "<span") {
			t.Errorf("language %q was tokenized:\n%s", lang, h)
		}
	}
}

func TestSweepMediaHighlightDropsTrailingBlankLine(t *testing.T) {
	if lines := HighlightLines("a\nb\n", "go"); len(lines) != 2 {
		t.Errorf("lines = %q, want two: a trailing newline is not a row", lines)
	}
}

func TestSweepMediaCodeTabsCSSCoversEveryPanelIndex(t *testing.T) {
	css := codeTabsCSS(style.Theme{})
	for i := range headless.TabsMaxPanels() {
		for _, want := range []string{
			fmt.Sprintf(`.fui-code-tabs__strip[data-active="%d"] .fui-code-tabs__panel[data-cui-tab-index="%d"]{display:block}`, i, i),
			fmt.Sprintf(`.fui-code-tabs__strip[data-active="%d"] .fui-code-tabs__tab[data-cui-tab-index="%d"]{`, i, i),
		} {
			if !strings.Contains(css, want) {
				t.Errorf("codeTabsCSS has no rule for tab %d: %s", i, want)
			}
		}
	}
}

func TestSweepMediaFenceMetaKeepsEarlierValidValue(t *testing.T) {
	got := parseFenceMeta(`{1,3} highlight=5-3 words=a words=`)
	if !rangesEqual(got.highlight, []LineRange{{From: 1}, {From: 3}}) {
		t.Errorf("an invalid later highlight replaced a valid one: %v", got.highlight)
	}
	if !slicesEqual(got.words, []string{"a"}) {
		t.Errorf("an empty later words spec changed the words: %v", got.words)
	}
}

func TestSweepMediaThemeToggleButtonsCarryLoaderMarker(t *testing.T) {
	for _, v := range []ThemeToggleVariant{ThemeToggleIcon, ThemeToggleLabel, ThemeTogglePill} {
		out := string(ThemeToggle(ThemeToggleConfig{Variant: v}))
		root := out[:strings.Index(out, ">")+1]
		if !strings.Contains(root, `data-hui-theme-toggle=""`) {
			t.Errorf("%s toggle root lacks the module's loader marker: %s", v, root)
		}
	}
}

func TestSweepMediaPricingHeadingLevelFallsBack(t *testing.T) {
	for level, want := range map[int]string{0: "<h3", 9: "<h3", 2: "<h2"} {
		out := string(PricingCard(PricingCardConfig{Name: "Pro", Price: "$9", HeadingLevel: level}))
		if !strings.Contains(out, want+` class="fui-pricing-card__name"`) {
			t.Errorf("HeadingLevel %d: want %s heading:\n%s", level, want, out)
		}
	}
}

func TestSweepMediaOptimizedImageLeavesCallerSourcesAlone(t *testing.T) {
	sources := []ImageSource{{URL: "javascript:alert(1)", Width: 320}, {URL: "/a.jpg", Width: 640}}
	OptimizedImage(OptimizedImageConfig{Src: "/a.jpg", Alt: "A", Width: 8, Height: 6, Sources: sources})
	if sources[0].URL != "javascript:alert(1)" {
		t.Errorf("OptimizedImage rewrote the caller's Sources slice: %v", sources)
	}
}

func TestSweepMediaCommentThreadMarksAnEmptyCommentAboveHeldReplies(t *testing.T) {
	out := string(CommentThread(CommentThreadConfig{Label: "Comments", Comments: []Comment{{
		Replies: []Comment{{Body: render.HTML(`<i ` + sentinelAttr + `="1"></i>`)}},
	}}}))
	for _, v := range markingViolations(out) {
		t.Error(v)
	}
}

func TestSweepMediaStepWizardClassesAreStyled(t *testing.T) {
	out := string(StepWizard(StepWizardConfig{Action: "/w", Steps: []StepWizardStep{{Heading: "A", Description: "d"}, {Heading: "B"}}}))
	css := stepWizardCSS(style.Theme{})
	for _, m := range regexp.MustCompile(`class="([^"]*)"`).FindAllStringSubmatch(out, -1) {
		for _, tok := range strings.Fields(m[1]) {
			if strings.HasPrefix(tok, "fui-step-wizard") && !strings.Contains(css, "."+tok) {
				t.Errorf("StepWizard emits class %s that its sheet never selects", tok)
			}
		}
	}
}

func TestSweepMediaSourcesFromHeadersSkipIncomplete(t *testing.T) {
	got := PipelineSourcesFromHeaders([]HeaderInfo{
		{Name: "a", Width: 320, MIME: "image/webp"}, {Name: "b", Width: 640, MIME: "image/webp"},
		{Name: "c", Width: 0, MIME: "image/webp"}, {Name: "d", Width: 800, MIME: ""},
	},
		func(name string) string {
			if name == "a" {
				return ""
			}
			return "/" + name
		})
	if len(got) != 1 || got[0].URL != "/b" {
		t.Errorf("an incomplete header was kept: %v", got)
	}
}

func TestSweepMediaPipelineImageOneSourcePerType(t *testing.T) {
	out := string(PipelineImage(PipelineImageConfig{Fallback: "/a.jpg", Alt: "A", Width: 8, Height: 6, Sources: []PipelineSource{
		{URL: "/a.webp", Width: 320, Type: "image/webp"}, {URL: "/b.webp", Width: 640, Type: "image/webp"},
		{URL: "/a.jpg", Width: 320, Type: "image/jpeg"},
	}}))
	if n := strings.Count(out, "<source"); n != 2 {
		t.Errorf("<source> elements = %d, want one per type (2):\n%s", n, out)
	}
}
