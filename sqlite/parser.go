package sqlite

import (
	"strconv"
	"strings"
)

// Parser parses SQL statements into AST nodes.
type Parser struct {
	lexer         *Lexer
	cur           Token
	peek          Token
	param         int // auto-incrementing parameter index
	compoundDepth int // when > 0, parseSelect skips ORDER BY/LIMIT
}

// NewParser creates a new Parser for the given SQL string.
func NewParser(sql string) *Parser {
	l := NewLexer(sql)
	p := &Parser{lexer: l}
	// Prime the two-token lookahead
	p.advance()
	p.advance()
	return p
}

// advance moves to the next token.
func (p *Parser) advance() {
	p.cur = p.peek
	p.peek = p.lexer.Next()
}

// expect checks that the current token is of the given type and advances.
func (p *Parser) expect(t TokenType) (Token, error) {
	if p.cur.Type != t {
		return Token{}, p.errorf("expected %s, got %s (%q)", tokenTypeName(t), tokenTypeName(p.cur.Type), p.cur.Value)
	}
	tok := p.cur
	p.advance()
	return tok, nil
}

// match returns true if the current token type is one of the given types.
func (p *Parser) match(types ...TokenType) bool {
	for _, t := range types {
		if p.cur.Type == t {
			return true
		}
	}
	return false
}

// matchKeyword checks if cur is a keyword token matching one of the given types.
func (p *Parser) matchKeyword(types ...TokenType) bool {
	for _, t := range types {
		if p.cur.Type == t {
			return true
		}
	}
	return false
}

// curIsIdent checks if cur is an identifier (TokenIdent or TokenQuotedID).
func (p *Parser) curIsIdent() bool {
	return p.cur.Type == TokenIdent || p.cur.Type == TokenQuotedID
}

// curIdentValue returns the identifier value for the current token.
func (p *Parser) curIdentValue() string {
	return p.cur.Value
}

// curIsKeywordIdent checks if the current token is a keyword that can be used as an identifier
// in certain contexts (e.g., table names, column names).
func (p *Parser) curIsKeywordIdent() bool {
	// Many keywords can double as identifiers in SQLite
	switch p.cur.Type {
	case TokenSELECT, TokenFROM, TokenWHERE, TokenINSERT, TokenINTO, TokenVALUES,
		TokenUPDATE, TokenSET, TokenDELETE, TokenCREATE, TokenTABLE, TokenDROP,
		TokenINDEX, TokenIF, TokenNOT, TokenNULL, TokenEXISTS, TokenPRIMARY,
		TokenKEY, TokenUNIQUE, TokenDEFAULT, TokenCHECK, TokenFOREIGN,
		TokenREFERENCES, TokenBEGIN, TokenCOMMIT, TokenROLLBACK, TokenTRANSACTION,
		TokenAND, TokenOR, TokenORDER, TokenBY, TokenASC, TokenDESC, TokenLIMIT,
		TokenOFFSET, TokenAS, TokenON, TokenLEFT, TokenRIGHT, TokenINNER,
		TokenOUTER, TokenJOIN, TokenIN, TokenIS, TokenLIKE, TokenGLOB,
		TokenBETWEEN, TokenCASE, TokenWHEN, TokenTHEN, TokenELSE, TokenEND,
		TokenCAST, TokenCOLLATE, TokenDISTINCT, TokenALL, TokenHAVING,
		TokenGROUP, TokenUNION, TokenINTERSECT, TokenEXCEPT,
		TokenINTEGER_KW, TokenTEXT_KW, TokenREAL_KW, TokenBLOB_KW,
		TokenAUTOINCREMENT, TokenROWID, TokenVACUUM, TokenREINDEX,
		TokenALTER, TokenADD, TokenCOLUMN, TokenRENAME, TokenTO, TokenPRAGMA, TokenVIEW:
		return true
	}
	return false
}

// parseIdentifierOrKeyword returns the current token as an identifier string,
// handling both TokenIdent and keyword tokens that can be identifiers.
func (p *Parser) parseIdentifierOrKeyword() (string, error) {
	if p.curIsIdent() {
		v := p.cur.Value
		p.advance()
		return v, nil
	}
	if p.curIsKeywordIdent() {
		v := p.cur.Value
		p.advance()
		return v, nil
	}
	return "", p.errorf("expected identifier, got %s (%q)", tokenTypeName(p.cur.Type), p.cur.Value)
}

// errorf creates a parse error.
func (p *Parser) errorf(format string, args ...interface{}) error {
	msg := format
	if len(args) > 0 {
		// Simple fmt.Sprintf replacement
		msg = sprintf(format, args...)
	}
	return &ParseError{Msg: msg, Line: p.cur.Line, Col: p.cur.Col}
}

// ParseError represents a parse error.
type ParseError struct {
	Msg  string
	Line int
	Col  int
}

func (e *ParseError) Error() string {
	return sprintf("%d:%d: %s", e.Line, e.Col, e.Msg)
}

// sprintf is a minimal sprintf for parser error messages.
func sprintf(format string, args ...interface{}) string {
	var buf strings.Builder
	argIdx := 0
	for i := 0; i < len(format); i++ {
		if format[i] == '%' && i+1 < len(format) {
			i++
			switch format[i] {
			case 's':
				if argIdx < len(args) {
					if s, ok := args[argIdx].(string); ok {
						buf.WriteString(s)
					}
					argIdx++
				}
			case 'd':
				if argIdx < len(args) {
					switch v := args[argIdx].(type) {
					case int:
						buf.WriteString(formatInt64(int64(v)))
					case int64:
						buf.WriteString(formatInt64(v))
					}
					argIdx++
				}
			case 'q':
				if argIdx < len(args) {
					if s, ok := args[argIdx].(string); ok {
						buf.WriteByte('\'')
						buf.WriteString(s)
						buf.WriteByte('\'')
					}
					argIdx++
				}
			case '%':
				buf.WriteByte('%')
			default:
				buf.WriteByte('%')
				buf.WriteByte(format[i])
			}
		} else {
			buf.WriteByte(format[i])
		}
	}
	return buf.String()
}

// ============================================================================
// Top-level parsing
// ============================================================================

// Parse parses a single SQL statement.
func (p *Parser) Parse() (Statement, error) {
	stmt, err := p.parseStatement()
	if err != nil {
		return nil, err
	}
	if p.cur.Type == TokenSemicolon {
		p.advance()
	}
	if p.cur.Type != TokenEOF {
		return nil, p.errorf("unexpected trailing token %s (%q)", tokenTypeName(p.cur.Type), p.cur.Value)
	}
	return stmt, nil
}

// ParseAll parses all SQL statements separated by semicolons.
func (p *Parser) ParseAll() ([]Statement, error) {
	var stmts []Statement
	for p.cur.Type != TokenEOF {
		// Skip semicolons between statements
		if p.cur.Type == TokenSemicolon {
			p.advance()
			continue
		}
		stmt, err := p.parseStatement()
		if err != nil {
			return stmts, err
		}
		stmts = append(stmts, stmt)
		// Consume optional semicolon
		if p.cur.Type == TokenSemicolon {
			p.advance()
		}
	}
	return stmts, nil
}

// parseStatement dispatches to the appropriate statement parser.
func (p *Parser) parseStatement() (Statement, error) {
	switch p.cur.Type {
	case TokenSELECT:
		return p.parseSelectCompound()
	case TokenINSERT:
		return p.parseInsert()
	case TokenUPDATE:
		return p.parseUpdate()
	case TokenDELETE:
		return p.parseDelete()
	case TokenCREATE:
		return p.parseCreate()
	case TokenDROP:
		return p.parseDrop()
	case TokenALTER:
		return p.parseAlter()
	case TokenPRAGMA:
		return p.parsePragma()
	case TokenVACUUM:
		p.advance()
		return &VacuumStmt{}, nil
	case TokenREINDEX:
		return p.parseReindex()
	case TokenBEGIN:
		return p.parseBegin()
	case TokenCOMMIT:
		return p.parseCommit()
	case TokenROLLBACK:
		return p.parseRollback()
	default:
		return nil, p.errorf("unexpected token %s (%q)", tokenTypeName(p.cur.Type), p.cur.Value)
	}
}

