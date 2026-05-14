package sqlite

import (
	"testing"
)

// ============================================================================
// Basic tokenization tests
// ============================================================================

func TestLexEmpty(t *testing.T) {
	tokens, err := NewLexer("").Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0].Type != TokenEOF {
		t.Fatalf("expected [EOF], got %v", tokens)
	}
}

func TestLexWhitespace(t *testing.T) {
	tokens, err := NewLexer("   \t\n  ").Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	if len(tokens) != 1 || tokens[0].Type != TokenEOF {
		t.Fatalf("expected [EOF] from whitespace, got %v", tokens)
	}
}

// ============================================================================
// Number tokens
// ============================================================================

func TestLexInteger(t *testing.T) {
	tests := []struct {
		input  string
		value  string
		isFloat bool
	}{
		{"0", "0", false},
		{"42", "42", false},
		{"007", "007", false},
		{"999999", "999999", false},
		{"0x1F", "0x1F", false},
		{"0xFF", "0xFF", false},
		{"0x0", "0x0", false},
		{"0xABCDEF", "0xABCDEF", false},
	}

	for _, tt := range tests {
		tokens, err := NewLexer(tt.input).Tokenize()
		if err != nil {
			t.Errorf("lex %q: error: %v", tt.input, err)
			continue
		}
		expectedType := TokenInteger
		if tt.isFloat {
			expectedType = TokenFloat
		}
		if tokens[0].Type != expectedType {
			t.Errorf("lex %q: got %v, want %v", tt.input, tokens[0].Type, expectedType)
		}
		if tokens[0].Value != tt.value {
			t.Errorf("lex %q: got value %q, want %q", tt.input, tokens[0].Value, tt.value)
		}
	}
}

func TestLexFloat(t *testing.T) {
	tests := []struct {
		input string
		value string
	}{
		{"3.14", "3.14"},
		{"0.0", "0.0"},
		{".5", ".5"},
		{"1e10", "1e10"},
		{"1E10", "1E10"},
		{"1.5e-3", "1.5e-3"},
		{"1.5E+3", "1.5E+3"},
		{"1.0e0", "1.0e0"},
	}

	for _, tt := range tests {
		tokens, err := NewLexer(tt.input).Tokenize()
		if err != nil {
			t.Errorf("lex %q: error: %v", tt.input, err)
			continue
		}
		if tokens[0].Type != TokenFloat {
			t.Errorf("lex %q: got type %v, want TokenFloat", tt.input, tokens[0].Type)
		}
		if tokens[0].Value != tt.value {
			t.Errorf("lex %q: got value %q, want %q", tt.input, tokens[0].Value, tt.value)
		}
	}
}

// ============================================================================
// String tokens
// ============================================================================

func TestLexString(t *testing.T) {
	tests := []struct {
		input string
		value string
	}{
		{"'hello'", "hello"},
		{"''", ""},               // empty string
		{"'it''s'", "it's"},      // escaped quote
		{"'a''b''c'", "a'b'c"},   // multiple escaped quotes
	}

	for _, tt := range tests {
		tokens, err := NewLexer(tt.input).Tokenize()
		if err != nil {
			t.Errorf("lex %q: error: %v", tt.input, err)
			continue
		}
		if tokens[0].Type != TokenString {
			t.Errorf("lex %q: got type %v, want TokenString", tt.input, tokens[0].Type)
		}
		if tokens[0].Value != tt.value {
			t.Errorf("lex %q: got value %q, want %q", tt.input, tokens[0].Value, tt.value)
		}
	}
}

func TestLexUnterminatedString(t *testing.T) {
	_, err := NewLexer("'unterminated").Tokenize()
	if err == nil {
		t.Error("expected error for unterminated string")
	}
}

// ============================================================================
// Blob tokens
// ============================================================================

