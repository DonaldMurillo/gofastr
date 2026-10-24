package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	uiapp "github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/html"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/examples/hn/hnheader"
	ui "github.com/DonaldMurillo/gofastr/framework/ui"
	"github.com/DonaldMurillo/gofastr/framework/ui/theme"
)

// siteName is the name in the bar and after every page title.
const siteName = "GoFastr News"

// buildSite declares the theme, the one layout and the screens.
func buildSite(st *store) *uiapp.App {
	site := uiapp.NewApp(siteName)
	// A darker orange than the classic bar, so white text on it and the
	// links on the page keep 4.5:1; a lighter one for dark mode.
	site.WithTheme(theme.Default(theme.Overrides{
		Primary: "#C2410C",
		Dark:    &theme.Overrides{Primary: "#FB923C"},
	}))
	site.SetDefaultLayout(uiapp.NewLayout("reader", uiapp.LayoutSpec{}, readerShell))

	for _, l := range []list{listTop, listNewest, listAsk, listShow, listJobs} {
		site.Register(l.Path, &listScreen{st: st, l: l}, nil)
	}
	// /news is the top list under its other name, like the original.
	site.Register("/news", &listScreen{st: st, l: listTop, self: "/news"}, nil)
	site.Register("/item/{id}", &itemScreen{st: st}, nil)
	site.Register("/user/{id}", &userScreen{st: st}, nil)
	return site
}

// readerShell is the page frame: the top bar over the page column. No
// footer, so the stack is not a screen-tall one (that would push main,
// its last child, to the bottom of a short page).
func readerShell(ctx context.Context, l *uiapp.LayoutTree) render.HTML {
	return ui.Stack(ui.StackConfig{Gap: ui.GapNone},
		hnheader.Render(hnheader.Config{
			Ctx:  ctx,
			Name: siteName,
			Mark: "G",
			Links: []hnheader.Link{
				{Label: "top", Href: "/", Also: []string{"/news"}},
				{Label: "new", Href: "/newest"},
				{Label: "ask", Href: "/ask"},
				{Label: "show", Href: "/show"},
				{Label: "jobs", Href: "/jobs"},
			},
		}),
		l.Primary(),
	)
}

// column is the page column every screen draws in.
func column(children ...render.HTML) render.HTML {
	return ui.Container(ui.ContainerConfig{Width: ui.ContainerPage, Pad: ui.ContainerPadPage},
		ui.Stack(ui.StackConfig{Gap: ui.GapLG}, children...))
}

// notFound marks a Load miss: the host answers 404 with the
// not-found screen.
func notFound(what string) error { return fmt.Errorf("%s: %w", what, uiapp.ErrNotFound) }

// pageParam reads ?page=N; absent is 1, anything but a positive number
// is a miss.
func pageParam(ctx context.Context) (int, error) {
	v := uiapp.QueryFromContext(ctx).Get("page")
	if v == "" {
		return 1, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 0, notFound("page " + v)
	}
	return n, nil
}

// ── Lists ───────────────────────────────────────────────────────────

// listScreen is one ranked list, thirty stories a page.
type listScreen struct {
	component.ContextOnly
	st *store
	l  list
	// self is the path the pager links to, when it is not the list's
	// own (/news).
	self string

	page, pages int
	stories     []*Story
}

func (s *listScreen) Load(ctx context.Context) error {
	page, err := pageParam(ctx)
	if err != nil {
		return err
	}
	stories, pages, err := s.st.page(ctx, s.l, page)
	if errors.Is(err, errNotFound) {
		return notFound(fmt.Sprintf("%s page %d", s.l.Path, page))
	}
	if err != nil {
		return err
	}
	s.page, s.pages, s.stories = page, pages, stories
	return nil
}

func (s *listScreen) ScreenTitle(ctx context.Context) string {
	if s.page > 1 {
		return fmt.Sprintf("%s, page %d", s.l.Title, s.page)
	}
	return s.l.Title
}

func (s *listScreen) ScreenDescription(ctx context.Context) string {
	return s.l.Title + " on " + siteName + ", a sample news reader built with GoFastr."
}

