package sqlite

// TokenType represents the type of a SQL token.
type TokenType int

const (
	// Special tokens
	TokenEOF    TokenType = iota
	TokenError           // Lexer error

	// Literals
	TokenInteger // 123, 0x1F
	TokenFloat   // 3.14, 1.0e10
	TokenString  // 'hello', 'it''s'
	TokenBlob    // X'0123AB'

	// Identifiers
	TokenIdent   // unquoted identifier
	TokenQuotedID // "quoted identifier"

	// Operators
	TokenPlus     // +
	TokenMinus    // -
	TokenStar     // *
	TokenSlash    // /
	TokenPercent  // %
	TokenEqual    // =
	TokenDoubleEqual // == (treated as =)
	TokenNotEqual // != or <>
	TokenLess     // <
	TokenLessEq   // <=
	TokenGreater  // >
	TokenGreaterEq // >=
	TokenAnd      // AND keyword (but lexed as keyword)
	TokenOr       // OR keyword (but lexed as keyword)
	TokenNot      // NOT keyword
	TokenConcat   // ||

	// Punctuation
	TokenLParen   // (
	TokenRParen   // )
	TokenComma    // ,
	TokenDot      // .
	TokenSemicolon // ;
	TokenTilde    // ~ (bitwise NOT)

	// Keywords (a selection — we'll lex these as identifiers and check in parser)
	// We keep keyword tokens for better error messages
	TokenSELECT
	TokenFROM
	TokenWHERE
	TokenINSERT
	TokenINTO
	TokenVALUES
	TokenUPDATE
	TokenSET
	TokenDELETE
	TokenCREATE
	TokenTABLE
	TokenDROP
	TokenINDEX
	TokenIF
	TokenNOT
	TokenNULL
	TokenEXISTS
	TokenPRIMARY
	TokenKEY
	TokenUNIQUE
	TokenDEFAULT
	TokenCHECK
	TokenFOREIGN
	TokenREFERENCES
	TokenBEGIN
	TokenCOMMIT
	TokenROLLBACK
	TokenTRANSACTION
	TokenAND
	TokenOR
	TokenORDER
	TokenBY
	TokenASC
	TokenDESC
	TokenLIMIT
	TokenOFFSET
	TokenAS
	TokenON
	TokenLEFT
	TokenRIGHT
	TokenINNER
	TokenOUTER
	TokenJOIN
	TokenIN
	TokenIS
	TokenLIKE
	TokenGLOB
	TokenBETWEEN
	TokenCASE
	TokenWHEN
	TokenTHEN
	TokenELSE
	TokenEND
	TokenCAST
	TokenCOLLATE
	TokenDISTINCT
	TokenALL
	TokenHAVING
	TokenGROUP
	TokenUNION
	TokenINTERSECT
	TokenEXCEPT
	TokenINTEGER_KW
	TokenTEXT_KW
	TokenREAL_KW
	TokenBLOB_KW
	TokenAUTOINCREMENT
	TokenROWID
	TokenVACUUM
	TokenREINDEX
	TokenALTER
	TokenPRAGMA
	TokenVIEW
	TokenADD
	TokenCOLUMN
	TokenRENAME
	TokenTO
	TokenQuestion // ? parameter placeholder
)

// Token represents a lexical token in a SQL statement.
type Token struct {
	Type  TokenType
	Value string // Raw text of the token
	Pos   int    // Byte offset in the input
	Line  int    // Line number (1-based)
	Col   int    // Column number (1-based)
}

// String returns a human-readable representation of the token.
func (t Token) String() string {
	switch t.Type {
	case TokenEOF:
		return "EOF"
	case TokenError:
		return "ERROR: " + t.Value
	default:
		if t.Value != "" {
			return t.Value
		}
		return tokenTypeName(t.Type)
	}
}

