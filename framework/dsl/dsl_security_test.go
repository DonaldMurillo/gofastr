package dsl

import "testing"

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
