// Package printkey is battery/auth's dummy-hash cache before 5ade7a35:
// hasherKey named a configured PasswordHasher by fmt.Sprintf("%T|%+v"),
// so a hasher holding a pointer or a mutable field minted a new key per
// call.
package printkey

import (
	"errors"
	"fmt"
	"reflect"
	"sync"
)

type PasswordHasher interface {
	Hash(string) (string, error)
}

type BcryptHasher struct{ Cost int }

func (BcryptHasher) Hash(string) (string, error) { return "", nil }

type Argon2Hasher struct {
	Time, Memory uint32
	Threads      uint8
}

func (Argon2Hasher) Hash(string) (string, error) { return "", nil }

var dummyHashes sync.Map

// Pre-fix hasherKey.
func hasherKey(h PasswordHasher) string {
	return fmt.Sprintf("%T|%+v", h, h)
}

func dummyHashFor(h PasswordHasher) string {
	key := hasherKey(h)
	if v, ok := dummyHashes.Load(key); ok { // want `map key built from %\+v of h \(an interface value prints its dynamic value's fields\)`
		return v.(string)
	}
	hash, _ := h.Hash("dummy")
	dummyHashes.Store(key, hash)
	return hash
}

// The fix: the built-ins, whose fields are plain values, keep %+v inside
// a type-switch clause that pins them; everything else is keyed by type.
func hasherKeyFixed(h PasswordHasher) string {
	switch h.(type) {
	case BcryptHasher, *BcryptHasher, Argon2Hasher, *Argon2Hasher:
		return fmt.Sprintf("%T|%+v", h, h)
	}
	return fmt.Sprintf("%T", h)
}

func dummyHashFixed(h PasswordHasher) string {
	key := hasherKeyFixed(h)
	if v, ok := dummyHashes.Load(key); ok {
		return v.(string)
	}
	return ""
}

// The other fix spelling: key by reflect.Type.
var byType = map[reflect.Type]string{}

func dummyByType(h PasswordHasher) string {
	return byType[reflect.TypeOf(h)]
}

type pool struct{ conns map[string]int }

type Conn struct {
	Addr string
	next *Conn
}

// A plain map, inline Sprintf on a pointer to a struct holding a pointer.
func (p *pool) count(c *Conn) {
	p.conns[fmt.Sprintf("%v", c)]++ // want `map key built from %v of c \(a pointer prints an address\)`
}

// fmt.Sprint prints every operand with %v.
func (p *pool) countSprint(c *Conn) {
	p.conns[fmt.Sprint("conn:", c)]++ // want `map key built from %v of c`
}

// Near miss: a pointer to a struct of plain values prints its contents.
func (p *pool) countArgon(a *Argon2Hasher) {
	p.conns[fmt.Sprintf("%+v", a)]++
}

// Near miss: %T and %s/%d do not print fields.
func (p *pool) countType(h PasswordHasher, n int) {
	p.conns[fmt.Sprintf("%T/%d", h, n)]++
}

// Near miss: an any value is usually a scalar id from a decoded row.
func (p *pool) countAny(id any) {
	p.conns[fmt.Sprintf("%v", id)]++
}

// Near miss: an interface declaring Error() (or String()) prints by it.
func (p *pool) countErr(err error) {
	p.conns[fmt.Sprintf("%v", err)]++
}

// Near miss: the Sprintf is never a key.
func describe(h PasswordHasher) error {
	return errors.New(fmt.Sprintf("hasher %+v", h))
}
