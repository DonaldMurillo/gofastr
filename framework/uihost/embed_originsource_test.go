package uihost

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	fembed "github.com/DonaldMurillo/gofastr/framework/embed"
)

// perCustomerSource is a table-backed OriginSource for the shell tests: it
// returns the origins registered under a customer id, standing in for the
// app's own table. "fails" is the customer id whose lookup errors, to exercise
// the fail-closed path.
type perCustomerSource struct {
	byCustomer map[string][]string
	fails      string
	err        error
	calls      atomic.Int32
}

func (s *perCustomerSource) Origins(_ context.Context, _, customer string) ([]string, error) {
	s.calls.Add(1)
	if s.err != nil && customer == s.fails {
		return nil, s.err
	}
	if os, ok := s.byCustomer[customer]; ok {
		return append([]string(nil), os...), nil
	}
	return nil, nil // unknown customer: no origins
}

// originSourceFixture wires a source into the reports surface. The STATIC
// Origins list is the universe the grant path binds (every origin any customer
// may be minted for); the source narrows the SHELL to one customer's subset.
// Keeping both is what makes the feature additive: an origin not in the static
// list cannot obtain a grant even when the source would name it.
func originSourceFixture(t *testing.T, src fembed.OriginSource) embedFixture {
	t.Helper()
	return newEmbedFixture(t, func(cfg *fembed.Config) {
		cfg.Surfaces[0].Origins = []string{
			"https://acme.com", "https://shop.acme.com", "https://globex.com",
		}
		cfg.OriginSource = src
	})
}

// With an OriginSource configured, the shell serves ONLY the customer named in
// the request, not the whole allowlist. That is the per-customer model: each
// response leaks one customer's origins instead of every customer's.
func TestEmbedShellServesPerCustomerOrigins(t *testing.T) {
	src := &perCustomerSource{byCustomer: map[string][]string{
		"acme":   {"https://acme.com", "https://shop.acme.com"},
		"globex": {"https://globex.com"},
	}}
	f := originSourceFixture(t, src)

	for _, tc := range []struct {
		customer    string
		want        []string
		mustNotHave string
	}{
		{"acme", []string{"https://acme.com", "https://shop.acme.com"}, "https://globex.com"},
		{"globex", []string{"https://globex.com"}, "https://acme.com"},
	} {
		rec := f.do(t, "GET", "/__gofastr/embed/reports?customer="+tc.customer, "")
		if rec.Code != 200 {
			t.Fatalf("customer %s: status %d: %s", tc.customer, rec.Code, rec.Body)
		}
		ancestors := frameAncestorsOf(t, rec.Header().Get("Content-Security-Policy"))
		for _, o := range tc.want {
			if !strings.Contains(ancestors, o) {
				t.Errorf("customer %s: frame-ancestors %q omits %q", tc.customer, ancestors, o)
			}
		}
		if strings.Contains(ancestors, tc.mustNotHave) {
			t.Errorf("customer %s: frame-ancestors %q leaked another customer's origin %q", tc.customer, ancestors, tc.mustNotHave)
		}
		if strings.Contains(ancestors, "'none'") || strings.Contains(ancestors, "*") {
			t.Errorf("customer %s: frame-ancestors must name exact origins, got %q", tc.customer, ancestors)
		}
	}
}

// The enumerability trade-off, pinned exactly: a caller passing ANOTHER
// customer's id receives THAT customer's frame-ancestors (a smaller leak than
// today's whole-list-on-every-response), and STILL cannot obtain a usable
// grant for the surface from an origin that is not allowed. The per-customer
// shell leak grants no framing: the browser enforces against the real ancestor
// chain, and a grant stays bound to the origin it was minted for, which the
// static grant path refuses to widen.
func TestEmbedShellPerCustomerLeakGrantsNoFraming(t *testing.T) {
	src := &perCustomerSource{byCustomer: map[string][]string{
		"acme":   {"https://acme.com"},
		"globex": {"https://globex.com"},
	}}
	f := originSourceFixture(t, src)

	// An attacker who is customer "acme" requests globex's shell by id. They
	// learn globex's origin (the accepted, smaller leak) ...
	rec := f.do(t, "GET", "/__gofastr/embed/reports?customer=globex", "")
	ancestors := frameAncestorsOf(t, rec.Header().Get("Content-Security-Policy"))
	if !strings.Contains(ancestors, "https://globex.com") {
		t.Fatalf("requesting globex's shell must leak globex's origin: %q", ancestors)
	}
	if strings.Contains(ancestors, "https://acme.com") {
		t.Fatalf("globex's shell must NOT leak acme's origin (the whole list is the old bug): %q", ancestors)
	}

	// ... but they cannot turn that leak into a grant for an origin the surface
	// does not allow. evil.com is in no customer's set and not in the static
	// universe, so the grant path refuses to mint for it. The shell leak and
	// the grant binding are independent controls.
	if _, err := f.embed.MintNonce(context.Background(), "reports", "attacker", "https://evil.com", nil); err == nil {
		t.Fatal("minting for an origin not allowed by the surface succeeded — the per-customer shell leak must not widen grant authority")
	}
}

