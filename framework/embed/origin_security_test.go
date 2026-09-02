package embed

import (
	"testing"
)

// Property: origins a browser serializes differently never collapse into one
// allowlist entry — every confusable spelling of a DIFFERENT origin is
// rejected at the door (boot static list, runtime source list, and candidate
// lookup alike), so pinning can never be widened by spelling tricks. The
// punycode rule is the documented contract: internationalized domains must
// be supplied in xn-- form, which is what the browser sends anyway.
func TestOriginFamiliesNeverCrossMatch(t *testing.T) {
	// Entries that must be REFUSED wherever an origin is accepted.
	badEntries := []string{
		"https://bücher.example",   // unicode IDN: the browser sends punycode, this can never match
		"https://acme.com.",        // trailing-dot FQDN splits one origin into two
		"https://.acme.com",        // leading dot reads as a wildcard-ish name
		"https://acme..com",        // empty label
		"https://_unders.example",  // underscore is not a hostname character
		"https://[fe80::1%25eth0]", // IPv6 zone id is not an origin
		"https://*.acme.com",       // wildcard host, compared literally = worst of both readings
		"https://acme.com:0",       // port outside the valid range
		"https://acme.com:65536",
		"https://user:pw@acme.com", // userinfo is not part of an origin
		"https://acme.com/path",    // path, query and fragment are not origin bytes
		"https://acme.com/?q=1#f",
	}
	// Surfaces: the boot-time static set and the per-customer runtime set
	// go through the same normalization; both must refuse.
	for _, bad := range badEntries {
		if _, err := newOriginSet([]string{"https://acme.com", bad}); err == nil {
			t.Errorf("static allowlist accepted confusable entry %q", bad)
		}
		if _, err := buildCustomerOriginSet([]string{"https://acme.com", bad}); err == nil {
			t.Errorf("customer origin set accepted confusable entry %q", bad)
		}
	}

	// The punycode form of the same domain IS the entry the browser matches.
	set, err := newOriginSet([]string{"https://xn--80ak6aa92e.com"})
	if err != nil {
		t.Fatalf("punycode entry rejected: %v", err)
	}

	// Candidates: a browser-attested ancestor can arrive as the opaque
	// "null" string, as garbage, or in an equal-looking but different
	// family; none of those may match any entry.
	for _, cand := range []string{"null", "", "https://bücher.example", "https://xn--80ak6aa92e.com.evil.com"} {
		if set.Has(cand) {
			t.Errorf("candidate %q matched an allowlist it must not", cand)
		}
	}
	if !set.Has("https://XN--80AK6AA92E.COM") {
		t.Error("case-folded punycode spelling of the allowed origin must match")
	}
}

// Property: every spelling of ONE IPv6 origin collapses to exactly one
// canonical entry, on both sides of the comparison (entry and candidate), so
// the browser-attested ancestor always matches the entry the app minted for
// — and the v4-mapped family never widens into the unbracketed IPv4 family.
//
// FLAG (not asserted either way): Go's net.IP.String() renders a v4-mapped
// literal as a bracketed dotted quad ("[127.0.0.1]"), which is not a form
// browsers emit in an Origin header (they keep "[::ffff:127.0.0.1]") and not
// a valid CSP host-source grammar. Matching is self-consistent because BOTH
// sides normalize through NormalizeOrigin, so pinning holds; whether the
// frame-ancestors directive containing "[127.0.0.1]" is usable by a browser
// is a serialization question for a human to rule on, not this test.
func TestIPv6SpellingsCollapseToCanonical(t *testing.T) {
	// Two spellings of ::1 and two of ::ffff:127.0.0.1 must dedupe to two
	// entries, not four: enumeration surface stays minimal and no spelling
	// silently never matches.
	set, err := newOriginSet([]string{
		"https://[::1]",
		"https://[0:0:0:0:0:0:0:1]",
		"https://[::ffff:127.0.0.1]",
		"https://[::ffff:7f00:1]",
	})
	if err != nil {
		t.Fatalf("newOriginSet: %v", err)
	}
	if got := set.List(); len(got) != 2 {
		t.Fatalf("IPv6 spellings deduped to %d entries (%v), want 2", len(got), got)
	}

	// Every alternate spelling of each family matches, from the candidate
	// side, exactly like a browser-attested ancestor would send it.
	for _, cand := range []string{
		"https://[0:0:0:0:0:0:0:1]",
		"https://[::1]",
		"https://[::1]:443", // default port for the scheme folds away
		"https://[::FFFF:127.0.0.1]",
		"https://[::ffff:7f00:1]",
	} {
		if !set.Has(cand) {
			t.Errorf("candidate %q did not match its canonical IPv6 entry", cand)
		}
	}

	// The families do not widen into each other: a bracketed v4-mapped
	// entry must not admit the unbracketed IPv4 host a browser uses for
	// "https://127.0.0.1", and vice versa.
	if set.Has("https://127.0.0.1") {
		t.Error("unbracketed IPv4 candidate matched a v4-mapped entry")
	}
	v4only, err := newOriginSet([]string{"https://127.0.0.1"})
	if err != nil {
		t.Fatalf("newOriginSet(v4): %v", err)
	}
	if v4only.Has("https://[::ffff:127.0.0.1]") {
		t.Error("v4-mapped candidate matched an unbracketed IPv4 entry")
	}
}
