package auth

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"math/big"
	"slices"
	"strings"
	"testing"
)

// ─── a test-only CBOR encoder (canonical enough for fixtures) ─────────

type cborMap [][2]any // ordered pairs so fixtures can repeat a key

func cborHead(major byte, n uint64) []byte {
	switch {
	case n < 24:
		return []byte{major<<5 | byte(n)}
	case n <= 0xff:
		return []byte{major<<5 | 24, byte(n)}
	case n <= 0xffff:
		return binary.BigEndian.AppendUint16([]byte{major<<5 | 25}, uint16(n))
	case n <= 0xffffffff:
		return binary.BigEndian.AppendUint32([]byte{major<<5 | 26}, uint32(n))
	}
	return binary.BigEndian.AppendUint64([]byte{major<<5 | 27}, n)
}

func cborEnc(v any) []byte {
	switch x := v.(type) {
	case int:
		if x >= 0 {
			return cborHead(0, uint64(x))
		}
		return cborHead(1, uint64(-1-x))
	case []byte:
		return append(cborHead(2, uint64(len(x))), x...)
	case string:
		return append(cborHead(3, uint64(len(x))), x...)
	case []any:
		out := cborHead(4, uint64(len(x)))
		for _, e := range x {
			out = append(out, cborEnc(e)...)
		}
		return out
	case cborMap:
		out := cborHead(5, uint64(len(x)))
		for _, kv := range x {
			out = append(out, cborEnc(kv[0])...)
			out = append(out, cborEnc(kv[1])...)
		}
		return out
	case bool:
		if x {
			return []byte{0xf5}
		}
		return []byte{0xf4}
	case nil:
		return []byte{0xf6}
	}
	panic(fmt.Sprintf("cborEnc: %T", v))
}

// ─── a fake authenticator ─────────────────────────────────────────────

type fakeAuthenticator struct {
	rpID   string
	origin string
	credID []byte
	alg    int64
	signer crypto.Signer
	count  uint32
	flags  byte // flags for the next ceremony; default UP|UV|BE|BS
}

func newFakeAuthenticator(t testing.TB, alg int64) *fakeAuthenticator {
	t.Helper()
	var signer crypto.Signer
	var err error
	switch alg {
	case coseAlgES256:
		signer, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	case coseAlgEdDSA:
		_, signer, err = ed25519.GenerateKey(rand.Reader)
	case coseAlgRS256:
		signer, err = rsa.GenerateKey(rand.Reader, 2048)
	}
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return &fakeAuthenticator{
		rpID: "example.com", origin: "https://example.com",
		credID: id, alg: alg, signer: signer,
		flags: flagUP | flagUV | flagBE | flagBS,
	}
}

func (a *fakeAuthenticator) coseKey() []byte {
	switch pub := a.signer.Public().(type) {
	case *ecdsa.PublicKey:
		raw, _ := pub.Bytes()
		return cborEnc(cborMap{{1, 2}, {3, -7}, {-1, 1}, {-2, raw[1:33]}, {-3, raw[33:]}})
	case ed25519.PublicKey:
		return cborEnc(cborMap{{1, 1}, {3, -8}, {-1, 6}, {-2, []byte(pub)}})
	case *rsa.PublicKey:
		return cborEnc(cborMap{{1, 3}, {3, -257}, {-1, pub.N.Bytes()}, {-2, big.NewInt(int64(pub.E)).Bytes()}})
	}
	panic("unreachable")
}

func (a *fakeAuthenticator) clientData(typ, challenge string) []byte {
	b, _ := json.Marshal(map[string]any{
		"type": typ, "challenge": base64.RawURLEncoding.EncodeToString([]byte(challenge)),
		"origin": a.origin, "crossOrigin": false,
	})
	return b
}

func (a *fakeAuthenticator) authData(flags byte, attested []byte) []byte {
	h := sha256.Sum256([]byte(a.rpID))
	out := append(h[:], flags)
	out = binary.BigEndian.AppendUint32(out, a.count)
	return append(out, attested...)
}

// register returns clientDataJSON and attestationObject for challenge.
func (a *fakeAuthenticator) register(challenge string) (cdj, att []byte) {
	attested := make([]byte, 16) // AAGUID
	attested = binary.BigEndian.AppendUint16(attested, uint16(len(a.credID)))
	attested = append(attested, a.credID...)
	attested = append(attested, a.coseKey()...)
	ad := a.authData(a.flags|flagAT, attested)
	att = cborEnc(cborMap{{"fmt", "none"}, {"attStmt", cborMap{}}, {"authData", ad}})
	return a.clientData("webauthn.create", challenge), att
}

