package auth

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/DonaldMurillo/gofastr/core/handler"
)

// This file verifies the two WebAuthn ceremonies (Level 3, §7.1
// registration and §7.2 authentication) with the standard library only:
// a bounded CBOR decoder for the attestation object and the COSE public
// key, and crypto/ecdsa, crypto/ed25519 and crypto/rsa for signatures.
//
// Attestation is requested as "none" and never verified: the battery has
// no trust policy for authenticator makers, so an attestation statement
// would prove nothing it acts on. Every registration is treated as
// attestation type None. What binds a credential to its user is the
// session that registered it and every later assertion's signature.

// COSE algorithm identifiers the battery accepts (IANA COSE registry).
const (
	coseAlgES256 int64 = -7
	coseAlgEdDSA int64 = -8
	coseAlgRS256 int64 = -257
)

// passkeyAlgs is the pubKeyCredParams order sent to the browser.
var passkeyAlgs = []int64{coseAlgEdDSA, coseAlgES256, coseAlgRS256}

// Authenticator data flag bits.
const (
	flagUP byte = 1 << 0 // user present
	flagUV byte = 1 << 2 // user verified
	flagBE byte = 1 << 3 // backup eligible
	flagBS byte = 1 << 4 // backed up
	flagAT byte = 1 << 6 // attested credential data included
	flagED byte = 1 << 7 // extension data included
)

// maxCredentialIDLen is the spec's ceiling for a credential id.
const maxCredentialIDLen = 1023

// webauthnRP is the relying party: the RP ID credentials are scoped to
// and the exact origins whose ceremonies are accepted.
type webauthnRP struct {
	ID      string
	Origins []string
}

// validate refuses an RP ID or origin set that cannot work or would
// accept more than the host meant: every origin is scheme://host[:port]
// with nothing after it, https unless the host is localhost, and its
// host is the RP ID or a subdomain of it.
func (rp webauthnRP) validate() error {
	if rp.ID == "" || strings.ContainsAny(rp.ID, "/:@ ") || rp.ID != strings.ToLower(rp.ID) {
		return fmt.Errorf("RPID %q must be a lowercase domain name", rp.ID)
	}
	if len(rp.Origins) == 0 {
		return errors.New("Origins is required: the exact origins (https://app.example.com) the login page is served from")
	}
	for _, o := range rp.Origins {
		u, err := url.Parse(o)
		if err != nil || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
			return fmt.Errorf("origin %q must be scheme://host[:port] with nothing after it", o)
		}
		host := u.Hostname()
		// Browsers serialise an origin without its scheme's default
		// port, so "https://example.com:443" would match no ceremony.
		if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
			return fmt.Errorf("origin %q names its default port; browsers report it as %s://%s", o, u.Scheme, host)
		}
		switch {
		case u.Scheme == "https":
		case u.Scheme == "http" && host == "localhost":
		default:
			return fmt.Errorf("origin %q must use https (http is accepted only for localhost)", o)
		}
		if host != rp.ID && !strings.HasSuffix(host, "."+rp.ID) {
			return fmt.Errorf("origin %q is not RPID %q or a subdomain of it", o, rp.ID)
		}
	}
	return nil
}

