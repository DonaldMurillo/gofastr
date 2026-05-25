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
	// Bracket form ONLY — bare $VAR is intentionally left verbatim.
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

func TestExpand_NestedExpansion(t *testing.T) {
	local := map[string]string{
		"BIN":  "/usr/bin",
		"PATH": "${BIN}:/local",
	}
	got := dotenv.Expand("${PATH}", local, nil)
	mustEq(t, got, "/usr/bin:/local")
}

// HARDENING

func TestExpand_SelfReferenceTerminates(t *testing.T) {
	// A=${A} must not loop forever; the cycle yields empty for A and
	// the surrounding string keeps going.
	got := dotenv.Expand("[${A}]", map[string]string{"A": "${A}"}, nil)
	mustEq(t, got, "[]")
}

func TestExpand_MutualCycleTerminates(t *testing.T) {
	local := map[string]string{
		"A": "${B}",
		"B": "${A}",
	}
	got := dotenv.Expand("[${A}]", local, nil)
	mustEq(t, got, "[]")
}

func TestExpand_DeepChainBounded(t *testing.T) {
	// Build a chain longer than the depth cap; the cap should kick
	// in before stack-blowing, leaving the deeper refs verbatim.
	local := map[string]string{}
	for i := 0; i < 50; i++ {
		local["K"+strings.Repeat("X", i)] = "${K" + strings.Repeat("X", i+1) + "}"
	}
	// Add a terminator so a depth-bounded result is meaningful.
	local["K"+strings.Repeat("X", 50)] = "terminus"
	got := dotenv.Expand("${K}", local, nil)
	// We do NOT assert the exact result — only that the call returns
	// without panic / stack overflow. The depth cap leaves the
	// deepest unresolved name verbatim.
	if got == "" || strings.Contains(got, "terminus") {
		// Either is acceptable depending on depth cap; we just want
		// to confirm termination.
	}
}

func TestExpand_MalformedUnclosedLeftVerbatim(t *testing.T) {
	got := dotenv.Expand("prefix ${OOPS", map[string]string{"OOPS": "ignored"}, nil)
	mustEq(t, got, "prefix ${OOPS")
}

func TestExpand_EmptyBraceLeftVerbatim(t *testing.T) {
	got := dotenv.Expand("a${}b", nil, nil)
	mustEq(t, got, "a${}b")
}

// PARSER INTEGRATION — confirm parsed double-quoted values expand
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