// ============================================================================
// SELECT
// ============================================================================

// parseSelectCompound parses a SELECT possibly followed by UNION/INTERSECT/EXCEPT.
func (p *Parser) parseSelectCompound() (Statement, error) {
	leftStmt, err := p.parseSelect()
	if err != nil {
		return nil, err
	}
	var left Statement = leftStmt

	for {
		var op SetOp
		switch p.cur.Type {
		case TokenUNION:
			p.advance()
			if p.cur.Type == TokenALL {
				p.advance()
				op = SetOpUnionAll
			} else {
				op = SetOpUnion
			}
		case TokenINTERSECT:
			p.advance()
			op = SetOpIntersect
		case TokenEXCEPT:
			p.advance()
			op = SetOpExcept
		default:
			return left, nil
		}

		p.compoundDepth++
		right, err := p.parseSelect()
		p.compoundDepth--
		if err != nil {
			return nil, err
		}

		cs := &CompoundSelect{Left: left, Right: right, Op: op}

		// ORDER BY on compound
		if p.cur.Type == TokenORDER {
			p.advance()
			if _, err := p.expect(TokenBY); err != nil {
				return nil, err
			}
			orderBy, err := p.parseOrderBy()
			if err != nil {
				return nil, err
			}
			cs.OrderBy = orderBy
		}

		// LIMIT on compound
		if p.cur.Type == TokenLIMIT {
			p.advance()
			limit, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			cs.Limit = limit
			if p.cur.Type == TokenOFFSET {
				p.advance()
				offset, err := p.parseExpression()
				if err != nil {
					return nil, err
				}
				cs.Offset = offset
			}
		}

		left = cs
	}
}

func (p *Parser) parseSelect() (*SelectStmt, error) {
	if _, err := p.expect(TokenSELECT); err != nil {
		return nil, err
	}

	stmt := &SelectStmt{}

	// DISTINCT | ALL
	if p.cur.Type == TokenDISTINCT {
		stmt.Distinct = true
		p.advance()
	} else if p.cur.Type == TokenALL {
		p.advance()
	}

	// Column list
	cols, err := p.parseSelectColumns()
	if err != nil {
		return nil, err
	}
	stmt.Columns = cols

	// FROM clause
	if p.cur.Type == TokenFROM {
		p.advance()
		from, err := p.parseFromClause()
		if err != nil {
			return nil, err
		}
		stmt.From = from
	}

	// WHERE clause
	if p.cur.Type == TokenWHERE {
		p.advance()
		where, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		stmt.Where = where
	}

	// GROUP BY clause
	if p.cur.Type == TokenGROUP {
		p.advance()
		if _, err := p.expect(TokenBY); err != nil {
			return nil, err
		}
		groupBy, err := p.parseExprList()
		if err != nil {
			return nil, err
		}
		stmt.GroupBy = groupBy

		// HAVING clause
		if p.cur.Type == TokenHAVING {
			p.advance()
			having, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			stmt.Having = having
		}
	}

	// ORDER BY clause (skip if inside compound — ORDER BY applies to compound)
	if p.compoundDepth == 0 && p.cur.Type == TokenORDER {
		p.advance()
		if _, err := p.expect(TokenBY); err != nil {
			return nil, err
		}
		orderBy, err := p.parseOrderBy()
		if err != nil {
			return nil, err
		}
		stmt.OrderBy = orderBy
	}

	// LIMIT clause (skip if inside compound)
	if p.compoundDepth == 0 && p.cur.Type == TokenLIMIT {
		p.advance()
		limit, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		stmt.Limit = limit

		// OFFSET
		if p.cur.Type == TokenOFFSET {
			p.advance()
			offset, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			stmt.Offset = offset
		} else if p.cur.Type == TokenComma {
			// LIMIT count OFFSET offset (comma syntax)
			p.advance()
			offset, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			stmt.Offset = offset
		}
	}

	return stmt, nil
}

// parseSelectColumns parses the column list in a SELECT statement.
func (p *Parser) parseSelectColumns() ([]SelectColumn, error) {
	var cols []SelectColumn

	// Check for SELECT *
	if p.cur.Type == TokenStar {
		cols = append(cols, SelectColumn{Expr: StarColumn{}})
		p.advance()
		return cols, nil
	}

	for {
		col, err := p.parseSelectColumn()
		if err != nil {
			return nil, err
		}
		cols = append(cols, col)

		if p.cur.Type != TokenComma {
			break
		}
		p.advance()
	}

	return cols, nil
}

// parseSelectColumn parses a single column in a SELECT list.
func (p *Parser) parseSelectColumn() (SelectColumn, error) {
	// Check for table.*
	if p.curIsIdentOrKeyword() && p.peek.Type == TokenDot && p.peekAhead(1).Type == TokenStar {
		table := p.cur.Value
		p.advance() // ident
		p.advance() // .
		p.advance() // *
		return SelectColumn{Expr: ColumnRef{Table: table, Column: "*"}}, nil
	}

	expr, err := p.parseExpression()
	if err != nil {
		return SelectColumn{}, err
	}

	// Check for alias
	as := ""
	if p.cur.Type == TokenAS {
		p.advance()
		name, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return SelectColumn{}, err
		}
		as = name
	} else if p.curIsIdentOrKeyword() && !p.isReservedInSelectContext() {
		// Implicit alias (no AS keyword)
		name, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return SelectColumn{}, err
		}
		as = name
	}

	return SelectColumn{Expr: expr, As: as}, nil
}

// isReservedInSelectContext returns true if the current token is a keyword
// that should NOT be treated as an implicit alias.
func (p *Parser) isReservedInSelectContext() bool {
	switch p.cur.Type {
	case TokenFROM, TokenWHERE, TokenGROUP, TokenHAVING, TokenORDER,
		TokenLIMIT, TokenOFFSET, TokenUNION, TokenINTERSECT, TokenEXCEPT,
		TokenSemicolon, TokenRParen, TokenEOF, TokenAS,
		TokenINNER, TokenLEFT, TokenRIGHT, TokenJOIN, TokenON:
		return true
	}
	// Also check for CROSS as identifier
	if p.curIsIdent() && strings.EqualFold(p.cur.Value, "CROSS") {
		return true
	}
	if p.curIsIdent() && strings.EqualFold(p.cur.Value, "OUTER") {
		return true
	}
	return false
}

// curIsIdentOrKeyword returns true if current token can serve as an identifier.
func (p *Parser) curIsIdentOrKeyword() bool {
	return p.curIsIdent() || p.curIsKeywordIdent()
}

// peekAhead looks ahead n tokens from peek (0 = peek, 1 = peek+1, etc.)
// This is expensive - only use for simple lookahead.
func (p *Parser) peekAhead(n int) Token {
	// Save state
	savedLexer := p.lexer
	// We can't easily do this with the current lexer, so we save/restore cur/peek
	// and advance n times. This is hacky but works for the rare case of 2-token lookahead.
	// For now, we only need peekAhead(1) for table.* detection.
	// We'll just look at the raw input for this case.
	_ = savedLexer
	return Token{} // placeholder - we handle table.* differently
}

// parseFromClause parses a FROM clause.
func (p *Parser) parseFromClause() (*FromClause, error) {
	from := &FromClause{}

	// Table name
	table, err := p.parseTableRef()
	if err != nil {
		return nil, err
	}
	from.Table = &table

	// JOINs
	for {
		jt, hasJoin := p.tryParseJoinType()
		if !hasJoin {
			break
		}
		// tryParseJoinType already consumed all join modifiers and JOIN keyword

		rightTable, err := p.parseTableRef()
		if err != nil {
			return nil, err
		}

		join := JoinClause{Type: jt, Table: rightTable}

		if p.cur.Type == TokenON {
			p.advance()
			on, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			join.On = on
		}

		from.Joins = append(from.Joins, join)
	}

	return from, nil
}