// assert returns clientDataJSON, authenticatorData and signature.
func (a *fakeAuthenticator) assert(challenge string) (cdj, ad, sig []byte) {
	a.count++
	cdj = a.clientData("webauthn.get", challenge)
	ad = a.authData(a.flags, nil)
	sig = a.sign(ad, cdj)
	return cdj, ad, sig
}

func (a *fakeAuthenticator) sign(ad, cdj []byte) []byte {
	ch := sha256.Sum256(cdj)
	msg := append(slices.Clone(ad), ch[:]...)
	var sig []byte
	var err error
	switch a.alg {
	case coseAlgEdDSA:
		sig, err = a.signer.Sign(rand.Reader, msg, crypto.Hash(0))
	default:
		d := sha256.Sum256(msg)
		sig, err = a.signer.Sign(rand.Reader, d[:], crypto.SHA256)
	}
	if err != nil {
		panic(err)
	}
	return sig
}

var testRP = webauthnRP{ID: "example.com", Origins: []string{"https://example.com"}}

// ─── CBOR ─────────────────────────────────────────────────────────────

func TestCBORDecodesNestedValues(t *testing.T) {
	in := cborEnc(cborMap{{"a", []any{1, -2, "x", []byte{9}}}, {3, true}, {-4, nil}})
	v, n, err := cborDecode(in)
	if err != nil || n != len(in) {
		t.Fatalf("decode: n=%d err=%v", n, err)
	}
	m := v.(map[any]any)
	arr := m["a"].([]any)
	if arr[0] != int64(1) || arr[1] != int64(-2) || arr[2] != "x" || !bytes.Equal(arr[3].([]byte), []byte{9}) {
		t.Fatalf("array: %#v", arr)
	}
	if m[int64(3)] != true || m[int64(-4)] != nil {
		t.Fatalf("map: %#v", m)
	}
}

func TestCBORRefusesMalformedInput(t *testing.T) {
	deep := []byte{}
	for range 20 {
		deep = append(deep, 0x81) // array of one, nested
	}
	deep = append(deep, 0x00)
	cases := map[string][]byte{
		"empty":            {},
		"truncated bytes":  {0x45, 1, 2},
		"huge length":      {0x5b, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff},
		"indefinite":       {0x5f, 0x41, 0x00, 0xff},
		"tag":              {0xc0, 0x00},
		"float":            {0xfa, 0, 0, 0, 0},
		"reserved info":    {0x1c},
		"too deep":         deep,
		"duplicate key":    cborEnc(cborMap{{1, 1}, {1, 2}}),
		"array key":        {0xa1, 0x80, 0x00},
		"uint over int64":  {0x1b, 0x80, 0, 0, 0, 0, 0, 0, 0},
		"bad utf8 text":    {0x62, 0xff, 0xfe},
		"map count > rest": {0xb8, 0xff, 0x01},
	}
	for name, in := range cases {
		if _, _, err := cborDecode(in); err == nil {
			t.Errorf("%s: decoded % x without error", name, in)
		}
	}
}

func FuzzCBORDecodeNeverPanics(f *testing.F) {
	a := newFakeAuthenticator(f, coseAlgES256)
	_, att := a.register("seed")
	f.Add(att)
	f.Add(a.coseKey())
	f.Fuzz(func(t *testing.T, in []byte) {
		v, n, err := cborDecode(in)
		if err == nil && (n <= 0 || n > len(in)) {
			t.Fatalf("n=%d for %d bytes (v=%v)", n, len(in), v)
		}
		_, _ = parseAuthData(in, true)
		_, _ = parseCOSEKey(in)
	})
}

// ─── registration ─────────────────────────────────────────────────────

func TestRegistrationAcceptsEachAlgorithm(t *testing.T) {
	for _, alg := range []int64{coseAlgES256, coseAlgEdDSA, coseAlgRS256} {
		a := newFakeAuthenticator(t, alg)
		cdj, att := a.register("chal")
		ch, err := testRP.checkClientData(cdj, "webauthn.create")
		if err != nil || ch != "chal" {
			t.Fatalf("alg %d client data: %q %v", alg, ch, err)
		}
		res, err := testRP.verifyAttestation(att)
		if err != nil {
			t.Fatalf("alg %d: %v", alg, err)
		}
		if !bytes.Equal(res.credID, a.credID) || res.alg != alg || !res.backupEligible || !res.backedUp {
			t.Fatalf("alg %d result %+v", alg, res)
		}
	}
}

