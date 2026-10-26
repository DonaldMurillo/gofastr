package runtime

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// formIntercept e2e suite, verifies the runtime.js submit interceptor's
// safe-by-default behaviour:
//
//   - urlencoded / multipart / unspecified-enctype forms are NOT
//     intercepted; the browser submits natively (cookies, Location
//     follow, file uploads, password-manager UX all work).
//   - enctype="application/json" → intercepted as JSON RPC.
//   - data-cui-spa GET → successful controls become a client-navigation URL.
//   - data-cui-spa on other methods → intercepted with a urlencoded body,
//     following a response Location as a native document navigation.

type formRequest struct {
	method      string
	contentType string
	formValues  map[string]string
	jsonBody    string
}

// The dispatcher hands marked GET forms to the existing navigator with a
// native-shaped query, rather than asking fetch to send a GET body. The site
// icon catalog test exercises the real navigator and asserts document reuse.
func TestFormIntercept_GETUsesClientNavigation(t *testing.T) {
	var recv atomic.Pointer[formRequest]
	base := startFormE2EServer(t, &recv)
	ctx := chromedptest.Context(t)
	var destination string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/?spa=1"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Poll(`typeof __gofastr.navigate === 'function'`, nil),
		chromedp.Evaluate(`(() => {
			const form = document.getElementById('f');
			form.method = 'post'; form.action = '/wrong';
			form.innerHTML = '<input name="q" value="arrow &amp; down"><input name="tag" value="files"><input name="tag" value="arrows"><input name="ignored" value="disabled" disabled><button id="go" type="submit" name="view" value="grid" formaction="/landing?stale=1#results" formmethod="get" formtarget="_SELF">Search</button>';
			window.__gofastr.navigate = async path => { window.getFormDestination = path; };
		})()`, nil),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.Poll(`typeof window.getFormDestination === 'string'`, nil),
		chromedp.Evaluate(`window.getFormDestination`, &destination),
	); err != nil {
		t.Fatal(err)
	}
	got, err := url.Parse(destination)
	if err != nil {
		t.Fatal(err)
	}
	query := got.Query()
	if got.Path != "/landing" || got.Fragment != "results" || query.Get("q") != "arrow & down" || query.Get("view") != "grid" || strings.Join(query["tag"], ",") != "files,arrows" || query.Has("stale") || query.Has("ignored") {
		t.Fatalf("GET navigation lost native form semantics: %s", destination)
	}
	if recv.Load() != nil {
		t.Error("GET navigation posted to the form action")
	}
}

// startFormE2EServer spins a tiny test app with:
//   - GET /                  HTML page hosting the test form
//   - GET /__gofastr/runtime.js  the live runtime bundle
//   - GET /landing           HTML target the form redirects to
//   - POST /submit           records the incoming request and returns 303 → /landing
//
// The HTML form's enctype, action, and opt-out attribute are all
// controlled by ?enctype= / ?spa= query params on /.
func startFormE2EServer(t *testing.T, recv *atomic.Pointer[formRequest]) string {
	t.Helper()
	js, err := RuntimeJS()
	if err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/__gofastr/runtime.js", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/javascript")
		w.Write([]byte(js))
	})
	handleRuntimeModules(t, mux)

	mux.HandleFunc("/landing", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!doctype html><html><body><h1 id="landed">landed</h1></body></html>`)
	})

	mux.HandleFunc("/submit", func(w http.ResponseWriter, r *http.Request) {
		req := &formRequest{
			method:      r.Method,
			contentType: r.Header.Get("Content-Type"),
			formValues:  map[string]string{},
		}
		ct := req.contentType
		if i := strings.IndexByte(ct, ';'); i >= 0 {
			ct = ct[:i]
		}
		ct = strings.TrimSpace(strings.ToLower(ct))
		switch ct {
		case "application/x-www-form-urlencoded", "multipart/form-data":
			if ct == "multipart/form-data" {
				_ = r.ParseMultipartForm(4096)
				if r.MultipartForm != nil {
					defer r.MultipartForm.RemoveAll()
				}
			} else {
				_ = r.ParseForm()
			}
			for k, v := range r.PostForm {
				if len(v) > 0 {
					req.formValues[k] = v[0]
				}
			}
		case "application/json", "text/plain":
			buf := make([]byte, 4096)
			n, _ := r.Body.Read(buf)
			req.jsonBody = string(buf[:n])
		}
		recv.Store(req)
		http.Redirect(w, r, "/landing", http.StatusSeeOther)
	})

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		enctype := r.URL.Query().Get("enctype")
		attrs := ""
		if r.URL.Query().Get("spa") == "1" {
			attrs += ` data-cui-spa`
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!doctype html>
<html>
<head><title>form-intercept e2e</title></head>
<body>
  <form id="f" action="/submit" method="POST" enctype="%s"%s>
    <input type="text" name="email" value="a@b.com">
    <input type="text" name="password" value="hunter22">
    <button id="go" type="submit">Submit</button>
  </form>
  <script src="/__gofastr/runtime.js"></script>
</body>
</html>`, enctype, attrs)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestFormIntercept_FormEnctypeSendsFormEncoded(t *testing.T) {
	var recv atomic.Pointer[formRequest]
	base := startFormE2EServer(t, &recv)
	ctx := chromedptest.Context(t)

	var finalURL string
	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/?enctype=application/x-www-form-urlencoded"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.WaitVisible(`#landed`, chromedp.ByID),
		chromedp.Location(&finalURL),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}

	if !strings.HasSuffix(finalURL, "/landing") {
		t.Errorf("did not navigate to /landing — got %q", finalURL)
	}
	got := recv.Load()
	if got == nil {
		t.Fatal("server did not receive submit")
	}
	if !strings.HasPrefix(strings.ToLower(got.contentType), "application/x-www-form-urlencoded") {
		t.Errorf("Content-Type = %q, want application/x-www-form-urlencoded", got.contentType)
	}
	if got.formValues["email"] != "a@b.com" || got.formValues["password"] != "hunter22" {
		t.Errorf("form values not parsed by ParseForm: %+v", got.formValues)
	}
}