// tryParseJoinType checks if the current tokens form a JOIN type.
// Returns the JoinType and whether a join was found.
// If found, all join-related tokens (including JOIN) are consumed.
func (p *Parser) tryParseJoinType() (JoinType, bool) {
	switch {
	case p.cur.Type == TokenJOIN:
		p.advance()
		return JoinInner, true

	case p.cur.Type == TokenINNER:
		p.advance() // consume INNER
		if p.cur.Type == TokenJOIN {
			p.advance() // consume JOIN
		}
		return JoinInner, true

	case p.cur.Type == TokenLEFT:
		p.advance() // consume LEFT
		if p.cur.Type == TokenOUTER {
			p.advance() // consume OUTER
		}
		if p.cur.Type == TokenJOIN {
			p.advance() // consume JOIN
		}
		return JoinLeft, true

	case p.cur.Type == TokenRIGHT:
		p.advance() // consume RIGHT
		if p.cur.Type == TokenOUTER {
			p.advance() // consume OUTER
		}
		if p.cur.Type == TokenJOIN {
			p.advance() // consume JOIN
		}
		return JoinRight, true

	case p.curIsIdent() && strings.EqualFold(p.cur.Value, "CROSS"):
		p.advance() // consume CROSS
		if p.cur.Type == TokenJOIN {
			p.advance() // consume JOIN
		}
		return JoinCross, true

	case p.cur.Type == TokenOUTER:
		p.advance() // consume OUTER
		if p.cur.Type == TokenJOIN {
			p.advance() // consume JOIN
		}
		// bare OUTER JOIN is a FULL join
		return JoinFull, true

	case p.curIsIdent() && strings.EqualFold(p.cur.Value, "FULL"):
		p.advance() // consume FULL
		if p.cur.Type == TokenOUTER {
			p.advance() // consume OUTER
		}
		if p.cur.Type == TokenJOIN {
			p.advance() // consume JOIN
		}
		return JoinFull, true
	}

	return 0, false
}

// parseTableRef parses a table name with optional alias.
func (p *Parser) parseTableRef() (TableRef, error) {
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return TableRef{}, err
	}

	ref := TableRef{Name: name}

	// Optional alias
	if p.cur.Type == TokenAS {
		p.advance()
		alias, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return TableRef{}, err
		}
		ref.As = alias
	} else if p.curIsIdentOrKeyword() && !p.isReservedInTableRefContext() {
		alias, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return TableRef{}, err
		}
		ref.As = alias
	}

	return ref, nil
}

// isReservedInTableRefContext returns true if the current token should not
// be treated as an implicit table alias.
func (p *Parser) isReservedInTableRefContext() bool {
	switch p.cur.Type {
	case TokenON, TokenWHERE, TokenGROUP, TokenHAVING, TokenORDER,
		TokenLIMIT, TokenOFFSET, TokenUNION, TokenINTERSECT, TokenEXCEPT,
		TokenSemicolon, TokenRParen, TokenEOF, TokenComma,
		TokenINNER, TokenLEFT, TokenRIGHT, TokenJOIN,
		TokenSET, TokenVALUES, TokenSELECT:
		return true
	}
	if p.curIsIdent() && (strings.EqualFold(p.cur.Value, "CROSS") || strings.EqualFold(p.cur.Value, "OUTER") || strings.EqualFold(p.cur.Value, "FULL")) {
		return true
	}
	return false
}

// parseOrderBy parses an ORDER BY clause.
func (p *Parser) parseOrderBy() ([]OrderItem, error) {
	var items []OrderItem

	for {
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}

		item := OrderItem{Expr: expr}

		if p.cur.Type == TokenASC {
			p.advance()
		} else if p.cur.Type == TokenDESC {
			item.Desc = true
			p.advance()
		}

		items = append(items, item)

		if p.cur.Type != TokenComma {
			break
		}
		p.advance()
	}

	return items, nil
}

// ============================================================================
// INSERT
// ============================================================================

func (p *Parser) parseInsert() (*InsertStmt, error) {
	stmt := &InsertStmt{}
	if _, err := p.expect(TokenINSERT); err != nil {
		return nil, err
	}
	if p.cur.Type == TokenOR {
		p.advance()
		if p.consumeWord("IGNORE") {
			stmt.OrIgnore = true
		} else if p.consumeWord("REPLACE") {
			stmt.OrReplace = true
		} else {
			return nil, p.errorf("expected IGNORE or REPLACE, got %s (%q)", tokenTypeName(p.cur.Type), p.cur.Value)
		}
	}
	if _, err := p.expect(TokenINTO); err != nil {
		return nil, err
	}

	// Table name
	table, err := p.parseTableRef()
	if err != nil {
		return nil, err
	}
	stmt.Table = table

	// Optional column list
	if p.cur.Type == TokenLParen {
		p.advance()
		cols, err := p.parseColumnList()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokenRParen); err != nil {
			return nil, err
		}
		stmt.Columns = cols
	}

	// VALUES
	if p.cur.Type == TokenVALUES {
		p.advance()
		for {
			if _, err := p.expect(TokenLParen); err != nil {
				return nil, err
			}
			row, err := p.parseExprList()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(TokenRParen); err != nil {
				return nil, err
			}
			stmt.Values = append(stmt.Values, row)

			if p.cur.Type != TokenComma {
				break
			}
			p.advance()
		}
	} else if p.cur.Type == TokenSELECT {
		// INSERT ... SELECT
		sel, err := p.parseSelect()
		if err != nil {
			return nil, err
		}
		stmt.Select = sel
	}

	if err := p.parseInsertTail(stmt); err != nil {
		return nil, err
	}
	return stmt, nil
}

// ============================================================================
// UPDATE
// ============================================================================

func (p *Parser) parseUpdate() (*UpdateStmt, error) {
	if _, err := p.expect(TokenUPDATE); err != nil {
		return nil, err
	}

	stmt := &UpdateStmt{}

	// Table name
	table, err := p.parseTableRef()
	if err != nil {
		return nil, err
	}
	stmt.Table = table

	// SET clause
	if _, err := p.expect(TokenSET); err != nil {
		return nil, err
	}

	for {
		col, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokenEqual); err != nil {
			return nil, err
		}
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}

		stmt.Sets = append(stmt.Sets, SetClause{Column: col, Expr: expr})

		if p.cur.Type != TokenComma {
			break
		}
		p.advance()
	}

	// WHERE clause
	if p.cur.Type == TokenWHERE {
		p.advance()
		where, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		stmt.Where = where
	}

	if p.consumeWord("RETURNING") {
		columns, err := p.parseColumnList()
		if err != nil {
			return nil, err
		}
		stmt.Returning = columns
	}

	return stmt, nil
}

// ============================================================================
// DELETE
// ============================================================================

func (p *Parser) parseDelete() (*DeleteStmt, error) {
	if _, err := p.expect(TokenDELETE); err != nil {
		return nil, err
	}
	if _, err := p.expect(TokenFROM); err != nil {
		return nil, err
	}

	stmt := &DeleteStmt{}

	// Table name
	table, err := p.parseTableRef()
	if err != nil {
		return nil, err
	}
	stmt.Table = table

	// WHERE clause
	if p.cur.Type == TokenWHERE {
		p.advance()
		where, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		stmt.Where = where
	}

	return stmt, nil
}

// ============================================================================
// CREATE TABLE / CREATE INDEX
// ============================================================================

func (p *Parser) parseCreate() (Statement, error) {
	if _, err := p.expect(TokenCREATE); err != nil {
		return nil, err
	}

	if p.cur.Type == TokenTABLE {
		return p.parseCreateTable()
	}
	if p.cur.Type == TokenINDEX {
		return p.parseCreateIndex()
	}
	if p.cur.Type == TokenUNIQUE {
		p.advance()
		if _, err := p.expect(TokenINDEX); err != nil {
			return nil, err
		}
		return p.parseCreateIndexBody(true)
	}
	if p.cur.Type == TokenVIEW {
		return p.parseCreateView()
	}

	return nil, p.errorf("expected TABLE or INDEX after CREATE, got %s", tokenTypeName(p.cur.Type))
}

