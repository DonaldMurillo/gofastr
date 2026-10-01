package ownstyle

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core-ui/style"
	"github.com/chromedp/chromedp"
)

// TestDarkChainMatchesInBrowser pins a hoisted (--dark) block to the
// elements its source selects, in a real browser. The first hoist
// composed selectors token by token and ignored commas: `.a, .b { .c
// { … } }` became `.a,.b .c`, painting every bare .a. The negatives
// (#t3, #u3, #v3) are the elements that composition wrongly caught.
func TestDarkChainMatchesInBrowser(t *testing.T) {
	src := `
.a, .b { .c { @media (--dark) { color: rgb(255, 0, 0); } } }
.e, .f { .x & { @media (--dark) { color: rgb(255, 0, 0); } } }
.g { .h, .i { @media (--dark) { color: rgb(255, 0, 0); } } }
.k { &.on, &:focus { @media (--dark) { color: rgb(255, 0, 0); } } }
.m { @media (--above-md) { @media (--dark) { color: rgb(255, 0, 0); } } }
`
	sheet, diags := Parse(src)
	if len(diags) > 0 {
		t.Fatal(diags)
	}
	css, err := Compile(sheet, "p", KindScoped, style.ThemeToTokens(style.DefaultTheme()))
	if err != nil {
		t.Fatal(err)
	}
	body := `<div data-fui-scope="p">
<div class="a"><span class="c" id="t1">x</span></div>
<div class="b"><span class="c" id="t2">x</span></div>
<div class="a" id="t3">x</div>
<div class="x"><span class="e" id="u1">x</span><span class="f" id="u2">x</span></div>
<span class="f" id="u3">x</span>
<div class="g"><span class="h" id="v1">x</span><span class="i" id="v2">x</span></div>
<span class="i" id="v3">x</span>
<span class="k on" id="w1">x</span><span class="k" id="w2">x</span>
<span class="m" id="z1">x</span>
</div>`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, `<!doctype html><html data-color-scheme="%s"><meta charset=utf-8><style>%s</style>%s`, r.URL.Query().Get("s"), css, body)
	}))
	t.Cleanup(srv.Close)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:], chromedp.NoSandbox)...)
	t.Cleanup(cancelAlloc)
	ctx, cancel := chromedp.NewContext(allocCtx)
	t.Cleanup(cancel)
	ctx, c2 := context.WithTimeout(ctx, 60*time.Second)
	t.Cleanup(c2)
	const js = `Object.fromEntries([...document.querySelectorAll('[id]')].map(e => [e.id, getComputedStyle(e).color === 'rgb(255, 0, 0)']))`
	want := map[string]bool{"t1": true, "t2": true, "t3": false, "u1": true, "u2": true, "u3": false, "v1": true, "v2": true, "v3": false, "w1": true, "w2": false, "z1": true}
	for _, scheme := range []string{"dark", "light"} {
		var got map[string]bool
		if err := chromedp.Run(ctx, chromedp.EmulateViewport(1024, 700), chromedp.Navigate(srv.URL+"?s="+scheme), chromedp.Evaluate(js, &got)); err != nil {
			t.Fatal(err)
		}
		for id, w := range want {
			if scheme == "light" {
				w = false
			}
			if got[id] != w {
				t.Errorf("%s: #%s red=%v want %v", scheme, id, got[id], w)
			}
		}
	}
}
