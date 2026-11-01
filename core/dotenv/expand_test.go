package dotenv_test

import (
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/dotenv"
)

func TestExpand_BasicSubstitution(t *testing.T) {
	got := dotenv.Expand("hello ${NAME}", map[string]string{"NAME": "world"}, nil)
	mustEq(t, got, "hello world")
}

func TestExpand_BareDollarVarNotExpanded(t *testing.T) {
	// Bracket form ONLY. Bare $VAR is intentionally left verbatim.
	got := dotenv.Expand("$NAME and ${NAME}", map[string]string{"NAME": "world"}, nil)
	mustEq(t, got, "$NAME and world")
}

func TestExpand_UndefinedIsEmpty(t *testing.T) {
	got := dotenv.Expand("x=${MISSING}y", nil, nil)
	mustEq(t, got, "x=y")
}

func TestExpand_PrefersLocalOverEnv(t *testing.T) {
	envFn := func(k string) (string, bool) {
		if k == "X" {
			return "from-env", true
		}
		return "", false
	}
	got := dotenv.Expand("${X}", map[string]string{"X": "from-local"}, envFn)
	mustEq(t, got, "from-local")
}

func TestExpand_FallsBackToEnv(t *testing.T) {
	envFn := func(k string) (string, bool) {
		if k == "X" {
			return "from-env", true
		}
		return "", false
	}
	got := dotenv.Expand("${X}", nil, envFn)
	mustEq(t, got, "from-env")
}

// A looked-up value goes in as stored: it is not expanded a second
// time, so a self-reference or a cycle has nothing to loop on.
func TestExpand_StoredValueNotReexpanded(t *testing.T) {
	local := map[string]string{
		"BIN":  "/usr/bin",
		"PATH": "${BIN}:/local",
		"A":    "${A}",
	}
	mustEq(t, dotenv.Expand("${PATH}", local, nil), "${BIN}:/local")
	mustEq(t, dotenv.Expand("[${A}]", local, nil), "[${A}]")
}

func TestExpand_MalformedUnclosedLeftVerbatim(t *testing.T) {
	got := dotenv.Expand("prefix ${OOPS", map[string]string{"OOPS": "ignored"}, nil)
	mustEq(t, got, "prefix ${OOPS")
}

func TestExpand_EmptyBraceLeftVerbatim(t *testing.T) {
	got := dotenv.Expand("a${}b", nil, nil)
	mustEq(t, got, "a${}b")
}

// PARSER INTEGRATION: confirm parsed double-quoted values expand
// against earlier keys in the same file.

func TestParse_DoubleQuotedExpandsLocal(t *testing.T) {
	in := `BASE=hello
GREET="${BASE} world"
`
	got, err := dotenv.Parse(strings.NewReader(in))
	mustOK(t, err)
	mustEq(t, got["GREET"], "hello world")
}

func TestParse_SingleQuotedDoesNotExpand(t *testing.T) {
	in := `BASE=hello
GREET='${BASE} world'
`
	got, err := dotenv.Parse(strings.NewReader(in))
	mustOK(t, err)
	mustEq(t, got["GREET"], "${BASE} world")
}

func TestParse_EscapedDollarLiteral(t *testing.T) {
	// \$ in a double-quoted value should produce a literal $ and NOT
	// trigger expansion at that position.
	in := `BASE=hello
GREET="\${BASE} stays literal"
`
	got, err := dotenv.Parse(strings.NewReader(in))
	mustOK(t, err)
	mustEq(t, got["GREET"], "${BASE} stays literal")
}

// A referenced value is already resolved: it goes in as stored, so a
// single-quoted literal or an escaped \$ stays literal when another line
// pulls it in.
func TestParse_RefKeepsStoredValue(t *testing.T) {
	in := `BASE=hello
PW='a${BASE}c'
DSN="postgres://u:${PW}@h/db"
GREET="\${BASE}"
MSG="${GREET}!"
`
	got, err := dotenv.Parse(strings.NewReader(in))
	mustOK(t, err)
	mustEq(t, got["DSN"], "postgres://u:a${BASE}c@h/db")
	mustEq(t, got["MSG"], "${BASE}!")
}

// An escaped backslash is a literal backslash; the ${...} after it still
// expands.
func TestParse_BackslashThenRefExpands(t *testing.T) {
	in := `DIR=tools
P="C:\\${DIR}"
`
	got, err := dotenv.Parse(strings.NewReader(in))
	mustOK(t, err)
	mustEq(t, got["P"], `C:\tools`)
}

// An environment value goes in verbatim, the way a shell substitutes it.
func TestExpand_EnvValueVerbatim(t *testing.T) {
	env := func(k string) (string, bool) { return "$y${z}", k == "X" }
	got := dotenv.Expand("[${X}]", map[string]string{"z": "boom"}, env)
	mustEq(t, got, "[$y${z}]")
}

// Each double-quoted escape reads as one character; any other character
// after a backslash keeps the backslash.
func TestParse_DoubleQuotedEscapes(t *testing.T) {
	for in, want := range map[string]string{
		`V="a\tb"`:       "a\tb",
		`V="a\rb"`:       "a\rb",
		`V="a\nb"`:       "a\nb",
		`V="a\\b"`:       `a\b`,
		`V="say \"hi\""`: `say "hi"`,
		`V="a\qb"`:       `a\qb`,
		`V="${open \t"`:  "${open \t",
	} {
		got, err := dotenv.Parse(strings.NewReader(in + "\n"))
		mustOK(t, err)
		mustEq(t, got["V"], want)
	}
}

// A double-quoted value ending in an unpaired backslash is refused.
func TestParse_TrailingBackslashRefused(t *testing.T) {
	if _, err := dotenv.Parse(strings.NewReader(`V="a\"` + "\n")); err == nil {
		t.Fatal(`V="a\" parsed; want an error`)
	}
}

// A tab after export is a separator too.
func TestParse_ExportTabPrefix(t *testing.T) {
	got, err := dotenv.Parse(strings.NewReader("export\tV=1\n"))
	mustOK(t, err)
	mustEq(t, got["V"], "1")
}