func (s *listScreen) RenderCtx(ctx context.Context) render.HTML {
	entries := make([]ui.Entry, len(s.stories))
	for i, st := range s.stories {
		entries[i] = storyEntry(ctx, st)
	}
	path := s.l.Path
	if s.self != "" {
		path = s.self
	}
	var pager render.HTML
	if s.pages > 1 {
		pager = ui.Pagination(ui.PaginationConfig{
			Ctx: ctx, Page: s.page, Pages: s.pages, Path: path, PageParam: "page",
		})
	}
	return column(
		ui.EntryList(ui.EntryListConfig{
			Label: s.l.Title, Ranked: true, Start: (s.page-1)*perPage + 1, Entries: entries,
		}),
		pager,
	)
}

// storyEntry is a story's row: the title, its domain, and the line of
// points, author, age and comments under it. An Ask (or any story
// without a link) leads to its own page.
func storyEntry(ctx context.Context, st *Story) ui.Entry {
	item := "/item/" + st.ID
	e := ui.Entry{Title: st.Title, Href: item, ID: "story-" + st.ID}
	if st.URL != "" {
		e.Href = st.URL
		e.Source = "(" + st.Domain + ")"
	}
	age := html.Link(html.LinkConfig{Href: item, Text: ago(ctx, st.Posted)})
	if st.Kind == "job" {
		e.Meta = age
		return e
	}
	e.Meta = render.Join(
		render.Text(plural(st.Points, "point")+" by "),
		userLink(st.Author),
		render.Text(" "),
		age,
		render.Text(" | "),
		html.Link(html.LinkConfig{Href: item, Text: commentCount(st.Descendants)}),
	)
	return e
}

func commentCount(n int) string {
	if n == 0 {
		return "discuss"
	}
	return plural(n, "comment")
}

// plural counts a word: "1 point", "12 comments". A no-break space
// keeps the number with its word when the meta line wraps.
func plural(n int, word string) string {
	if n == 1 {
		return "1\u00a0" + word
	}
	return strconv.Itoa(n) + "\u00a0" + word + "s"
}

func ago(ctx context.Context, unix int64) string {
	return ui.Ago(ctx, refTime, time.Unix(unix, 0))
}

func userLink(id string) render.HTML {
	return html.Link(html.LinkConfig{Href: "/user/" + id, Text: id})
}

// ── Item ────────────────────────────────────────────────────────────

// itemScreen is a story with its whole thread.
type itemScreen struct {
	component.ContextOnly
	st *store
	id string

	story    *Story
	comments []ui.Comment
}

func (s *itemScreen) SetParams(p map[string]string) { s.id = p["id"] }

func (s *itemScreen) Load(ctx context.Context) error {
	story, err := s.st.story(ctx, s.id)
	if errors.Is(err, errNotFound) {
		return notFound("item " + s.id)
	}
	if err != nil {
		return err
	}
	rows, err := s.st.thread(ctx, s.id)
	if err != nil {
		return err
	}
	s.story = story
	s.comments = commentTree(ctx, rows)
	return nil
}

func (s *itemScreen) ScreenTitle(ctx context.Context) string {
	if s.story == nil {
		return "Story"
	}
	return s.story.Title
}

func (s *itemScreen) ScreenDescription(ctx context.Context) string {
	if s.story == nil {
		return ""
	}
	return fmt.Sprintf("%s, with %s.", s.story.Title, plural(s.story.Descendants, "comment"))
}

func (s *itemScreen) RenderCtx(ctx context.Context) render.HTML {
	parts := []render.HTML{ui.EntryList(ui.EntryListConfig{
		Label: "Story", Entries: []ui.Entry{storyEntry(ctx, s.story)},
	})}
	if s.story.Text != "" {
		parts = append(parts, ui.Stack(ui.StackConfig{Gap: ui.GapSM, TrimMargins: true}, paragraphList(s.story.Text)...))
	}
	if len(s.comments) > 0 {
		parts = append(parts, ui.CommentThread(ui.CommentThreadConfig{Label: "Comments", Comments: s.comments}))
	}
	return column(parts...)
}

