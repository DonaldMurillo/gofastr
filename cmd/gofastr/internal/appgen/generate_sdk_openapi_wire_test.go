package appgen

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/internal/chromedptest"
	"github.com/chromedp/chromedp"
)

// oaWireRequest is one request the fixture API saw.
type oaWireRequest struct {
	Method, Path, Query, Auth, Key, Trace, ContentType, Accept string
	Body                                                       string
}

// oaWireServer answers the oaSDKFixture operations under /api, and under
// /big/api with bodies past the 1 MiB cap. Under /slow a body takes a
// second to arrive, under /slowerr a 500's body does, and under /slowhead
// the headers do; under /fail every
// operation answers 500. A request under /redir is
// redirected to /followed, which records that a client followed it.
func oaWireServer(t *testing.T) (*httptest.Server, func() []oaWireRequest) {
	h, seen := oaWireHandler()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv, seen
}

func oaWireHandler() (http.Handler, func() []oaWireRequest) {
	var mu sync.Mutex
	var seen []oaWireRequest
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		mu.Lock()
		seen = append(seen, oaWireRequest{
			Method: r.Method, Path: r.URL.EscapedPath(), Query: r.URL.RawQuery,
			Auth: r.Header.Get("Authorization"), Key: r.Header.Get("X-Barc-Key"), Trace: r.Header.Get("X-Trace"),
			ContentType: r.Header.Get("Content-Type"), Accept: r.Header.Get("Accept"), Body: string(body),
		})
		mu.Unlock()
		big := strings.HasPrefix(r.URL.Path, "/big/")
		path := strings.TrimPrefix(r.URL.Path, "/big")
		if strings.HasPrefix(path, "/slowhead/") {
			time.Sleep(700 * time.Millisecond)
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte("PNG!"))
			return
		}
		if strings.HasPrefix(path, "/slowerr/") {
			w.WriteHeader(http.StatusInternalServerError)
			for range 4 {
				_, _ = w.Write([]byte("error "))
				w.(http.Flusher).Flush()
				time.Sleep(250 * time.Millisecond)
			}
			return
		}
		if strings.HasPrefix(path, "/slow/") {
			w.Header().Set("Content-Type", "image/png")
			w.WriteHeader(http.StatusOK)
			for range 4 {
				_, _ = w.Write(bytes.Repeat([]byte("x"), 1024))
				w.(http.Flusher).Flush()
				time.Sleep(250 * time.Millisecond)
			}
			return
		}
		if strings.HasPrefix(path, "/fail/") {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"render failed"}`))
			return
		}
		if strings.HasPrefix(path, "/redir/") {
			http.Redirect(w, r, "/followed", http.StatusTemporaryRedirect)
			return
		}
		switch path {
		case "/api/generate", "/api/batch":
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(bytes.Repeat([]byte("PNG!"), 512<<10))
		case "/api/validate":
			if len(body) == 0 {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnprocessableEntity)
				_, _ = w.Write([]byte(`{"error":"data is required"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if big {
				_, _ = w.Write([]byte(`{"valid":true,"extra":"` + strings.Repeat("x", 1<<20) + `"}`))
				return
			}
			_, _ = w.Write([]byte(`{"valid":true,"serial":9007199254740993,"checksum":null,"meta":{"a":"b"},"extra":{"k":[1,2.50]}}`))
		case "/api/decode":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"text":"hi"}]`))
		case "/api/ping":
			w.WriteHeader(http.StatusNoContent)
		default:
			if strings.HasPrefix(path, "/api/spec/") {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"name":"qr","lengths":[1,9007199254740993]}`))
				return
			}
			http.NotFound(w, r)
		}
	})
	return h, func() []oaWireRequest {
		mu.Lock()
		defer mu.Unlock()
		return append([]oaWireRequest(nil), seen...)
	}
}

