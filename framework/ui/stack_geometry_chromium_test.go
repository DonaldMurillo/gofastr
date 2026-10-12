//go:build chromium

package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
	"github.com/chromedp/chromedp"
)

// geometryOf renders page with css at a 900px body, under the
// border-box reset every uihost page carries, and returns what probe
// measures.
func geometryOf(t *testing.T, css string, page render.HTML, probe string) map[string]float64 {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<style>*,*::before,*::after{box-sizing:border-box}body{margin:0;width:900px}</style>
<style>%s</style>%s`, css, string(page))
	}))
	defer srv.Close()

	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.WSURLReadTimeout(90*time.Second),
			chromedp.NoSandbox, chromedp.WindowSize(1000, 800))...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelTimeout := context.WithTimeout(ctx, 60*time.Second)
	defer cancelTimeout()

	var m map[string]float64
	if err := chromedp.Run(ctx, chromedp.Navigate(srv.URL), chromedp.Evaluate(probe, &m)); err != nil {
		t.Fatal(err)
	}
	return m
}

// Hard rule 9: each connector ran from its own step's left edge to its
// marker, so it started in the gap a column away from the previous
// marker and the rail read as four floating dashes (caught on a
// four-step onboarding band). A connector reaches back to the previous
// step's marker, and only a line into a reached step is tinted: the
// one leading on from the current step lit too, claiming progress
// nobody made.
func TestProgressStepsConnectorsMeetMarkers(t *testing.T) {
	page := ProgressSteps(ProgressStepsConfig{Label: "Onboarding", Steps: []ProgressStep{
		{Label: "One", Status: ProgressStepComplete},
		{Label: "Two", Status: ProgressStepCurrent},
		{Label: "Three"},
	}})
	m := geometryOf(t, progressStepsStyle.Entry().CSSFor(theme.Default()), page, `(() => {
		const items = document.querySelectorAll('.fui-progress-steps__item');
		const marker = i => items[i].querySelector('.fui-progress-steps__marker').getBoundingClientRect();
		const line = getComputedStyle(items[1], '::before');
		const box = items[1].getBoundingClientRect();
		const tint = i => getComputedStyle(items[i], '::before').backgroundColor;
		return {intoCurrentIsUpcoming: tint(1) === tint(2) ? 1 : 0, prevMarkerRight: marker(0).right, markerLeft: marker(1).left,
			lineLeft: box.left + parseFloat(line.left), lineRight: box.right - parseFloat(line.right),
			lineH: parseFloat(line.height)};
	})()`)
	if m["lineH"] == 0 || m["markerLeft"] <= m["prevMarkerRight"] {
		t.Fatalf("connector or markers not painted: %v", m)
	}
	if m["lineLeft"] > m["prevMarkerRight"]+1 {
		t.Errorf("connector starts %.0fpx past the previous marker; it must reach it", m["lineLeft"]-m["prevMarkerRight"])
	}
	if m["lineRight"] < m["markerLeft"]-1 {
		t.Errorf("connector stops %.0fpx short of its marker", m["markerLeft"]-m["lineRight"])
	}
	if m["intoCurrentIsUpcoming"] == 1 {
		t.Error("the line into the upcoming step is tinted like the line into the current one")
	}
}

// Hard rule 9: the connector was placed with left/right, so in a
// right-to-left page it ran away from the previous step and out past
// the list's edge (caught in review). It follows the inline direction.
func TestProgressStepsConnectorsInRTL(t *testing.T) {
	page := render.HTML(`<div dir="rtl">`) + ProgressSteps(ProgressStepsConfig{Label: "Onboarding", Steps: []ProgressStep{
		{Label: "One", Status: ProgressStepComplete},
		{Label: "Two", Status: ProgressStepCurrent},
		{Label: "Three"},
	}}) + `</div>`
	m := geometryOf(t, progressStepsStyle.Entry().CSSFor(theme.Default()), page, `(() => {
		const items = document.querySelectorAll('.fui-progress-steps__item');
		const marker = i => items[i].querySelector('.fui-progress-steps__marker').getBoundingClientRect();
		const line = getComputedStyle(items[1], '::before');
		const box = items[1].getBoundingClientRect();
		const list = items[0].parentElement.getBoundingClientRect();
		return {prevMarkerLeft: marker(0).left, ownMarkerRight: marker(1).right, listLeft: list.left,
			lineLeft: box.left + parseFloat(line.left), lineRight: box.right - parseFloat(line.right)};
	})()`)
	if m["ownMarkerRight"] >= m["prevMarkerLeft"] {
		t.Fatalf("steps not laid out right to left: %v", m)
	}
	if m["lineRight"] < m["prevMarkerLeft"]-1 || m["lineLeft"] > m["ownMarkerRight"]+1 {
		t.Errorf("connector [%.0f, %.0f] does not join markers at %.0f and %.0f", m["lineLeft"], m["lineRight"], m["ownMarkerRight"], m["prevMarkerLeft"])
	}
	if m["lineLeft"] < m["listLeft"]-1 {
		t.Errorf("connector starts %.0fpx outside the list", m["listLeft"]-m["lineLeft"])
	}
}

// Hard rule 9: a 2.4px overlap on 24px avatars read as a row of loose
// circles, not a stack (caught on a "trusted by" strip). Each avatar
// tucks a quarter of its width under the one before it, the shadcn
// spacing, at every size.
func TestAvatarGroupOverlapsAQuarter(t *testing.T) {
	people := []AvatarConfig{{Name: "Ada Lovelace"}, {Name: "Grace Hopper"}, {Name: "Alan Turing"}}
	var page render.HTML
	for _, sz := range []AvatarSize{AvatarSm, AvatarMd, AvatarLg, AvatarXl} {
		page += render.HTML(`<div data-size="`+string(sz)+`">`) +
			AvatarGroup(AvatarGroupConfig{Avatars: people, Size: sz}) + `</div>`
	}
	css := avatarStyle.Entry().CSSFor(theme.Default()) + avatarGroupStyle.Entry().CSSFor(theme.Default())
	m := geometryOf(t, css, page, `(() => {
		const out = {};
		for (const wrap of document.querySelectorAll('[data-size]')) {
			const kids = wrap.querySelectorAll('.fui-avatar');
			const a = kids[0].getBoundingClientRect(), b = kids[1].getBoundingClientRect();
			out[wrap.dataset.size || 'md'] = a.width ? (a.right - b.left) / a.width : 0;
		}
		return out;
	})()`)
	if len(m) != 4 {
		t.Fatalf("measured %d sizes, want 4: %v", len(m), m)
	}
	for size, frac := range m {
		if frac < 0.2 || frac > 0.35 {
			t.Errorf("size %s: avatars overlap %.0f%% of their width, want about 25%%", size, frac*100)
		}
	}
}

// Hard rule 9: the slides stretched to the tallest card, but a card
// inside a slide kept its own height, so a short quote's card ended
// above its neighbours' and the row read ragged (caught on a
// three-up testimonial carousel). A slide's content fills the slide.
func TestCarouselCardsShareAHeight(t *testing.T) {
	quote := func(text string) CarouselSlide {
		return CarouselSlide{Label: text, Content: Card(CardConfig{}, render.HTML("<p>"+text+"</p>"))}
	}
	page := Carousel(CarouselConfig{Label: "Stories", VisiblePerView: 3, Slides: []CarouselSlide{
		quote("Short."),
		quote("A much longer quote that wraps onto several lines in a third of the page."),
		quote("Medium length, two lines maybe."),
	}})
	css := carouselStyle.Entry().CSSFor(theme.Default()) + cardStyle.Entry().CSSFor(theme.Default())
	m := geometryOf(t, css, page, `(() => {
		const cards = [...document.querySelectorAll('.fui-carousel__slide > *')].map(c => c.getBoundingClientRect());
		return {n: cards.length, short: cards[0].height, long: cards[1].height, top0: cards[0].top, top1: cards[1].top};
	})()`)
	if m["n"] != 3 || m["top0"] != m["top1"] {
		t.Fatalf("cards not laid out in one row: %v", m)
	}
	if m["long"]-m["short"] > 1 {
		t.Errorf("short card is %.0fpx shorter than its neighbour; cards in a row share a height", m["long"]-m["short"])
	}
}

// Hard rule 9: filling the slide's height stretched a slide's rows too,
// so a caption under an image started 61px below it (caught in review).
// A lone child fills the slide; several children stack from the top.
func TestCarouselCaptionHugsImage(t *testing.T) {
	figure := CarouselSlide{Label: "Photo", Content: render.HTML(
		`<div data-img style="block-size:120px"></div><p data-cap style="margin:0">Caption</p>`)}
	tall := CarouselSlide{Label: "Tall", Content: render.HTML(`<div style="block-size:400px"></div>`)}
	page := Carousel(CarouselConfig{Label: "Photos", VisiblePerView: 2, NoArrows: true, Slides: []CarouselSlide{figure, tall}})
	m := geometryOf(t, carouselStyle.Entry().CSSFor(theme.Default()), page, `(() => ({
		img: document.querySelector('[data-img]').getBoundingClientRect().bottom,
		cap: document.querySelector('[data-cap]').getBoundingClientRect().top}))()`)
	if m["img"] == 0 {
		t.Fatalf("slide not laid out: %v", m)
	}
	if gap := m["cap"] - m["img"]; gap > 1 {
		t.Errorf("caption starts %.0fpx below its image; a slide's children stack from the top", gap)
	}
}

// Hard rule 9: the carousel's width containment gave it no width of
// its own, so as a flex item with an auto basis (inside a Cluster) it
// measured 0px and painted nothing (caught in review). It fills the
// row it sits in.
func TestCarouselShowsInAFlexRow(t *testing.T) {
	page := Cluster(ClusterConfig{}, Carousel(CarouselConfig{Label: "Row", NoArrows: true, Slides: []CarouselSlide{
		{Label: "One", Content: render.HTML("<p>One</p>")}, {Label: "Two", Content: render.HTML("<p>Two</p>")},
	}}))
	css := layoutStyle.Entry().CSSFor(theme.Default()) + carouselStyle.Entry().CSSFor(theme.Default())
	m := geometryOf(t, css, page, `(() => ({
		w: document.querySelector('.fui-carousel').getBoundingClientRect().width,
		row: document.querySelector('.fui-cluster').getBoundingClientRect().width}))()`)
	if m["row"] < 800 {
		t.Fatalf("row not laid out: %v", m)
	}
	if m["w"] < m["row"]/2 {
		t.Errorf("carousel is %.0fpx wide in a %.0fpx row; it must fill the row", m["w"], m["row"])
	}
}

// Hard rule 9: the arrow gutters took 104px of a 320px phone column, so
// a slide got two thirds of the width the page gives it (caught in
// review). A phone-narrow carousel drops its arrows (the dots and a
// swipe still move it) and gives the slide the whole column.
func TestCarouselPhoneSlideIsFullWidth(t *testing.T) {
	page := render.HTML(`<div data-col style="inline-size:320px">`) + Carousel(CarouselConfig{Label: "Phone", Slides: []CarouselSlide{
		{Label: "One", Content: render.HTML("<p>One</p>")}, {Label: "Two", Content: render.HTML("<p>Two</p>")},
	}}) + `</div>`
	m := geometryOf(t, carouselStyle.Entry().CSSFor(theme.Default()), page, `(() => ({
		slide: document.querySelector('.fui-carousel__slide').getBoundingClientRect().width,
		col: document.querySelector('[data-col]').getBoundingClientRect().width,
		dots: document.querySelectorAll('.fui-carousel__dot').length,
		arrows: [...document.querySelectorAll('.fui-carousel__prev, .fui-carousel__next')].filter(a => a.getBoundingClientRect().width > 0).length}))()`)
	if m["col"] != 320 || m["dots"] < 2 {
		t.Fatalf("column or dots not laid out: %v", m)
	}
	if m["slide"] < m["col"]-1 {
		t.Errorf("a slide is %.0fpx wide in a %.0fpx phone column; arrows must not take the width", m["slide"], m["col"])
	}
	// With the gutter gone, a painted arrow would cover the slide's text.
	if m["arrows"] != 0 {
		t.Errorf("%.0f arrows still paint over a phone-width slide", m["arrows"])
	}
}

// Hard rule 9: an overlaid arrow covered a card slide's first letters.
// At a wide width the arrows sit in gutters beside the track, clear of
// every slide; with NoArrows the track takes the whole width.
func TestCarouselArrowsSitInAGutter(t *testing.T) {
	slides := []CarouselSlide{{Label: "One", Content: render.HTML("<p>One</p>")}, {Label: "Two", Content: render.HTML("<p>Two</p>")}}
	css := carouselStyle.Entry().CSSFor(theme.Default())
	m := geometryOf(t, css, Carousel(CarouselConfig{Label: "Wide", Slides: slides}), `(() => {
		const r = s => document.querySelector(s).getBoundingClientRect();
		return {prev: r('.fui-carousel__prev').right, next: r('.fui-carousel__next').left,
			start: r('.fui-carousel__track').left, end: r('.fui-carousel__track').right};
	})()`)
	if m["end"] == 0 {
		t.Fatalf("track not laid out: %v", m)
	}
	if m["prev"] > m["start"] || m["next"] < m["end"] {
		t.Errorf("arrows overlap the track: prev ends %.0f, track %.0f to %.0f, next starts %.0f", m["prev"], m["start"], m["end"], m["next"])
	}
	bare := geometryOf(t, css, Carousel(CarouselConfig{Label: "Bare", NoArrows: true, Slides: slides}), `(() => ({
		track: document.querySelector('.fui-carousel__track').getBoundingClientRect().width,
		root: document.querySelector('.fui-carousel').getBoundingClientRect().width}))()`)
	if bare["track"] < bare["root"]-1 {
		t.Errorf("without arrows the track is %.0fpx of %.0fpx; it keeps the width", bare["track"], bare["root"])
	}
}

// The rating root declared --ui-rating-color itself, so a page that set
// the knob on an ancestor never reached a star (caught in review). An
// ancestor's value colours the filled glyphs; unset, they stay amber.
func TestRatingColorFromAncestor(t *testing.T) {
	css := ratingDisplayStyle.Entry().CSSFor(theme.Default())
	probe := `(() => {
		const [r, g, b] = getComputedStyle(document.querySelector('.fui-rating-display__glyph.is-on')).color.match(/\d+/g).map(Number);
		return {r, g, b};
	})()`
	knob := `<div style="--ui-rating-color: rgb(1, 2, 3)">`
	for _, tc := range []struct {
		wrap    string
		shape   RatingShape
		r, g, b float64
	}{
		{knob, RatingShapeStar, 1, 2, 3},
		{`<div>`, RatingShapeStar, 217, 119, 6},
		// A shape's own colour is a default under the knob, not over it.
		{knob, RatingShapeHeart, 1, 2, 3},
		{`<div>`, RatingShapeHeart, 220, 38, 38},
	} {
		page := render.HTML(tc.wrap) + Rating(RatingDisplayConfig{Value: 4, Shape: tc.shape}) + "</div>"
		m := geometryOf(t, css, page, probe)
		if m["r"] != tc.r || m["g"] != tc.g || m["b"] != tc.b {
			t.Errorf("%s %q: filled glyph is rgb(%v, %v, %v), want rgb(%v, %v, %v)", tc.wrap, tc.shape, m["r"], m["g"], m["b"], tc.r, tc.g, tc.b)
		}
	}
}

// The module hides the dots past the last reachable position with the
// hidden attribute, but the dot's own display beat the UA's [hidden]
// rule, so a page without a global reset showed them all (caught in
// review). A hidden dot takes no space on any page.
func TestCarouselHiddenDotTakesNoSpace(t *testing.T) {
	page := Carousel(CarouselConfig{Label: "Dots", Slides: []CarouselSlide{
		{Label: "One", Content: render.HTML("<p>One</p>")}, {Label: "Two", Content: render.HTML("<p>Two</p>")},
	}})
	m := geometryOf(t, carouselStyle.Entry().CSSFor(theme.Default()), page, `(() => {
		const dots = document.querySelectorAll('.fui-carousel__dot');
		dots[1].hidden = true;
		return {n: dots.length, shown: dots[0].getBoundingClientRect().width, hidden: dots[1].getBoundingClientRect().width};
	})()`)
	if m["n"] != 2 || m["shown"] == 0 {
		t.Fatalf("dots not laid out: %v", m)
	}
	if m["hidden"] != 0 {
		t.Errorf("a hidden dot is %.0fpx wide; it must not show", m["hidden"])
	}
}

// The zinc palette made the primary colour body text's colour, so an
// inline link in prose differed from its sentence only by weight (WCAG
// 1.4.1; caught in review). An inline link is underlined; an action or
// muted link, which sits outside prose, is not.
func TestInlineLinkIsUnderlined(t *testing.T) {
	page := render.HTML("<p>Read ") + Link(LinkConfig{Text: "the guide", Href: "/guide"}) +
		" or " + Link(LinkConfig{Text: "edit", Href: "/edit", Variant: LinkAction}) +
		" or " + Link(LinkConfig{Text: "see all", Href: "/all", Variant: LinkMuted}) + ".</p>"
	m := geometryOf(t, linkStyle.Entry().CSSFor(theme.Default()), page, `(() => {
		const line = a => getComputedStyle(a).textDecorationLine === 'underline' ? 1 : 0;
		const [inline, action, muted] = document.querySelectorAll('a');
		return {inline: line(inline), action: line(action), muted: line(muted)};
	})()`)
	if m["inline"] != 1 {
		t.Error("an inline link in prose is not underlined; colour alone no longer tells it from the text")
	}
	if m["action"] != 0 || m["muted"] != 0 {
		t.Errorf("action %v / muted %v link underlined at rest", m["action"], m["muted"])
	}
}

// The addons' padding was physical (left on the prepend, right on the
// append), so in a right-to-left page "$" and "USD" sat 1px from the
// border (caught in review). Each addon clears the border on its own
// outer side in either direction.
func TestInputGroupAddonsPadInRTL(t *testing.T) {
	group := InputGroup(InputGroupConfig{Prepend: render.Text("$"), Input: render.HTML(`<input>`), Append: render.Text("USD")})
	probe := `(() => {
		const g = document.querySelector('.fui-input-group').getBoundingClientRect();
		const text = el => { const r = document.createRange(); r.selectNodeContents(el); return r.getBoundingClientRect(); };
		const pre = text(document.querySelector('.fui-input-group__prepend'));
		const app = text(document.querySelector('.fui-input-group__append'));
		const rtl = document.dir === 'rtl';
		return {pre: rtl ? g.right - pre.right : pre.left - g.left, app: rtl ? app.left - g.left : g.right - app.right};
	})()`
	css := inputGroupStyle.Entry().CSSFor(theme.Default())
	for _, dir := range []string{"ltr", "rtl"} {
		page := render.HTML(`<script>document.dir="`+dir+`"</script><div style="inline-size:320px">`) + group + "</div>"
		m := geometryOf(t, css, page, probe)
		if m["pre"] < 8 || m["app"] < 8 {
			t.Errorf("%s: addon text sits %.0fpx and %.0fpx from the border, want at least 8px", dir, m["pre"], m["app"])
		}
	}
}

// Hard rule 9: a plan head stretched to its row's tallest head spread
// its own rows too, so beside a featured card a plain plan's description
// floated 15px below its name (caught in review). A head's name and
// description stay together at the top.
func TestPricingHeadKeepsNameOnCopy(t *testing.T) {
	card := func(desc string, featured bool) render.HTML {
		return PricingCard(PricingCardConfig{Name: "Team", Description: desc, Price: "$9", Period: "/mo",
			Features: []string{"One"}, CTALabel: "Choose", CTAHref: "/x", Featured: featured})
	}
	page := Grid(GridConfig{Min: "14rem"}, card("For side projects.", false),
		card("For teams that ship every day and need every seat, every audit log and every support channel they can get.", true),
		card("For larger orgs.", false))
	css := layoutStyle.Entry().CSSFor(theme.Default()) + pricingCardStyle.Entry().CSSFor(theme.Default())
	m := geometryOf(t, css, page, `(() => {
		const c = document.querySelector('[data-cui-comp="ui-pricing-card"]');
		return {name: c.querySelector('.fui-pricing-card__name').getBoundingClientRect().bottom,
			desc: c.querySelector('.fui-pricing-card__desc').getBoundingClientRect().top};
	})()`)
	if m["name"] == 0 {
		t.Fatalf("plan not laid out: %v", m)
	}
	if gap := m["desc"] - m["name"]; gap > 8 {
		t.Errorf("a plain plan's description starts %.0fpx below its name; the head keeps them together", gap)
	}
}

// Hard rule 9: outside a plans-only grid the card keeps its own rows,
// so in a stretched flex row a short plan must still grow to its
// neighbour's height and drop its button to the shared bottom line.
// height: 100% stopped the stretch and left the short card short.
func TestPricingCardStretchesInARow(t *testing.T) {
	card := func(features ...string) render.HTML {
		return PricingCard(PricingCardConfig{Name: "Team", Price: "$9", Period: "/mo",
			Features: features, CTALabel: "Choose", CTAHref: "/x"})
	}
	page := Cluster(ClusterConfig{Align: AlignStretch, NoWrap: true},
		card("One"), card("One", "Two", "Three", "Four", "Five", "Six"))
	css := layoutStyle.Entry().CSSFor(theme.Default()) + pricingCardStyle.Entry().CSSFor(theme.Default())
	m := geometryOf(t, css, page, `(() => {
		const [a, b] = document.querySelectorAll('[data-cui-comp="ui-pricing-card"]');
		const cta = c => c.querySelector('.fui-pricing-card__cta').getBoundingClientRect().bottom;
		return {ah: a.getBoundingClientRect().height, bh: b.getBoundingClientRect().height, actA: cta(a), actB: cta(b)};
	})()`)
	if m["bh"] == 0 {
		t.Fatalf("plans not laid out: %v", m)
	}
	if d := m["bh"] - m["ah"]; d > 1 || d < -1 {
		t.Errorf("short plan is %.0fpx tall beside a %.0fpx one; a stretched row grows it", m["ah"], m["bh"])
	}
	if d := m["actB"] - m["actA"]; d > 1 || d < -1 {
		t.Errorf("buttons end %.0fpx apart; both sit on the card's bottom line", d)
	}
}

// Hard rule 9: a section is a band of its column, so in a stack aligned
// to start it still spans the column; shrunk to its content, the
// auto-fit grid inside it sized three cards as two plus one.
func TestSectionSpansStartAlignedStack(t *testing.T) {
	cell := `<div style="height:20px"></div>`
	page := Stack(StackConfig{Align: AlignStart},
		Section(SectionConfig{Heading: "Plans"}, Grid(GridConfig{Min: "12rem"}, render.HTML(cell), render.HTML(cell), render.HTML(cell))))
	css := layoutStyle.Entry().CSSFor(theme.Default()) + sectionStyle.Entry().CSSFor(theme.Default())
	m := geometryOf(t, css, page, `(() => {
		const s = document.querySelector('[data-cui-comp="ui-section"]').getBoundingClientRect();
		const tops = new Set([...document.querySelectorAll('.fui-grid > div')].map(c => c.getBoundingClientRect().top));
		return {w: s.width, rows: tops.size};
	})()`)
	if m["w"] == 0 {
		t.Fatalf("section not laid out: %v", m)
	}
	if m["rows"] != 1 {
		t.Errorf("three 12rem cells in a 900px band take %.0f rows, want 1", m["rows"])
	}
}

// Hard rule 9: a section's body is a grid of its own, and with an auto
// column it grew to a code block's longest line, 405px inside a 280px
// phone section (caught screenshotting the lab at 320px). The body's
// column holds the section's width; the code block scrolls inside it.
func TestSectionBodyHoldsCodeBlock(t *testing.T) {
	code := CodeBlock(CodeBlockConfig{Code: strings.Repeat("handler := framework.Handler(", 4) + "nil)"})
	page := render.HTML(`<div style="width:300px">`) +
		Section(SectionConfig{Heading: "Install"}, Grid(GridConfig{}, Stack(StackConfig{}, code))) + render.HTML(`</div>`)
	css := layoutStyle.Entry().CSSFor(theme.Default()) + sectionStyle.Entry().CSSFor(theme.Default()) +
		codeBlockStyle.Entry().CSSFor(theme.Default())
	m := geometryOf(t, css, page, `({w: document.querySelector('[data-cui-comp="ui-code-block"]').getBoundingClientRect().width,
		page: document.documentElement.scrollWidth})`)
	if m["w"] == 0 || m["w"] > 300.5 {
		t.Errorf("code block in a 300px section is %.0fpx wide; the section body's column must hold it", m["w"])
	}
}

// Hard rule 9: a card with a heading and a footer but no body closed
// its header only when nothing followed the empty body, so the footer's
// border touched the description (caught in review). The header keeps
// its bottom padding whenever the body is empty.
func TestCardHeaderClearsFooter(t *testing.T) {
	page := Card(CardConfig{Heading: "Plan", Description: "Billed monthly.", Footer: render.HTML("<p>Footer</p>")})
	m := geometryOf(t, cardStyle.Entry().CSSFor(theme.Default()), page, `(() => ({
		desc: document.querySelector('.fui-card__description').getBoundingClientRect().bottom,
		foot: document.querySelector('.fui-card__footer').getBoundingClientRect().top}))()`)
	if m["foot"] == 0 {
		t.Fatalf("footer not laid out: %v", m)
	}
	if gap := m["foot"] - m["desc"]; gap < 16 {
		t.Errorf("footer starts %.0fpx under the description; the header keeps its bottom padding", gap)
	}
}

// Hard rule 9: a segmented control is an inline grid, but as a stack
// or section-body item it stretched across the column, so a two-option
// billing toggle above a pricing row spanned 1056px. It keeps its own
// width wherever it sits.
func TestSegmentedKeepsItsWidth(t *testing.T) {
	page := Stack(StackConfig{}, SegmentedControl(SegmentedControlConfig{Name: "billing", Label: "Billing", Selected: "m",
		Options: []SegmentedOption{{Label: "Monthly", Value: "m"}, {Label: "Annual", Value: "a"}}}))
	css := layoutStyle.Entry().CSSFor(theme.Default()) + segmentedStyle.Entry().CSSFor(theme.Default())
	m := geometryOf(t, css, page, `(() => ({
		w: document.querySelector('[data-cui-comp~="ui-segmented"]').getBoundingClientRect().width,
		col: document.querySelector('.fui-stack').getBoundingClientRect().width}))()`)
	if m["col"] < 800 || m["w"] == 0 {
		t.Fatalf("column or control not laid out: %v", m)
	}
	if m["w"] > m["col"]/2 {
		t.Errorf("a two-option control is %.0fpx wide in a %.0fpx column; it must keep its own width", m["w"], m["col"])
	}
}

// Hard rule 9: a FullWidth sparkline stretches its 120-wide viewBox to
// the column with preserveAspectRatio none, and the stroke stretched
// with it: a 470px card drew the line about four times its width
// (caught on a latency card). The line keeps its stroke width at any
// stretch.
func TestSparklineStrokeHoldsWhenStretched(t *testing.T) {
	page := render.HTML(`<div style="width:470px">`) +
		Sparkline(SparklineConfig{Values: []float64{4, 2, 6, 3, 5}, FullWidth: true, Height: 96}) + `</div>`
	m := geometryOf(t, sparklineStyle.Entry().CSSFor(theme.Default()), page, `(() => {
		const svg = document.querySelector('svg.fui-sparkline');
		const line = svg.querySelector('.fui-sparkline__line');
		const vb = svg.viewBox.baseVal;
		return {w: svg.getBoundingClientRect().width, vbw: vb.width,
			nonScaling: getComputedStyle(line).vectorEffect === 'non-scaling-stroke' ? 1 : 0};
	})()`)
	if m["w"] < 2*m["vbw"] {
		t.Fatalf("sparkline not stretched, nothing to prove: %v", m)
	}
	if m["nonScaling"] != 1 {
		t.Errorf("a %.0fpx sparkline over a %.0f-wide viewBox scales its stroke with it", m["w"], m["vbw"])
	}
}