// commentTree nests the thread's rows under their parents, keeping the
// stored order (oldest first) at every level.
func commentTree(ctx context.Context, rows []*CommentRow) []ui.Comment {
	kids := map[string][]*CommentRow{}
	for _, r := range rows {
		kids[r.ParentID] = append(kids[r.ParentID], r)
	}
	var build func(parent string) []ui.Comment
	build = func(parent string) []ui.Comment {
		rs := kids[parent]
		if len(rs) == 0 {
			return nil
		}
		out := make([]ui.Comment, len(rs))
		for i, r := range rs {
			anchor := "c" + r.ID
			out[i] = ui.Comment{
				ID: anchor,
				Byline: render.Join(
					userLink(r.Author),
					render.Text(" "),
					html.Link(html.LinkConfig{Href: "#" + anchor, Text: ago(ctx, r.Posted)}),
				),
				Body:    paragraphs(r.Body),
				Replies: build(r.ID),
			}
		}
		return out
	}
	return build("")
}

var urlRE = regexp.MustCompile(`https?://[^\s]+`)

// paragraphs draws stored text: blank lines split paragraphs, and bare
// URLs become links. Everything else is escaped text.
func paragraphs(text string) render.HTML { return render.Join(paragraphList(text)...) }

func paragraphList(text string) []render.HTML {
	var ps []render.HTML
	for p := range strings.SplitSeq(text, "\n\n") {
		var parts []render.HTML
		rest := p
		for _, loc := range urlRE.FindAllStringIndex(p, -1) {
			u := strings.TrimRight(p[loc[0]:loc[1]], ".,;:!?)")
			before := rest[:strings.Index(rest, u)]
			parts = append(parts, render.Text(before), ui.Link(ui.LinkConfig{
				Href: u, Text: u, ExtraAttrs: html.Attrs{"rel": "nofollow noopener"},
			}))
			rest = rest[len(before)+len(u):]
		}
		parts = append(parts, render.Text(rest))
		ps = append(ps, html.Paragraph(html.TextConfig{}, parts...))
	}
	return ps
}

// ── User ────────────────────────────────────────────────────────────

// userScreen is one account's profile.
type userScreen struct {
	component.ContextOnly
	st *store
	id string

	user        *User
	submissions int
}

func (s *userScreen) SetParams(p map[string]string) { s.id = p["id"] }

func (s *userScreen) Load(ctx context.Context) error {
	u, err := s.st.user(ctx, s.id)
	if errors.Is(err, errNotFound) {
		return notFound("user " + s.id)
	}
	if err != nil {
		return err
	}
	n, err := s.st.submissions(ctx, s.id)
	if err != nil {
		return err
	}
	s.user, s.submissions = u, n
	return nil
}

func (s *userScreen) ScreenTitle(ctx context.Context) string { return "Profile: " + s.id }

func (s *userScreen) ScreenDescription(ctx context.Context) string {
	return s.id + "'s profile on " + siteName + "."
}

func (s *userScreen) RenderCtx(ctx context.Context) render.HTML {
	u := s.user
	created := time.Unix(u.Created, 0).UTC()
	items := []ui.DetailItem{
		{Label: "User", Value: render.Text(u.ID)},
		{Label: "Created", Value: render.Text(created.Format("January 2, 2006"))},
		{Label: "Karma", Value: render.Text(strconv.Itoa(u.Karma))},
		{Label: "Submissions", Value: render.Text(strconv.Itoa(s.submissions))},
	}
	if u.About != "" {
		items = append(items, ui.DetailItem{Label: "About", Value: ui.Stack(ui.StackConfig{Gap: ui.GapSM, TrimMargins: true}, paragraphList(u.About)...)})
	}
	return column(
		ui.PageHeader(ui.PageHeaderConfig{Title: u.ID, Eyebrow: "Profile", Compact: true}),
		ui.DetailList(ui.DetailListConfig{Items: items}),
	)
}

// ── Not found ───────────────────────────────────────────────────────

// notFoundScreen is the 404 body: unknown paths, items, users and pages
// past the end of a list.
type notFoundScreen struct{}

func (notFoundScreen) Render() render.HTML {
	return column(ui.EmptyState(ui.EmptyStateConfig{
		Title:        "Not found",
		Description:  "There is no story, user or page at this address.",
		Action:       ui.Link(ui.LinkConfig{Href: "/", Text: "Back to top stories"}),
		HeadingLevel: 1,
	}))
}