// checkClientData parses clientDataJSON, checks its type, origin and
// cross-origin bit, and returns the challenge the browser signed over,
// decoded from base64url. The caller redeems that challenge: issuing it
// and spending it once is what makes the ceremony fresh.
func (rp webauthnRP) checkClientData(raw []byte, wantType string) (string, error) {
	// Browsers may add members (the spec reserves room for them), so
	// unknown keys are allowed; a duplicate or case-folded key pair is
	// refused, since two readers could disagree about which one counts.
	var m map[string]any
	//gofastr:allow(floatnum) clientData members are read as strings and a bool only
	if err := handler.UnmarshalStrict(raw, &m); err != nil {
		return "", fmt.Errorf("client data: %w", err)
	}
	typ, err1 := clientDataField[string](m, "type")
	challenge, err2 := clientDataField[string](m, "challenge")
	origin, err3 := clientDataField[string](m, "origin")
	crossOrigin, err4 := clientDataField[bool](m, "crossOrigin")
	if err := errors.Join(err1, err2, err3, err4); err != nil {
		return "", err
	}
	if typ != wantType {
		return "", fmt.Errorf("client data type %q, want %q", typ, wantType)
	}
	if !slices.Contains(rp.Origins, origin) {
		return "", fmt.Errorf("client data origin %q is not an allowed origin", origin)
	}
	if crossOrigin {
		return "", errors.New("client data reports a cross-origin iframe")
	}
	ch, err := base64.RawURLEncoding.Strict().DecodeString(challenge)
	if err != nil || len(ch) == 0 {
		return "", errors.New("client data challenge is not unpadded base64url")
	}
	return string(ch), nil
}

// clientDataField reads one clientDataJSON member: absent is the zero
// value, present with another JSON type is an error.
func clientDataField[T string | bool](m map[string]any, key string) (T, error) {
	var zero T
	v, ok := m[key]
	if !ok {
		return zero, nil
	}
	t, ok := v.(T)
	if !ok {
		return zero, fmt.Errorf("client data %s has the wrong JSON type", key)
	}
	return t, nil
}

// registrationResult is what a verified registration stores.
type registrationResult struct {
	credID         []byte
	publicKey      []byte // the COSE_Key bytes as the authenticator sent them
	alg            int64
	signCount      uint32
	backupEligible bool
	backedUp       bool
}

// verifyAttestation checks an attestation object's authenticator data
// against the RP: RP ID hash, user presence and verification, the backup
// bits, and attested credential data carrying a supported public key.
// The attestation statement is not verified (see the file comment).
func (rp webauthnRP) verifyAttestation(attObj []byte) (registrationResult, error) {
	v, n, err := cborDecode(attObj)
	if err != nil {
		return registrationResult{}, fmt.Errorf("attestation object: %w", err)
	}
	if n != len(attObj) {
		return registrationResult{}, errors.New("attestation object: trailing bytes")
	}
	m, ok := v.(map[any]any)
	if !ok {
		return registrationResult{}, errors.New("attestation object is not a map")
	}
	if _, ok := m["fmt"].(string); !ok {
		return registrationResult{}, errors.New("attestation object: fmt is not text")
	}
	if _, ok := m["attStmt"].(map[any]any); !ok {
		return registrationResult{}, errors.New("attestation object: attStmt is not a map")
	}
	raw, ok := m["authData"].([]byte)
	if !ok {
		return registrationResult{}, errors.New("attestation object: authData is not bytes")
	}
	ad, err := parseAuthData(raw, true)
	if err != nil {
		return registrationResult{}, err
	}
	if err := rp.checkAuthData(ad); err != nil {
		return registrationResult{}, err
	}
	return registrationResult{
		credID:         ad.credID,
		publicKey:      ad.coseKey,
		alg:            ad.key.alg,
		signCount:      ad.signCount,
		backupEligible: ad.flags&flagBE != 0,
		backedUp:       ad.flags&flagBS != 0,
	}, nil
}

// assertionResult is what a verified assertion reports back.
type assertionResult struct {
	signCount      uint32
	backupEligible bool
	backedUp       bool
}

// verifyAssertion checks an assertion against the stored COSE key:
// authenticator data against the RP, then the signature over
// authenticatorData || SHA-256(clientDataJSON). clientDataJSON must have
// passed checkClientData already.
func (rp webauthnRP) verifyAssertion(coseKey, clientDataJSON, authData, sig []byte) (assertionResult, error) {
	key, err := parseCOSEKey(coseKey)
	if err != nil {
		return assertionResult{}, fmt.Errorf("stored key: %w", err)
	}
	ad, err := parseAuthData(authData, false)
	if err != nil {
		return assertionResult{}, err
	}
	if err := rp.checkAuthData(ad); err != nil {
		return assertionResult{}, err
	}
	ch := sha256.Sum256(clientDataJSON)
	msg := append(slices.Clone(authData), ch[:]...)
	if !key.verify(msg, sig) {
		return assertionResult{}, errors.New("assertion signature does not verify")
	}
	return assertionResult{signCount: ad.signCount, backupEligible: ad.flags&flagBE != 0, backedUp: ad.flags&flagBS != 0}, nil
}

