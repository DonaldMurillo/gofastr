package sqlite

import (
	"strconv"
	"strings"
)

func (p *Parser) parseParameter() (Expr, error) {
	value := p.cur.Value
	p.advance()
	if strings.HasPrefix(value, "$") {
		n, err := strconv.Atoi(strings.TrimPrefix(value, "$"))
		if err != nil || n <= 0 {
			return nil, p.errorf("invalid numbered parameter %q", value)
		}
		return ParamExpr{Index: n - 1, Name: value}, nil
	}
	idx := p.param
	p.param++
	return ParamExpr{Index: idx}, nil
}

func (p *Parser) consumeWord(word string) bool {
	if !strings.EqualFold(p.cur.Value, word) {
		return false
	}
	p.advance()
	return true
}

func (p *Parser) expectWord(word string) error {
	if !p.consumeWord(word) {
		return p.errorf("expected %s, got %s (%q)", word, tokenTypeName(p.cur.Type), p.cur.Value)
	}
	return nil
}

func (p *Parser) parseInsertTail(stmt *InsertStmt) error {
	if p.cur.Type == TokenON {
		p.advance()
		if err := p.expectWord("CONFLICT"); err != nil {
			return err
		}
		conflict := &InsertConflict{}
		if p.cur.Type == TokenLParen {
			p.advance()
			cols, err := p.parseColumnList()
			if err != nil {
				return err
			}
			if _, err := p.expect(TokenRParen); err != nil {
				return err
			}
			conflict.Target = cols
		}
		if err := p.expectWord("DO"); err != nil {
			return err
		}
		if p.consumeWord("NOTHING") {
			conflict.DoNothing = true
		} else {
			if _, err := p.expect(TokenUPDATE); err != nil {
				return err
			}
			if _, err := p.expect(TokenSET); err != nil {
				return err
			}
			for {
				column, err := p.parseIdentifierOrKeyword()
				if err != nil {
					return err
				}
				if _, err := p.expect(TokenEqual); err != nil {
					return err
				}
				expr, err := p.parseExpression()
				if err != nil {
					return err
				}
				conflict.Updates = append(conflict.Updates, SetClause{Column: column, Expr: expr})
				if p.cur.Type != TokenComma {
					break
				}
				p.advance()
			}
		}
		stmt.Conflict = conflict
	}
	if p.consumeWord("RETURNING") {
		columns, err := p.parseColumnList()
		if err != nil {
			return err
		}
		stmt.Returning = columns
	}
	return nil
}

func (p *Parser) parseTableElements() ([]ColumnDefAST, []TableConstraint, error) {
	var columns []ColumnDefAST
	var constraints []TableConstraint
	for {
		if p.cur.Type == TokenUNIQUE {
			p.advance()
			cols, err := p.parseParenthesizedColumns()
			if err != nil {
				return nil, nil, err
			}
			constraints = append(constraints, TableConstraint{Type: ConstraintUnique, Columns: cols})
		} else if p.cur.Type == TokenPRIMARY {
			p.advance()
			if _, err := p.expect(TokenKEY); err != nil {
				return nil, nil, err
			}
			cols, err := p.parseParenthesizedColumns()
			if err != nil {
				return nil, nil, err
			}
			constraints = append(constraints, TableConstraint{Type: ConstraintPrimaryKey, Columns: cols})
		} else if p.cur.Type == TokenFOREIGN {
			con, err := p.parseTableForeignKey()
			if err != nil {
				return nil, nil, err
			}
			constraints = append(constraints, con)
		} else if p.cur.Type == TokenCHECK {
			// A table-level CHECK. The engine does not evaluate table CHECKs,
			// but refusing to parse one means a whole schema will not open —
			// and named CHECK constraints are standard ORM output. Consume the
			// balanced expression and move on.
			if err := p.skipTableCheck(); err != nil {
				return nil, nil, err
			}
		} else if strings.EqualFold(p.cur.Value, "CONSTRAINT") && !p.cur.Quoted {
			// `CONSTRAINT <name> FOREIGN KEY ...` is what sqlite3 .dump and
			// most ORMs emit. CONSTRAINT is not a keyword token here, so
			// without this branch it fell through to parseColumnDef and became
			// a phantom column named "CONSTRAINT" — the same bug the bare
			// FOREIGN spelling had. The name is consumed and discarded: this
			// engine has no use for it, and dropping it changes no behavior.
			p.advance()
			if _, err := p.parseIdentifierOrKeyword(); err != nil {
				return nil, nil, err
			}
			continue
		} else {
			column, err := p.parseColumnDef()
			if err != nil {
				return nil, nil, err
			}
			columns = append(columns, column)
		}
		if p.cur.Type != TokenComma {
			break
		}
		p.advance()
	}
	return columns, constraints, nil
}

func (p *Parser) parseParenthesizedColumns() ([]string, error) {
	if _, err := p.expect(TokenLParen); err != nil {
		return nil, err
	}
	columns, err := p.parseColumnList()
	if err != nil {
		return nil, err
	}
	if _, err := p.expect(TokenRParen); err != nil {
		return nil, err
	}
	return columns, nil
}