func TestClientDataRefusals(t *testing.T) {
	a := newFakeAuthenticator(t, coseAlgES256)
	good := func() map[string]any {
		var m map[string]any
		_ = json.Unmarshal(a.clientData("webauthn.create", "chal"), &m)
		return m
	}
	cases := map[string]func(m map[string]any){
		"wrong type":         func(m map[string]any) { m["type"] = "webauthn.get" },
		"foreign origin":     func(m map[string]any) { m["origin"] = "https://evil.example" },
		"origin with path":   func(m map[string]any) { m["origin"] = "https://example.com/" },
		"cross origin":       func(m map[string]any) { m["crossOrigin"] = true },
		"padded challenge":   func(m map[string]any) { m["challenge"] = base64.URLEncoding.EncodeToString([]byte("chal!")) },
		"empty challenge":    func(m map[string]any) { m["challenge"] = "" },
		"challenge not text": func(m map[string]any) { m["challenge"] = 7 },
	}
	for name, mut := range cases {
		m := good()
		mut(m)
		b, _ := json.Marshal(m)
		if _, err := testRP.checkClientData(b, "webauthn.create"); err == nil {
			t.Errorf("%s: accepted %s", name, b)
		}
	}
	if _, err := testRP.checkClientData([]byte(`{"type":"webauthn.create","type":"x"}`), "webauthn.create"); err == nil {
		t.Error("duplicate key accepted")
	}
}

