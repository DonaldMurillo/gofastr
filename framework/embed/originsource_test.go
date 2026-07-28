package embed

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// stubOriginSource is a table-backed OriginSource for tests: it returns the
// origins registered under a customer id, or notOK (a stored error) to stand
// in for a failing lookup.
type stubOriginSource struct {
	byCustomer map[string][]string
	notOK      error // returned for customer "fails"
	calls      int
}

func (s *stubOriginSource) Origins(_ context.Context, _, customer string) ([]string, error) {
	s.calls++
	if customer == "fails" {
		return nil, s.notOK
	}
	if os, ok := s.byCustomer[customer]; ok {
		return append([]string(nil), os...), nil
	}
	return nil, nil // unknown customer: no origins
}

func TestResolveCustomerOriginsNormalizesAndDeduplicates(t *testing.T) {
	src := &stubOriginSource{byCustomer: map[string][]string{
		"acme": {"https://acme.com/", "https://ACME.com:443", "https://shop.acme.com"},
	}}
	got, err := ResolveCustomerOrigins(context.Background(), src, "reports", "acme")
	if err != nil {
		t.Fatalf("ResolveCustomerOrigins: %v", err)
	}
	// Trailing slash, case, default port all collapse to one origin; the
	// distinct subdomain survives. A source that returned the raw strings
	// verbatim would put four-byte-different entries in the directive.
	want := []string{"https://acme.com", "https://shop.acme.com"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("got %v, want normalized %v", got, want)
	}
}

// A store is not a trusted input. A wildcard, a userinfo URL, or a path that
// NormalizeOrigin rejects on a boot-time list must be rejected here too — the
// source backing this is the app's own table, and a row that slips in should
// not widen framing past an exact origin.
func TestResolveCustomerOriginsRejectsUntrustedInput(t *testing.T) {
	for _, bad := range []string{"*", "https://*.acme.com", "https://evil.com/path", "https://user:pw@acme.com", "not-a-url"} {
		src := &stubOriginSource{byCustomer: map[string][]string{"acme": {bad}}}
		if _, err := ResolveCustomerOrigins(context.Background(), src, "reports", "acme"); err == nil {
			t.Errorf("source origin %q must be rejected by NormalizeOrigin, not served", bad)
		}
	}
}

// An unknown customer returns no origins. That must fail CLOSED — never an
// empty frame-ancestors directive a browser could read as permissive, and
// never a fallback to "allow everyone".
func TestResolveCustomerOriginsFailsClosedOnUnknownCustomer(t *testing.T) {
	src := &stubOriginSource{byCustomer: map[string][]string{"acme": {"https://acme.com"}}}
	if _, err := ResolveCustomerOrigins(context.Background(), src, "reports", "ghost"); err == nil {
		t.Fatal("an unknown customer must fail closed, not return an empty allowlist")
	}
}

// An app that opts into a source commits to sending a customer id. A request
// without one is a misconfigured snippet, and the safe answer is no framing.
func TestResolveCustomerOriginsFailsClosedOnEmptyCustomerID(t *testing.T) {
	src := &stubOriginSource{byCustomer: map[string][]string{"acme": {"https://acme.com"}}}
	if _, err := ResolveCustomerOrigins(context.Background(), src, "reports", ""); err == nil {
		t.Fatal("an empty customer id must fail closed, never widen to allow everyone")
	}
}

// A source that errors must fail closed. A broken lookup must not fall back to
// "allow" — that would turn a DB outage into every surface framed by anyone.
func TestResolveCustomerOriginsFailsClosedOnSourceError(t *testing.T) {
	src := &stubOriginSource{
		byCustomer: map[string][]string{"acme": {"https://acme.com"}},
		notOK:      errors.New("db down"),
	}
	if _, err := ResolveCustomerOrigins(context.Background(), src, "reports", "fails"); err == nil {
		t.Fatal("a source error must fail closed, not fall back to allow")
	}
}

// The customer id is attacker-chosen on an unauthenticated route. An unbounded
// id would ride into the app's lookup at request-line length; bound it and
// fail closed past the bound.
func TestResolveCustomerOriginsFailsClosedOnOverlongCustomerID(t *testing.T) {
	src := &stubOriginSource{byCustomer: map[string][]string{"acme": {"https://acme.com"}}}
	huge := strings.Repeat("a", maxCustomerIDBytes+1)
	if _, err := ResolveCustomerOrigins(context.Background(), src, "reports", huge); err == nil {
		t.Fatal("an over-long customer id must fail closed")
	}
}

// The per-customer list is bounded at RESPONSE time, not boot time. A single
// customer whose origins would overflow the frame-ancestors directive fails
// closed for THAT customer only — strictly better than the boot-time refusal
// that broke every customer at once.
func TestResolveCustomerOriginsCapsPerResponse(t *testing.T) {
	many := make([]string, 0, 300)
	for i := range 300 {
		many = append(many, fmt.Sprintf("https://c-%03d.example.com", i))
	}
	src := &stubOriginSource{byCustomer: map[string][]string{"big": many}}
	_, err := ResolveCustomerOrigins(context.Background(), src, "reports", "big")
	if err == nil {
		t.Fatal("a customer over the per-response cap must fail closed")
	}
	if !strings.Contains(err.Error(), "frame-ancestors") {
		t.Fatalf("cap error must name frame-ancestors so the operator can act: %q", err)
	}
}

func TestResolveCustomerOriginsRejectsNilSource(t *testing.T) {
	if _, err := ResolveCustomerOrigins(context.Background(), nil, "reports", "acme"); err == nil {
		t.Fatal("a nil source must error, not return an allowlist")
	}
}

// Allows answers the grant path's question: is this origin allowed for ANY
// customer of this surface. Scanning every customer is fine in a fixture; a
// real source indexes it.
func (s *stubOriginSource) Allows(_ context.Context, _, origin string) (bool, error) {
	for _, list := range s.byCustomer {
		for _, o := range list {
			if o == origin {
				return true, nil
			}
		}
	}
	return false, nil
}