func (p *Parser) parseCreateTable() (*CreateTableStmt, error) {
	if _, err := p.expect(TokenTABLE); err != nil {
		return nil, err
	}

	stmt := &CreateTableStmt{}

	// IF NOT EXISTS
	if p.cur.Type == TokenIF {
		p.advance()
		if _, err := p.expect(TokenNOT); err != nil {
			return nil, err
		}
		if _, err := p.expect(TokenEXISTS); err != nil {
			return nil, err
		}
		stmt.IfNotExists = true
	}

	// Table name
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	stmt.Name = name

	// Column definitions
	if _, err := p.expect(TokenLParen); err != nil {
		return nil, err
	}
	cols, constraints, err := p.parseTableElements()
	if err != nil {
		return nil, err
	}
	stmt.Columns = cols
	if _, err := p.expect(TokenRParen); err != nil {
		return nil, err
	}
	stmt.TableConstraints = constraints

	return stmt, nil
}

func (p *Parser) parseColumnDefs() ([]ColumnDefAST, error) {
	var cols []ColumnDefAST

	for {
		col, err := p.parseColumnDef()
		if err != nil {
			return nil, err
		}
		cols = append(cols, col)

		if p.cur.Type != TokenComma {
			break
		}
		p.advance()
	}

	return cols, nil
}

func (p *Parser) parseColumnDef() (ColumnDefAST, error) {
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return ColumnDefAST{}, err
	}

	col := ColumnDefAST{Name: name}

	// Type name (optional)
	if p.curIsIdentOrKeyword() && !p.isColumnConstraintStart() {
		typeStr, err := p.parseTypeName()
		if err != nil {
			return ColumnDefAST{}, err
		}
		col.Type = typeStr
	}

	// Constraints
	for p.isColumnConstraintStart() {
		con, err := p.parseColumnConstraint()
		if err != nil {
			return ColumnDefAST{}, err
		}
		col.Constraints = append(col.Constraints, con)
	}

	return col, nil
}

// isColumnConstraintStart checks if the current token starts a column constraint.
func (p *Parser) isColumnConstraintStart() bool {
	switch p.cur.Type {
	case TokenPRIMARY, TokenNOT, TokenUNIQUE, TokenCHECK, TokenDEFAULT,
		TokenFOREIGN, TokenCOLLATE, TokenREFERENCES,
		TokenAUTOINCREMENT:
		return true
	case TokenNULL:
		// NOT NULL, not bare NULL
		return false
	}
	// Handle CONSTRAINT as identifier
	if p.curIsIdent() && strings.EqualFold(p.cur.Value, "CONSTRAINT") {
		return true
	}
	return false
}

// parseTypeName parses a type name like INTEGER, TEXT, VARCHAR(255), etc.
func (p *Parser) parseTypeName() (string, error) {
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return "", err
	}

	// Handle type modifiers like VARCHAR(255)
	if p.cur.Type == TokenLParen {
		p.advance()
		var inner strings.Builder
		inner.WriteString(name)
		inner.WriteByte('(')
		for p.cur.Type != TokenRParen && p.cur.Type != TokenEOF {
			inner.WriteString(p.cur.Value)
			p.advance()
		}
		if _, err := p.expect(TokenRParen); err != nil {
			return "", err
		}
		inner.WriteByte(')')
		return inner.String(), nil
	}

	return name, nil
}

func (p *Parser) parseColumnConstraint() (ColumnConstraint, error) {
	switch p.cur.Type {
	case TokenPRIMARY:
		p.advance()
		if _, err := p.expect(TokenKEY); err != nil {
			return ColumnConstraint{}, err
		}
		// Check for AUTOINCREMENT
		con := ColumnConstraint{Type: ConstraintPrimaryKey}
		if p.cur.Type == TokenAUTOINCREMENT {
			p.advance()
			// Add AUTOINCREMENT as a separate constraint marker
			con2 := ColumnConstraint{Type: ConstraintPrimaryKey}
			_ = con2 // AUTOINCREMENT is absorbed into the PRIMARY KEY constraint
		}
		return con, nil

	case TokenNOT:
		p.advance()
		if _, err := p.expect(TokenNULL); err != nil {
			return ColumnConstraint{}, err
		}
		return ColumnConstraint{Type: ConstraintNotNull}, nil

	case TokenUNIQUE:
		p.advance()
		return ColumnConstraint{Type: ConstraintUnique}, nil

	case TokenCHECK:
		p.advance()
		if _, err := p.expect(TokenLParen); err != nil {
			return ColumnConstraint{}, err
		}
		expr, err := p.parseExpression()
		if err != nil {
			return ColumnConstraint{}, err
		}
		if _, err := p.expect(TokenRParen); err != nil {
			return ColumnConstraint{}, err
		}
		return ColumnConstraint{Type: ConstraintCheck, Value: expr}, nil

	case TokenDEFAULT:
		p.advance()
		expr, err := p.parseExpression()
		if err != nil {
			return ColumnConstraint{}, err
		}
		return ColumnConstraint{Type: ConstraintDefault, Value: expr}, nil

	case TokenREFERENCES:
		p.advance()
		table, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return ColumnConstraint{}, err
		}
		con := ColumnConstraint{Type: ConstraintForeignKey, RefTable: table}
		if p.cur.Type == TokenLParen {
			p.advance()
			cols, err := p.parseColumnList()
			if err != nil {
				return ColumnConstraint{}, err
			}
			if _, err := p.expect(TokenRParen); err != nil {
				return ColumnConstraint{}, err
			}
			con.RefCols = cols
		}
		return con, nil

	case TokenCOLLATE:
		p.advance()
		collName, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return ColumnConstraint{}, err
		}
		return ColumnConstraint{Type: ConstraintCheck, Collate: collName}, nil

	case TokenAUTOINCREMENT:
		p.advance()
		// AUTOINCREMENT without PRIMARY KEY - still record it
		return ColumnConstraint{Type: ConstraintPrimaryKey}, nil

	case TokenFOREIGN:
		p.advance()
		if _, err := p.expect(TokenKEY); err != nil {
			return ColumnConstraint{}, err
		}
		return ColumnConstraint{Type: ConstraintForeignKey}, nil

	default:
		// Handle CONSTRAINT keyword as identifier
		if p.curIsIdent() && strings.EqualFold(p.cur.Value, "CONSTRAINT") {
			p.advance()
			name, err := p.parseIdentifierOrKeyword()
			if err != nil {
				return ColumnConstraint{}, err
			}
			// Recurse to parse the actual constraint
			con, err := p.parseColumnConstraint()
			if err != nil {
				return ColumnConstraint{}, err
			}
			con.Name = name
			return con, nil
		}
		return ColumnConstraint{}, p.errorf("expected column constraint, got %s (%q)", tokenTypeName(p.cur.Type), p.cur.Value)
	}
}

func (p *Parser) parseCreateIndex() (*CreateIndexStmt, error) {
	if _, err := p.expect(TokenINDEX); err != nil {
		return nil, err
	}
	return p.parseCreateIndexBody(false)
}

func (p *Parser) parseCreateIndexBody(unique bool) (*CreateIndexStmt, error) {
	stmt := &CreateIndexStmt{Unique: unique}

	// IF NOT EXISTS
	if p.cur.Type == TokenIF {
		p.advance()
		if _, err := p.expect(TokenNOT); err != nil {
			return nil, err
		}
		if _, err := p.expect(TokenEXISTS); err != nil {
			return nil, err
		}
		stmt.IfNotExists = true
	}

	// Index name
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	stmt.Name = name

	// ON
	if _, err := p.expect(TokenON); err != nil {
		return nil, err
	}

	// Table name
	table, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	stmt.Table = table

	// Column list
	if _, err := p.expect(TokenLParen); err != nil {
		return nil, err
	}

	for {
		colName, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return nil, err
		}
		ic := IndexedColumn{Name: colName}

		// COLLATE
		if p.cur.Type == TokenCOLLATE {
			p.advance()
			collName, err := p.parseIdentifierOrKeyword()
			if err != nil {
				return nil, err
			}
			ic.Collate = collName
		}

		// ASC / DESC
		if p.cur.Type == TokenASC {
			p.advance()
		} else if p.cur.Type == TokenDESC {
			ic.Desc = true
			p.advance()
		}

		stmt.Columns = append(stmt.Columns, ic)

		if p.cur.Type != TokenComma {
			break
		}
		p.advance()
	}

	if _, err := p.expect(TokenRParen); err != nil {
		return nil, err
	}

	// Optional WHERE clause for partial indexes
	if p.cur.Type == TokenWHERE {
		p.advance()
		where, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		stmt.Where = where
	}

	return stmt, nil
}

