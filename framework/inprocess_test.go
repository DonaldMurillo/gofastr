package framework_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestInProcessHandlerRefusesCredentials(t *testing.T) {
	application := framework.NewApp()
	called := false
	application.Router().Get("/check", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		called = true
		if !framework.IsInProcessRequest(request) {
			t.Error("missing in-process marker")
		}
		if user, _ := handler.GetUser(request.Context()); user != "trusted" {
			t.Errorf("context user = %v", user)
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	for _, credential := range []string{"Cookie", "Authorization", "Proxy-Authorization", "X-API-Key", "cookie", "EmptyCookie", "URLUser", "TLS"} {
		t.Run(credential, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/check", nil)
			request = request.WithContext(handler.SetUser(context.Background(), "trusted"))
			switch credential {
			case "URLUser":
				request.URL.User = url.UserPassword("user", "password")
			case "TLS":
				request.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{}}}
			case "EmptyCookie":
				request.Header["Cookie"] = nil
			default:
				request.Header[credential] = []string{"credential"}
			}
			called = false
			recorder := httptest.NewRecorder()
			application.InProcessHandler().ServeHTTP(recorder, request)
			if recorder.Code != http.StatusForbidden || called {
				t.Fatalf("credential accepted: status=%d dispatched=%v", recorder.Code, called)
			}
		})
	}
	request := httptest.NewRequest(http.MethodGet, "/check", nil)
	request = request.WithContext(handler.SetUser(request.Context(), "trusted"))
	recorder := httptest.NewRecorder()
	application.InProcessHandler().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusNoContent || !called {
		t.Fatalf("trusted call: %d, dispatched=%v", recorder.Code, called)
	}
	if framework.IsInProcessRequest(request) {
		t.Fatal("entry point mutated caller's request")
	}
}
