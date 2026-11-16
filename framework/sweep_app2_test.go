package framework

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
	"github.com/DonaldMurillo/gofastr/framework/access"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	frameworkopenapi "github.com/DonaldMurillo/gofastr/framework/openapi"
	"github.com/DonaldMurillo/gofastr/framework/routegroup"
	"github.com/DonaldMurillo/gofastr/internal/clientip"
)

type sweepApp2SessionKeysHost struct {
	current  []byte
	previous [][]byte
}

func (h *sweepApp2SessionKeysHost) Mount(*router.Router) {}
func (h *sweepApp2SessionKeysHost) SetSessionKeys(current []byte, previous ...[]byte) {
	h.current, h.previous = current, previous
}

type sweepApp2SessionKeyHost struct{ key []byte }

func (h *sweepApp2SessionKeyHost) Mount(*router.Router)   {}
func (h *sweepApp2SessionKeyHost) SetSessionKey(k []byte) { h.key = k }

type sweepApp2PlainHost struct{}

func (sweepApp2PlainHost) Mount(*router.Router) {}

// Mount hands a session host the current key and every previous secret's
// key as verify-only, so a rotation does not sign everyone out.
func TestSweepApp2MountHandsPreviousSessionKeys(t *testing.T) {
	cur, prev := strings.Repeat("c", 32), strings.Repeat("p", 32)
	app := NewApp(WithSecretRotation(cur, prev))
	h := &sweepApp2SessionKeysHost{}
	app.Mount(h)
	if !bytes.Equal(h.current, deriveKey([]byte(cur), uihostSessionPurpose)) {
		t.Fatalf("current session key is not derived from the current secret")
	}
	if len(h.previous) != 1 || !bytes.Equal(h.previous[0], deriveKey([]byte(prev), uihostSessionPurpose)) {
		t.Fatalf("previous session keys = %d keys, want the one derived from the previous secret", len(h.previous))
	}

	old := &sweepApp2SessionKeyHost{}
	NewApp(WithSecret(cur)).Mount(old)
	if !bytes.Equal(old.key, deriveKey([]byte(cur), uihostSessionPurpose)) {
		t.Fatalf("a SetSessionKey host did not get the current key")
	}
}

// A host that holds no sessions is never asked about secrets, so a fanout
// app with no secret still mounts it.
func TestSweepApp2MountSkipsSecretsForHostWithoutSessions(t *testing.T) {
	app := NewApp(WithFanout(fanout.NewInProcess()))
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("mounting a host with no session seam panicked: %v", r)
		}
	}()
	app.Mount(sweepApp2PlainHost{})
}

// An embed grant minted before a secret rotation still verifies on a host
// mounted with the rotated secret.
func TestSweepApp2MountHandsPreviousEmbedKeys(t *testing.T) {
	oldSecret, newSecret := strings.Repeat("o", 32), strings.Repeat("n", 32)
	before := newTestEmbedHost(t)
	NewApp(WithSecret(oldSecret)).Mount(&embedMountable{host: before})
	nonce, err := before.MintNonce(context.Background(), "reports", "u-1", "https://acme.com", nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	res, err := before.Exchange(context.Background(), nonce, "https://acme.com")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}

	after := newTestEmbedHost(t)
	NewApp(WithSecretRotation(newSecret, oldSecret)).Mount(&embedMountable{host: after})
	if _, err := after.VerifyGrant(context.Background(), res.Grant); err != nil {
		t.Fatalf("a grant from the previous secret failed after rotation: %v", err)
	}
}

// Mountables returns a copy; writing to it does not change the App.
func TestSweepApp2MountablesReturnsCopy(t *testing.T) {
	app := NewApp()
	app.Mount(sweepApp2PlainHost{})
	got := app.Mountables()
	got[0] = nil
	if app.Mountables()[0] == nil {
		t.Fatal("writing to the returned slice changed the App's mountables")
	}
}

// The trusted-proxy list rides on every request context even without the
// default chain.
func TestSweepApp2TrustedProxiesOnContextWithoutDefaults(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware(), WithConfig(AppConfig{TrustedProxies: []string{"10.0.0.0/8"}}))
	var trusted bool
	app.Router().Get("/p", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		trusted = clientip.TrustedFrom(r.Context()).ContainsAddr("10.1.2.3:5000")
	}))
	app.Router().ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/p", nil))
	if !trusted {
		t.Fatal("the trusted-proxy list was not on the request context")
	}
}

