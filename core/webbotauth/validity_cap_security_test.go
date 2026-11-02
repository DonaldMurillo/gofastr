package webbotauth

import (
	"testing"
	"time"
)

// A signature is a bearer credential for every request it covers until
// its own expires, so a captured one replays for as long as the signer
// chose. The verifier caps expires - created at DefaultMaxValidity. The
// draft's E.2.1 vector signs a 100-year window, so the default refuses
// it and a verifier configured past that window accepts it.
func TestSignatureValidityWindowCapped(t *testing.T) {
	v := loadVector[wbaVector](t, "testdata/wba-e.2.1-ed25519-dictionary.json")
	now := time.Unix(1735689600+30, 0) // 30 s after created, inside expires

	run := func(opts ...Option) Result {
		res, _ := directoryServer(t, v.DirectoryHost, v.DirectoryBody)
		ver := New(false, nil, opts...)
		ver.resolver = res
		ver.now = func() time.Time { return now }
		r := v.Request.toHTTPRequest(t)
		r.Header.Set("Signature-Input", v.SignatureInput)
		r.Header.Set("Signature", v.SignatureLabel+"=:"+v.Signature+":")
		return ver.VerifyRequest(r)
	}

	if got := run(); got.Outcome != OutcomeInvalid {
		t.Fatalf("100-year window under the default cap: outcome %s (%s), want invalid", got.Outcome, got.Reason)
	}
	if got := run(WithMaxValidity(150 * 365 * 24 * time.Hour)); got.Outcome != OutcomeVerified {
		t.Fatalf("window within a configured cap: outcome %s (%s), want verified", got.Outcome, got.Reason)
	}
}

func TestWithMaxValidityRefusesNegative(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("negative MaxValidity accepted")
		}
	}()
	New(false, nil, WithMaxValidity(-time.Second))
}
