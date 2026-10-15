package ui

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// chain builds a thread depth comments deep, one reply per level.
func chain(depth int) []Comment {
	var c *Comment
	for i := depth; i >= 1; i-- {
		n := Comment{
			ID:     "c" + strings.Repeat("x", i),
			Byline: render.Text("level"),
			Body:   render.HTML("<p>body</p>"),
		}
		if c != nil {
			n.Replies = []Comment{*c}
		}
		c = &n
	}
	return []Comment{*c}
}

func TestCommentThreadNestsReplies(t *testing.T) {
	h := string(CommentThread(CommentThreadConfig{Label: "Comments", Comments: chain(6)}))
	if !strings.HasPrefix(h, `<ul `) || !strings.Contains(h, `aria-label="Comments"`) {
		t.Fatalf("root is not a labelled list:\n%s", h)
	}
	// Six levels: the root list plus five nested reply lists.
	if n := strings.Count(h, `class="fui-comment-thread__replies"`); n != 5 {
		t.Errorf("reply lists = %d, want 5:\n%s", n, h)
	}
	if n := strings.Count(h, "<article"); n != 6 {
		t.Errorf("articles = %d, want 6", n)
	}
	// The deepest comment sits inside every reply list above it.
	deep := strings.Index(h, `id="cxxxxxx"`)
	if deep < 0 || strings.Count(h[:deep], `<ul `) != 6 {
		t.Errorf("deepest comment is not nested six lists deep:\n%s", h)
	}
}

func TestCommentThreadKeepsCallerMarkup(t *testing.T) {
	h := string(CommentThread(CommentThreadConfig{Label: "Comments", Comments: []Comment{{
		ID:     "c1",
		Byline: render.HTML(`<a href="/user/ada">ada</a>`),
		Body:   render.HTML(`<p>one</p><p>two</p>`),
	}}}))
	for _, want := range []string{`id="c1"`, `<a href="/user/ada">ada</a>`, `<p>one</p><p>two</p>`} {
		if !strings.Contains(h, want) {
			t.Errorf("missing %q:\n%s", want, h)
		}
	}
	if strings.Contains(h, "fui-comment-thread__replies") {
		t.Errorf("a comment with no replies drew a reply list:\n%s", h)
	}
}

func TestCommentThreadRequiresLabel(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("CommentThread without Label did not panic")
		}
	}()
	CommentThread(CommentThreadConfig{Comments: chain(1)})
}

func TestCommentThreadSheetUsesTokens(t *testing.T) {
	css := commentThreadCSS(style.Theme{})
	for _, want := range []string{"--ui-comment-thread-indent", "var(--color-border", "@container", "overflow-wrap: anywhere"} {
		if !strings.Contains(css, want) {
			t.Errorf("sheet missing %q", want)
		}
	}
}
