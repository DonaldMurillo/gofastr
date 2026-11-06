package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework"
)

func TestInProcessSessionAndCSRF(t *testing.T) {
	manager := newMultiTenantTestMgr(t)
	application := framework.NewApp()
	application.Use(SessionMiddleware(manager))
	application.Use(CSRF())
	testInProcessAuthPosture(t, application, http.StatusForbidden)
}

func TestInProcessBFFPosture(t *testing.T) {
	application := framework.NewApp(WithBFFPosture(newMultiTenantTestMgr(t), BFFPostureConfig{
		AllowedOrigins: []string{"https://app.example.com"},
	}))
	testInProcessAuthPosture(t, application, http.StatusForbidden)
}

func TestInProcessRequireAuth(t *testing.T) {
	application := framework.NewApp()
	application.Use(RequireAuth(NewJWTAuth("inprocess-requireauth", time.Hour)))
	testInProcessAuthPosture(t, application, http.StatusUnauthorized)
}

// refused is the status a network request with a context user (and no
// credential) gets: CSRF answers 403, RequireAuth 401.
func testInProcessAuthPosture(t *testing.T, application *framework.App, refused int) {
	t.Helper()
	application.Router().Post("/check", http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		user, _ := handler.GetUser(request.Context())
		if user == nil {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		writer.WriteHeader(http.StatusNoContent)
	}))
	for _, test := range []struct {
		name    string
		handler http.Handler
		user    bool
		status  int
	}{
		{"Local", application.InProcessHandler(), true, http.StatusNoContent},
		{"AnonymousLocal", application.InProcessHandler(), false, http.StatusUnauthorized},
		{"Network", application.Router(), true, refused},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/check", nil)
			if test.user {
				request = request.WithContext(handler.SetUser(request.Context(), &BasicUser{ID: "alice"}))
			}
			recorder := httptest.NewRecorder()
			test.handler.ServeHTTP(recorder, request)
			if recorder.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", recorder.Code, test.status, recorder.Body.String())
			}
		})
	}
}
