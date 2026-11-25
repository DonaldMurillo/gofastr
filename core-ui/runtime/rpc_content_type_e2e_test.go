package runtime

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/mediatype"
	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

func TestRPCRequestContentTypes(t *testing.T) {
	for _, method := range []string{"POST", "PUT", "PATCH", "DELETE", "GET", "HEAD", "multipart"} {
		t.Run(method, func(t *testing.T) {
			var received atomic.Pointer[formRequest]
			mux := http.NewServeMux()
			mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/javascript")
				fmt.Fprint(w, MustRuntimeJS())
			})
			handleRuntimeModules(t, mux)
			mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
				body, err := io.ReadAll(r.Body)
				if err != nil {
					http.Error(w, err.Error(), 500)
					return
				}
				received.Store(&formRequest{method: r.Method, contentType: r.Header.Get("Content-Type"), jsonBody: string(body)})
				w.WriteHeader(http.StatusNoContent)
			})
			mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/html")
				trigger := fmt.Sprintf(`<button id="go" data-cui-rpc="/rpc" data-cui-rpc-method="%s" data-cui-rpc-after-text="Done">Go</button>`, method)
				if method == "multipart" {
					trigger = `<form data-cui-rpc="/rpc" enctype="multipart/form-data" data-cui-rpc-after-text="Done"><input name="title" value="First deal"><button id="go" type="submit">Go</button></form>`
				}
				fmt.Fprint(w, `<!doctype html><html><body>`+trigger+`<script src="/__gofastr/runtime.js"></script></body></html>`)
			})
			srv := httptest.NewServer(mux)
			t.Cleanup(srv.Close)
			ctx := chromedptest.Context(t)
			if err := chromedp.Run(ctx,
				chromedp.Navigate(srv.URL),
				chromedp.Click(`#go`, chromedp.ByID),
				chromedp.Poll(`document.body.textContent.includes('Done')`, nil, chromedp.WithPollingTimeout(5*time.Second)),
			); err != nil {
				t.Fatal(err)
			}
			r := received.Load()
			if r == nil {
				t.Fatal("RPC was not received")
			}
			switch method {
			case "GET", "HEAD":
				if r.contentType != "" || r.jsonBody != "" {
					t.Fatalf("bodyless read received content type %q and body %q", r.contentType, r.jsonBody)
				}
			case "multipart":
				if !strings.HasPrefix(r.contentType, "multipart/form-data; boundary=") || !strings.Contains(r.jsonBody, "First deal") {
					t.Fatalf("multipart request lost its boundary or form data: %+v", r)
				}
			default:
				if r.contentType != "application/json" || r.jsonBody != "" {
					t.Fatalf("bodyless write received content type %q and body %q", r.contentType, r.jsonBody)
				}
			}
		})
	}
}

func TestRPCMixedCaseJSONResponse(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		fmt.Fprint(w, MustRuntimeJS())
	})
	handleRuntimeModules(t, mux)
	mux.HandleFunc("/rpc", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", r.URL.Query().Get("ct"))
		fmt.Fprint(w, `{"v":1}`)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html><button id="go" data-cui-rpc="/rpc?%s" data-cui-rpc-signal="result">Go</button>
<script src="/__gofastr/runtime.js"></script>`, (url.Values{"ct": {r.URL.Query().Get("ct")}}).Encode())
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ctx := chromedptest.Context(t)
	for _, ct := range []string{"Application/JSON; charset=utf-8", "application/json charset=utf-8", "text/plain", ""} {
		want := "string"
		if strings.Contains(mediatype.Fold(ct), "application/json") {
			want = "object"
		}
		var got string
		if err := chromedp.Run(ctx,
			chromedp.Navigate(srv.URL+"/?"+url.Values{"ct": {ct}}.Encode()),
			chromedp.Click("#go", chromedp.ByID),
			chromedp.Poll(`window.__gofastr._signals.result?.value !== undefined`, nil, chromedp.WithPollingTimeout(5*time.Second)),
			chromedp.Evaluate(`typeof window.__gofastr._signals.result.value`, &got),
		); err != nil {
			t.Fatalf("Content-Type %q: %v", ct, err)
		}
		if got != want {
			t.Errorf("Content-Type %q: signal type = %s, want %s", ct, got, want)
		}
	}
}