func tokenTypeName(tt TokenType) string {
	switch tt {
	case TokenEOF:
		return "EOF"
	case TokenError:
		return "ERROR"
	case TokenInteger:
		return "INTEGER"
	case TokenFloat:
		return "FLOAT"
	case TokenString:
		return "STRING"
	case TokenBlob:
		return "BLOB"
	case TokenIdent:
		return "IDENT"
	case TokenQuotedID:
		return "QUOTED_ID"
	case TokenPlus:
		return "+"
	case TokenMinus:
		return "-"
	case TokenStar:
		return "*"
	case TokenSlash:
		return "/"
	case TokenPercent:
		return "%"
	case TokenEqual:
		return "="
	case TokenNotEqual:
		return "<>"
	case TokenLess:
		return "<"
	case TokenLessEq:
		return "<="
	case TokenGreater:
		return ">"
	case TokenGreaterEq:
		return ">="
	case TokenConcat:
		return "||"
	case TokenLParen:
		return "("
	case TokenRParen:
		return ")"
	case TokenComma:
		return ","
	case TokenDot:
		return "."
	case TokenSemicolon:
		return ";"
	case TokenTilde:
		return "~"
	case TokenSELECT:
		return "SELECT"
	case TokenFROM:
		return "FROM"
	case TokenWHERE:
		return "WHERE"
	case TokenINSERT:
		return "INSERT"
	case TokenINTO:
		return "INTO"
	case TokenVALUES:
		return "VALUES"
	case TokenUPDATE:
		return "UPDATE"
	case TokenSET:
		return "SET"
	case TokenDELETE:
		return "DELETE"
	case TokenCREATE:
		return "CREATE"
	case TokenTABLE:
		return "TABLE"
	case TokenDROP:
		return "DROP"
	case TokenINDEX:
		return "INDEX"
	case TokenAND:
		return "AND"
	case TokenOR:
		return "OR"
	case TokenNOT:
		return "NOT"
	case TokenNULL:
		return "NULL"
	case TokenORDER:
		return "ORDER"
	case TokenBY:
		return "BY"
	case TokenLIMIT:
		return "LIMIT"
	case TokenQuestion:
		return "?"
	default:
		return "UNKNOWN"
	}
}

// KeywordMap maps keyword strings (uppercase) to their token types.
var KeywordMap = map[string]TokenType{
	"SELECT":        TokenSELECT,
	"FROM":          TokenFROM,
	"WHERE":         TokenWHERE,
	"INSERT":        TokenINSERT,
	"INTO":          TokenINTO,
	"VALUES":        TokenVALUES,
	"UPDATE":        TokenUPDATE,
	"SET":           TokenSET,
	"DELETE":        TokenDELETE,
	"CREATE":        TokenCREATE,
	"TABLE":         TokenTABLE,
	"DROP":          TokenDROP,
	"INDEX":         TokenINDEX,
	"IF":            TokenIF,
	"NOT":           TokenNOT,
	"NULL":          TokenNULL,
	"EXISTS":        TokenEXISTS,
	"PRIMARY":       TokenPRIMARY,
	"KEY":           TokenKEY,
	"UNIQUE":        TokenUNIQUE,
	"DEFAULT":       TokenDEFAULT,
	"CHECK":         TokenCHECK,
	"FOREIGN":       TokenFOREIGN,
	"REFERENCES":    TokenREFERENCES,
	"BEGIN":         TokenBEGIN,
	"COMMIT":        TokenCOMMIT,
	"ROLLBACK":      TokenROLLBACK,
	"TRANSACTION":   TokenTRANSACTION,
	"AND":           TokenAND,
	"OR":            TokenOR,
	"ORDER":         TokenORDER,
	"BY":            TokenBY,
	"ASC":           TokenASC,
	"DESC":          TokenDESC,
	"LIMIT":         TokenLIMIT,
	"OFFSET":        TokenOFFSET,
	"AS":            TokenAS,
	"ON":            TokenON,
	"LEFT":          TokenLEFT,
	"RIGHT":         TokenRIGHT,
	"INNER":         TokenINNER,
	"OUTER":         TokenOUTER,
	"JOIN":          TokenJOIN,
	"IN":            TokenIN,
	"IS":            TokenIS,
	"LIKE":          TokenLIKE,
	"GLOB":          TokenGLOB,
	"BETWEEN":       TokenBETWEEN,
	"CASE":          TokenCASE,
	"WHEN":          TokenWHEN,
	"THEN":          TokenTHEN,
	"ELSE":          TokenELSE,
	"END":           TokenEND,
	"CAST":          TokenCAST,
	"COLLATE":       TokenCOLLATE,
	"DISTINCT":      TokenDISTINCT,
	"ALL":           TokenALL,
	"HAVING":        TokenHAVING,
	"GROUP":         TokenGROUP,
	"UNION":         TokenUNION,
	"INTERSECT":     TokenINTERSECT,
	"EXCEPT":        TokenEXCEPT,
	"INTEGER":       TokenINTEGER_KW,
	"TEXT":          TokenTEXT_KW,
	"REAL":          TokenREAL_KW,
	"BLOB":          TokenBLOB_KW,
	"AUTOINCREMENT": TokenAUTOINCREMENT,
	"ROWID":         TokenROWID,
	"VACUUM":        TokenVACUUM,
	"REINDEX":       TokenREINDEX,
	"ALTER":         TokenALTER,
	"PRAGMA":        TokenPRAGMA,
	"VIEW":          TokenVIEW,
	"ADD":           TokenADD,
	"COLUMN":        TokenCOLUMN,
	"RENAME":        TokenRENAME,
	"TO":            TokenTO,
}