func TestAttestationRefusals(t *testing.T) {
	cases := map[string]func(a *fakeAuthenticator){
		"other rp":       func(a *fakeAuthenticator) { a.rpID = "evil.example" },
		"no user check":  func(a *fakeAuthenticator) { a.flags &^= flagUP },
		"no user verify": func(a *fakeAuthenticator) { a.flags &^= flagUV },
		"BS without BE":  func(a *fakeAuthenticator) { a.flags &^= flagBE },
	}
	for name, mut := range cases {
		a := newFakeAuthenticator(t, coseAlgES256)
		mut(a)
		_, att := a.register("chal")
		if _, err := testRP.verifyAttestation(att); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	a := newFakeAuthenticator(t, coseAlgES256)
	_, att := a.register("chal")
	if _, err := testRP.verifyAttestation(append(slices.Clone(att), 0)); err == nil {
		t.Error("trailing byte after the attestation object accepted")
	}
	noAT := cborEnc(cborMap{{"fmt", "none"}, {"attStmt", cborMap{}}, {"authData", a.authData(a.flags, nil)}})
	if _, err := testRP.verifyAttestation(noAT); err == nil {
		t.Error("authData without attested credential data accepted")
	}
}

func TestCOSEKeyRefusals(t *testing.T) {
	ec, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	raw, _ := ec.PublicKey.Bytes()
	x, y := raw[1:33], raw[33:]
	offCurve := slices.Clone(y)
	offCurve[31] ^= 1
	small, _ := rsa.GenerateKey(rand.Reader, 1024)
	large, _ := rsa.GenerateKey(rand.Reader, 2048)
	cases := map[string]cborMap{
		"ES256 off curve":   {{1, 2}, {3, -7}, {-1, 1}, {-2, x}, {-3, offCurve}},
		"ES256 short x":     {{1, 2}, {3, -7}, {-1, 1}, {-2, x[1:]}, {-3, y}},
		"ES256 P-384 curve": {{1, 2}, {3, -7}, {-1, 2}, {-2, x}, {-3, y}},
		"kty/alg mismatch":  {{1, 1}, {3, -7}, {-1, 1}, {-2, x}, {-3, y}},
		"EdDSA wrong crv":   {{1, 1}, {3, -8}, {-1, 4}, {-2, x}},
		"unknown alg":       {{1, 2}, {3, -35}, {-1, 2}, {-2, x}, {-3, y}},
		"RSA 1024 bits":     {{1, 3}, {3, -257}, {-1, small.N.Bytes()}, {-2, []byte{1, 0, 1}}},
		"RSA even exponent": {{1, 3}, {3, -257}, {-1, large.N.Bytes()}, {-2, []byte{2}}},
		"missing alg":       {{1, 2}, {-1, 1}, {-2, x}, {-3, y}},
		// Verification cost grows with the square of the modulus: one
		// sign-in against a 4-Mbit key took 54s (2026-11-01 bug bash).
		"RSA past 8192 bits": {{1, 3}, {3, -257}, {-1, oversizeModulus(8193)}, {-2, []byte{1, 0, 1}}},
		"RSA even modulus":   {{1, 3}, {3, -257}, {-1, evenModulus(large.N)}, {-2, []byte{1, 0, 1}}},
	}
	for name, m := range cases {
		if _, err := parseCOSEKey(cborEnc(m)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// oversizeModulus returns an odd modulus of exactly bits bits.
func oversizeModulus(bits int) []byte {
	n := new(big.Int).Lsh(big.NewInt(1), uint(bits-1))
	return n.SetBit(n, 0, 1).Bytes()
}

func evenModulus(n *big.Int) []byte {
	return new(big.Int).SetBit(n, 0, 0).Bytes()
}

func TestCOSEKeyAcceptsRSAAtTheCap(t *testing.T) {
	if _, err := parseCOSEKey(cborEnc(cborMap{{1, 3}, {3, -257}, {-1, oversizeModulus(maxCOSERSABits)}, {-2, []byte{1, 0, 1}}})); err != nil {
		t.Errorf("%d-bit modulus refused: %v", maxCOSERSABits, err)
	}
}

// ─── assertion ────────────────────────────────────────────────────────

func registered(t *testing.T, a *fakeAuthenticator) registrationResult {
	t.Helper()
	_, att := a.register("chal")
	res, err := testRP.verifyAttestation(att)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAssertionVerifiesEachAlgorithm(t *testing.T) {
	for _, alg := range []int64{coseAlgES256, coseAlgEdDSA, coseAlgRS256} {
		a := newFakeAuthenticator(t, alg)
		res := registered(t, a)
		cdj, ad, sig := a.assert("login")
		got, err := testRP.verifyAssertion(res.publicKey, cdj, ad, sig)
		if err != nil {
			t.Fatalf("alg %d: %v", alg, err)
		}
		if got.signCount != a.count || !got.backedUp {
			t.Fatalf("alg %d: %+v", alg, got)
		}
	}
}

func TestAssertionRefusals(t *testing.T) {
	a := newFakeAuthenticator(t, coseAlgES256)
	res := registered(t, a)
	other := newFakeAuthenticator(t, coseAlgES256)

	cdj, ad, sig := a.assert("login")
	flip := slices.Clone(sig)
	flip[len(flip)-1] ^= 1
	cdj2 := []byte(strings.Replace(string(cdj), `"crossOrigin":false`, `"crossOrigin":false `, 1))
	_, _, otherSig := other.assert("login")

	cases := map[string][3][]byte{
		"tampered signature":   {cdj, ad, flip},
		"other key's sig":      {cdj, ad, otherSig},
		"client data changed":  {cdj2, ad, sig},
		"short authData":       {cdj, ad[:36], sig},
		"authData extra bytes": {cdj, append(slices.Clone(ad), 0), sig},
	}
	for name, c := range cases {
		if _, err := testRP.verifyAssertion(res.publicKey, c[0], c[1], c[2]); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}

	for name, mut := range map[string]func(*fakeAuthenticator){
		"no user verify": func(a *fakeAuthenticator) { a.flags &^= flagUV },
		"other rp":       func(a *fakeAuthenticator) { a.rpID = "evil.example" },
		"AT on assert":   func(a *fakeAuthenticator) { a.flags |= flagAT },
	} {
		b := *a
		mut(&b)
		cdj, ad, sig := b.assert("login")
		if _, err := testRP.verifyAssertion(res.publicKey, cdj, ad, sig); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestOriginConfigRefusals(t *testing.T) {
	for _, c := range []struct {
		rpID   string
		origin string
	}{
		{"example.com", "http://example.com"},
		{"example.com", "https://notexample.com"},
		{"example.com", "https://example.com/path"},
		{"example.com", "https://user@example.com"},
		{"sub.example.com", "https://example.com"},
		{"", "https://example.com"},
		{"example.com", ""},
	} {
		rp := webauthnRP{ID: c.rpID, Origins: []string{c.origin}}
		if err := rp.validate(); err == nil {
			t.Errorf("rp %q origin %q: accepted", c.rpID, c.origin)
		}
	}
	for _, c := range []struct{ rpID, origin string }{
		{"example.com", "https://example.com"},
		{"example.com", "https://app.example.com:8443"},
		{"localhost", "http://localhost:8080"},
	} {
		rp := webauthnRP{ID: c.rpID, Origins: []string{c.origin}}
		if err := rp.validate(); err != nil {
			t.Errorf("rp %q origin %q: %v", c.rpID, c.origin, err)
		}
	}
}
