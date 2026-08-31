package uihost

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// ssrCtxSlot renders the chrome-context mark: what a per-entity dialog
// slot sees. "ctx=<v>" with v from widget.ChromeContext — "" on the
// SSR-inline path (arrival by URL has no trigger), the trigger's
// data-fui-ctx value on the /chrome fetch path.
type ssrCtxSlot struct {
	component.ContextOnly
}

func (ssrCtxSlot) RenderCtx(ctx context.Context) render.HTML {
	return render.HTML(`<span id="ctxmark">ctx=` + widget.ChromeContext(ctx) + `</span>`)
}

// registerSSRCtxWidgets mounts the three shapes injectWidgetSSR decides
// between. Names are unique to this file: the widget registry is
// process-global and has no reset.
func registerSSRCtxWidgets(t *testing.T) {
	t.Helper()
	rtr := router.New()
	// The gallery / architecture-doc shape: Hidden + deep link + param.
	widget.MountBuilder(rtr, widget.New("ssrctx-dialog").
		Mount(widget.Center).
		Hidden().
		DeepLink("modal", "ssrctx-edit").
		DeepLinkParam("user_id").
		Slot("body", ssrCtxSlot{}))
	// Hidden click-to-open, no deep link: never inlined.
	widget.MountBuilder(rtr, widget.New("ssrctx-plain-hidden").
		Hidden().
		Slot("body", ssrCtxSlot{}))
	// Non-hidden auto-mount: always inlined.
	widget.MountBuilder(rtr, widget.New("ssrctx-auto").
		Slot("body", ssrCtxSlot{}))
}

func injectFor(t *testing.T, url string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, url, nil)
	return injectWidgetSSR(`<!doctype html><html><body><main>x</main></body></html>`, req)
}

// TestInjectWidgetSSR_HiddenDeepLinkInlinedOnMatch pins the inline
// decision for the release-blocker shape (#321): a Hidden().DeepLink()
// widget IS inlined when the URL matches — rendered open, ctx-less —
// because arrival by deep link has no trigger to carry data-fui-ctx.
// The runtime's _mountByName replaces that ctx-less node when a
// ctx-carrying trigger opens the widget (see
// core-ui/runtime/widget_chromectx_ssr_e2e_test.go); this test pins
// the server half of that contract.
func TestInjectWidgetSSR_HiddenDeepLinkInlinedOnMatch(t *testing.T) {
	registerSSRCtxWidgets(t)

	page := injectFor(t, "/users?modal=ssrctx-edit&user_id=42")

	if !strings.Contains(page, `data-fui-widget="ssrctx-dialog"`) {
		t.Fatalf("deep-link match must SSR-inline the Hidden dialog chrome; got:\n%s", page)
	}
	// The inlined chrome renders with NO ctx: this is the exact node the
	// runtime must drop (not hydrate) when a data-fui-ctx trigger opens.
	if !strings.Contains(page, `<span id="ctxmark">ctx=</span>`) {
		t.Fatalf("SSR-inlined chrome must render ctx-less (no trigger exists at page load); got:\n%s", page)
	}
	// Open at first paint: no hidden attribute on the inlined root.
	if root := substringAfter(page, `data-fui-widget="ssrctx-dialog"`); strings.HasPrefix(root, ` hidden`) {
		t.Fatalf("deep-link inline must render open, got hidden root")
	}
	// Chrome lands just inside </body>.
	if !strings.Contains(page, `data-fui-widget="ssrctx-dialog"</body>`) &&
		!containsBeforeBodyClose(page, `data-fui-widget="ssrctx-dialog"`) {
		t.Fatalf("inline chrome must sit before </body>; got:\n%s", page)
	}

	// Same URL without the deep-link query: the Hidden dialog is NOT
	// inlined (fetch path, which is ctx-aware), the plain-hidden widget
	// never is, the auto-mount one always is.
	plain := injectFor(t, "/users")
	if strings.Contains(plain, `data-fui-widget="ssrctx-dialog"`) {
		t.Fatalf("Hidden dialog must not inline without a deep-link match; got:\n%s", plain)
	}
	if strings.Contains(plain, `data-fui-widget="ssrctx-plain-hidden"`) {
		t.Fatalf("hidden click-to-open widget (no deep link) must never inline; got:\n%s", plain)
	}
	if !strings.Contains(plain, `data-fui-widget="ssrctx-auto"`) {
		t.Fatalf("non-hidden auto-mount widget must inline on every page; got:\n%s", plain)
	}
}

func substringAfter(s, marker string) string {
	i := strings.Index(s, marker)
	if i < 0 {
		return ""
	}
	return s[i+len(marker):]
}

func containsBeforeBodyClose(page, marker string) bool {
	i := strings.Index(page, "</body>")
	return i >= 0 && strings.Contains(page[:i], marker)
}

// TestInjectWidgetSSR_WrongDeepLinkValueSkips pins the match precision:
// the URL must carry THIS widget's DeepLinkValue, not just the key.
func TestInjectWidgetSSR_WrongDeepLinkValueSkips(t *testing.T) {
	registerSSRCtxWidgets(t)

	page := injectFor(t, "/users?modal=other-dialog")
	if strings.Contains(page, `data-fui-widget="ssrctx-dialog"`) {
		t.Fatalf("a different DeepLinkValue must not inline this dialog; got:\n%s", page)
	}
}
