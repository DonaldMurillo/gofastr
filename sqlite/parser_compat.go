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
