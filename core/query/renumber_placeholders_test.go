package query

import (
	"testing"
)

// TestWhere_PlaceholdersAreRenumberedPositionally pins the composition
// contract: renumbering is positional by encounter, so two fragments that
// each independently emit $1 (exactly what framework/entity's And/Or/Not
// produce) become $1, $2 across the composed clause — one placeholder per
// argument. A repeated original $N is NOT a back-reference; the caller
// passes one argument per placeholder.
func TestWhere_PlaceholdersAreRenumberedPositionally(t *testing.T) {
	sqlStr, args := Select("*").From("t").
		Where("a = $1 OR b = $1", "x", "y").Build()

	want := "SELECT * FROM t WHERE (a = $1 OR b = $2)"
	if sqlStr != want {
		t.Errorf("SQL = %q\nwant   %q", sqlStr, want)
	}
	if len(args) != 2 || args[0] != "x" || args[1] != "y" {
		t.Errorf("args = %v, want [x y] (one bind per placeholder)", args)
	}
}

// TestWhere_QuotedLiteralNotRenumbered verifies that a $N appearing inside
// a single-quoted SQL string literal is left untouched. Previously the
// scanner was quote-blind, so `label = '$5 off' AND x = $1` became
// `label = '$1 off' AND x = $2` — corrupting the literal value AND
// misaligning the real placeholder.
func TestWhere_QuotedLiteralNotRenumbered(t *testing.T) {
	sqlStr, args := Select("*").From("t").
		Where("label = '$5 off' AND x = $1", "v").Build()

	want := "SELECT * FROM t WHERE (label = '$5 off' AND x = $1)"
	if sqlStr != want {
		t.Errorf("SQL = %q\nwant   %q", sqlStr, want)
	}
	if len(args) != 1 || args[0] != "v" {
		t.Errorf("args = %v, want [v]", args)
	}
}

// TestWhere_EscapedQuoteInLiteral guards the ” escape inside a quoted
// literal: an embedded quote must not prematurely close the literal and
// expose a later $N to renumbering.
func TestWhere_EscapedQuoteInLiteral(t *testing.T) {
	// Inside the literal: 'it''s $9 here' — $9 is data, must survive.
	// Outside: y = $1 is the only real placeholder.
	sqlStr, args := Select("*").From("t").
		Where("m = 'it''s $9 here' AND y = $1", "v").Build()

	want := "SELECT * FROM t WHERE (m = 'it''s $9 here' AND y = $1)"
	if sqlStr != want {
		t.Errorf("SQL = %q\nwant   %q", sqlStr, want)
	}
	if len(args) != 1 || args[0] != "v" {
		t.Errorf("args = %v, want [v]", args)
	}
}

// TestWhere_RenumberStartsFromBuilderOffset pins that renumbering is
// relative to the placeholders already emitted by earlier clauses: a
// second Where starts where the first left off, by encounter order.
func TestWhere_RenumberStartsFromBuilderOffset(t *testing.T) {
	sqlStr, args := Select("*").From("t").
		Where("a = $1", "x").
		Where("b = $1 OR c = $1", "y", "z").
		Build()

	want := "SELECT * FROM t WHERE (a = $1) AND (b = $2 OR c = $3)"
	if sqlStr != want {
		t.Errorf("SQL = %q\nwant   %q", sqlStr, want)
	}
	if len(args) != 3 || args[0] != "x" || args[1] != "y" || args[2] != "z" {
		t.Errorf("args = %v, want [x y z]", args)
	}
}