// ============================================================================
// DROP TABLE / DROP INDEX
// ============================================================================

func (p *Parser) parseDrop() (Statement, error) {
	if _, err := p.expect(TokenDROP); err != nil {
		return nil, err
	}

	switch p.cur.Type {
	case TokenTABLE:
		return p.parseDropTable()
	case TokenINDEX:
		return p.parseDropIndex()
	case TokenVIEW:
		return p.parseDropView()
	default:
		return nil, p.errorf("expected TABLE, INDEX, or VIEW after DROP, got %s", tokenTypeName(p.cur.Type))
	}
}

func (p *Parser) parseDropTable() (*DropTableStmt, error) {
	if _, err := p.expect(TokenTABLE); err != nil {
		return nil, err
	}

	stmt := &DropTableStmt{}

	if p.cur.Type == TokenIF {
		p.advance()
		if _, err := p.expect(TokenEXISTS); err != nil {
			return nil, err
		}
		stmt.IfExists = true
	}

	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	stmt.Name = name

	return stmt, nil
}

func (p *Parser) parseDropIndex() (*DropIndexStmt, error) {
	if _, err := p.expect(TokenINDEX); err != nil {
		return nil, err
	}

	stmt := &DropIndexStmt{}

	if p.cur.Type == TokenIF {
		p.advance()
		if _, err := p.expect(TokenEXISTS); err != nil {
			return nil, err
		}
		stmt.IfExists = true
	}

	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	stmt.Name = name

	return stmt, nil
}

func (p *Parser) parseCreateView() (Statement, error) {
	if _, err := p.expect(TokenVIEW); err != nil {
		return nil, err
	}

	// IF NOT EXISTS
	ifNotExists := false
	if p.cur.Type == TokenIF {
		p.advance()
		if _, err := p.expect(TokenNOT); err != nil {
			return nil, err
		}
		if _, err := p.expect(TokenEXISTS); err != nil {
			return nil, err
		}
		ifNotExists = true
	}

	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}

	if _, err := p.expect(TokenAS); err != nil {
		return nil, err
	}

	sel, err := p.parseSelect()
	if err != nil {
		return nil, err
	}

	_ = ifNotExists
	return &CreateViewStmt{Name: name, As: sel}, nil
}

func (p *Parser) parseDropView() (Statement, error) {
	if _, err := p.expect(TokenVIEW); err != nil {
		return nil, err
	}
	stmt := &DropViewStmt{}
	if p.cur.Type == TokenIF {
		p.advance()
		if _, err := p.expect(TokenEXISTS); err != nil {
			return nil, err
		}
		stmt.IfExists = true
	}
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	stmt.Name = name
	return stmt, nil
}

func (p *Parser) parseReindex() (Statement, error) {
	p.advance() // consume REINDEX
	if p.cur.Type == TokenEOF || p.cur.Type == TokenSemicolon {
		return &ReindexStmt{}, nil
	}
	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}
	return &ReindexStmt{TableName: name}, nil
}

func (p *Parser) parseAlter() (Statement, error) {
	if _, err := p.expect(TokenALTER); err != nil {
		return nil, err
	}
	if _, err := p.expect(TokenTABLE); err != nil {
		return nil, err
	}

	tableName, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}

	switch p.cur.Type {
	case TokenADD:
		p.advance()
		// Optional COLUMN keyword
		if p.cur.Type == TokenCOLUMN {
			p.advance()
		}
		col, err := p.parseColumnDef()
		if err != nil {
			return nil, err
		}
		return &AlterAddColumnStmt{Table: tableName, Column: col}, nil

	case TokenRENAME:
		p.advance()
		// Optional COLUMN keyword
		if p.cur.Type == TokenCOLUMN {
			p.advance()
			oldName, err := p.parseIdentifierOrKeyword()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(TokenTO); err != nil {
				return nil, err
			}
			newName, err := p.parseIdentifierOrKeyword()
			if err != nil {
				return nil, err
			}
			return &AlterRenameColumnStmt{Table: tableName, OldName: oldName, NewName: newName}, nil
		}
		// RENAME TO new_name
		newName, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return nil, err
		}
		if !strings.EqualFold(newName, "TO") {
			return nil, p.errorf("expected TO after RENAME")
		}
		actualName, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return nil, err
		}
		return &AlterRenameTableStmt{OldName: tableName, NewName: actualName}, nil

	default:
		return nil, p.errorf("expected ADD or RENAME after ALTER TABLE")
	}
}

func (p *Parser) parsePragma() (Statement, error) {
	if _, err := p.expect(TokenPRAGMA); err != nil {
		return nil, err
	}

	name, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return nil, err
	}

	stmt := &PragmaStmt{Name: name}

	// PRAGMA name = value
	if p.cur.Type == TokenEqual {
		p.advance()
		val, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		stmt.Value = val
	}
	// SQLite also accepts function-call syntax, most notably
	// PRAGMA table_info(table_name). A bare identifier here is data, not a
	// column reference, so preserve it as a text literal.
	if p.cur.Type == TokenLParen {
		p.advance()
		if p.cur.Type == TokenString || p.cur.Type == TokenInteger || p.cur.Type == TokenFloat {
			val, err := p.parseExpression()
			if err != nil {
				return nil, err
			}
			stmt.Value = val
		} else {
			value, err := p.parseIdentifierOrKeyword()
			if err != nil {
				return nil, err
			}
			stmt.Value = LiteralExpr{Type: DataTypeText, TextVal: value}
		}
		if _, err := p.expect(TokenRParen); err != nil {
			return nil, err
		}
	}

	return stmt, nil
}

// ============================================================================
// Transaction statements
// ============================================================================

func (p *Parser) parseBegin() (*BeginStmt, error) {
	if _, err := p.expect(TokenBEGIN); err != nil {
		return nil, err
	}
	// Optional TRANSACTION keyword
	if p.cur.Type == TokenTRANSACTION {
		p.advance()
	}
	return &BeginStmt{}, nil
}

func (p *Parser) parseCommit() (*CommitStmt, error) {
	if _, err := p.expect(TokenCOMMIT); err != nil {
		return nil, err
	}
	// Optional TRANSACTION keyword
	if p.cur.Type == TokenTRANSACTION {
		p.advance()
	}
	return &CommitStmt{}, nil
}

func (p *Parser) parseRollback() (*RollbackStmt, error) {
	if _, err := p.expect(TokenROLLBACK); err != nil {
		return nil, err
	}
	// Optional TRANSACTION keyword
	if p.cur.Type == TokenTRANSACTION {
		p.advance()
	}
	return &RollbackStmt{}, nil
}

// ============================================================================
// Expression parsing - precedence climbing
//
// Precedence (low to high):
// 1. OR
// 2. AND
// 3. NOT (prefix)
// 4. Comparison (=, !=, <, <=, >, >=, IS, IS NOT, IN, BETWEEN, LIKE, GLOB)
// 5. Addition (+, -)
// 6. Multiplication (*, /, %)
// 7. Concatenation (||)
// 8. Unary (-, +, ~, NOT)
// 9. Primary (literal, column ref, function, parens, CASE, CAST, ?)
// ============================================================================

func (p *Parser) parseExpression() (Expr, error) {
	return p.parseOrExpr()
}

func (p *Parser) parseOrExpr() (Expr, error) {
	left, err := p.parseAndExpr()
	if err != nil {
		return nil, err
	}

	for p.cur.Type == TokenOR {
		p.advance()
		right, err := p.parseAndExpr()
		if err != nil {
			return nil, err
		}
		left = BinaryExpr{Left: left, Op: OpOr, Right: right}
	}

	return left, nil
}