// oaGoWireProbe exercises every fixture operation through the generated
// Go SDK; it runs as a test inside the generated module.
const oaGoWireProbe = `package client_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	client "example.com/barc-sdk"
)

func TestWire(t *testing.T) {
	ctx := context.Background()
	base := os.Getenv("BARC_URL")
	c := client.NewClient(base+client.APIPrefix, nil)
	c.Token = "tok-1"

	rc, err := c.GenerateBarcode(ctx, client.GenerateRequest{Data: "x", Symbology: "qr"})
	if err != nil {
		t.Fatalf("GenerateBarcode: %v", err)
	}
	n, err := io.Copy(io.Discard, rc)
	rc.Close()
	if err != nil || n != 2<<20 {
		t.Fatalf("GenerateBarcode streamed %d bytes (%v), want the whole 2 MiB image", n, err)
	}

	v, err := c.ValidateData(ctx, &client.ValidateDataRequest{Data: "d"})
	if err != nil {
		t.Fatalf("ValidateData: %v", err)
	}
	if !v.Valid || v.Serial == nil || *v.Serial != 9007199254740993 || v.Checksum != nil || v.Meta["a"] != "b" || string(v.Extra) != "{\"k\":[1,2.50]}" {
		t.Fatalf("ValidateData decoded %+v", v)
	}

	trace := "t-1"
	s, err := c.GetSpec(ctx, client.GetSpecParams{Symbology: "qr/x", Limit: 0, Tag: []string{"a", "b"}, Fields: []string{"x,y", "z"}, XTrace: &trace})
	if err != nil {
		t.Fatalf("GetSpec: %v", err)
	}
	if s.Name == nil || *s.Name != "qr" || len(s.Lengths) != 2 || s.Lengths[1] != 9007199254740993 {
		t.Fatalf("GetSpec decoded %+v", s)
	}

	items, err := c.DecodeImage(ctx, bytes.NewReader([]byte("PNG")))
	if err != nil || len(items) != 1 || items[0].Text == nil || *items[0].Text != "hi" {
		t.Fatalf("DecodeImage: %+v %v", items, err)
	}

	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	var apiErr *client.APIError
	if _, err := c.ValidateData(ctx, nil); !errors.As(err, &apiErr) || apiErr.Status != 422 || !strings.Contains(string(apiErr.Body), "data is required") {
		t.Fatalf("ValidateData(nil): %v, want the 422 as *APIError", err)
	}

	failing := client.NewClient(base+"/fail"+client.APIPrefix, nil)
	if rc, err := failing.GenerateBarcode(ctx, client.GenerateRequest{Data: "x", Symbology: "qr"}); !errors.As(err, &apiErr) || apiErr.Status != 500 || rc != nil {
		t.Fatalf("a binary operation answering 500: %v, want the 500 as *APIError and no body", err)
	}

	redir := client.NewClient(base+"/redir"+client.APIPrefix, nil)
	redir.Token = "tok-1"
	if err := redir.Ping(ctx); !errors.As(err, &apiErr) || apiErr.Status != 307 {
		t.Fatalf("a redirect: %v, want the 307 as *APIError", err)
	}

	if d := client.NewClient(base, nil).HTTP.Timeout; d != 30*time.Second {
		t.Fatalf("the default client's timeout is %s, want 30s", d)
	}
	short := &http.Client{Timeout: 300 * time.Millisecond, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	slow := client.NewClient(base+"/slow"+client.APIPrefix, short)
	rc, err = slow.BatchZip(ctx, nil)
	if err != nil {
		t.Fatalf("BatchZip on a slow body: %v", err)
	}
	n, err = io.Copy(io.Discard, rc)
	rc.Close()
	if err != nil || n != 4096 {
		t.Fatalf("a stream slower than Client.Timeout read %d bytes (%v), want all 4096: the timeout bounds the headers, not the download", n, err)
	}
	if _, err := slow.ValidateData(ctx, &client.ValidateDataRequest{Data: "d"}); err == nil {
		t.Fatal("a buffered JSON call outlived Client.Timeout")
	}
	if _, err := client.NewClient(base+"/slowhead"+client.APIPrefix, short).BatchZip(ctx, nil); err == nil || !strings.Contains(err.Error(), "no response headers within 300ms") {
		t.Fatalf("a stream whose headers outlive Client.Timeout: %v, want the header timeout", err)
	}

	start := time.Now()
	if _, err := client.NewClient(base+"/slowerr"+client.APIPrefix, short).BatchZip(ctx, nil); err == nil || time.Since(start) > 800*time.Millisecond {
		t.Fatalf("a 500 whose body trickles: %v after %s, want an error within Client.Timeout", err, time.Since(start))
	}

	big := client.NewClient(base+"/big"+client.APIPrefix, nil)
	if _, err := big.ValidateData(ctx, &client.ValidateDataRequest{Data: "d"}); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("a JSON answer past the cap: %v, want the cap error", err)
	}
}
`

