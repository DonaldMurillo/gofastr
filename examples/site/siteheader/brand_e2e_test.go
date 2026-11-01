package siteheader_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/cdproto/runtime"
	"github.com/chromedp/chromedp"
)

func TestBrandAcrossThemesAndViewports(t *testing.T) {
	server := menuSite(t)
	for _, width := range []int{390, 1280} {
		t.Run(fmt.Sprintf("width-%d", width), func(t *testing.T) {
			ctx := chromedptest.Context(t, chromedptest.WindowSize(width, 800), chromedptest.Timeout(60*time.Second))
			if err := chromedp.Run(ctx, chromedp.Navigate(server.URL), chromedp.WaitVisible("header .brand-mark svg", chromedp.ByQuery)); err != nil {
				t.Fatal(err)
			}
			colors := make(map[string]string)
			for _, scheme := range []string{"light", "dark"} {
				var result struct {
					Visible    bool   `json:"visible"`
					Named      bool   `json:"named"`
					Outlined   bool   `json:"outlined"`
					Inherits   bool   `json:"inherits"`
					Contrasts  bool   `json:"contrasts"`
					NoOverflow bool   `json:"noOverflow"`
					Color      string `json:"color"`
				}
				script := fmt.Sprintf(`(async () => {
                    document.documentElement.setAttribute('data-color-scheme', %q);
                    await new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)));
                    const logo = document.querySelector('header .brand-mark svg');
                    const link = logo.closest('a');
                    const bounds = logo.getBoundingClientRect();
                    const header = logo.closest('header').getBoundingClientRect();
                    const color = getComputedStyle(logo).color;
                    const context = document.createElement('canvas').getContext('2d');
                    const luminance = css => {
                        context.fillStyle = css;
                        context.fillRect(0, 0, 1, 1);
                        const linear = [...context.getImageData(0, 0, 1, 1).data].slice(0, 3).map(value => {
                            const channel = value / 255;
                            return channel <= .04045 ? channel / 12.92 : ((channel + .055) / 1.055) ** 2.4;
                        });
                        return .2126 * linear[0] + .7152 * linear[1] + .0722 * linear[2];
                    };
                    const ink = luminance(color), paper = luminance(getComputedStyle(logo.closest('header')).backgroundColor);
                    return {
                        visible: bounds.width >= 70 && bounds.height >= 45 && bounds.top >= header.top && bounds.bottom <= header.bottom,
                        named: link.getAttribute('href') === '/' && link.getAttribute('aria-label').startsWith('gofastr, dev'),
                        outlined: !!logo.querySelector('#fastr path') && !!logo.querySelector('#symbol path') && !logo.querySelector('text, image'),
                        inherits: [...logo.querySelectorAll('path')].every(path => getComputedStyle(path).fill === color),
                        contrasts: (Math.max(ink, paper) + .05) / (Math.min(ink, paper) + .05) >= 4.5,
                        noOverflow: document.documentElement.scrollWidth <= innerWidth,
                        color
                    };
                })()`, scheme)
				if err := chromedp.Run(ctx, chromedp.Evaluate(script, &result, func(parameters *runtime.EvaluateParams) *runtime.EvaluateParams {
					return parameters.WithAwaitPromise(true)
				})); err != nil {
					t.Fatal(err)
				}
				if !result.Visible || !result.Named || !result.Outlined || !result.Inherits || !result.Contrasts || !result.NoOverflow {
					t.Errorf("%s brand failed: %+v", scheme, result)
				}
				colors[scheme] = result.Color
			}
			if colors["light"] == colors["dark"] {
				t.Error("brand did not adapt to the theme")
			}
		})
	}
}
