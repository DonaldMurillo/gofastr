package infinitescroll

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// stubTheme is a zero-value placeholder for styleFn invocations in
// tests — styleFn currently reads from CSS custom properties so the
// theme parameter is unused.
type stubTheme = style.Theme

func TestRenderBasic(t *testing.T) {
	out := string(Render(Config{
		RPCPath: "/feed/page",
		Items: []render.HTML{
			render.HTML(`<div class="card">Item 1</div>`),
			render.HTML(`<div class="card">Item 2</div>`),
		},
		Cursor: "abc123",
	}))
	wants := []string{
		`role="feed"`,
		`aria-label="Feed"`,
		`aria-busy="false"`,
		`data-fui-infinite-scroll="/feed/page"`,
		`data-fui-infinite-cursor="abc123"`,
		`data-fui-infinite-items=".infinitescroll__items"`,
		`data-fui-infinite-root-margin="200px"`,
		`class="infinitescroll__items"`,
		`Item 1`,
		`Item 2`,
		`data-fui-infinite-sentinel=""`,
		`aria-hidden="true"`,
		`<noscript>`,
		`action="/feed/page"`,
		`value="abc123"`,
		`>Load more</button>`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("InfiniteScroll missing %q\nout: %s", w, out)
		}
	}
}

func TestRenderCustomLabels(t *testing.T) {
	out := string(Render(Config{
		RPCPath:       "/feed",
		Items:         []render.HTML{render.HTML(`<li>x</li>`)},
		AriaLabel:     "Activity feed",
		LoadMoreLabel: "Show more",
		RootMargin:    "400px",
	}))
	wants := []string{
		`aria-label="Activity feed"`,
		`data-fui-infinite-root-margin="400px"`,
		`>Show more</button>`,
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("missing %q\nout: %s", w, out)
		}
	}
}

func TestRenderPanicsOnMissingRPCPath(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on missing RPCPath")
		}
	}()
	Render(Config{Items: []render.HTML{render.HTML("x")}})
}

func TestRenderPanicsOnEmptyItems(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("expected panic on empty Items")
		}
	}()
	Render(Config{RPCPath: "/feed"})
}

func TestRenderEscapesInjectedStrings(t *testing.T) {
	out := string(Render(Config{
		RPCPath: `/feed?q=`,
		Items:   []render.HTML{render.HTML("x")},
		Cursor:  `<script>alert("x")</script>`,
	}))
	if strings.Contains(out, "<script>alert") {
		t.Errorf("cursor must be escaped in noscript fallback, got: %s", out)
	}
	if !strings.Contains(out, `&lt;script&gt;`) {
		t.Errorf("expected escaped cursor in noscript output, got: %s", out)
	}
}

func TestStyleScopedToComponent(t *testing.T) {
	css := styleFn(stubTheme{})
	for _, w := range []string{
		`[data-fui-comp="infinitescroll"]`,
		`infinitescroll__sentinel`,
		`infinitescroll__loadmore`,
		`aria-busy="true"`,
		"prefers-reduced-motion",
	} {
		if !strings.Contains(css, w) {
			t.Errorf("styleFn missing %q", w)
		}
	}
	// No unscoped selectors — every rule must be inside a
	// [data-fui-comp="infinitescroll"] scope (or a @keyframes / @media
	// block, both of which the scanner permits).
	for _, leak := range []string{"\n.infinitescroll {", "\n.infinitescroll[", "\n.infinitescroll__"} {
		if strings.Contains(css, leak) {
			t.Errorf("styleFn leaks unscoped rule %q", leak)
		}
	}
}

func TestRenderEmitsDataFuiComp(t *testing.T) {
	out := string(Render(Config{
		RPCPath: "/feed",
		Items:   []render.HTML{render.HTML("x")},
	}))
	if !strings.Contains(out, `data-fui-comp="infinitescroll"`) {
		t.Errorf("Render must emit data-fui-comp so the runtime auto-loads CSS, got: %s", out)
	}
}
