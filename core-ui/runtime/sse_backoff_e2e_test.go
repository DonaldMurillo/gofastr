package runtime

import (
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestSSERetriesBackOffWithJitter(t *testing.T) {
	base := startPollServer(t, `<!doctype html><html><head><title>Retry policy</title>
<meta name="gofastr-sse" content="/__gofastr/sse?session=dead"></head><body><div data-island="live">Live</div>
<script>window.__streams=[];window.EventSource=class{constructor(){window.__streams.push(this)}addEventListener(){}close(){}};</script>
<script src="/__gofastr/runtime.js"></script></body></html>`, nil)
	ctx := chromedptest.Context(t, chromedptest.Timeout(45*time.Second))
	var failures []string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base),
		chromedp.Poll(`window.__streams.length && window.__gofastr?.loadedModules?.sse`, nil),
		chromedp.Evaluate(`(() => {
			const failures = [], G = window.__gofastr;
			const timeout = window.setTimeout, random = Math.random;
			function fail(draw) {
				let retry;
				window.setTimeout = (callback, delay) => {
					retry = {callback, delay};
					return timeout(() => {}, 10000000);
				};
				Math.random = () => draw;
				try { window.__streams.at(-1).onerror(); }
				finally { window.setTimeout = timeout; Math.random = random; }
				if (!retry) throw Error('missing retry timer');
				const count = window.__streams.length;
				G._moduleScanners.sse(document);
				if (window.__streams.length !== count) failures.push('DOM scan bypassed retry '+G.sseStatus.retryCount);
				return retry;
			}
			for (let attempt = 0; attempt < 12; attempt++) {
				const draw = attempt % 2 ? 0.999999 : 0;
				const retry = fail(draw);
				const ceiling = Math.min(60000, 3000 * 2 ** attempt);
				const expected = Math.round(ceiling * (0.5 + draw / 2));
				if (retry.delay !== expected) failures.push('attempt '+attempt+' delay '+retry.delay+', want '+expected);
				retry.callback();
			}
			window.__streams.at(-1).onopen();
			if (G.sseStatus.retryCount !== 0) failures.push('open did not reset retries');
			const reset = fail(0);
			if (reset.delay !== 1500) failures.push('retry after open did not reset its delay');
			reset.callback();
			window.__streams.at(-1).onopen();
			return failures;
		})()`, &failures),
	); err != nil {
		t.Fatal(err)
	}
	if len(failures) > 0 {
		t.Fatal(strings.Join(failures, "; "))
	}
}
