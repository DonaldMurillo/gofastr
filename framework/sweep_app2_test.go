package framework

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/fanout"
	"github.com/DonaldMurillo/gofastr/core/i18n"
	"github.com/DonaldMurillo/gofastr/core/router"
	"github.com/DonaldMurillo/gofastr/core/schema"
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

// WithI18n lives in the default chain, so NewApp refuses it alongside
// WithoutDefaultMiddleware.
func TestSweepApp2I18nWithoutDefaultsPanics(t *testing.T) {
	defer func() {
		r := recover()
		msg, _ := r.(string)
		if !strings.Contains(msg, "WithI18n is incompatible with WithoutDefaultMiddleware") {
			t.Fatalf("panic = %v, want the WithI18n incompatibility", r)
		}
	}()
	NewApp(WithI18n(i18n.NewTranslator(i18n.NewMapCatalog(), "en")), WithoutDefaultMiddleware())
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
