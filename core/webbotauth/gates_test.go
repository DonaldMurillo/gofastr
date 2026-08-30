package webbotauth

// gates_test.go: Gate B (independent implementation cross-check via
// Node's WebCrypto) and Gate C (mutation proofs for each verification
// guard). The artifacts under testdata/gateb-* were produced by the
// flow in this file:
//
//	WBA_GATEB_DUMP=1 go test -run TestGateB_DumpExchange ./core/webbotauth/
//	node testdata/gateb_exchange.mjs
//
// The exchange file carries the base bytes Go assembled; the committed
// node-crosscheck.json carries a signature Node produced over those
// bytes with its own generated key. TestGateB_NodeSignedBase re-derives
// the base in Go and verifies Node's signature with Go's Ed25519: the
// two implementations agree on the exact signed bytes.

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// gateBSignatureInput exercises the assembly corners: derived
// components (@method/@authority/@path/@query), @query-param encoding,
// field OWS canonicalization, and a dictionary member selection.
const gateBSignatureInput = `sig1=("@method" "@authority" "@path" "@query" "@query-param";name="q" "date" "x-ows-header" "signature-agent";key="sig1");created=1735689600;keyid="gate-b-key";tag="web-bot-auth"`

func gateBRequest() *http.Request {
	r := httptest.NewRequest("POST", "/search?q=web+bot&lang=en", nil)
	r.Host = "api.example.com"
	r.Header.Set("Date", "Tue, 20 Apr 2021 02:07:55 GMT")
	r.Header.Set("X-Ows-Header", "   value with outer spaces   ")
	r.Header.Set("Signature-Agent", `sig1="https://agent.example"`)
	r.Header.Set("Signature-Input", gateBSignatureInput)
	return r
}

func gateBBase(t *testing.T) string {
	t.Helper()
	dict, err := parseSFDictionary(gateBSignatureInput)
	if err != nil {
		t.Fatal(err)
	}
	m := dict.get("sig1")
	base, err := buildSignatureBase(newRequestCtx(gateBRequest()), m.list.items, m.list.params)
	if err != nil {
		t.Fatal(err)
	}
	return base
}

// TestGateB_BaseShape pins the assembled bytes so any drift in
// canonicalization (whitespace, param order, query-param encoding)
// fails here before it reaches the cross-check.
func TestGateB_BaseShape(t *testing.T) {
	want := "\"@method\": POST\n" +
		"\"@authority\": api.example.com\n" +
		"\"@path\": /search\n" +
		"\"@query\": ?q=web+bot&lang=en\n" +
		"\"@query-param\";name=\"q\": web%20bot\n" +
		"\"date\": Tue, 20 Apr 2021 02:07:55 GMT\n" +
		"\"x-ows-header\": value with outer spaces\n" +
		"\"signature-agent\";key=\"sig1\": \"https://agent.example\"\n" +
		"\"@signature-params\": (\"@method\" \"@authority\" \"@path\" \"@query\" \"@query-param\";name=\"q\" \"date\" \"x-ows-header\" \"signature-agent\";key=\"sig1\");created=1735689600;keyid=\"gate-b-key\";tag=\"web-bot-auth\""
	if got := gateBBase(t); got != want {
		t.Errorf("base mismatch:\n got: %q\nwant: %q", got, want)
	}
}

