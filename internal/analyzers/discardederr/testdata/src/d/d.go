// Package d holds the discardederr spelling the 2026-09-02 adversarial
// review showed was out of contract: a refused two-result method whose
// kept value marches on, and the database/sql accessor exclusion.
package d

import (
	"database/sql"
	"errors"
)

var errRefused = errors.New("refused")

type conn struct{ fd int }

type pool struct{}

func (p *pool) acquire(tag string) (*conn, error) { return nil, errRefused }

// grab keeps the connection and drops a two-result refusal: a nil conn
// marches on. The doc's own bug class ("an empty list that reads as
// 'nothing found'") is this shape.
func grab(p *pool, tag string) *conn {
	c, _ := p.acquire(tag) // want `assignment discards the error from acquire`
	return c
}

// grabChecked is the fix posture.
func grabChecked(p *pool, tag string) (*conn, error) {
	c, err := p.acquire(tag)
	if err != nil {
		return nil, err
	}
	return c, nil
}

// rowsTouched reads a database/sql result accessor after an
// already-checked Exec: the count is display-only, and the accessor's
// error duplicates the statement's. Excluded by receiver type.
func rowsTouched(res sql.Result) int64 {
	n, _ := res.RowsAffected()
	return n
}
