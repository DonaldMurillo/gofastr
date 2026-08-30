package dotenv

import (
	"strings"
	"testing"
)

// Property: a parse error reports WHERE the file is wrong without echoing
// WHAT the file contains. A .env is by definition secrets; the error
// string travels to callers that log it verbatim (host apps wrap
// LoadAndApply in log.Fatal), so a malformed line's payload must not ride
// along into the log.
//
// The missing-'=' branch quoted the whole raw line. The realistic shape is
// a secret typed with a space or a missing equals, exactly the line whose
// value must not reach stderr.
func TestParseErrorDoesNotEchoValue(t *testing.T) {
	cases := []struct {
		name string
		in   string
	}{
		{"missing equals leaks the secret", "API_TOKEN sk-live-QQ77zzWWvvUU11223344\n"},
		{"space instead of equals", "PASSWORD hunter2 with spaces\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse(strings.NewReader(tc.in))
			if err == nil {
				t.Fatalf("expected a parse error for %q", tc.in)
			}
			// The raw payload (everything after the key) must not appear.
			if strings.Contains(err.Error(), "sk-live-QQ77") ||
				strings.Contains(err.Error(), "hunter2") {
				t.Errorf("SECURITY: [dotenv] parse error echoes file content: %v", err)
			}
		})
	}
}

// The happy-path error surfaces that already avoid echoing: an invalid key
// names the key only, an unterminated quote names the shape only. Pin them
// so a refactor cannot regress the branches that are already clean.
func TestParseErrorNamesShapeNotContent(t *testing.T) {
	_, err := Parse(strings.NewReader("BAD-KEY=\"some long secret value\""))
	if err == nil {
		t.Fatal("expected invalid-key error")
	}
	if strings.Contains(err.Error(), "some long secret value") {
		t.Errorf("invalid-key error echoes the value: %v", err)
	}

	_, err = Parse(strings.NewReader("MSG=\"never closed\n"))
	if err == nil {
		t.Fatal("expected unterminated-quote error")
	}
	if strings.Contains(err.Error(), "never closed") {
		t.Errorf("unterminated-quote error echoes the value: %v", err)
	}
}

// bombEnv builds A=a followed by k lines where each key's double-quoted
// value references the PREVIOUS key three times. Each ${PREV} lookup is
// depth <= 2 (the stored value is already fully expanded), so neither
// maxExpandDepth nor the cycle detector in expand.go applies; only the
// cross-line compounding grows the output: 3^k.
func bombEnv(k int) string {
	lines := make([]string, 0, k+1)
	lines = append(lines, "A=a")
	for i := 1; i <= k; i++ {
		ref := "${" + string(rune('A'+i-1)) + "}"
		lines = append(lines, string(rune('A'+i))+`="`+strings.Repeat(ref, 3)+`"`)
	}
	return strings.Join(lines, "\n")
}

// Property: cumulative ${VAR} expansion across lines is bounded by a
// sane multiple of the input size. Parse (dotenv.go:74,78) expands each
// double-quoted value against `out` — already-EXPANDED earlier keys —
// and stores the expanded result back, so N refs per line over k lines
// compounds to N^k total output with no budget anywhere in the package
// (a billion-laughs file whose every line is well under the 1 MiB
// scanner cap). Every GoFastr app boot runs this parser on .env files
// from the process CWD (framework/app.go:1464), so an attacker-
// influenced .env hangs or OOMs app start.
//
// The fixture is deliberately tiny: 12 lines x 3 refs = 3^12 = 531,441
// bytes final value from a ~210-byte file (~3,800x). Today Parse
// returns no error and ~800 KB of values; the 20-line/1000-ref variant
// is the OOM. Acceptable outcomes: an expansion-budget error, or total
// expanded bytes <= 10x input (a linear chain of legitimate references
// stays under 3x, so 10x is generous headroom).
func TestParseExpansionOutputBounded(t *testing.T) {
	in := bombEnv(12)
	out, err := Parse(strings.NewReader(in))
	if err != nil {
		return // budget-exceeded error is an acceptable outcome
	}
	total := 0
	for _, v := range out {
		total += len(v)
	}
	if total > 10*len(in) {
		t.Errorf("SECURITY: [dotenv] Parse expanded a %d-byte file into %d bytes of values (final key %d bytes) with no error — cross-line ${VAR} compounding is unbounded (dotenv.go:74-78); expected a budget error or <= 10x input",
			len(in), total, len(out[string(rune('A'+12))]))
	}
}

// False-positive guard: a linear chain of legitimate single references
// must still expand fully. A budget fix must not ban referencing
// earlier keys outright.
func TestParseLinearChainStillExpands(t *testing.T) {
	out, err := Parse(strings.NewReader("A=hello\nB=\"${A} world\"\nC=\"${B}!\"\n"))
	if err != nil {
		t.Fatalf("legitimate reference chain must parse: %v", err)
	}
	if out["C"] != "hello world!" {
		t.Errorf("chained expansion broken: C=%q", out["C"])
	}
}