func (rp webauthnRP) checkAuthData(ad authData) error {
	want := sha256.Sum256([]byte(rp.ID))
	if subtle.ConstantTimeCompare(ad.rpIDHash, want[:]) != 1 {
		return errors.New("authenticator data is for another RP ID")
	}
	if ad.flags&flagUP == 0 {
		return errors.New("authenticator data: user not present")
	}
	if ad.flags&flagUV == 0 {
		return errors.New("authenticator data: user not verified")
	}
	if ad.flags&flagBS != 0 && ad.flags&flagBE == 0 {
		return errors.New("authenticator data: backed up but not backup eligible")
	}
	return nil
}

// authData is parsed authenticator data (§6.1).
type authData struct {
	rpIDHash  []byte
	flags     byte
	signCount uint32
	credID    []byte
	coseKey   []byte
	key       coseKey
}

// parseAuthData parses authenticator data. Registration (attested=true)
// requires attested credential data; an assertion refuses it. Extension
// data is skipped over when flagged, and nothing may follow it.
func parseAuthData(b []byte, attested bool) (authData, error) {
	if len(b) < 37 {
		return authData{}, errors.New("authenticator data shorter than 37 bytes")
	}
	ad := authData{rpIDHash: b[:32], flags: b[32], signCount: binary.BigEndian.Uint32(b[33:37])}
	rest := b[37:]
	hasAT := ad.flags&flagAT != 0
	if hasAT != attested {
		if attested {
			return authData{}, errors.New("authenticator data has no attested credential data")
		}
		return authData{}, errors.New("assertion authenticator data carries attested credential data")
	}
	if hasAT {
		if len(rest) < 18 {
			return authData{}, errors.New("attested credential data truncated")
		}
		idLen := int(binary.BigEndian.Uint16(rest[16:18]))
		rest = rest[18:]
		if idLen == 0 || idLen > maxCredentialIDLen || idLen > len(rest) {
			return authData{}, errors.New("credential id length out of range")
		}
		ad.credID = slices.Clone(rest[:idLen])
		rest = rest[idLen:]
		_, n, err := cborDecode(rest)
		if err != nil {
			return authData{}, fmt.Errorf("credential public key: %w", err)
		}
		ad.coseKey = slices.Clone(rest[:n])
		if ad.key, err = parseCOSEKey(ad.coseKey); err != nil {
			return authData{}, err
		}
		rest = rest[n:]
	}
	if ad.flags&flagED != 0 {
		v, n, err := cborDecode(rest)
		if err != nil {
			return authData{}, fmt.Errorf("extension data: %w", err)
		}
		if _, ok := v.(map[any]any); !ok {
			return authData{}, errors.New("extension data is not a map")
		}
		rest = rest[n:]
	}
	if len(rest) != 0 {
		return authData{}, errors.New("authenticator data: trailing bytes")
	}
	return ad, nil
}

// coseKey is a parsed credential public key.
type coseKey struct {
	alg int64
	pub crypto.PublicKey
}