// TestFormIntercept_NativeByDefaultForNoEnctype pins the safe-by-default
// behaviour: a form with no enctype attribute is NOT intercepted; the
// browser submits it natively as urlencoded (the HTML default). This
// is what auth/login forms want, cookies set, Location followed.
func TestFormIntercept_NativeByDefaultForNoEnctype(t *testing.T) {
	var recv atomic.Pointer[formRequest]
	base := startFormE2EServer(t, &recv)
	ctx := chromedptest.Context(t)

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.WaitVisible(`#landed`, chromedp.ByID),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	got := recv.Load()
	if got == nil {
		t.Fatal("server did not receive submit")
	}
	if !strings.HasPrefix(strings.ToLower(got.contentType), "application/x-www-form-urlencoded") {
		t.Errorf("expected native urlencoded for default-enctype form; Content-Type=%q (got intercepted?)", got.contentType)
	}
	if got.formValues["email"] != "a@b.com" {
		t.Errorf("native submit form values missing: %+v", got.formValues)
	}
}

// TestFormIntercept_JSONEnctypeIsIntercepted pins that explicit
// enctype="application/json" still opts INTO the SPA interceptor,
// the runtime sends JSON body + follows Location via the SPA router.
func TestFormIntercept_JSONEnctypeIsIntercepted(t *testing.T) {
	var recv atomic.Pointer[formRequest]
	base := startFormE2EServer(t, &recv)
	ctx := chromedptest.Context(t)

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/?enctype=application/json"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.WaitVisible(`#landed`, chromedp.ByID),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	got := recv.Load()
	if got == nil {
		t.Fatal("server did not receive submit")
	}
	if !strings.HasPrefix(strings.ToLower(got.contentType), "application/json") {
		t.Errorf("enctype=application/json should produce JSON; Content-Type=%q", got.contentType)
	}
	if !strings.Contains(got.jsonBody, `"email":"a@b.com"`) {
		t.Errorf("JSON body missing email: %q", got.jsonBody)
	}
}

// TestFormIntercept_DataFuiSPAOptsIn pins that data-cui-spa on a plain
// urlencoded form opts INTO interception with urlencoded body, for
// hosts that explicitly want SPA-style nav on a non-JSON form.
func TestFormIntercept_DataFuiSPAOptsIn(t *testing.T) {
	var recv atomic.Pointer[formRequest]
	base := startFormE2EServer(t, &recv)
	ctx := chromedptest.Context(t)

	err := chromedp.Run(ctx,
		chromedp.Navigate(base+"/?spa=1"),
		chromedp.WaitVisible(`#go`, chromedp.ByID),
		chromedp.Click(`#go`, chromedp.ByID),
		chromedp.WaitVisible(`#landed`, chromedp.ByID),
	)
	if err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	got := recv.Load()
	if got == nil {
		t.Fatal("server did not receive submit")
	}
	// data-cui-spa + no enctype → urlencoded body via the interceptor.
	if !strings.HasPrefix(strings.ToLower(got.contentType), "application/x-www-form-urlencoded") {
		t.Errorf("data-cui-spa should keep urlencoded body; Content-Type=%q", got.contentType)
	}
	if got.formValues["email"] != "a@b.com" {
		t.Errorf("spa intercept form values missing: %+v", got.formValues)
	}
}

// Compile-time check: sync is in use for the formRequest tests above.
var _ sync.Mutex
