package a

import (
	"crypto/rsa"
	"crypto/x509"
	"errors"
	"math/big"
)

var errKey = errors.New("bad key")

const maxBits = 8192

// preFix5c074b43 is parseCOSEKey before 5c074b43: a lower bound only.
func preFix5c074b43(nb []byte, e int) (*rsa.PublicKey, error) {
	mod := new(big.Int).SetBytes(nb)
	if mod.BitLen() < 2048 {
		return nil, errKey
	}
	return &rsa.PublicKey{N: mod, E: e}, nil // want `rsakeysize: the RSA modulus mod comes from new\(big.Int\).SetBytes`
}

// fixed5c074b43 is the fix spelling: the upper bound refuses first.
func fixed5c074b43(nb []byte, e int) (*rsa.PublicKey, error) {
	mod := new(big.Int).SetBytes(nb)
	if mod.BitLen() < 2048 {
		return nil, errKey
	}
	if mod.BitLen() > maxBits || mod.Bit(0) == 0 {
		return nil, errKey
	}
	return &rsa.PublicKey{N: mod, E: e}, nil
}

// oneCondition bounds both sides in one disjunction.
func oneCondition(nb []byte) *rsa.PublicKey {
	n := new(big.Int).SetBytes(nb)
	if n.BitLen() < 2048 || n.BitLen() > maxBits {
		return nil
	}
	return &rsa.PublicKey{N: n, E: 65537}
}

// reversedOperands spells the bound with the constant on the left.
func reversedOperands(nb []byte) *rsa.PublicKey {
	n := new(big.Int).SetBytes(nb)
	if maxBits < n.BitLen() {
		panic("too big")
	}
	return &rsa.PublicKey{N: n, E: 65537}
}

// lenBound bounds the source bytes instead of the decoded value.
func lenBound(nb []byte) *rsa.PublicKey {
	if len(nb) > maxBits/8 {
		return nil
	}
	n := new(big.Int).SetBytes(nb)
	return &rsa.PublicKey{N: n, E: 65537}
}

// enclosing proceeds only inside the bound.
func enclosing(nb []byte) *rsa.PublicKey {
	n := new(big.Int).SetBytes(nb)
	if n.BitLen() <= maxBits {
		return &rsa.PublicKey{N: n, E: 65537}
	}
	return nil
}

func checkSize(n *big.Int) error {
	if n.BitLen() > maxBits {
		return errKey
	}
	return nil
}

// helperBound delegates the refusal to a same-package checker.
func helperBound(nb []byte) (*rsa.PublicKey, error) {
	n := new(big.Int).SetBytes(nb)
	if err := checkSize(n); err != nil {
		return nil, err
	}
	return &rsa.PublicKey{N: n, E: 65537}, nil
}

// Near miss: a bound inside an unrelated branch dominates nothing.
func flagGatedBound(nb []byte, strict bool) *rsa.PublicKey {
	n := new(big.Int).SetBytes(nb)
	if strict {
		if n.BitLen() > maxBits {
			return nil
		}
	}
	return &rsa.PublicKey{N: n, E: 65537} // want `rsakeysize: the RSA modulus n `
}

// Near miss: the bound is on a different big.Int (the exponent).
func boundsTheWrongValue(nb, eb []byte) *rsa.PublicKey {
	n := new(big.Int).SetBytes(nb)
	e := new(big.Int).SetBytes(eb)
	if e.BitLen() > 31 {
		return nil
	}
	return &rsa.PublicKey{N: n, E: int(e.Int64())} // want `rsakeysize: the RSA modulus n `
}

// Near miss: a bound that does not refuse is no bound.
func boundThatLogs(nb []byte, log func(string)) *rsa.PublicKey {
	n := new(big.Int).SetBytes(nb)
	if n.BitLen() > maxBits {
		log("large key")
	}
	return &rsa.PublicKey{N: n, E: 65537} // want `rsakeysize: the RSA modulus n `
}

// inline decodes inside the literal: nothing can bound it.
func inline(nb []byte) *rsa.PublicKey {
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: 65537} // want `rsakeysize: the RSA modulus new\(big.Int\).SetBytes\(nb\)`
}

// statementForm fills the modulus with a statement, then assigns N.
func statementForm(s string) *rsa.PublicKey {
	n := new(big.Int)
	n.SetString(s, 16)
	var k rsa.PublicKey
	k.N = n // want `rsakeysize: the RSA modulus n comes from n.SetString`
	k.E = 65537
	return &k
}

// constantKey is compiled into the binary: not attacker input.
func constantKey() *rsa.PublicKey {
	n, _ := new(big.Int).SetString("c0ffee", 16)
	return &rsa.PublicKey{N: n, E: 65537}
}

// generated keys never pass through SetBytes.
func generated(priv *rsa.PrivateKey) *rsa.PublicKey {
	return &rsa.PublicKey{N: priv.N, E: priv.E}
}

// x509 arm: the parsers bound nothing above.
func pkixUnbounded(der []byte) (*rsa.PublicKey, error) {
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	pk, ok := parsed.(*rsa.PublicKey) // want `rsakeysize: the RSA key pk is parsed from input`
	if !ok {
		return nil, errKey
	}
	return pk, nil
}

func pkixBounded(der []byte) (*rsa.PublicKey, error) {
	parsed, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return nil, err
	}
	pk, ok := parsed.(*rsa.PublicKey)
	if !ok || pk.N.BitLen() > maxBits {
		return nil, errKey
	}
	return pk, nil
}

func pkcs1Unbounded(der []byte) (*rsa.PublicKey, error) {
	pk, err := x509.ParsePKCS1PublicKey(der) // want `rsakeysize: the RSA key pk is parsed from input`
	return pk, err
}

func pkcs1Bounded(der []byte) (*rsa.PublicKey, error) {
	pk, err := x509.ParsePKCS1PublicKey(der)
	if err != nil {
		return nil, err
	}
	if pk.Size() > maxBits/8 {
		return nil, errKey
	}
	return pk, nil
}

// Near miss: a type assertion on a value the code built, not parsed.
func notParsed(v any) *rsa.PublicKey {
	pk, _ := v.(*rsa.PublicKey)
	return pk
}
