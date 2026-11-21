package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/handler"
	"github.com/DonaldMurillo/gofastr/framework/owner"
)

type orderPlugin struct {
	name    string
	log     *[]string
	stopErr error
}

func (p *orderPlugin) Name() string            { return p.name }
func (p *orderPlugin) Init(*AuthManager) error { return nil }
func (p *orderPlugin) OnStop(context.Context) error {
	*p.log = append(*p.log, "stop:"+p.name)
	return p.stopErr
}
func (p *orderPlugin) Middleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*p.log = append(*p.log, "mw:"+p.name)
			next.ServeHTTP(w, r)
		})
	}
}

func TestOwnerExtractorReadsCurrentUser(t *testing.T) {
	ex := owner.GetExtractor()
	if ex == nil {
		t.Fatal("importing battery/auth installs no owner extractor")
	}
	if _, ok := ex(context.Background()); ok {
		t.Error("no user in context still yields an owner")
	}
	if _, ok := ex(handler.SetUser(context.Background(), &BasicUser{})); ok {
		t.Error("a user with an empty ID yields an owner")
	}
	id, ok := ex(handler.SetUser(context.Background(), &BasicUser{ID: "u1"}))
	if !ok || id != "u1" {
		t.Errorf("owner = %v, %v; want u1, true", id, ok)
	}
}

func TestZeroSessionTTLDefaultsToSevenDays(t *testing.T) {
	cfg := AuthConfig{}
	cfg.defaults()
	if cfg.SessionTTL != 7*24*time.Hour {
		t.Errorf("SessionTTL = %v, want 168h", cfg.SessionTTL)
	}
	if cfg.BasePath != "/auth" {
		t.Errorf("BasePath = %q, want /auth", cfg.BasePath)
	}
}

func TestDefaultThrottlesInstalledInDevModeToo(t *testing.T) {
	for _, dev := range []bool{false, true} {
		cfg := AuthConfig{DevMode: dev}
		cfg.defaults()
		want := map[string]RateLimiterConfig{
			"login":       {MaxAttempts: 30, Window: time.Minute, BlockDuration: 5 * time.Minute},
			"account":     {MaxAttempts: 10, Window: time.Minute, BlockDuration: 15 * time.Minute},
			"register":    {MaxAttempts: 10, Window: time.Minute, BlockDuration: 15 * time.Minute},
			"mailAddress": {MaxAttempts: 1, Window: 2 * time.Minute, BlockDuration: 2 * time.Minute},
		}
		got := map[string]*RateLimiterConfig{
			"login": cfg.LoginRateLimit, "account": cfg.LoginRateLimitPerAccount,
			"register": cfg.RegisterRateLimit, "mailAddress": cfg.MailAddressRateLimit,
		}
		for k, w := range want {
			if got[k] == nil || !reflect.DeepEqual(*got[k], w) {
				t.Errorf("DevMode=%v %s limit = %+v, want %+v", dev, k, got[k], w)
			}
		}
	}
}

func TestDefaultRolesReturnsACopy(t *testing.T) {
	mgr := New(AuthConfig{JWTSecret: "s", DefaultRoles: []string{"member"}})
	r := mgr.DefaultRoles()
	r[0] = "admin"
	if got := mgr.DefaultRoles(); got[0] != "member" {
		t.Errorf("mutating DefaultRoles' result changed the config: %v", got)
	}
	if got := New(AuthConfig{JWTSecret: "s"}).DefaultRoles(); !reflect.DeepEqual(got, []string{"user"}) {
		t.Errorf("empty DefaultRoles = %v, want [user]", got)
	}
}

type acceptAnyRolesStore struct{ *memoryUserStore }

func (acceptAnyRolesStore) UpdateRoles(context.Context, string, []string) error { return nil }

type swapStorePlugin struct{ store UserStore }

func (p *swapStorePlugin) Name() string { return "swap" }
func (p *swapStorePlugin) Init(m *AuthManager) error {
	m.SetUserStore(p.store)
	return nil
}

func TestJWTResolvesAgainstStoreAPluginInstalled(t *testing.T) {
	first, second := newMemoryUserStore(), newMemoryUserStore()
	u := seedUser(t, first, "a@example.com", "supersecret1")
	mgr := New(AuthConfig{JWTSecret: "s", DevMode: true, UserStore: first})
	mgr.Use(&swapStorePlugin{store: second})
	if err := mgr.Init(nil); err != nil {
		t.Fatal(err)
	}
	j := NewJWTAuth("s", time.Hour)
	j.SetUserStore(first)
	tok, err := j.GenerateToken(u)
	if err != nil {
		t.Fatal(err)
	}
	h := RequireAuth(mgr.JWT())(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("a token whose owner exists only in the replaced store = %d, want 401", rec.Code)
	}
}

func TestSetUserRolesRefusesUnstorableID(t *testing.T) {
	store := acceptAnyRolesStore{newMemoryUserStore()}
	sink := &capturingSink{}
	mgr := New(AuthConfig{JWTSecret: "s", UserStore: store, AuditSink: sink, AllowInMemoryStores: true})
	if err := mgr.SetUserRoles(context.Background(), "u\x00", []string{"admin"}); !errors.Is(err, ErrUserNotFound) {
		t.Errorf("SetUserRoles on an unstorable id = %v, want ErrUserNotFound", err)
	}
	if sinkHasEvent(sink, "roles.updated") {
		t.Error("a refused SetUserRoles emitted roles.updated")
	}
}

func TestPluginMiddlewareFirstRegisteredOutermostAndStopReversed(t *testing.T) {
	var log []string
	boom := errors.New("boom")
	mgr := New(AuthConfig{JWTSecret: "s"})
	mgr.Use(&orderPlugin{name: "a", log: &log, stopErr: boom})
	mgr.Use(&orderPlugin{name: "b", log: &log, stopErr: errors.New("second")})
	h := mgr.Middleware()(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	err := mgr.OnStop(context.Background())
	want := []string{"mw:a", "mw:b", "stop:b", "stop:a"}
	if !reflect.DeepEqual(log, want) {
		t.Errorf("order = %v, want %v", log, want)
	}
	if err == nil || errors.Is(err, boom) {
		t.Errorf("OnStop error = %v, want the first failure in stop order (plugin b)", err)
	}
}