func TestLexBlob(t *testing.T) {
	tests := []struct {
		input string
		value string
	}{
		{"X''", "X''"},
		{"X'0123'", "X'0123'"},
		{"X'DEADBEEF'", "X'DEADBEEF'"},
		{"x'abcd'", "X'abcd'"},
	}

	for _, tt := range tests {
		tokens, err := NewLexer(tt.input).Tokenize()
		if err != nil {
			t.Errorf("lex %q: error: %v", tt.input, err)
			continue
		}
		if tokens[0].Type != TokenBlob {
			t.Errorf("lex %q: got type %v, want TokenBlob", tt.input, tokens[0].Type)
		}
		if tokens[0].Value != tt.value {
			t.Errorf("lex %q: got value %q, want %q", tt.input, tokens[0].Value, tt.value)
		}
	}
}

// ============================================================================
// Identifier and keyword tokens
// ============================================================================

func TestLexKeywords(t *testing.T) {
	keywords := []struct {
		input     string
		tokenType TokenType
	}{
		{"SELECT", TokenSELECT},
		{"select", TokenSELECT},
		{"Select", TokenSELECT},
		{"FROM", TokenFROM},
		{"WHERE", TokenWHERE},
		{"INSERT", TokenINSERT},
		{"INTO", TokenINTO},
		{"VALUES", TokenVALUES},
		{"UPDATE", TokenUPDATE},
		{"SET", TokenSET},
		{"DELETE", TokenDELETE},
		{"CREATE", TokenCREATE},
		{"TABLE", TokenTABLE},
		{"DROP", TokenDROP},
		{"INDEX", TokenINDEX},
		{"BEGIN", TokenBEGIN},
		{"COMMIT", TokenCOMMIT},
		{"ROLLBACK", TokenROLLBACK},
		{"AND", TokenAND},
		{"OR", TokenOR},
		{"NOT", TokenNOT},
		{"NULL", TokenNULL},
		{"ORDER", TokenORDER},
		{"BY", TokenBY},
		{"LIMIT", TokenLIMIT},
	}

	for _, tt := range keywords {
		tokens, err := NewLexer(tt.input).Tokenize()
		if err != nil {
			t.Errorf("lex %q: error: %v", tt.input, err)
			continue
		}
		if tokens[0].Type != tt.tokenType {
			t.Errorf("lex %q: got type %v, want %v", tt.input, tokens[0].Type, tt.tokenType)
		}
	}
}

func TestLexIdentifiers(t *testing.T) {
	tests := []struct {
		input string
		value string
	}{
		{"foo", "foo"},
		{"_bar", "_bar"},
		{"my_table", "my_table"},
		{"CamelCase", "CamelCase"},
		{"abc123", "abc123"},
	}

	for _, tt := range tests {
		tokens, err := NewLexer(tt.input).Tokenize()
		if err != nil {
			t.Errorf("lex %q: error: %v", tt.input, err)
			continue
		}
		if tokens[0].Type != TokenIdent {
			t.Errorf("lex %q: got type %v, want TokenIdent", tt.input, tokens[0].Type)
		}
		if tokens[0].Value != tt.value {
			t.Errorf("lex %q: got value %q, want %q", tt.input, tokens[0].Value, tt.value)
		}
	}
}

func TestLexQuotedIdentifiers(t *testing.T) {
	tests := []struct {
		input string
		value string
	}{
		{`"foo"`, "foo"},
		{`"foo bar"`, "foo bar"},
		{`"foo""bar"`, `foo"bar`},
		{"`foo`", "foo"},
		{"`foo bar`", "foo bar"},
		{"[foo]", "foo"},
		{"[foo bar]", "foo bar"},
	}

	for _, tt := range tests {
		tokens, err := NewLexer(tt.input).Tokenize()
		if err != nil {
			t.Errorf("lex %q: error: %v", tt.input, err)
			continue
		}
		// All non-keyword quoted identifiers should be TokenIdent or TokenQuotedID
		tok := tokens[0]
		if tok.Type != TokenIdent && tok.Type != TokenQuotedID {
			t.Errorf("lex %q: got type %v", tt.input, tok.Type)
		}
		if tok.Value != tt.value {
			t.Errorf("lex %q: got value %q, want %q", tt.input, tok.Value, tt.value)
		}
	}
}

// ============================================================================
// Operator tokens
// ============================================================================