// parseTableForeignKey reads a table-level
// `FOREIGN KEY (col) REFERENCES target(col)`.
//
// Before this existed the FOREIGN token fell through to parseColumnDef,
// which happily read it as a column named "FOREIGN" of type "KEY(col)" —
// so the table gained a phantom column, `SELECT *` returned it, and the
// REFERENCES constraint was registered against that column instead of the
// real one. A foreign key on a column no row ever fills cannot fail, which
// is why every FOREIGN KEY framework/migrate wrote went unenforced.
func (p *Parser) parseTableForeignKey() (TableConstraint, error) {
	if _, err := p.expect(TokenFOREIGN); err != nil {
		return TableConstraint{}, err
	}
	if _, err := p.expect(TokenKEY); err != nil {
		return TableConstraint{}, err
	}
	cols, err := p.parseParenthesizedColumns()
	if err != nil {
		return TableConstraint{}, err
	}
	if _, err := p.expect(TokenREFERENCES); err != nil {
		return TableConstraint{}, err
	}
	refTable, err := p.parseIdentifierOrKeyword()
	if err != nil {
		return TableConstraint{}, err
	}
	var refCols []string
	if p.cur.Type == TokenLParen {
		refCols, err = p.parseParenthesizedColumns()
		if err != nil {
			return TableConstraint{}, err
		}
	}
	// ForeignKeyInfo holds a single FromCol, so a composite key has nowhere
	// to go. Refusing here is deliberate: silently dropping the constraint is
	// exactly the failure this function exists to fix, and a wrong-but-quiet
	// database is worse than one that will not open.
	if len(cols) != 1 {
		return TableConstraint{}, p.errorf("composite FOREIGN KEY (%d columns) is not supported", len(cols))
	}
	if len(refCols) > 1 {
		return TableConstraint{}, p.errorf("composite FOREIGN KEY reference (%d columns) is not supported", len(refCols))
	}
	if err := p.skipForeignKeyTrailers(); err != nil {
		return TableConstraint{}, err
	}
	return TableConstraint{Type: ConstraintForeignKey, Columns: cols, RefTable: refTable, RefCols: refCols}, nil
}

// skipForeignKeyTrailers consumes the clauses SQLite allows after a
// `REFERENCES target(cols)` and this engine does not implement: referential
// actions (`ON DELETE|UPDATE <action>`), `MATCH <name>`, and the deferrability
// clauses.
//
// Ignoring them is a real limitation, but REFUSING them is a worse one: the
// named-constraint-plus-cascade spelling is what `sqlite3 .dump`, Django, and
// SQLAlchemy emit, so a parse error means such a schema cannot be opened at
// all. Ignored actions fail safe here — with no cascade machinery the engine
// refuses a delete that would orphan rows, which is stricter than the declared
// CASCADE, never looser.
func (p *Parser) skipForeignKeyTrailers() error {
	for {
		switch {
		case p.cur.Type == TokenON || strings.EqualFold(p.cur.Value, "ON"):
			p.advance() // ON
			// DELETE or UPDATE
			p.advance()
			// The action: CASCADE | RESTRICT | NO ACTION | SET NULL | SET DEFAULT
			switch {
			case strings.EqualFold(p.cur.Value, "NO"), strings.EqualFold(p.cur.Value, "SET"):
				p.advance()
				p.advance()
			default:
				p.advance()
			}
		case strings.EqualFold(p.cur.Value, "MATCH"):
			p.advance()
			p.advance() // the match type name
		case p.cur.Type == TokenNOT || strings.EqualFold(p.cur.Value, "NOT"):
			// NOT DEFERRABLE [INITIALLY ...]. p.peek is the real one-token
			// lookahead; peekAhead is a stub that returns an empty Token, so
			// a check against it silently never matches.
			if !strings.EqualFold(p.peek.Value, "DEFERRABLE") {
				return nil
			}
			p.advance()
			p.advance()
			p.skipInitially()
		case strings.EqualFold(p.cur.Value, "DEFERRABLE"):
			p.advance()
			p.skipInitially()
		default:
			return nil
		}
	}
}

func (p *Parser) skipInitially() {
	if strings.EqualFold(p.cur.Value, "INITIALLY") {
		p.advance()
		p.advance() // DEFERRED | IMMEDIATE
	}
}

// skipTableCheck consumes a table-level `CHECK ( <expr> )`, tracking nesting so
// a parenthesised subexpression does not end the scan early.
func (p *Parser) skipTableCheck() error {
	if _, err := p.expect(TokenCHECK); err != nil {
		return err
	}
	if _, err := p.expect(TokenLParen); err != nil {
		return err
	}
	depth := 1
	for depth > 0 {
		switch p.cur.Type {
		case TokenLParen:
			depth++
		case TokenRParen:
			depth--
		case TokenEOF:
			return p.errorf("unterminated CHECK constraint")
		}
		p.advance()
	}
	return nil
}