// TestGateB_DumpExchange regenerates testdata/gateb-exchange.json (a
// Go-assembled base plus a Go signature over it). Env-gated: it is a
// generator, not a test.
func TestGateB_DumpExchange(t *testing.T) {
	if os.Getenv("WBA_GATEB_DUMP") == "" {
		t.Skip("generator; set WBA_GATEB_DUMP=1")
	}
	base := gateBBase(t)
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sig := ed25519.Sign(priv, []byte(base))
	out := map[string]string{
		"base":            base,
		"pub_x_b64url":    base64.RawURLEncoding.EncodeToString(pub),
		"sig_b64":         base64.StdEncoding.EncodeToString(sig),
		"signature_input": gateBSignatureInput,
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	if err := os.WriteFile("testdata/gateb-exchange.json", append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Log("wrote testdata/gateb-exchange.json")
}

// TestGateB_NodeSignedBase verifies, in Go, the signature Node's
// WebCrypto produced over the Go-assembled base.
func TestGateB_NodeSignedBase(t *testing.T) {
	b, err := os.ReadFile("testdata/node-crosscheck.json")
	if err != nil {
		t.Fatalf("node cross-check artifact missing (run testdata/gateb_exchange.mjs): %v", err)
	}
	var art struct {
		Base   string `json:"base"`
		PubX   string `json:"pub_x_b64url"`
		SigB64 string `json:"sig_b64"`
		Note   string `json:"note"`
	}
	if err := json.Unmarshal(b, &art); err != nil {
		t.Fatal(err)
	}
	if art.Base != gateBBase(t) {
		t.Fatalf("Go no longer assembles the base Node signed:\n go: %q\nnode: %q", gateBBase(t), art.Base)
	}
	pub := mustDecodeB64URL(t, art.PubX)
	sig, err := base64.StdEncoding.DecodeString(art.SigB64)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(art.Base), sig) {
		t.Error("Node's signature over the Go-assembled base failed Go verification")
	}
	t.Logf("node artifact: %s", art.Note)
}

// ── Gate C: mutation proofs ─────────────────────────────────────────

// gateBVerifiedVector reuses the E.2.1 vector end to end.
func gateE21Verify(t *testing.T, mutate func(r *http.Request), now time.Time) Result {
	t.Helper()
	v := loadVector[wbaVector](t, "testdata/wba-e.2.1-ed25519-dictionary.json")
	res, _ := directoryServer(t, v.DirectoryHost, v.DirectoryBody)
	ver := testVerifier(res, now, false)
	r := v.Request.toHTTPRequest(t)
	r.Header.Set("Signature-Input", v.SignatureInput)
	r.Header.Set("Signature", v.SignatureLabel+"=:"+v.Signature+":")
	if mutate != nil {
		mutate(r)
	}
	return ver.VerifyRequest(r)
}

func TestGateC_FlippedSignedByte(t *testing.T) {
	// Mutation: the covered @authority changes after signing. The
	// rebuilt signature base differs from the signed base, so the
	// signature must fail.
	res := gateE21Verify(t, func(r *http.Request) { r.Host = "attacker.example.com" }, time.Now())
	if res.Outcome != OutcomeInvalid {
		t.Fatalf("mutated authority: outcome %s (%s), want invalid", res.Outcome, res.Reason)
	}
	if res.Reason != "signature verification failed" {
		t.Errorf("reason = %q", res.Reason)
	}
}

func TestGateC_ExpiredSignature(t *testing.T) {
	// Mutation: the verification clock sits past expires=4889289600.
	res := gateE21Verify(t, nil, time.Unix(4889289601, 0))
	if res.Outcome != OutcomeInvalid {
		t.Fatalf("expired signature: outcome %s (%s), want invalid", res.Outcome, res.Reason)
	}
	if res.Reason == "" || res.Reason[:3] != "sig" {
		t.Errorf("reason = %q, want an expiry message", res.Reason)
	}
}

func TestGateC_WrongKey(t *testing.T) {
	// Mutation: the directory serves a different key than keyid names,
	// so nothing verifies the signature and attribution is refused.
	v := loadVector[wbaVector](t, "testdata/wba-e.2.1-ed25519-dictionary.json")
	wrongKey := `{"keys":[{"kty":"OKP","crv":"Ed25519","x":"nAbx7kkSTy1igV4dw4JYVxmU0RgsYy7cYK6WVz0IP2Q","use":"sig"}]}`
	res, _ := directoryServer(t, v.DirectoryHost, wrongKey)
	ver := testVerifier(res, time.Now(), false)
	r := v.Request.toHTTPRequest(t)
	r.Header.Set("Signature-Input", v.SignatureInput)
	r.Header.Set("Signature", v.SignatureLabel+"=:"+v.Signature+":")
	got := ver.VerifyRequest(r)
	if got.Outcome != OutcomeUnverified {
		t.Fatalf("unknown key: outcome %s (%s), want unverified", got.Outcome, got.Reason)
	}
	if got.Agent != nil {
		t.Error("an unverified request must not carry an agent identity")
	}
}

func TestGateC_MissingCoveredComponent(t *testing.T) {
	// Mutation: the signature does not cover the Signature-Agent
	// member. The profile rejects it before any key is fetched.
	v := loadVector[wbaVector](t, "testdata/wba-e.2.1-ed25519-dictionary.json")
	noAgent := `sig2=("@authority");created=1735689600;keyid="poqkLGiymh_W0uP6PZFw-dvez3QJT5SolqXBCW38r0U";alg="ed25519";expires=4889289600;tag="web-bot-auth"`
	res, _ := directoryServer(t, v.DirectoryHost, v.DirectoryBody)
	ver := testVerifier(res, time.Now(), false)
	r := v.Request.toHTTPRequest(t)
	r.Header.Set("Signature-Input", noAgent)
	r.Header.Set("Signature", "sig2=:"+v.Signature+":")
	got := ver.VerifyRequest(r)
	if got.Outcome != OutcomeInvalid {
		t.Fatalf("uncovered signature-agent: outcome %s (%s), want invalid", got.Outcome, got.Reason)
	}
}

func TestGateC_CreatedInFuture(t *testing.T) {
	// Mutation: created is 30 days ahead of the verifier clock, past
	// the skew allowance.
	res := gateE21Verify(t, nil, time.Unix(1735689600-30*86400, 0))
	if res.Outcome != OutcomeInvalid {
		t.Fatalf("future-created signature: outcome %s (%s), want invalid", res.Outcome, res.Reason)
	}
}

func TestGateC_NoAuthorityCoverage(t *testing.T) {
	// Mutation: covered components exclude both @authority and
	// @target-uri, so the signature binds to nothing at this origin.
	v := loadVector[wbaVector](t, "testdata/wba-e.2.1-ed25519-dictionary.json")
	noAuthority := `sig2=("@method" "signature-agent";key="agent2");created=1735689600;keyid="poqkLGiymh_W0uP6PZFw-dvez3QJT5SolqXBCW38r0U";alg="ed25519";expires=4889289600;tag="web-bot-auth"`
	res, _ := directoryServer(t, v.DirectoryHost, v.DirectoryBody)
	ver := testVerifier(res, time.Unix(1735690000, 0), false)
	r := v.Request.toHTTPRequest(t)
	r.Header.Set("Signature-Input", noAuthority)
	r.Header.Set("Signature", "sig2=:"+v.Signature+":")
	got := ver.VerifyRequest(r)
	if got.Outcome != OutcomeInvalid {
		t.Fatalf("no authority coverage: outcome %s (%s), want invalid", got.Outcome, got.Reason)
	}
}