// An unknown customer id must fail CLOSED: frame-ancestors 'none', never a
// wildcard, never an empty directive a browser could read as permissive.
func TestEmbedShellFailsClosedOnUnknownCustomer(t *testing.T) {
	src := &perCustomerSource{byCustomer: map[string][]string{"acme": {"https://acme.com"}}}
	f := originSourceFixture(t, src)

	rec := f.do(t, "GET", "/__gofastr/embed/reports?customer=ghost", "")
	ancestors := frameAncestorsOf(t, rec.Header().Get("Content-Security-Policy"))
	if ancestors != "frame-ancestors 'none'" {
		t.Fatalf("unknown customer must fail closed to frame-ancestors 'none', got %q", ancestors)
	}
}

// An app that opts into a source commits to sending a customer id on every
// shell request. An empty value, and an absent one, are both a misconfigured
// snippet, and the safe answer in both is no framing.
func TestEmbedShellFailsClosedOnEmptyOrAbsentCustomerID(t *testing.T) {
	src := &perCustomerSource{byCustomer: map[string][]string{"acme": {"https://acme.com"}}}
	f := originSourceFixture(t, src)

	for _, path := range []string{
		"/__gofastr/embed/reports?customer=", // present but empty
		"/__gofastr/embed/reports",           // absent entirely
	} {
		rec := f.do(t, "GET", path, "")
		ancestors := frameAncestorsOf(t, rec.Header().Get("Content-Security-Policy"))
		if ancestors != "frame-ancestors 'none'" {
			t.Errorf("%s: must fail closed to frame-ancestors 'none', got %q", path, ancestors)
		}
	}
}

// A source that errors must fail closed. A broken lookup must never fall back
// to "allow". That would turn a DB outage into every surface framed by anyone.
func TestEmbedShellFailsClosedOnSourceError(t *testing.T) {
	src := &perCustomerSource{
		byCustomer: map[string][]string{"acme": {"https://acme.com"}},
		fails:      "acme",
		err:        errors.New("db down"),
	}
	f := originSourceFixture(t, src)

	rec := f.do(t, "GET", "/__gofastr/embed/reports?customer=acme", "")
	ancestors := frameAncestorsOf(t, rec.Header().Get("Content-Security-Policy"))
	if ancestors != "frame-ancestors 'none'" {
		t.Fatalf("a source error must fail closed to frame-ancestors 'none', got %q", ancestors)
	}
}

// An app that never configures a source must behave exactly as today: the shell
// serves the static allowlist regardless of any customer parameter. The source
// is purely opt-in; a stray ?customer= on a static-only app changes nothing.
func TestEmbedShellStaticOriginsWhenNoSource(t *testing.T) {
	f := newEmbedFixture(t) // no OriginSource

	rec := f.do(t, "GET", "/__gofastr/embed/reports?customer=acme", "")
	ancestors := frameAncestorsOf(t, rec.Header().Get("Content-Security-Policy"))
	for _, o := range []string{embedTestOrigin, embedTestOrigin2} {
		if !strings.Contains(ancestors, o) {
			t.Errorf("without a source the static allowlist must be served: %q omits %q", ancestors, o)
		}
	}
}

// Allows answers the grant path's question: allowed for ANY customer.
func (s *perCustomerSource) Allows(_ context.Context, _, origin string) (bool, error) {
	if s.err != nil && s.fails == "*" {
		return false, s.err
	}
	for _, list := range s.byCustomer {
		if slices.Contains(list, origin) {
			return true, nil
		}
	}
	return false, nil
}