// parseCOSEKey parses a COSE_Key (RFC 9052/9053) for one of the three
// supported algorithms and checks the key itself: a P-256 point on the
// curve, a 32-byte Ed25519 key, an RSA modulus of at least 2048 bits
// with an odd exponent.
func parseCOSEKey(b []byte) (coseKey, error) {
	v, n, err := cborDecode(b)
	if err != nil {
		return coseKey{}, fmt.Errorf("COSE key: %w", err)
	}
	if n != len(b) {
		return coseKey{}, errors.New("COSE key: trailing bytes")
	}
	m, ok := v.(map[any]any)
	if !ok {
		return coseKey{}, errors.New("COSE key is not a map")
	}
	kty, ok1 := m[int64(1)].(int64)
	alg, ok2 := m[int64(3)].(int64)
	if !ok1 || !ok2 {
		return coseKey{}, errors.New("COSE key: kty and alg must be integers")
	}
	switch {
	case kty == 2 && alg == coseAlgES256:
		crv, _ := m[int64(-1)].(int64)
		x, _ := m[int64(-2)].([]byte)
		y, _ := m[int64(-3)].([]byte)
		if crv != 1 || len(x) != 32 || len(y) != 32 {
			return coseKey{}, errors.New("COSE key: ES256 needs a P-256 key with 32-byte coordinates")
		}
		pub, err := ecdsa.ParseUncompressedPublicKey(elliptic.P256(), slices.Concat([]byte{4}, x, y))
		if err != nil {
			return coseKey{}, fmt.Errorf("COSE key: %w", err)
		}
		return coseKey{alg: alg, pub: pub}, nil
	case kty == 1 && alg == coseAlgEdDSA:
		crv, _ := m[int64(-1)].(int64)
		x, _ := m[int64(-2)].([]byte)
		if crv != 6 || len(x) != ed25519.PublicKeySize {
			return coseKey{}, errors.New("COSE key: EdDSA needs an Ed25519 key")
		}
		return coseKey{alg: alg, pub: ed25519.PublicKey(slices.Clone(x))}, nil
	case kty == 3 && alg == coseAlgRS256:
		nb, _ := m[int64(-1)].([]byte)
		eb, _ := m[int64(-2)].([]byte)
		mod := new(big.Int).SetBytes(nb)
		if mod.BitLen() < 2048 || len(eb) == 0 || len(eb) > 4 {
			return coseKey{}, errors.New("COSE key: RS256 needs a modulus of at least 2048 bits")
		}
		// Verification cost grows with the square of the modulus, and
		// attestation "none" checks nothing about the key: a 4-Mbit modulus
		// registered once made every anonymous sign-in attempt against it
		// burn about a minute of CPU. crypto/tls caps peer RSA keys at 8192
		// bits for the same reason (CVE-2023-29409). Login re-parses the
		// stored key, so an oversized key already stored is refused too.
		if mod.BitLen() > maxCOSERSABits || mod.Bit(0) == 0 {
			return coseKey{}, errors.New("COSE key: RS256 needs an odd modulus of at most 8192 bits")
		}
		e := int(new(big.Int).SetBytes(eb).Int64())
		if e < 3 || e%2 == 0 {
			return coseKey{}, errors.New("COSE key: RS256 exponent must be odd and at least 3")
		}
		return coseKey{alg: alg, pub: &rsa.PublicKey{N: mod, E: e}}, nil
	}
	return coseKey{}, fmt.Errorf("COSE key: unsupported kty %d / alg %d", kty, alg)
}

// maxCOSERSABits is the largest RSA modulus a passkey may register.
const maxCOSERSABits = 8192

func (k coseKey) verify(msg, sig []byte) bool {
	switch pub := k.pub.(type) {
	case *ecdsa.PublicKey:
		d := sha256.Sum256(msg)
		return ecdsa.VerifyASN1(pub, d[:], sig)
	case ed25519.PublicKey:
		return ed25519.Verify(pub, msg, sig)
	case *rsa.PublicKey:
		d := sha256.Sum256(msg)
		return rsa.VerifyPKCS1v15(pub, crypto.SHA256, d[:], sig) == nil
	}
	return false
}

// ─── CBOR (RFC 8949), the subset WebAuthn uses ────────────────────────

// cborMaxDepth bounds nesting; WebAuthn structures nest three deep.
const cborMaxDepth = 8