// The generated Go SDK compiles, passes go vet, and calls every fixture
// operation the way the document describes: typed JSON bodies and
// results with exact integers, a streamed binary answer past the JSON
// cap, path, query and header parameters, a binary upload under the
// apiKey scheme one operation names, and the bearer token elsewhere.
func TestOpenAPISDKGoWire(t *testing.T) {
	out := oaSDKGenerate(t, oaSDKFixture)
	srv, seen := oaWireServer(t)
	goDir := filepath.Join(out, "go")
	if err := os.WriteFile(filepath.Join(goDir, "wire_test.go"), []byte(oaGoWireProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"vet", "./..."}, {"test", "-count=1", "./..."}} {
		cmd := exec.Command("go", args...)
		cmd.Dir = goDir
		cmd.Env = append(os.Environ(), "BARC_URL="+srv.URL, "GOFLAGS=-mod=mod", "GOPROXY=off")
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("go %s over the generated SDK: %v\n%s", strings.Join(args, " "), err, output)
		}
	}

	checkOAWire(t, "the Go SDK", seen())
}

// checkOAWire checks the first request each fixture operation sent.
func checkOAWire(t *testing.T, who string, seen []oaWireRequest) {
	t.Helper()
	byPath := map[string]oaWireRequest{}
	for _, r := range seen {
		if r.Accept == "*/*" {
			r.Accept = "" // fetch's own default: the client sent none
		}
		if _, dup := byPath[r.Path]; !dup {
			byPath[r.Path] = r
		}
	}
	checks := []struct {
		path string
		want oaWireRequest
	}{
		{"/api/generate", oaWireRequest{Method: "POST", Auth: "Bearer tok-1", ContentType: "application/json", Accept: "image/png, image/svg+xml", Body: `{"data":"x","symbology":"qr"}`}},
		{"/api/validate", oaWireRequest{Method: "POST", Auth: "Bearer tok-1", ContentType: "application/json", Accept: "application/json", Body: `{"data":"d"}`}},
		{"/api/spec/qr%2Fx", oaWireRequest{Method: "GET", Auth: "Bearer tok-1", Query: "limit=0&tag=a&tag=b&fields=x%2Cy,z", Trace: "t-1", Accept: "application/json"}},
		{"/api/decode", oaWireRequest{Method: "POST", Key: "tok-1", ContentType: "image/png", Accept: "application/json", Body: "PNG"}},
		{"/api/ping", oaWireRequest{Method: "DELETE", Auth: "Bearer tok-1"}},
	}
	for _, c := range checks {
		got, ok := byPath[c.path]
		c.want.Path = c.path
		if !ok || got != c.want {
			t.Errorf("%s %s: got %+v, want %+v", who, c.path, got, c.want)
		}
	}
	if _, followed := byPath["/followed"]; followed {
		t.Errorf("%s followed a redirect, re-sending the token", who)
	}
}

