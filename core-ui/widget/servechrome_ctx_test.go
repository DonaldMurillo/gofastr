package widget

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/component"
	"github.com/DonaldMurillo/gofastr/core/render"
)

// ctxEchoSlot renders the trigger-carried chrome context (#321) so tests can
// observe exactly what serveChrome threaded into the slot render.
type ctxEchoSlot struct {
	component.ContextOnly
}

func (ctxEchoSlot) RenderCtx(ctx context.Context) render.HTML {
	return render.HTML("ctx:[" + ChromeContext(ctx) + "]")
}

// serveChromeFor runs the chrome endpoint for def with the given raw query.
func serveChromeFor(t *testing.T, def Definition, rawQuery string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/core-ui/widget/"+def.Name+"/chrome?"+rawQuery, nil)
	(&server{def: def}).serveChrome(rec, req)
	return rec
}

func ctxEchoDef() Definition {
	return New("ctx-echo").Slot("body", ctxEchoSlot{}).Build()
}

// TestServeChromeThreadsTriggerCtx (#321): the open trigger's context
// (`data-fui-ctx`, forwarded by the runtime as ?ctx=…) must reach the slot
// render. Per-entity dialog chrome (a form whose action embeds the entity id)
// is impossible without it: serveChrome has no other knowledge of the
// originating page or row.
func TestServeChromeThreadsTriggerCtx(t *testing.T) {
	rec := serveChromeFor(t, ctxEchoDef(), "ctx=inv-42")
	if rec.Code != http.StatusOK {
		t.Fatalf("chrome with valid ctx: status %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ctx:[inv-42]") {
		t.Fatalf("slot must render the trigger ctx; got:\n%s", rec.Body.String())
	}
}

// TestServeChromeNoCtxRendersEmpty: no ?ctx means the slot sees the empty
// string, the same value the SSR path (RenderChromeCtx with a plain request
// context) and the static exporter (context.Background) produce. One contract
// for "no context carried", not two.
func TestServeChromeNoCtxRendersEmpty(t *testing.T) {
	rec := serveChromeFor(t, ctxEchoDef(), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("chrome without ctx: status %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "ctx:[]") {
		t.Fatalf("absent ctx must render empty, not an error; got:\n%s", rec.Body.String())
	}
}

// TestServeChromeRejectsOversizeCtx: ctx is an attacker-chosen value in a
// URL. Past MaxChromeContext the endpoint refuses (400) rather than
// truncating — truncation would silently render chrome for the wrong entity,
// which is worse than a visible error.
func TestServeChromeRejectsOversizeCtx(t *testing.T) {
	def := ctxEchoDef()
	rec := serveChromeFor(t, def, "ctx="+strings.Repeat("a", MaxChromeContext+1))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("oversize ctx: status %d, want 400", rec.Code)
	}
	// Exactly at the bound is still legal.
	rec = serveChromeFor(t, def, "ctx="+strings.Repeat("a", MaxChromeContext))
	if rec.Code != http.StatusOK {
		t.Fatalf("ctx at exactly MaxChromeContext: status %d, want 200", rec.Code)
	}
}

// TestServeChromeRejectsControlCharsInCtx: control runes in ctx are a
// log-poisoning and header-smuggling surface (the value ends up in access
// logs and can be reflected into markup), and no legitimate entity key
// contains them. Percent-decoded CR/LF/NUL/DEL/C1 must all be refused.
func TestServeChromeRejectsControlCharsInCtx(t *testing.T) {
	for _, q := range []string{
		"ctx=inv%0A42",    // LF
		"ctx=inv%0D42",    // CR
		"ctx=inv%0042",    // NUL
		"ctx=inv%7F42",    // DEL
		"ctx=inv%C2%8542", // U+0085 (C1 NEL)
	} {
		rec := serveChromeFor(t, ctxEchoDef(), q)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400", q, rec.Code)
		}
	}
}

// TestServeChromeRejectsInvalidUTF8Ctx: ranging over a Go string decodes
// every invalid byte to U+FFFD, which is NOT a control rune — so raw C1
// bytes that arrive un-decoded (a lone %9D, the %9B%9C pair, a truncated
// %C2 lead) passed the rune loop and reached render, logs, and error
// text as raw bytes. The sequences below are the actual invalid-UTF-8
// bytes, not a literal U+FFFD: feeding the replacement character itself
// does not reproduce the bug, because a literal � is valid UTF-8 and
// outside every reject range.
func TestServeChromeRejectsInvalidUTF8Ctx(t *testing.T) {
	for _, q := range []string{
		"ctx=inv%9D42",    // lone C1 continuation byte
		"ctx=inv%9B%9C42", // two raw C1 bytes (the reviewer's pair)
		"ctx=inv%C242",    // truncated 2-byte sequence (lead, then 'B')
		"ctx=inv%FF42",    // never-valid byte
	} {
		rec := serveChromeFor(t, ctxEchoDef(), q)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: status %d, want 400 (invalid UTF-8 must be refused, not decoded to U+FFFD)", q, rec.Code)
		}
	}
	// Valid multibyte ctx is NOT collateral damage: the gate is UTF-8
	// validity, not non-ASCII rejection. A slug like "café-42" passes.
	rec := serveChromeFor(t, ctxEchoDef(), "ctx=caf%C3%A9-42")
	if rec.Code != http.StatusOK {
		t.Errorf("valid multibyte ctx: status %d, want 200", rec.Code)
	}
}

// TestChromeContextRoundTrip pins the context helper pair: WithChromeContext
// followed by ChromeContext returns the value; an un-wrapped context returns
// the empty string (never panics, never leaks a stale value).
func TestChromeContextRoundTrip(t *testing.T) {
	ctx := context.Background()
	if got := ChromeContext(ctx); got != "" {
		t.Fatalf("ChromeContext on bare ctx = %q, want empty", got)
	}
	ctx = WithChromeContext(ctx, "inv-42")
	if got := ChromeContext(ctx); got != "inv-42" {
		t.Fatalf("ChromeContext after With = %q, want inv-42", got)
	}
	// A nested value replaces, it does not accumulate.
	ctx = WithChromeContext(ctx, "inv-99")
	if got := ChromeContext(ctx); got != "inv-99" {
		t.Fatalf("ChromeContext after re-wrap = %q, want inv-99", got)
	}
}