// cborDecode decodes one CBOR data item from the front of b and returns
// it with the number of bytes it took. Integers decode to int64, byte
// strings to []byte, text to string, arrays to []any, maps to
// map[any]any (keys int64 or string, duplicates refused), and the simple
// values false, true and null. Indefinite lengths, tags and floats are
// refused, and so is any length longer than the bytes left, so a hostile
// length prefix never sizes an allocation.
func cborDecode(b []byte) (any, int, error) {
	d := cborDecoder{b: b}
	v, err := d.item(0)
	if err != nil {
		return nil, 0, err
	}
	return v, d.off, nil
}

type cborDecoder struct {
	b   []byte
	off int
}

var errCBORTruncated = errors.New("cbor: truncated")

func (d *cborDecoder) head() (major byte, arg uint64, err error) {
	if d.off >= len(d.b) {
		return 0, 0, errCBORTruncated
	}
	ib := d.b[d.off]
	d.off++
	major, info := ib>>5, ib&0x1f
	switch {
	case info < 24:
		return major, uint64(info), nil
	case info <= 27:
		size := 1 << (info - 24)
		if len(d.b)-d.off < size {
			return 0, 0, errCBORTruncated
		}
		for _, c := range d.b[d.off : d.off+size] {
			arg = arg<<8 | uint64(c)
		}
		d.off += size
		return major, arg, nil
	case info == 31:
		return 0, 0, errors.New("cbor: indefinite lengths are not supported")
	}
	return 0, 0, fmt.Errorf("cbor: reserved additional info %d", info)
}

// count checks a length or element count against the bytes left: every
// element takes at least one byte.
func (d *cborDecoder) count(n uint64) (int, error) {
	if n > uint64(len(d.b)-d.off) {
		return 0, errCBORTruncated
	}
	return int(n), nil
}

func (d *cborDecoder) item(depth int) (any, error) {
	if depth > cborMaxDepth {
		return nil, errors.New("cbor: nested too deep")
	}
	if d.off < len(d.b) {
		if ib := d.b[d.off]; ib>>5 == 7 && ib&0x1f >= 25 && ib&0x1f <= 27 {
			return nil, errors.New("cbor: floats are not supported")
		}
	}
	major, arg, err := d.head()
	if err != nil {
		return nil, err
	}
	switch major {
	case 0, 1:
		if arg > 1<<63-1 {
			return nil, errors.New("cbor: integer out of range")
		}
		if major == 0 {
			return int64(arg), nil
		}
		return -1 - int64(arg), nil
	case 2, 3:
		n, err := d.count(arg)
		if err != nil {
			return nil, err
		}
		s := d.b[d.off : d.off+n]
		d.off += n
		if major == 2 {
			return slices.Clone(s), nil
		}
		if !utf8.Valid(s) {
			return nil, errors.New("cbor: text is not UTF-8")
		}
		return string(s), nil
	case 4:
		n, err := d.count(arg)
		if err != nil {
			return nil, err
		}
		out := make([]any, 0, n)
		for range n {
			v, err := d.item(depth + 1)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	case 5:
		n, err := d.count(arg)
		if err != nil {
			return nil, err
		}
		out := make(map[any]any, n)
		for range n {
			k, err := d.item(depth + 1)
			if err != nil {
				return nil, err
			}
			switch k.(type) {
			case int64, string:
			default:
				return nil, errors.New("cbor: map key is not an integer or text")
			}
			if _, dup := out[k]; dup {
				return nil, fmt.Errorf("cbor: duplicate map key %v", k)
			}
			v, err := d.item(depth + 1)
			if err != nil {
				return nil, err
			}
			out[k] = v
		}
		return out, nil
	case 6:
		return nil, errors.New("cbor: tags are not supported")
	}
	switch arg {
	case 20:
		return false, nil
	case 21:
		return true, nil
	case 22:
		return nil, nil
	}
	return nil, fmt.Errorf("cbor: unsupported simple value %d", arg)
}
