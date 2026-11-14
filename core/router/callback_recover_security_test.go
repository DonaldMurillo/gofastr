package router

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/textsafe"
)

func TestPanickingRouteGateFailsClosed(t *testing.T) {
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		t.Run(method, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("route gate panic escaped ServeHTTP: %v", recovered)
				}
			}()
			router := New()
			called := false
			router.Post("/guarded", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				called = true
			}))
			router.SetRouteGate(func(string) bool { panic("gate boom") })
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(method, "/guarded", nil))
			if called || response.Code != http.StatusNotFound || response.Header().Get("Allow") != "" {
				t.Fatalf("handler called = %v, status = %d, Allow = %q; want false, 404, empty", called, response.Code, response.Header().Get("Allow"))
			}
		})
	}
}

func TestPanickingPreRouteFailsClosed(t *testing.T) {
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("pre-route panic escaped ServeHTTP: %v", recovered)
		}
	}()
	router := New()
	called := false
	router.Get("/guarded", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		called = true
	}))
	router.PreRoute(func(*http.Request) (*http.Request, string) { panic("pre-route boom") })
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/guarded", nil)
	catalog := i18n.NewMapCatalog()
	catalog.Set("fr", i18n.StatusPhrase(http.StatusInternalServerError).Key(), i18n.Message{Text: "erreur interne"})
	ctx := i18n.WithTranslator(i18n.WithContext(request.Context(), i18n.Locale{Tag: "fr"}), i18n.NewTranslator(catalog, "en"))
	router.ServeHTTP(response, request.WithContext(ctx))
	if called || response.Code != http.StatusInternalServerError {
		t.Fatalf("handler called = %v, status = %d; want false, 500", called, response.Code)
	}
	if response.Body.String() != "erreur interne\n" {
		t.Fatalf("body = %q, want translated error", response.Body.String())
	}
}

func TestRouterHookPanicParity(t *testing.T) {
	const panicValue = "callback\n\x1b[31m\u202e boom"
	for _, test := range []struct {
		name    string
		install func(*Router)
		method  string
		status  int
		allow   string
		served  bool
		message string
	}{
		{
			name: "route gate on route path",
			install: func(router *Router) {
				router.SetRouteGate(func(string) bool { panic(panicValue) })
			},
			method: http.MethodPost, status: http.StatusNotFound,
			message: "router: route gate panicked; refused",
		},
		{
			name: "route gate on 405 path",
			install: func(router *Router) {
				router.Put("/guarded", http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
				router.SetRouteGate(func(key string) bool {
					if key == "POST /guarded" {
						panic(panicValue)
					}
					return true
				})
			},
			method: http.MethodDelete, status: http.StatusMethodNotAllowed, allow: http.MethodPut,
			message: "router: route gate panicked; refused",
		},
		{
			name: "serve hook",
			install: func(router *Router) {
				router.SetServeHook(func(string, string) { panic(panicValue) })
			},
			method: http.MethodPost, status: http.StatusTeapot, served: true,
			message: "router: serve hook panicked; ignored",
		},
		{
			name: "pre-route hook",
			install: func(router *Router) {
				router.PreRoute(func(*http.Request) (*http.Request, string) { panic(panicValue) })
			},
			method: http.MethodPost, status: http.StatusInternalServerError,
			message: "router: pre-route hook panicked; request failed",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("callback panic escaped ServeHTTP: %v", recovered)
				}
			}()
			var output bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			router := New()
			served, chained := false, false
			router.Use(func(next http.Handler) http.Handler {
				return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
					chained = true
					next.ServeHTTP(writer, request)
				})
			})
			router.Post("/guarded", http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				served = true
				writer.WriteHeader(http.StatusTeapot)
			}))
			test.install(router)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(test.method, "/guarded", nil))
			if response.Code != test.status || response.Header().Get("Allow") != test.allow || served != test.served {
				t.Fatalf("status = %d, Allow = %q, served = %v; want %d, %q, %v", response.Code, response.Header().Get("Allow"), served, test.status, test.allow, test.served)
			}
			if test.name == "pre-route hook" && chained {
				t.Fatal("panicking pre-route hook reached the middleware chain")
			}
			if bytes.Count(output.Bytes(), []byte{'\n'}) != 1 {
				t.Fatalf("want one slog record, got %q", output.String())
			}
			var record map[string]any
			if err := json.Unmarshal(output.Bytes(), &record); err != nil {
				t.Fatal(err)
			}
			if record["level"] != "ERROR" || record["msg"] != test.message || record["panic"] != textsafe.Recovered(panicValue) {
				t.Fatalf("unexpected panic log: %v", record)
			}
			if stack, ok := record["stack"].(string); !ok || !strings.Contains(stack, "TestRouterHookPanicParity") {
				t.Fatalf("stack = %v, want callback stack", record["stack"])
			}
		})
	}
}
