// Package b holds discardederr positives in code that never existed in
// the repo: different names, same shape. After the 2026-09-02
// narrowing the rule fires only on last-position errors from method
// calls; the package-function spellings are the documented negatives.
package b

import "strconv"

type conn struct{}

func dial(addr string) (*conn, error) { return nil, nil }

type pool struct{}

func (p *pool) acquire(tag string) (*conn, func(), error) { return nil, func() {}, nil }

// grabFromPool keeps the connection and drops the dial error: a
// refused acquire marches on with a nil conn.
func grabFromPool(p *pool, tag string) *conn {
	c, release, _ := p.acquire(tag) // want `assignment discards the error from acquire`
	defer release()
	return c
}

// grabFromPoolChecked is the fix posture.
func grabFromPoolChecked(p *pool, tag string) (*conn, func(), error) {
	c, release, err := p.acquire(tag)
	if err != nil {
		return nil, func() {}, err
	}
	return c, release, nil
}

// atoiDefault keeps a parsed value and drops a package function's
// error: silent since the narrowing (measured at 165 broad findings,
// 2026-09-02).
func atoiDefault(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}

// borrow drops a package function's dial error the same way.
func borrow(addr string) *conn {
	c, _ := dial(addr)
	return c
}

// fireAndForget discards a single error on purpose: an idiomatic
// statement, not a hidden refusal.
func fireAndForget(p *pool, addr string) {
	c, release, _ := p.acquire(addr)
	_ = c
	_ = release
}

// nothingKept also drops the error but marches on with nothing: the
// kept value is itself silenced.
func nothingKept(p *pool, tag string) {
	c, release, _ := p.acquire(tag)
	_ = c
	_ = release
}