func (p *Parser) parseAndExpr() (Expr, error) {
	left, err := p.parseNotExpr()
	if err != nil {
		return nil, err
	}

	for p.cur.Type == TokenAND {
		p.advance()
		right, err := p.parseNotExpr()
		if err != nil {
			return nil, err
		}
		left = BinaryExpr{Left: left, Op: OpAnd, Right: right}
	}

	return left, nil
}

func (p *Parser) parseNotExpr() (Expr, error) {
	if p.cur.Type == TokenNOT {
		p.advance()
		expr, err := p.parseNotExpr()
		if err != nil {
			return nil, err
		}
		return UnaryExpr{Op: OpNot, Expr: expr}, nil
	}
	return p.parseComparison()
}

func (p *Parser) parseComparison() (Expr, error) {
	left, err := p.parseConcat()
	if err != nil {
		return nil, err
	}

	for {
		switch {
		case p.cur.Type == TokenEqual || p.cur.Type == TokenDoubleEqual:
			p.advance()
			right, err := p.parseConcat()
			if err != nil {
				return nil, err
			}
			left = BinaryExpr{Left: left, Op: OpEq, Right: right}

		case p.cur.Type == TokenNotEqual:
			p.advance()
			right, err := p.parseConcat()
			if err != nil {
				return nil, err
			}
			left = BinaryExpr{Left: left, Op: OpNe, Right: right}

		case p.cur.Type == TokenLess:
			p.advance()
			right, err := p.parseConcat()
			if err != nil {
				return nil, err
			}
			left = BinaryExpr{Left: left, Op: OpLt, Right: right}

		case p.cur.Type == TokenLessEq:
			p.advance()
			right, err := p.parseConcat()
			if err != nil {
				return nil, err
			}
			left = BinaryExpr{Left: left, Op: OpLe, Right: right}

		case p.cur.Type == TokenGreater:
			p.advance()
			right, err := p.parseConcat()
			if err != nil {
				return nil, err
			}
			left = BinaryExpr{Left: left, Op: OpGt, Right: right}

		case p.cur.Type == TokenGreaterEq:
			p.advance()
			right, err := p.parseConcat()
			if err != nil {
				return nil, err
			}
			left = BinaryExpr{Left: left, Op: OpGe, Right: right}

		case p.cur.Type == TokenIS:
			p.advance()
			negate := false
			if p.cur.Type == TokenNOT {
				negate = true
				p.advance()
			}
			if _, err := p.expect(TokenNULL); err != nil {
				return nil, err
			}
			left = IsNullExpr{Expr: left, Negate: negate}

		case p.cur.Type == TokenIN:
			p.advance()
			negate := false
			inExpr, err := p.parseInExpr(left, negate)
			if err != nil {
				return nil, err
			}
			left = inExpr

		case p.cur.Type == TokenNOT:
			// NOT IN, NOT LIKE, NOT BETWEEN, NOT GLOB
			saved := p.cur
			p.advance()
			switch p.cur.Type {
			case TokenIN:
				p.advance()
				inExpr, err := p.parseInExpr(left, true)
				if err != nil {
					return nil, err
				}
				left = inExpr
			case TokenLIKE:
				p.advance()
				likeExpr, err := p.parseLikeExpr(left, LikeLike, true)
				if err != nil {
					return nil, err
				}
				left = likeExpr
			case TokenGLOB:
				p.advance()
				globExpr, err := p.parseLikeExpr(left, LikeGlob, true)
				if err != nil {
					return nil, err
				}
				left = globExpr
			case TokenBETWEEN:
				p.advance()
				betweenExpr, err := p.parseBetweenExpr(left, true)
				if err != nil {
					return nil, err
				}
				left = betweenExpr
			default:
				// Not a comparison NOT - put it back
				p.cur = saved
				return left, nil
			}

		case p.cur.Type == TokenLIKE:
			p.advance()
			likeExpr, err := p.parseLikeExpr(left, LikeLike, false)
			if err != nil {
				return nil, err
			}
			left = likeExpr

		case p.cur.Type == TokenGLOB:
			p.advance()
			globExpr, err := p.parseLikeExpr(left, LikeGlob, false)
			if err != nil {
				return nil, err
			}
			left = globExpr

		case p.cur.Type == TokenBETWEEN:
			p.advance()
			betweenExpr, err := p.parseBetweenExpr(left, false)
			if err != nil {
				return nil, err
			}
			left = betweenExpr

		default:
			return left, nil
		}
	}
}

func (p *Parser) parseInExpr(left Expr, negate bool) (InExpr, error) {
	if _, err := p.expect(TokenLParen); err != nil {
		return InExpr{}, err
	}

	// Check for subquery
	if p.cur.Type == TokenSELECT {
		sel, err := p.parseSelect()
		if err != nil {
			return InExpr{}, err
		}
		if _, err := p.expect(TokenRParen); err != nil {
			return InExpr{}, err
		}
		return InExpr{Expr: left, Select: sel, Negate: negate}, nil
	}

	// Expression list
	values, err := p.parseExprList()
	if err != nil {
		return InExpr{}, err
	}
	if _, err := p.expect(TokenRParen); err != nil {
		return InExpr{}, err
	}

	return InExpr{Expr: left, Values: values, Negate: negate}, nil
}

func (p *Parser) parseLikeExpr(left Expr, op LikeOp, negate bool) (LikeExpr, error) {
	pattern, err := p.parseConcat()
	if err != nil {
		return LikeExpr{}, err
	}

	le := LikeExpr{Expr: left, Pattern: pattern, Op: op, Negate: negate}

	// Optional ESCAPE clause
	if p.cur.Type == TokenIdent && strings.EqualFold(p.cur.Value, "ESCAPE") {
		p.advance()
		esc, err := p.parseConcat()
		if err != nil {
			return LikeExpr{}, err
		}
		le.Escape = esc
	}

	return le, nil
}

func (p *Parser) parseBetweenExpr(left Expr, negate bool) (BetweenExpr, error) {
	low, err := p.parseConcat()
	if err != nil {
		return BetweenExpr{}, err
	}
	if _, err := p.expect(TokenAND); err != nil {
		return BetweenExpr{}, err
	}
	high, err := p.parseConcat()
	if err != nil {
		return BetweenExpr{}, err
	}
	return BetweenExpr{Expr: left, Low: low, High: high, Negate: negate}, nil
}

func (p *Parser) parseAddition() (Expr, error) {
	left, err := p.parseMultiplication()
	if err != nil {
		return nil, err
	}

	for p.cur.Type == TokenPlus || p.cur.Type == TokenMinus {
		op := OpAdd
		if p.cur.Type == TokenMinus {
			op = OpSub
		}
		p.advance()
		right, err := p.parseMultiplication()
		if err != nil {
			return nil, err
		}
		left = BinaryExpr{Left: left, Op: op, Right: right}
	}

	return left, nil
}

func (p *Parser) parseMultiplication() (Expr, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}

	for p.cur.Type == TokenStar || p.cur.Type == TokenSlash || p.cur.Type == TokenPercent {
		var op BinaryOp
		switch p.cur.Type {
		case TokenStar:
			op = OpMul
		case TokenSlash:
			op = OpDiv
		case TokenPercent:
			op = OpMod
		}
		p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = BinaryExpr{Left: left, Op: op, Right: right}
	}

	return left, nil
}

func (p *Parser) parseConcat() (Expr, error) {
	left, err := p.parseAddition()
	if err != nil {
		return nil, err
	}

	for p.cur.Type == TokenConcat {
		p.advance()
		right, err := p.parseAddition()
		if err != nil {
			return nil, err
		}
		left = BinaryExpr{Left: left, Op: OpConcat, Right: right}
	}

	return left, nil
}

func (p *Parser) parseUnary() (Expr, error) {
	switch p.cur.Type {
	case TokenMinus:
		p.advance()
		expr, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return UnaryExpr{Op: OpNegate, Expr: expr}, nil
	case TokenPlus:
		p.advance()
		return p.parseUnary()
	case TokenTilde:
		p.advance()
		expr, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return UnaryExpr{Op: OpBitNot, Expr: expr}, nil
	case TokenNOT:
		p.advance()
		expr, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return UnaryExpr{Op: OpNot, Expr: expr}, nil
	}
	return p.parsePrimary()
}