func TestLexOperators(t *testing.T) {
	tests := []struct {
		input     string
		tokenType TokenType
		value     string
	}{
		{"+", TokenPlus, "+"},
		{"-", TokenMinus, "-"},
		{"*", TokenStar, "*"},
		{"/", TokenSlash, "/"},
		{"%", TokenPercent, "%"},
		{"=", TokenEqual, "="},
		{"==", TokenEqual, "=="},
		{"!=", TokenNotEqual, "!="},
		{"<>", TokenNotEqual, "<>"},
		{"<", TokenLess, "<"},
		{"<=", TokenLessEq, "<="},
		{">", TokenGreater, ">"},
		{">=", TokenGreaterEq, ">="},
		{"||", TokenConcat, "||"},
		{"(", TokenLParen, "("},
		{")", TokenRParen, ")"},
		{",", TokenComma, ","},
		{".", TokenDot, "."},
		{";", TokenSemicolon, ";"},
		{"~", TokenTilde, "~"},
	}

	for _, tt := range tests {
		tokens, err := NewLexer(tt.input).Tokenize()
		if err != nil {
			t.Errorf("lex %q: error: %v", tt.input, err)
			continue
		}
		if tokens[0].Type != tt.tokenType {
			t.Errorf("lex %q: got type %v, want %v", tt.input, tokens[0].Type, tt.tokenType)
		}
		if tokens[0].Value != tt.value {
			t.Errorf("lex %q: got value %q, want %q", tt.input, tokens[0].Value, tt.value)
		}
	}
}

// ============================================================================
// Comment tests
// ============================================================================

func TestLexLineComment(t *testing.T) {
	input := "SELECT -- this is a comment\n42"
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	expectedTypes := []TokenType{TokenSELECT, TokenInteger, TokenEOF}
	for i, tt := range expectedTypes {
		if i >= len(tokens) {
			t.Fatalf("missing token at position %d", i)
		}
		if tokens[i].Type != tt {
			t.Errorf("token %d: got %v, want %v", i, tokens[i].Type, tt)
		}
	}
}

func TestLexBlockComment(t *testing.T) {
	input := "SELECT /* block\ncomment */ 42"
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	expectedTypes := []TokenType{TokenSELECT, TokenInteger, TokenEOF}
	for i, tt := range expectedTypes {
		if i >= len(tokens) {
			t.Fatalf("missing token at position %d", i)
		}
		if tokens[i].Type != tt {
			t.Errorf("token %d: got %v, want %v", i, tokens[i].Type, tt)
		}
	}
}

func TestLexNestedBlockComment(t *testing.T) {
	input := "SELECT /* outer /* inner */ still comment */ 42"
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	if tokens[0].Type != TokenSELECT {
		t.Errorf("token 0: got %v, want SELECT", tokens[0].Type)
	}
	if tokens[1].Type != TokenInteger {
		t.Errorf("token 1: got %v, want INTEGER", tokens[1].Type)
	}
}

// ============================================================================
// Position tracking
// ============================================================================

func TestLexPositionTracking(t *testing.T) {
	input := "SELECT\n  42"
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatal(err)
	}

	// SELECT should be at line 1, col 1
	if tokens[0].Line != 1 || tokens[0].Col != 1 {
		t.Errorf("SELECT position: got line=%d col=%d, want 1,1", tokens[0].Line, tokens[0].Col)
	}

	// 42 should be at line 2, col 3
	if tokens[1].Line != 2 || tokens[1].Col != 3 {
		t.Errorf("42 position: got line=%d col=%d, want 2,3", tokens[1].Line, tokens[1].Col)
	}
}

// ============================================================================
// Complex statement tests
// ============================================================================

func TestLexSelectStatement(t *testing.T) {
	input := "SELECT id, name FROM users WHERE age > 18 ORDER BY name LIMIT 10"
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatal(err)
	}

	expected := []TokenType{
		TokenSELECT, TokenIdent, TokenComma, TokenIdent,
		TokenFROM, TokenIdent,
		TokenWHERE, TokenIdent, TokenGreater, TokenInteger,
		TokenORDER, TokenBY, TokenIdent,
		TokenLIMIT, TokenInteger,
		TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(expected), tokenTypes(tokens))
	}

	for i, tt := range expected {
		if tokens[i].Type != tt {
			t.Errorf("token %d: got %v (%q), want %v", i, tokens[i].Type, tokens[i].Value, tt)
		}
	}
}

