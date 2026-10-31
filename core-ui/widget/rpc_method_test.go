package widget_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/widget"
	"github.com/DonaldMurillo/gofastr/core/router"
)

// A PATCH RPC was registered as POST (2026-10-27 U34): the runtime's
// PATCH got no handler and a POST reached it instead.
func TestMountRegistersEachRPCMethod(t *testing.T) {
	echo := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, r.Method) })
	def := widget.New("rpcmethods").
		RPC("GET", "/m/get", echo).
		RPC("POST", "/m/post", echo).
		RPC("PUT", "/m/put", echo).
		RPC("PATCH", "/m/patch", echo).
		RPC("DELETE", "/m/delete", echo).
		RPC("", "/m/default", echo).
		Build()
	def.RPCs = append(def.RPCs, widget.RPCEndpoint{Path: "/m/literal", Handler: echo})
	r := router.New()
	widget.Mount(r, &def)
	srv := httptest.NewServer(r)
	defer srv.Close()

	do := func(method, path string) (int, string) {
		t.Helper()
		req, err := http.NewRequest(method, srv.URL+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	for path, method := range map[string]string{
		"/m/get": "GET", "/m/post": "POST", "/m/put": "PUT", "/m/patch": "PATCH",
		"/m/delete": "DELETE", "/m/default": "POST", "/m/literal": "POST",
	} {
		if code, body := do(method, path); code != http.StatusOK || body != method {
			t.Errorf("%s %s = %d %q, want 200 from the handler", method, path, code, body)
		}
	}
	if code, body := do("POST", "/m/patch"); code == http.StatusOK {
		t.Errorf("POST reached the PATCH handler: %d %q", code, body)
	}
}

// A method Mount does not register is a mistake, not a POST.
func TestMountRefusesUnknownRPCMethod(t *testing.T) {
	for _, m := range []string{"patch", "TRACE", "OPTIONS"} {
		t.Run(m, func(t *testing.T) {
			def := widget.Definition{Name: "badmethod-" + m, RPCs: []widget.RPCEndpoint{{Method: m, Path: "/bad/" + m, Handler: http.NotFoundHandler()}}}
			defer func() {
				if p := recover(); p == nil || !strings.Contains(fmtPanic(p), m) {
					t.Fatalf("Mount with method %q: panic %v, want one naming the method", m, p)
				}
			}()
			widget.Mount(router.New(), &def)
		})
	}
}

func fmtPanic(p any) string {
	if s, ok := p.(string); ok {
		return s
	}
	if e, ok := p.(error); ok {
		return e.Error()
	}
	return ""
}
