package webbotauth

// vectors_test.go: Gate A. The vectors are committed under testdata/ and
// each carries its source. Two layers are gated separately:
//
//   - RFC 9421 B.2.6 gates signature-base assembly + Ed25519 (it is not
//     a Web Bot Auth profile signature: no tag, no Signature-Agent).
//   - draft-meunier-webbotauth-httpsig-protocol-02 E.2.1/E.2.2 gate the
//     full profile: header parsing, coverage rules, directory fetch
//     over real TLS, key selection by thumbprint, and crypto.
//   - E.2.3 gates the response-side base builder (directory possession
//     proofs).
//
// The directory server runs on loopback; the fetch transport maps the
// vector's hostname onto it and trusts its test certificate. The SSRF
// posture of the production transport is tested separately in
// directory_test.go — the mapping here is environment, not code under
// test.

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// vectorRequest is the request half of a testdata vector.
type vectorRequest struct {
	Method  string            `json:"method"`
	Target  string            `json:"target"`
	Host    string            `json:"host"`
	Headers map[string]string `json:"headers"`
}

type rfcVector struct {
	Source         string        `json:"source"`
	Request        vectorRequest `json:"request"`
	SignatureInput string        `json:"signature_input"`
	SignatureLabel string        `json:"signature_label"`
	Signature      string        `json:"signature"`
	KeyJWK         struct {
		X string `json:"x"`
	} `json:"key_jwk"`
	ExpectedBase string `json:"expected_base"`
}

type wbaVector struct {
	Source           string        `json:"source"`
	VerificationTime string        `json:"verification_time"`
	Request          vectorRequest `json:"request"`
	SignatureInput   string        `json:"signature_input"`
	SignatureLabel   string        `json:"signature_label"`
	Signature        string        `json:"signature"`
	DirectoryBody    string        `json:"directory_body"`
	DirectoryHost    string        `json:"directory_host"`
	ExpectedAgentURL string        `json:"expected_agent_url"`
	ExpectedBase     string        `json:"expected_base"`
}

func loadVector[T any](t *testing.T, path string) *T {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return &v
}

func (vr *vectorRequest) toHTTPRequest(t *testing.T) *http.Request {
	t.Helper()
	r := httptest.NewRequest(vr.Method, vr.Target, nil)
	r.Host = vr.Host
	for k, v := range vr.Headers {
		r.Header.Set(k, v)
	}
	return r
}

// TestRFC9421_Ed25519Vector runs RFC 9421 Appendix B.2.6: the assembled
// signature base must match the published base byte for byte and the
// published signature must verify against test-key-ed25519.
func TestRFC9421_Ed25519Vector(t *testing.T) {
	v := loadVector[rfcVector](t, "testdata/rfc9421-b.2.6-ed25519.json")
	r := v.Request.toHTTPRequest(t)
	r.Header.Set("Signature-Input", v.SignatureInput)
	r.Header.Set("Signature", v.SignatureLabel+"=:"+v.Signature+":")

	dict, err := parseSFDictionary(v.SignatureInput)
	if err != nil {
		t.Fatalf("parse Signature-Input: %v", err)
	}
	m := dict.get(v.SignatureLabel)
	if m == nil || m.list == nil {
		t.Fatalf("no inner list for label %s", v.SignatureLabel)
	}
	base, err := buildSignatureBase(newRequestCtx(r), m.list.items, m.list.params)
	if err != nil {
		t.Fatalf("build base: %v", err)
	}
	if base != v.ExpectedBase {
		t.Errorf("signature base mismatch:\n got: %q\nwant: %q", base, v.ExpectedBase)
	}
	pub := mustDecodeB64URL(t, v.KeyJWK.X)
	sig, err := base64.StdEncoding.DecodeString(v.Signature)
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(base), sig) {
		t.Errorf("RFC 9421 B.2.6 signature did not verify")
	}
	t.Logf("vector source: %s", v.Source)
}

// directoryServer starts an HTTPS test server answering the well-known
// directory route with body, under a certificate valid for host, plus a
// fetch transport that maps every hostname onto it. The returned hit
// counter counts directory requests served.
func directoryServer(t *testing.T, host, body string) (*directoryResolver, *int32) {
	t.Helper()
	cert, pool := testCertificate(t, host)
	var hits int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Path != directoryWellKnown {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/http-message-signatures-directory+json")
		w.Header().Set("Cache-Control", "max-age=86400")
		fmt.Fprint(w, body)
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)

	tr := guardedTransport(true) // tests dial loopback by design
	tr.TLSClientConfig = &tls.Config{RootCAs: pool}
	srvAddr := srv.Listener.Addr().String()
	tr.DialContext = func(ctx context.Context, network, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, network, srvAddr)
	}
	res := newDirectoryResolver(nil)
	res.client = &http.Client{
		Transport:     tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse },
	}
	return res, &hits
}

func testCertificate(t *testing.T, host string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, pub, priv)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: priv}, pool
}

func mustDecodeB64URL(t *testing.T, s string) []byte {
	t.Helper()
	b, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		t.Fatalf("base64url decode %q: %v", s, err)
	}
	return b
}

func testVerifier(res *directoryResolver, now time.Time, require bool) *Verifier {
	return &Verifier{resolver: res, log: discardLog(), now: func() time.Time { return now }, require: require}
}

