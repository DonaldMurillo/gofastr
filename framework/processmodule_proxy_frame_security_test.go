package framework

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/moduleproto"
)

// A body under the proxy's 1 MiB read cap can still overflow the module's
// frame once base64 grows it by a third. The proxy must answer that body
// 413, as it does for one over the read cap: it used to send the call,
// the codec refused the frame, and the client got a 503 + Retry-After for
// a request that can never succeed.
func TestProxyBodyOverFrameIs413(t *testing.T) {
	sup, sl, _, child, cleanup := toolTestSlot(t, nil)
	defer cleanup()
	reached := make(chan int, 4)
	if err := child.Handle(moduleproto.MethodHTTP, func(_ context.Context, raw json.RawMessage) (any, error) {
		var p moduleproto.HTTPRequestParams
		_ = json.Unmarshal(raw, &p)
		reached <- len(p.BodyB64)
		return moduleproto.HTTPResponseResult{
			Status: http.StatusOK,
			Body:   moduleproto.HTTPResponseBody{Kind: moduleproto.BodyKindText, Value: jsonRaw(`"ok"`)},
		}, nil
	}); err != nil {
		t.Fatal(err)
	}
	frame := sl.frameBytes()

	// Fits: three quarters of the frame after base64 growth.
	fits := bytes.Repeat([]byte("x"), frame/2)
	ok := httptest.NewRecorder()
	sup.serveProxy(sl.desc.Name, "r1", ok, httptest.NewRequest(http.MethodPost, "/r", bytes.NewReader(fits)))
	if ok.Code != http.StatusOK {
		t.Fatalf("body that fits the frame: status %d, want 200 (%q)", ok.Code, ok.Body.String())
	}
	<-reached

	// Under the read cap, over the frame once encoded.
	over := bytes.Repeat([]byte("x"), frame*4/5)
	if len(over) > maxModuleHTTPBodyBytes {
		t.Fatalf("test body %d is over the read cap; the case needs one under it", len(over))
	}
	rec := httptest.NewRecorder()
	sup.serveProxy(sl.desc.Name, "r1", rec, httptest.NewRequest(http.MethodPost, "/r", bytes.NewReader(over)))
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("body over the frame: status %d, want 413 (Retry-After %q)", rec.Code, rec.Header().Get("Retry-After"))
	}
	select {
	case n := <-reached:
		t.Fatalf("over-frame body reached the module (%d base64 bytes)", n)
	default:
	}
}
