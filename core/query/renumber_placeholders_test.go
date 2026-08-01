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

// A $N inside a string literal is data, not a placeholder. Renumbering it
// corrupts the literal AND shifts the real placeholders, so the statement
// ends up wanting more parameters than the caller bound.
func TestRenumberLeavesLiteralsAlone(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{
			"single-quoted literal",
			`note = '$5 off' AND id = $1`,
			`note = '$5 off' AND id = $1`,
		},
		{
			"doubled quote inside a literal",
			`note = 'it''s $5 off' AND id = $1`,
			`note = 'it''s $5 off' AND id = $1`,
		},
		{
			// E'…' honours backslash escapes, so \' does not end the
			// literal. Treating it as a terminator dropped the lexer out
			// early and renumbered the rest of the string.
			"E-string with an escaped quote",
			`note = E'it\'s $5 off' AND id = $1`,
			`note = E'it\'s $5 off' AND id = $1`,
		},
		{
			"bare dollar-quoted body",
			`body = $$price $5$$ AND id = $1`,
			`body = $$price $5$$ AND id = $1`,
		},
		{
			"tagged dollar-quoted body",
			`body = $tag$price $5$tag$ AND id = $1`,
			`body = $tag$price $5$tag$ AND id = $1`,
		},
		{
			"real placeholders around a literal still renumber positionally",
			`a = $1 AND note = '$9' AND b = $1`,
			`a = $1 AND note = '$9' AND b = $2`,
		},
		{
			"unterminated dollar quote runs to the end",
			`body = $$never closed $1`,
			`body = $$never closed $1`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := renumberPlaceholders(tc.in, 1); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}

// A $N inside a comment is not a placeholder either. Renumbering it
// consumed a positional index and shifted every real placeholder after it,
// so the statement asked PostgreSQL for a parameter the caller never bound
// ("could not determine data type of parameter $1").
func TestRenumberSkipsComments(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{
			"block comment",
			`active = TRUE /* keep $1 literal */ AND id = $1`,
			`active = TRUE /* keep $1 literal */ AND id = $1`,
		},
		{
			"line comment",
			"active = TRUE -- keep $1 literal\n AND id = $1",
			"active = TRUE -- keep $1 literal\n AND id = $1",
		},
		{
			// PostgreSQL block comments nest.
			"nested block comment",
			`a = $1 /* outer /* inner $9 */ still comment */ AND b = $1`,
			`a = $1 /* outer /* inner $9 */ still comment */ AND b = $2`,
		},
		{
			"unterminated line comment runs to the end",
			"id = $1 -- trailing $9",
			"id = $1 -- trailing $9",
		},
		{
			// A bare minus or slash is arithmetic, not a comment.
			"arithmetic is untouched",
			`qty - $1 > total / $1`,
			`qty - $1 > total / $2`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := renumberPlaceholders(tc.in, 1); got != tc.want {
				t.Errorf("got  %s\nwant %s", got, tc.want)
			}
		})
	}
}