func (p *Parser) parsePrimary() (Expr, error) {
	switch p.cur.Type {
	case TokenInteger:
		return p.parseIntegerLiteral()

	case TokenFloat:
		return p.parseFloatLiteral()

	case TokenString:
		return p.parseStringLiteral()

	case TokenBlob:
		return p.parseBlobLiteral()

	case TokenNULL:
		p.advance()
		return LiteralExpr{Type: DataTypeNull}, nil

	case TokenQuestion:
		return p.parseParameter()

	case TokenIdent, TokenQuotedID:
		return p.parseIdentOrFunction()

	case TokenEXISTS:
		p.advance()
		if _, err := p.expect(TokenLParen); err != nil {
			return nil, err
		}
		if p.cur.Type != TokenSELECT {
			return nil, p.errorf("expected SELECT after EXISTS (")
		}
		sel, err := p.parseSelect()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokenRParen); err != nil {
			return nil, err
		}
		return ExistsExpr{Select: sel}, nil

	// Keywords that can start a column reference
	case TokenLParen:
		p.advance()
		// Check for scalar subquery: (SELECT ...)
		if p.cur.Type == TokenSELECT {
			sel, err := p.parseSelect()
			if err != nil {
				return nil, err
			}
			if _, err := p.expect(TokenRParen); err != nil {
				return nil, err
			}
			return SubqueryExpr{Select: sel}, nil
		}
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokenRParen); err != nil {
			return nil, err
		}
		return ParenExpr{Expr: expr}, nil

	case TokenCASE:
		return p.parseCaseExpr()

	case TokenCAST:
		return p.parseCastExpr()

	case TokenROWID:
		p.advance()
		// Check for .something
		if p.cur.Type == TokenDot {
			p.advance()
			col, err := p.parseIdentifierOrKeyword()
			if err != nil {
				return nil, err
			}
			return ColumnRef{Table: "ROWID", Column: col}, nil
		}
		return RowIDExpr{}, nil

	case TokenStar:
		// Standalone * (e.g., in SELECT *)
		p.advance()
		return StarColumn{}, nil

	// All other keyword tokens that can serve as identifiers (column names, function names)
	case TokenINTEGER_KW, TokenTEXT_KW, TokenREAL_KW, TokenBLOB_KW,
		TokenKEY, TokenDEFAULT, TokenCHECK,
		TokenPRIMARY, TokenUNIQUE, TokenFOREIGN, TokenREFERENCES,
		TokenINDEX, TokenTABLE, TokenIF,
		TokenBEGIN, TokenCOMMIT, TokenROLLBACK, TokenTRANSACTION,
		TokenCOLLATE, TokenDISTINCT, TokenALL,
		TokenHAVING, TokenGROUP, TokenUNION, TokenINTERSECT, TokenEXCEPT,
		TokenVACUUM, TokenREINDEX, TokenALTER, TokenADD, TokenCOLUMN,
		TokenRENAME, TokenTO, TokenSELECT, TokenFROM, TokenWHERE,
		TokenINSERT, TokenINTO, TokenVALUES, TokenUPDATE, TokenSET,
		TokenPRAGMA,
		TokenVIEW,
		TokenDELETE, TokenCREATE, TokenDROP, TokenAND, TokenOR,
		TokenORDER, TokenBY, TokenASC, TokenDESC, TokenLIMIT, TokenOFFSET,
		TokenAS, TokenON, TokenLEFT, TokenRIGHT, TokenINNER, TokenOUTER,
		TokenJOIN, TokenIN, TokenIS, TokenLIKE, TokenGLOB, TokenBETWEEN,
		TokenWHEN, TokenTHEN, TokenELSE, TokenEND, TokenAUTOINCREMENT:
		// Treat as identifier - could be column name
		return p.parseIdentOrFunction()

	default:
		return nil, p.errorf("unexpected token in expression: %s (%q)", tokenTypeName(p.cur.Type), p.cur.Value)
	}
}

func (p *Parser) parseIntegerLiteral() (Expr, error) {
	val := p.cur.Value
	p.advance()

	// Parse hex
	if len(val) >= 3 && (val[0:2] == "0x" || val[0:2] == "0X") {
		n, err := strconv.ParseInt(val, 0, 64)
		if err != nil {
			return nil, p.errorf("invalid integer literal: %s", val)
		}
		return LiteralExpr{Type: DataTypeInteger, IntVal: n}, nil
	}

	n, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return nil, p.errorf("invalid integer literal: %s", val)
	}
	return LiteralExpr{Type: DataTypeInteger, IntVal: n}, nil
}

func (p *Parser) parseFloatLiteral() (Expr, error) {
	val := p.cur.Value
	p.advance()
	f, err := strconv.ParseFloat(val, 64)
	if err != nil {
		return nil, p.errorf("invalid float literal: %s", val)
	}
	return LiteralExpr{Type: DataTypeFloat, FloatVal: f}, nil
}

func (p *Parser) parseStringLiteral() (Expr, error) {
	val := p.cur.Value
	p.advance()
	return LiteralExpr{Type: DataTypeText, TextVal: val}, nil
}

func (p *Parser) parseBlobLiteral() (Expr, error) {
	val := p.cur.Value
	p.advance()
	// val is like X'0123AB'
	// Strip X' prefix and ' suffix
	hexStr := val[2 : len(val)-1]
	b, err := hexDecode(hexStr)
	if err != nil {
		return nil, p.errorf("invalid blob literal: %s", val)
	}
	return LiteralExpr{Type: DataTypeBlob, BlobVal: b}, nil
}

func (p *Parser) parseIdentOrFunction() (Expr, error) {
	name := p.cur.Value
	// quoted remembers whether the *current* token was written in a quoted
	// identifier form ("..", `..`, [..]). A quoted "true" / "false" is a
	// column named true/false — NOT a boolean literal — and must fall
	// through to the ColumnRef path. Capture this BEFORE advance() moves
	// p.cur to the next token.
	quoted := p.cur.Quoted
	p.advance()

	// Bare TRUE/FALSE are boolean literals (SQLite 3.23+): they evaluate to
	// integer 1 and 0 respectively. Only the unqualified, non-call,
	// non-quoted form is rewritten — a following '(' (function call) or '.'
	// (table qualifier) means the token is a real identifier such as
	// true() or t.true, and a quoted "true"/`true`/[true] is a column of
	// that name. This is what lets `DEFAULT true` / `DEFAULT false`
	// populate col.Default at CREATE TABLE time while still allowing a
	// column literally named true to round-trip.
	if !quoted {
		if upper := strings.ToUpper(name); (upper == "TRUE" || upper == "FALSE") && p.cur.Type != TokenLParen && p.cur.Type != TokenDot {
			if upper == "TRUE" {
				return LiteralExpr{Type: DataTypeInteger, IntVal: 1}, nil
			}
			return LiteralExpr{Type: DataTypeInteger, IntVal: 0}, nil
		}
	}

	// Function call?
	if p.cur.Type == TokenLParen {
		p.advance()
		fc := FunctionCall{Name: strings.ToUpper(name)}

		// COUNT(*)
		if p.cur.Type == TokenStar {
			fc.Star = true
			p.advance()
		} else if p.cur.Type != TokenRParen {
			// DISTINCT?
			if p.cur.Type == TokenDISTINCT {
				fc.Distinct = true
				p.advance()
			}

			args, err := p.parseExprList()
			if err != nil {
				return nil, err
			}
			fc.Args = args
		}
		if _, err := p.expect(TokenRParen); err != nil {
			return nil, err
		}
		return fc, nil
	}

	// Check for qualified name: ident.column
	if p.cur.Type == TokenDot {
		p.advance()
		// Check for table.*
		if p.cur.Type == TokenStar {
			p.advance()
			return ColumnRef{Table: name, Column: "*"}, nil
		}
		col, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return nil, err
		}
		return ColumnRef{Table: name, Column: col}, nil
	}

	return ColumnRef{Column: name}, nil
}