// oaJSWireProbe exercises every fixture operation through the generated
// client.js in a browser and reports what each returned or threw.
const oaJSWireProbe = `(async () => {
  const { Client, APIPrefix, ApiError } = await import('/client.js');
  const api = new Client({ baseURL: location.origin + APIPrefix, token: 'tok-1' });
  const out = {};
  const img = await api.generateBarcode({ data: 'x', symbology: 'qr' });
  out.img = [img instanceof Blob, img.size, img.type];
  const v = await api.validateData({ data: 'd' });
  out.v = [v.valid, typeof v.serial, String(v.serial), v.checksum];
  const s = await api.getSpec({ symbology: 'qr/x', limit: 0, tag: ['a', 'b'], fields: ['x,y', 'z'], 'X-Trace': 't-1' });
  out.s = [s.name, String(s.lengths[1])];
  const items = await api.decodeImage(new Blob(['PNG'], { type: 'image/png' }));
  out.items = items[0].text;
  out.ping = await api.ping();
  const fail = async (f) => { try { await f(); return 'resolved'; } catch (e) { return [e instanceof ApiError ? 'ApiError' : e.name, e.status ?? null, /exceeded/.test(String(e.body))]; } };
  out.err = await fail(() => api.validateData());
  out.blobCap = await fail(() => new Client({ baseURL: location.origin + APIPrefix, maxBlobBytes: 1024 }).generateBarcode({ data: 'x', symbology: 'qr' }));
  out.jsonCap = await fail(() => new Client({ baseURL: location.origin + '/big' + APIPrefix }).validateData({ data: 'd' }));
  out.blobErr = await fail(() => new Client({ baseURL: location.origin + '/fail' + APIPrefix }).generateBarcode({ data: 'x', symbology: 'qr' }));
  out.redirect = await fail(() => new Client({ baseURL: location.origin + '/redir' + APIPrefix, token: 'tok-1' }).ping());
  out.required = await fail(() => api.getSpec({ symbology: 'qr' }));
  window.__result = JSON.stringify(out);
})().catch((e) => { window.__result = 'fatal: ' + String(e); }); true`

// The generated client.js calls every fixture operation the way the Go
// SDK does on the wire, resolves a binary answer to a Blob carrying its
// Content-Type, keeps an integer past 2^53 exact, and throws ApiError
// for a non-2xx answer, a redirect, and a body past either cap.
func TestOpenAPISDKJSWireBrowser(t *testing.T) {
	if testing.Short() {
		t.Skip("boots Chrome")
	}
	out := oaSDKGenerate(t, oaSDKFixture)
	clientJS, err := os.ReadFile(filepath.Join(out, "js", "client.js"))
	if err != nil {
		t.Fatal(err)
	}
	api, seen := oaWireHandler()
	mux := http.NewServeMux()
	mux.HandleFunc("/client.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		_, _ = w.Write(clientJS)
	})
	mux.HandleFunc("/{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, "<!doctype html><title>harness</title><body>harness</body>")
	})
	mux.Handle("/", api)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	ctx := chromedptest.Context(t)
	var got string
	if err := chromedp.Run(ctx,
		chromedp.Navigate(srv.URL+"/"),
		chromedp.WaitReady("body", chromedp.ByQuery),
		chromedp.Evaluate(oaJSWireProbe, nil),
		chromedp.Poll(`!!window.__result`, nil, chromedp.WithPollingTimeout(15*time.Second)),
		chromedp.Evaluate(`window.__result`, &got),
	); err != nil {
		t.Fatalf("chromedp: %v", err)
	}
	want := `{"img":[true,2097152,"image/png"],"v":[true,"string","9007199254740993",null],"s":["qr","9007199254740993"],` +
		`"items":"hi","ping":null,"err":["ApiError",422,false],"blobCap":["ApiError",200,true],"jsonCap":["ApiError",200,true],` +
		`"blobErr":["ApiError",500,false],"redirect":["ApiError",0,false],"required":["TypeError",null,false]}`
	if got != want {
		t.Fatalf("client.js results\n got %s\nwant %s", got, want)
	}
	checkOAWire(t, "client.js", seen())
}