// A grouped entity's operations carry the group's OpenAPI tag.
func TestSweepApp2GroupEntityOperationsCarryGroupTag(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware())
	v1 := app.Group("/api/v1", routegroup.WithOpenAPITag("v1"))
	app.GroupEntity(v1, "posts", entity.EntityConfig{Table: "posts",
		Fields: []schema.Field{{Name: "title", Type: schema.String}},
	})
	doc := frameworkopenapi.EntityOpenAPI(app.Registry, "Test", "1.0.0", nil).Build()
	paths, _ := doc["paths"].(map[string]map[string]any)
	op, _ := paths["/api/v1/posts"]["get"].(map[string]any)
	tags, _ := op["tags"].([]string)
	if len(tags) != 1 || tags[0] != "v1" {
		t.Fatalf("GET /api/v1/posts tags = %v, want [v1]", op["tags"])
	}
}

// NewApp's own logger does not write through the slog.Default installed
// when it ran.
func TestSweepApp2SeededLoggerIgnoresSlogDefault(t *testing.T) {
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	var global bytes.Buffer
	slog.SetDefault(slog.New(slog.NewTextHandler(&global, nil)))
	NewApp().Logger().Info("sweep-app2-probe")
	if strings.Contains(global.String(), "sweep-app2-probe") {
		t.Fatalf("the App's seeded logger wrote through slog.Default: %q", global.String())
	}
}

// Web Bot Auth verification is installed without the default chain too.
func TestSweepApp2WebBotAuthVerifiesWithoutDefaults(t *testing.T) {
	app := NewApp(WithoutDefaultMiddleware(), WithWebBotAuth(WebBotAuthConfig{
		Keys:   []map[string]any{{"kty": "OKP"}},
		Verify: &WebBotAuthVerifyConfig{Require: true},
	}))
	app.Router().Get("/probe", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	app.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/probe", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unsigned request without the default chain: status %d, want 403", rec.Code)
	}
}

// WithOutbox without WithDB is refused by name.
func TestSweepApp2OutboxWithoutDBPanicsByName(t *testing.T) {
	defer func() {
		msg, _ := recover().(string)
		if !strings.Contains(msg, "WithOutbox requires WithDB") {
			t.Fatalf("panic = %q, want the WithOutbox requires WithDB refusal", msg)
		}
	}()
	NewApp(WithOutbox())
}

// The request timeout answers in the caller's locale: negotiation runs
// outside it.
func TestSweepApp2TimeoutAnswersInLocale(t *testing.T) {
	cat := i18n.NewMapCatalog()
	cat.Set("fr", "error.gateway_timeout", i18n.Message{Text: "délai dépassé"})
	a := NewApp(WithI18n(i18n.NewTranslator(cat, "en")), WithConfig(AppConfig{RequestTimeout: 10 * time.Millisecond}))
	a.Router().Get("/slow", http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	req.Header.Set("Accept-Language", "fr")
	rr := httptest.NewRecorder()
	a.Router().ServeHTTP(rr, req)
	if rr.Code != http.StatusGatewayTimeout || strings.TrimSpace(rr.Body.String()) != "délai dépassé" {
		t.Fatalf("got %d %q, want 504 in French", rr.Code, rr.Body.String())
	}
}

// Options run after the dotenv load, so an option reading the
// environment sees a value set only in .env.
func TestSweepApp2OptionsSeeDotEnv(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	const k = "GOFASTR_SWEEP_APP2_DOTENV"
	t.Setenv(k, "")
	os.Unsetenv(k)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte(k+"=from-file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var seen string
	NewApp(func(*App) { seen = os.Getenv(k) })
	if seen != "from-file" {
		t.Fatalf("option saw %s=%q, want the .env value", k, seen)
	}
}

// The default idempotency Scope gives one namespace per set of held
// scopes: order and repeats do not matter, and names cannot run together.
func TestSweepApp2HeldScopeKeyIsASet(t *testing.T) {
	key := func(held ...string) string {
		r := httptest.NewRequest(http.MethodPost, "/", nil)
		return heldScopeKey(r.WithContext(access.WithHeldScopes(r.Context(), held)))
	}
	if key("b", "a", "a") != key("a", "b") {
		t.Fatal("repeated or reordered scopes got a different namespace")
	}
	if key("ab") == key("a", "b") {
		t.Fatal(`"ab" and "a","b" share a namespace`)
	}
	if heldScopeKey(httptest.NewRequest(http.MethodPost, "/", nil)) != "" {
		t.Fatal("an unscoped request got a scoped namespace")
	}
}
