package framework

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core-ui/app"
	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/render"
	"github.com/DonaldMurillo/gofastr/framework/uihost"
)

type portabilityScreen struct{}

func (portabilityScreen) Render() render.HTML {
	return render.HTML("<html><head><title>Home</title></head><body><h1>Home</h1></body></html>")
}

func newReplica(t *testing.T, opts ...AppOption) *App {
	t.Helper()
	coreApp := app.NewApp("replica")
	coreApp.RegisterScreen(app.NewScreen("/", &portabilityScreen{}).WithTitle("Home"), nil)
	fw := NewApp(append([]AppOption{WithoutDefaultMiddleware()}, opts...)...)
	fw.Mount(uihost.New(coreApp))
	return fw
}

// TestSessionPortableAcrossReplicas is the #112 acceptance test at the
// HTTP layer: a session cookie minted by replica A is accepted by
// replica B when both share the app secret — no shared state, no sticky
// routing.
func TestSessionPortableAcrossReplicas(t *testing.T) {
	secret := strings.Repeat("s", 32)
	replicaA := newReplica(t, WithSecret(secret))
	replicaB := newReplica(t, WithSecret(secret))

	// Mint on A via a normal page render.
	rec := httptest.NewRecorder()
	replicaA.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	cookies := rec.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatal("replica A set no session cookie")
	}

	// Replay against B on a session-gated endpoint.
	req := httptest.NewRequest(http.MethodGet, "/__gofastr/actions.js", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec = httptest.NewRecorder()
	replicaB.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("replica B rejected replica A's session: %d %s", rec.Code, rec.Body)
	}

	// A replica with a DIFFERENT secret must reject the same cookie.
	replicaC := newReplica(t, WithSecret(strings.Repeat("c", 32)))
	req = httptest.NewRequest(http.MethodGet, "/__gofastr/actions.js", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec = httptest.NewRecorder()
	replicaC.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("foreign-secret replica accepted the session: %d", rec.Code)
	}
}

func TestMountFanoutWithoutSecretPanics(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("Mount with fanout and no secret did not fail closed")
		}
		if !strings.Contains(r.(string), "GOFASTR_SECRET") {
			t.Fatalf("panic message does not name the fix: %v", r)
		}
	}()
	newReplica(t, WithFanout(fanout.NewInProcess()))
}

func TestMountFanoutWithSecretBoots(t *testing.T) {
	newReplica(t, WithFanout(fanout.NewInProcess()), WithSecret(strings.Repeat("s", 32)))
}