func (p *Parser) parseCaseExpr() (Expr, error) {
	if _, err := p.expect(TokenCASE); err != nil {
		return nil, err
	}

	ce := CaseExpr{}

	// Optional operand (CASE x WHEN ...)
	if p.cur.Type != TokenWHEN {
		operand, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		ce.Operand = operand
	}

	// WHEN clauses
	for p.cur.Type == TokenWHEN {
		p.advance()
		cond, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		if _, err := p.expect(TokenTHEN); err != nil {
			return nil, err
		}
		result, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		ce.Whens = append(ce.Whens, CaseWhen{Condition: cond, Result: result})
	}

	// Optional ELSE
	if p.cur.Type == TokenELSE {
		p.advance()
		elseExpr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		ce.Else = elseExpr
	}

	if _, err := p.expect(TokenEND); err != nil {
		return nil, err
	}

	return ce, nil
}

func (p *Parser) parseCastExpr() (Expr, error) {
	if _, err := p.expect(TokenCAST); err != nil {
		return nil, err
	}
	if _, err := p.expect(TokenLParen); err != nil {
		return nil, err
	}

	expr, err := p.parseExpression()
	if err != nil {
		return nil, err
	}

	if _, err := p.expect(TokenAS); err != nil {
		return nil, err
	}

	typeStr, err := p.parseTypeName()
	if err != nil {
		return nil, err
	}

	if _, err := p.expect(TokenRParen); err != nil {
		return nil, err
	}

	return CastExpr{Expr: expr, Type: typeStr}, nil
}

// ============================================================================
// Helper parsing functions
// ============================================================================

func (p *Parser) parseColumnList() ([]string, error) {
	var cols []string
	for {
		name, err := p.parseIdentifierOrKeyword()
		if err != nil {
			return nil, err
		}
		cols = append(cols, name)

		if p.cur.Type != TokenComma {
			break
		}
		p.advance()
	}
	return cols, nil
}

func (p *Parser) parseExprList() ([]Expr, error) {
	var exprs []Expr
	for {
		expr, err := p.parseExpression()
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, expr)

		if p.cur.Type != TokenComma {
			break
		}
		p.advance()
	}
	return exprs, nil
}

func (p *Parser) parseType() (string, error) {
	return p.parseTypeName()
}

// hexDecode decodes a hex string to bytes.
func hexDecode(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, pErr("odd length hex string")
	}
	b := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		hi, ok := hexVal(s[i])
		if !ok {
			return nil, pErr("invalid hex character")
		}
		lo, ok := hexVal(s[i+1])
		if !ok {
			return nil, pErr("invalid hex character")
		}
		b[i/2] = hi<<4 | lo
	}
	return b, nil
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	default:
		return 0, false
	}
}

func pErr(msg string) error {
	return &ParseError{Msg: msg}
}

// ============================================================================
// String methods for AST nodes
// ============================================================================

func (e LiteralExpr) String() string {
	switch e.Type {
	case DataTypeNull:
		return "NULL"
	case DataTypeInteger:
		return strconv.FormatInt(e.IntVal, 10)
	case DataTypeFloat:
		return strconv.FormatFloat(e.FloatVal, 'f', -1, 64)
	case DataTypeText:
		return "'" + strings.ReplaceAll(e.TextVal, "'", "''") + "'"
	case DataTypeBlob:
		return "X'" + strings.ToUpper(hexEncode(e.BlobVal)) + "'"
	default:
		return "NULL"
	}
}

func (e ColumnRef) String() string {
	if e.Table != "" {
		return e.Table + "." + e.Column
	}
	return e.Column
}

func (e StarColumn) String() string {
	return "*"
}

func (e BinaryExpr) String() string {
	var op string
	switch e.Op {
	case OpAdd:
		op = "+"
	case OpSub:
		op = "-"
	case OpMul:
		op = "*"
	case OpDiv:
		op = "/"
	case OpMod:
		op = "%"
	case OpConcat:
		op = "||"
	case OpEq:
		op = "="
	case OpNe:
		op = "<>"
	case OpLt:
		op = "<"
	case OpLe:
		op = "<="
	case OpGt:
		op = ">"
	case OpGe:
		op = ">="
	case OpAnd:
		op = "AND"
	case OpOr:
		op = "OR"
	case OpBitAnd:
		op = "&"
	case OpBitOr:
		op = "|"
	case OpShiftLeft:
		op = "<<"
	case OpShiftRight:
		op = ">>"
	}
	return "(" + exprStr(e.Left) + " " + op + " " + exprStr(e.Right) + ")"
}

func (e UnaryExpr) String() string {
	switch e.Op {
	case OpNegate:
		return "-" + exprStr(e.Expr)
	case OpNot:
		return "NOT " + exprStr(e.Expr)
	case OpBitNot:
		return "~" + exprStr(e.Expr)
	}
	return exprStr(e.Expr)
}

func (e FunctionCall) String() string {
	var args []string
	if e.Star {
		args = append(args, "*")
	} else if e.Distinct {
		for _, a := range e.Args {
			args = append(args, "DISTINCT "+exprStr(a))
		}
	} else {
		for _, a := range e.Args {
			args = append(args, exprStr(a))
		}
	}
	return e.Name + "(" + strings.Join(args, ", ") + ")"
}

func (e IsNullExpr) String() string {
	if e.Negate {
		return "(" + exprStr(e.Expr) + " IS NOT NULL)"
	}
	return "(" + exprStr(e.Expr) + " IS NULL)"
}

func (e BetweenExpr) String() string {
	s := "(" + exprStr(e.Expr)
	if e.Negate {
		s += " NOT"
	}
	s += " BETWEEN " + exprStr(e.Low) + " AND " + exprStr(e.High) + ")"
	return s
}

func (e InExpr) String() string {
	s := "(" + exprStr(e.Expr)
	if e.Negate {
		s += " NOT"
	}
	s += " IN ("
	if e.Select != nil {
		s += "SELECT ..."
	} else {
		var vals []string
		for _, v := range e.Values {
			vals = append(vals, exprStr(v))
		}
		s += strings.Join(vals, ", ")
	}
	s += "))"
	return s
}

func (e LikeExpr) String() string {
	op := "LIKE"
	if e.Op == LikeGlob {
		op = "GLOB"
	}
	s := "(" + exprStr(e.Expr)
	if e.Negate {
		s += " NOT"
	}
	s += " " + op + " " + exprStr(e.Pattern) + ")"
	return s
}

func (e ParenExpr) String() string {
	return "(" + exprStr(e.Expr) + ")"
}

func (e CaseExpr) String() string {
	s := "CASE "
	if e.Operand != nil {
		s += exprStr(e.Operand) + " "
	}
	for _, w := range e.Whens {
		s += "WHEN " + exprStr(w.Condition) + " THEN " + exprStr(w.Result) + " "
	}
	if e.Else != nil {
		s += "ELSE " + exprStr(e.Else) + " "
	}
	s += "END"
	return s
}

func (e CastExpr) String() string {
	return "CAST(" + exprStr(e.Expr) + " AS " + e.Type + ")"
}

func (e ParamExpr) String() string {
	if e.Name != "" {
		return e.Name
	}
	return "?"
}

func (e RowIDExpr) String() string {
	return "rowid"
}

func exprStr(e Expr) string {
	if e == nil {
		return ""
	}
	switch v := e.(type) {
	case LiteralExpr:
		return v.String()
	case ColumnRef:
		return v.String()
	case StarColumn:
		return v.String()
	case BinaryExpr:
		return v.String()
	case UnaryExpr:
		return v.String()
	case FunctionCall:
		return v.String()
	case IsNullExpr:
		return v.String()
	case BetweenExpr:
		return v.String()
	case InExpr:
		return v.String()
	case LikeExpr:
		return v.String()
	case ParenExpr:
		return v.String()
	case CaseExpr:
		return v.String()
	case CastExpr:
		return v.String()
	case ParamExpr:
		return v.String()
	case RowIDExpr:
		return v.String()
	default:
		return "?"
	}
}

func hexEncode(b []byte) string {
	const hexDigits = "0123456789ABCDEF"
	buf := make([]byte, len(b)*2)
	for i, v := range b {
		buf[i*2] = hexDigits[v>>4]
		buf[i*2+1] = hexDigits[v&0x0f]
	}
	return string(buf)
}
