//go:build chromium

package chromepdf

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chromedp/chromedp"
)

// TestZZReviewHostResolverRulesBlockIPLiteral asks the only question that
// matters about hostResolverRules: does "MAP * ~NOTFOUND" actually stop a
// print document from fetching an IP literal?
//
// Chromium applies host-resolver-rules inside the HostResolver. A URL whose
// host is already an IP address never reaches the resolver, so the MAP rule
// may not apply — which would make the allow-list a hostname-only gate and
// leave 127.0.0.1 / 169.254.169.254 reachable from a rendered document.
func TestResolverRulesBlockIPLiteral(t *testing.T) {
	// A loopback server standing in for any internal-only address.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	hit := make(chan string, 4)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case hit <- r.URL.Path:
		default:
		}
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "INTERNAL-SECRET-REACHED")
	})}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	internal := "http://" + ln.Addr().String() + "/metadata"
	t.Logf("internal target: %s", internal)

	// Exactly the flags the production allocator builds: an allow-list that
	// does NOT include our loopback host.
	rules := hostResolverRules([]string{"fonts.example.com"})
	t.Logf("host-resolver-rules = %q", rules)

	allocOpts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("host-resolver-rules", rules),
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
	)
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(context.Background(), allocOpts...)
	defer cancelAlloc()
	ctx, cancel := chromedp.NewContext(allocCtx)
	defer cancel()
	ctx, cancelT := context.WithTimeout(ctx, 45*time.Second)
	defer cancelT()

	// A document that fetches the internal address, exactly as a malicious
	// print payload would.
	doc := `<html><body><script>
	  fetch(` + "`" + internal + "`" + `).then(r=>r.text()).then(t=>{document.title=t})
	    .catch(e=>{document.title='BLOCKED:'+e});
	</script></body></html>`

	var title string
	err = chromedp.Run(ctx,
		chromedp.Navigate("data:text/html,"+strings.ReplaceAll(doc, "#", "%23")),
		chromedp.Sleep(3*time.Second),
		chromedp.Title(&title),
	)
	if err != nil {
		t.Skipf("chrome unavailable / navigation failed (%v) — cannot settle the question here", err)
	}
	t.Logf("document.title after fetch = %q", title)

	select {
	case p := <-hit:
		t.Fatalf("SSRF: the print document reached the internal loopback server at %s (path %q) "+
			"despite host-resolver-rules=%q — MAP * ~NOTFOUND does not apply to IP-literal hosts, "+
			"so AllowedHosts is a hostname-only gate", internal, p, rules)
	default:
		t.Logf("internal server was NOT reached — the resolver rule held for IP literals too")
	}
}
