package dsl

import (
	"strings"
	"testing"
)

// TestParseDSL_AfterStripsControlBytes pins that ParseDSL never leaves
// raw CR/LF/NUL inside a cursor literal. The cursor is opaque — its
// internal validation happens in framework/crud — but the parse-time
// scrub makes sure a CR/LF in after() can't reach a log line or a
// re-emitted DSL string.
func TestParseDSL_AfterStripsControlBytes(t *testing.T) {
	for _, payload := range []string{"foo\rbar", "foo\nbar", "foo\x00bar", "foo\x1bbar"} {
		t.Run(payload, func(t *testing.T) {
			q, err := ParseDSL(`Post.after("` + payload + `")`)
			if err != nil {
				t.Fatalf("ParseDSL: %v", err)
			}
			if strings.ContainsAny(q.After, "\r\n\x00\x1b") {
				t.Fatalf("after() retained control bytes: %q", q.After)
			}
		})
	}
}

func TestParseDSL_RejectsExcessiveLimit(t *testing.T) {
	if _, err := ParseDSL("Post.limit(9999999)"); err == nil {
		t.Fatal("SECURITY: [dsl] excessive limit accepted. Attack: unbounded query size via DSL limit().")
	}
}

func TestParseDSL_RejectsUnsafeIncludeName(t *testing.T) {
	if _, err := ParseDSL("Post.include('; DROP TABLE posts; --')"); err == nil {
		t.Fatal("SECURITY: [dsl] unsafe include name accepted. Attack: metachar-bearing relation names survive parser.")
	}
}

func TestParseDSL_RejectsUnsafeEntityName(t *testing.T) {
	if _, err := ParseDSL("Posts; DROP TABLE users --.limit(1)"); err == nil {
		t.Fatal("SECURITY: [dsl] unsafe entity name accepted. Attack: parser accepts metachar-bearing entity identifiers.")
	}
}

func TestParseDSL_RejectsUnsafeFilterFieldName(t *testing.T) {
	if _, err := ParseDSL(`Post.where(name;DROP="x")`); err == nil {
		t.Fatal("SECURITY: [dsl] unsafe filter field name accepted. Attack: parser accepts metachar-bearing field identifiers.")
	}
}
