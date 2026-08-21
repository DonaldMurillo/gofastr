package embed

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// liveSource is a source an operator mutates at runtime, the way a settings
// page writes a row.
type liveSource struct {
	origins map[string]bool
	err     error
}

func (s *liveSource) Origins(context.Context, string, string) ([]string, error) {
	out := make([]string, 0, len(s.origins))
	for o := range s.origins {
		out = append(out, o)
	}
	return out, nil
}

func (s *liveSource) Allows(_ context.Context, _, origin string) (bool, error) {
	if s.err != nil {
		return false, s.err
	}
	return s.origins[origin], nil
}

func sourceHost(t *testing.T, src OriginSource) *Host {
	t.Helper()
	h, err := New(Config{
		Surfaces: []Surface{{
			Name:    "reports",
			Screen:  testScreen{"/reports"},
			Origins: []string{"https://static.example"},
		}},
		BurnStore:    NewMemoryBurnStore(),
		OriginSource: src,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	h.SetKeys([]byte("nonce-key-nonce-key-nonce-key-32"), []byte("grant-key-grant-key-grant-key-32"))
	return h
}

// THE requirement: a customer types their domain into a settings page and the
// embed works. No deploy, no restart.
//
// Before this, the source narrowed the shell's frame-ancestors but the grant
// path still gated on the boot-time list, so the customer's domain appeared in
// the CSP header and then MintNonce refused it. The embed was framed and
// unauthenticated: strictly worse than not onboarding them at all.
func TestSourceOriginCanObtainAGrantWithoutADeploy(t *testing.T) {
	src := &liveSource{origins: map[string]bool{}}
	h := sourceHost(t, src)
	const newCustomer = "https://shop.acme.example"

	if _, err := h.MintNonce(context.Background(), "reports", "u-1", newCustomer, nil); err == nil {
		t.Fatal("an origin nobody has added should not mint")
	}

	// The operator adds the row. Nothing restarts.
	src.origins[newCustomer] = true

	nonce, err := h.MintNonce(context.Background(), "reports", "u-1", newCustomer, nil)
	if err != nil {
		t.Fatalf("MintNonce after adding the customer: %v — onboarding still needs a deploy", err)
	}
	res, err := h.Exchange(context.Background(), nonce, newCustomer)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if _, err := h.VerifyGrant(context.Background(), res.Grant); err != nil {
		t.Fatalf("VerifyGrant: %v", err)
	}
}

// Removing the row takes effect immediately, on the next request: it does not
// wait out the grant's TTL. Same property de-listing a static origin has.
func TestRemovingASourceOriginRevokesInFlightGrants(t *testing.T) {
	const customer = "https://shop.acme.example"
	src := &liveSource{origins: map[string]bool{customer: true}}
	h := sourceHost(t, src)

	nonce, err := h.MintNonce(context.Background(), "reports", "u-1", customer, nil)
	if err != nil {
		t.Fatalf("MintNonce: %v", err)
	}
	res, err := h.Exchange(context.Background(), nonce, customer)
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if _, err := h.VerifyGrant(context.Background(), res.Grant); err != nil {
		t.Fatalf("grant should verify while the customer is live: %v", err)
	}

	delete(src.origins, customer)

	if _, err := h.VerifyGrant(context.Background(), res.Grant); err == nil {
		t.Fatal("a grant kept verifying after its origin was removed from the source")
	}
}

// A source outage must not become an open framing policy.
func TestSourceErrorFailsClosedOnTheGrantPath(t *testing.T) {
	src := &liveSource{origins: map[string]bool{"https://shop.acme.example": true}, err: errors.New("store down")}
	h := sourceHost(t, src)

	_, err := h.MintNonce(context.Background(), "reports", "u-1", "https://shop.acme.example", nil)
	if err == nil {
		t.Fatal("MintNonce succeeded while the origin source was failing")
	}
	if !strings.Contains(err.Error(), "origin source") {
		t.Fatalf("error = %q, want it to name the source as the cause", err)
	}
}

// The static list still works on its own, and is checked WITHOUT consulting the
// source: an app that lists its origins at boot should not pay for a store
// lookup, and an app with no source at all must behave exactly as before.
func TestStaticOriginsBypassTheSource(t *testing.T) {
	src := &liveSource{origins: map[string]bool{}, err: errors.New("must not be consulted")}
	h := sourceHost(t, src)

	if _, err := h.MintNonce(context.Background(), "reports", "u-1", "https://static.example", nil); err != nil {
		t.Fatalf("a boot-listed origin consulted the source: %v", err)
	}
}

// A source is not a trusted input. Whatever it stores, the comparison happens
// on the canonical form, so a caller cannot slip through with a variant
// spelling of an allowed origin.
func TestSourceOriginsAreComparedCanonically(t *testing.T) {
	src := &liveSource{origins: map[string]bool{"https://shop.acme.example": true}}
	h := sourceHost(t, src)

	for _, spelling := range []string{
		"https://shop.acme.example",
		"https://shop.acme.example/",
		"https://SHOP.acme.example",
		"https://shop.acme.example:443",
	} {
		if _, err := h.MintNonce(context.Background(), "reports", "u-1", spelling, nil); err != nil {
			t.Fatalf("MintNonce(%q) = %v, want it to match the stored canonical origin", spelling, err)
		}
	}
	for _, bad := range []string{"https://evil.example", "http://shop.acme.example", "https://shop.acme.example:8443"} {
		if _, err := h.MintNonce(context.Background(), "reports", "u-1", bad, nil); err == nil {
			t.Fatalf("MintNonce(%q) succeeded — a different origin matched", bad)
		}
	}
}