// runWBAVector drives one draft request vector through VerifyRequest
// and returns the result plus the assembled base.
func runWBAVector(t *testing.T, path string) (*Verifier, *wbaVector, Result, string) {
	t.Helper()
	v := loadVector[wbaVector](t, path)
	res, hits := directoryServer(t, v.DirectoryHost, v.DirectoryBody)
	now := time.Now()
	if v.VerificationTime != "" {
		var err error
		now, err = time.Parse(time.RFC3339, v.VerificationTime)
		if err != nil {
			t.Fatalf("parse verification_time: %v", err)
		}
	}
	ver := testVerifier(res, now, false)
	r := v.Request.toHTTPRequest(t)
	r.Header.Set("Signature-Input", v.SignatureInput)
	r.Header.Set("Signature", v.SignatureLabel+"=:"+v.Signature+":")

	// Base equality, independently of the middleware path.
	dict, err := parseSFDictionary(v.SignatureInput)
	if err != nil {
		t.Fatalf("parse Signature-Input: %v", err)
	}
	m := dict.get(v.SignatureLabel)
	base, err := buildSignatureBase(newRequestCtx(r), m.list.items, m.list.params)
	if err != nil {
		t.Fatalf("build base: %v", err)
	}

	result := ver.VerifyRequest(r)
	if *hits == 0 {
		t.Errorf("directory was never fetched")
	}
	t.Logf("vector source: %s", v.Source)
	return ver, v, result, base
}

// TestWBA_DraftVector_E21 runs draft Appendix E.2.1 (dictionary
// Signature-Agent) end to end: profile checks, TLS directory fetch,
// thumbprint key selection, Ed25519 verification.
func TestWBA_DraftVector_E21(t *testing.T) {
	_, v, result, base := runWBAVector(t, "testdata/wba-e.2.1-ed25519-dictionary.json")
	if base != v.ExpectedBase {
		t.Errorf("signature base mismatch:\n got: %q\nwant: %q", base, v.ExpectedBase)
	}
	if result.Outcome != OutcomeVerified {
		t.Fatalf("want verified, got %s: %s", result.Outcome, result.Reason)
	}
	if result.Agent == nil || result.Agent.URL != v.ExpectedAgentURL {
		t.Errorf("agent URL = %+v, want %s", result.Agent, v.ExpectedAgentURL)
	}
	if result.Agent.KeyID != "poqkLGiymh_W0uP6PZFw-dvez3QJT5SolqXBCW38r0U" {
		t.Errorf("agent keyid = %q", result.Agent.KeyID)
	}
}

// TestWBA_DraftVector_E22 runs draft Appendix E.2.2 (legacy sf-string
// Signature-Agent) at its pinned verification time.
func TestWBA_DraftVector_E22(t *testing.T) {
	_, v, result, base := runWBAVector(t, "testdata/wba-e.2.2-ed25519-legacy.json")
	if base != v.ExpectedBase {
		t.Errorf("signature base mismatch:\n got: %q\nwant: %q", base, v.ExpectedBase)
	}
	if result.Outcome != OutcomeVerified {
		t.Fatalf("want verified, got %s: %s", result.Outcome, result.Reason)
	}
	if result.Agent.URL != v.ExpectedAgentURL {
		t.Errorf("agent URL = %q, want %s", result.Agent.URL, v.ExpectedAgentURL)
	}
}

// TestWBA_DraftVector_E23 runs draft Appendix E.2.3: the signed
// directory response (possession proof) verified through the
// response-side base builder.
func TestWBA_DraftVector_E23(t *testing.T) {
	body, err := os.ReadFile("testdata/wba-e.2.3-directory-proof.json")
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		FetchRequest    vectorRequest     `json:"fetch_request"`
		ResponseHeaders map[string]string `json:"response_headers"`
		SignatureInput  string            `json:"signature_input"`
		Signature       string            `json:"signature"`
		ExpectedBase    string            `json:"expected_base"`
	}
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatal(err)
	}
	reqCtx := newRequestCtx(v.FetchRequest.toHTTPRequest(t))
	respHdr := http.Header{}
	for k, val := range v.ResponseHeaders {
		respHdr.Set(k, val)
	}
	resp := &responseCtx{status: 200, header: respHdr, req: reqCtx}

	dict, err := parseSFDictionary(v.SignatureInput)
	if err != nil {
		t.Fatalf("parse Signature-Input: %v", err)
	}
	m := dict.get("binding")
	base, err := buildResponseSignatureBase(resp, m.list.items, m.list.params)
	if err != nil {
		t.Fatalf("build response base: %v", err)
	}
	if base != v.ExpectedBase {
		t.Errorf("response base mismatch:\n got: %q\nwant: %q", base, v.ExpectedBase)
	}
	sig, err := base64.StdEncoding.DecodeString(v.Signature)
	if err != nil {
		t.Fatal(err)
	}
	pub := mustDecodeB64URL(t, "JrQLj5P_89iXES9-vFgrIy29clF9CC_oPPsw3c5D0bs")
	if !ed25519.Verify(ed25519.PublicKey(pub), []byte(base), sig) {
		t.Errorf("E.2.3 directory proof signature did not verify")
	}
	// The vector's Content-Digest must match the vector's directory
	// body: sha-256 of the exact JWKS bytes.
	sum := fmt.Sprintf("sha-256=:%s:", base64.StdEncoding.EncodeToString(sha256Sum([]byte(`{"keys":[{"kty":"OKP","crv":"Ed25519","kid":"poqkLGiymh_W0uP6PZFw-dvez3QJT5SolqXBCW38r0U","x":"JrQLj5P_89iXES9-vFgrIy29clF9CC_oPPsw3c5D0bs","use":"sig"}]}`))))
	if sum != v.ResponseHeaders["Content-Digest"] {
		t.Errorf("content digest sanity: %s != %s", sum, v.ResponseHeaders["Content-Digest"])
	}
}

func discardLog() *slog.Logger { return slog.New(slog.DiscardHandler) }

func sha256Sum(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}
