package framework

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/mcp"
	"github.com/DonaldMurillo/gofastr/core/moduleproto"
	"github.com/DonaldMurillo/gofastr/framework/access"
	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
	"github.com/DonaldMurillo/gofastr/framework/entity"
	"github.com/DonaldMurillo/gofastr/framework/event"
)

// panickingRegistry is a host entity registry whose Get panics.
type panickingRegistry struct{}

func (panickingRegistry) All() map[string]*entity.Entity { return nil }
func (panickingRegistry) AllSorted() []*entity.Entity    { return nil }
func (panickingRegistry) Get(string) (*entity.Entity, error) {
	panic("registry exploded")
}

// A host registry that panics on lookup denies the reverse call; the panic
// does not escape into the child's read loop.
func TestSweepPMHostRegistryPanicDenies(t *testing.T) {
	var hit atomic.Bool
	b := NewBroker(fakeSuccessRouter(&hit), panickingRegistry{}, nil, "")
	h := b.entityHandler(ModuleGrantView{Name: "m", Grants: []access.Permission{"notes:read"}}, opQuery)
	_, err := callReverse(t, h, moduleproto.EntityQueryParams{Entity: "notes"})
	wantDenied(t, err)
	wantNotHit(t, &hit)
}

// With the grant held, a search call with no search endpoint and an event
// emit with no event bus are denied, not answered empty.
func TestSweepPMHostUnconfiguredSurfacesDeny(t *testing.T) {
	b := NewBroker(nil, nil, nil, "")
	view := ModuleGrantView{Name: "m", Grants: []access.Permission{"search:query", "orders:emit"}}

	_, err := callReverse(t, b.searchHandler(view), moduleproto.SearchQueryParams{Query: "x"})
	wantDenied(t, err)
	if we := moduleproto.AsError(err); we == nil || !strings.Contains(we.Message, "no search surface") {
		t.Fatalf("search error = %v, want the no-search-surface denial", err)
	}

	_, err = callReverse(t, b.eventHandler(view), moduleproto.EventEmitParams{Topic: "orders"})
	wantDenied(t, err)
	if we := moduleproto.AsError(err); we == nil || !strings.Contains(we.Message, "no event bus") {
		t.Fatalf("emit error = %v, want the no-event-bus denial", err)
	}

	// Control: the same emit with a bus configured succeeds.
	ok := NewBroker(nil, nil, event.NewEventBus(), "")
	if _, err := callReverse(t, ok.eventHandler(view), moduleproto.EventEmitParams{Topic: "orders"}); err != nil {
		t.Fatalf("emit with a bus: %v", err)
	}
}

// A delegation minted for a module tool call carries neither the calling
// request's credential headers nor its roles, so the child's reverse call
// re-dispatches with no identity.
func TestSweepPMHostToolDelegationCarriesNoAuthority(t *testing.T) {
	var sawCookie, sawAuth string
	var sawRoles []string
	var hit atomic.Bool
	router := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit.Store(true)
		sawCookie = r.Header.Get("Cookie")
		sawAuth = r.Header.Get("Authorization")
		sawRoles = access.GetRoles(r.Context())
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"total":0}`))
	})
	b := NewBroker(router, brokerRegistry(brokerEntity("notes", "notes", nil)), nil, "")

	orig := httptest.NewRequest(http.MethodPost, "/mcp", nil)
	orig.Header.Set("Cookie", "sid=alice")
	orig.Header.Set("Authorization", "Bearer agent-token")
	ctx := mcp.WithRequest(context.Background(), orig)
	ctx = access.WithRoles(ctx, []string{"super"})
	ctx = access.WithPolicy(ctx, access.NewRolePolicy())

	handle, release := b.MintDelegation(requestFromToolCtx(withDelegationModule(ctx, "m")), 1)
	defer release()
	if handle == "" {
		t.Fatal("tool delegation minted no handle")
	}
	b.mu.Lock()
	creds := b.handles[handle].credentials
	b.mu.Unlock()
	if len(creds) != 0 {
		t.Fatalf("tool delegation stashed credentials %v, want none", creds)
	}

	h := b.entityHandler(ModuleGrantView{Name: "m", Grants: []access.Permission{"notes:read"}}, opQuery)
	_, err := callReverse(t, h, moduleproto.EntityQueryParams{
		Entity: "notes",
		Caller: moduleproto.CallerRef{Delegation: handle},
	})
	wantOK(t, err)
	if !hit.Load() {
		t.Fatal("reverse call never reached the router")
	}
	if sawCookie != "" || sawAuth != "" {
		t.Fatalf("re-dispatch carried Cookie=%q Authorization=%q, want none", sawCookie, sawAuth)
	}
	if slices.Contains(sawRoles, "super") {
		t.Fatalf("re-dispatch carried the agent's roles %v", sawRoles)
	}
}

// The proxy forwards only allowlisted request headers to the child: no
// credential, forwarding or connection header reaches module.http.
func TestSweepPMHostProxyNeverForwardsCredentials(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/items", nil)
	r.Header.Set("Accept", "application/json")
	r.Header.Set("Cookie", "sid=alice")
	r.Header.Set("Authorization", "Bearer secret")
	r.Header.Set("X-API-Key", "key")
	r.Header.Set(fembed.GrantHeader, "grant")
	r.Header.Set("X-Forwarded-For", "10.0.0.1")
	r.Header.Set("Proxy-Authorization", "Basic x")
	params, err := buildHTTPRequestParams(r, "demo", "list", "req-1", "", time.Second)
	if err != nil {
		t.Fatalf("buildHTTPRequestParams: %v", err)
	}
	if params.Headers["Accept"] != "application/json" {
		t.Fatalf("Accept not forwarded: %v", params.Headers)
	}
	for k := range params.Headers {
		if _, ok := moduleProxyAllowedHeaders[k]; !ok {
			t.Errorf("forwarded non-allowlisted header %q", k)
		}
	}
	for _, k := range []string{"Cookie", "Authorization", "X-Api-Key", "X-API-Key", fembed.GrantHeader, "X-Forwarded-For", "Proxy-Authorization"} {
		if v, ok := params.Headers[k]; ok {
			t.Errorf("forwarded %s=%q to the child", k, v)
		}
	}
}

// The probe child's environment is the default allowlist plus the probe's
// own context; any other host variable stays out.
func TestSweepPMHostProbeChildEnvOmitsHostEnv(t *testing.T) {
	t.Setenv("GOFASTR_SWEEP_HOST_SECRET", "do-not-leak")
	env := buildProbeChildEnv(probeSetup{ProbeIDEnv: "2", HostUID: "0", HostPID: "1", Env: []string{"GOFASTR_PROBE_CANARY_NAME=X"}})
	joined := strings.Join(env, "\n")
	if strings.Contains(joined, "GOFASTR_SWEEP_HOST_SECRET") || strings.Contains(joined, "do-not-leak") {
		t.Fatalf("probe child env carries a host variable:\n%s", joined)
	}
	for _, want := range []string{probeEnvName + "=2", "GOFASTR_PROBE_CANARY_NAME=X"} {
		if !slices.Contains(env, want) {
			t.Errorf("probe child env lacks %q", want)
		}
	}
	allow := DefaultChildEnvAllowlist()
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !strings.HasPrefix(name, "GOFASTR_PROBE_") && name != probeEnvName && !slices.Contains(allow, name) {
			t.Errorf("probe child env has %q, neither allowlisted nor probe context", name)
		}
	}
}