func TestLexInsertStatement(t *testing.T) {
	input := "INSERT INTO users (name, age) VALUES ('Alice', 30)"
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatal(err)
	}

	expected := []TokenType{
		TokenINSERT, TokenINTO, TokenIdent,
		TokenLParen, TokenIdent, TokenComma, TokenIdent, TokenRParen,
		TokenVALUES, TokenLParen,
		TokenString, TokenComma, TokenInteger,
		TokenRParen, TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(expected), tokenTypes(tokens))
	}

	for i, tt := range expected {
		if tokens[i].Type != tt {
			t.Errorf("token %d: got %v (%q), want %v", i, tokens[i].Type, tokens[i].Value, tt)
		}
	}
}

func TestLexCreateTable(t *testing.T) {
	input := "CREATE TABLE IF NOT EXISTS users (id INTEGER PRIMARY KEY, name TEXT NOT NULL)"
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatal(err)
	}

	expected := []TokenType{
		TokenCREATE, TokenTABLE, TokenIF, TokenNOT, TokenEXISTS, TokenIdent,
		TokenLParen,
		TokenIdent, TokenINTEGER_KW, TokenPRIMARY, TokenKEY,
		TokenComma,
		TokenIdent, TokenTEXT_KW, TokenNOT, TokenNULL,
		TokenRParen,
		TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d: %v", len(tokens), len(expected), tokenTypes(tokens))
	}

	for i, tt := range expected {
		if tokens[i].Type != tt {
			t.Errorf("token %d: got %v (%q), want %v", i, tokens[i].Type, tokens[i].Value, tt)
		}
	}
}

func TestLexTransaction(t *testing.T) {
	input := "BEGIN TRANSACTION; COMMIT; ROLLBACK"
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatal(err)
	}

	expected := []TokenType{
		TokenBEGIN, TokenTRANSACTION, TokenSemicolon,
		TokenCOMMIT, TokenSemicolon,
		TokenROLLBACK, TokenEOF,
	}

	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expected))
	}

	for i, tt := range expected {
		if tokens[i].Type != tt {
			t.Errorf("token %d: got %v, want %v", i, tokens[i].Type, tt)
		}
	}
}

// ============================================================================
// Edge case tests
// ============================================================================

func TestLexXWithoutQuote(t *testing.T) {
	// "X" followed by non-quote should be identifier
	input := "X"
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	if tokens[0].Type != TokenIdent {
		t.Errorf("got %v, want TokenIdent", tokens[0].Type)
	}
}

func TestLexMultipleStatements(t *testing.T) {
	input := "SELECT 1; SELECT 2;"
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	semicolonCount := 0
	for _, tok := range tokens {
		if tok.Type == TokenSemicolon {
			semicolonCount++
		}
	}
	if semicolonCount != 2 {
		t.Errorf("got %d semicolons, want 2", semicolonCount)
	}
}

func TestLexAdjacentOperators(t *testing.T) {
	input := "<=<>!=<>"
	tokens, err := NewLexer(input).Tokenize()
	if err != nil {
		t.Fatal(err)
	}
	expected := []TokenType{TokenLessEq, TokenNotEqual, TokenNotEqual, TokenNotEqual, TokenEOF}
	if len(tokens) != len(expected) {
		t.Fatalf("got %d tokens, want %d", len(tokens), len(expected))
	}
	for i, tt := range expected {
		if tokens[i].Type != tt {
			t.Errorf("token %d: got %v, want %v", i, tokens[i].Type, tt)
		}
	}
}

func TestLexUnexpectedCharacter(t *testing.T) {
	_, err := NewLexer("^").Tokenize()
	if err == nil {
		t.Error("expected error for unexpected character ^")
	}
}

// ============================================================================
// Helpers
// ============================================================================

func tokenTypes(tokens []Token) []TokenType {
	types := make([]TokenType, len(tokens))
	for i, t := range tokens {
		types[i] = t.Type
	}
	return types
}
