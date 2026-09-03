// Package dominance provides the statement-dominance walk shared by
// the analyzers under internal/analyzers: which statements and
// conditions of a function body are guaranteed to execute before a
// given node.
//
// Dominance here is lexical. A statement dominates node when it sits
// in the same block before the statement holding node, or in an
// enclosing block before the statement on the path down to node. A
// comparison buried inside the body of a nested conditional of an
// earlier statement (a flag-gated guard) dominates nothing: it runs
// only when the branch is taken. The one exception in the other
// direction: the Init and Cond of an enclosing if whose then-branch
// holds node always ran by the time node executes, and the cond held.
package dominance

import "go/ast"

// Parents maps every node in body to its parent node.
func Parents(body ast.Node) map[ast.Node]ast.Node {
	parents := map[ast.Node]ast.Node{}
	var stack []ast.Node
	ast.Inspect(body, func(n ast.Node) bool {
		if n == nil {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			return true
		}
		if len(stack) > 0 {
			parents[n] = stack[len(stack)-1]
		}
		stack = append(stack, n)
		return true
	})
	return parents
}

// Prefix returns the statement lists that dominate node: for each
// enclosing block (BlockStmt, case or comm clause) on the path from
// node up to body, the statements before the one on the path.
func Prefix(node ast.Node, parents map[ast.Node]ast.Node, body ast.Node) [][]ast.Stmt {
	var out [][]ast.Stmt
	cur := node
	for {
		p, ok := parents[cur]
		if !ok || p == nil {
			break
		}
		var stmts []ast.Stmt
		switch b := p.(type) {
		case *ast.BlockStmt:
			stmts = b.List
		case *ast.CaseClause:
			stmts = b.Body
		case *ast.CommClause:
			stmts = b.Body
		}
		if stmts != nil {
			for i, st := range stmts {
				if st == cur || containsPath(st, node, parents) {
					out = append(out, stmts[:i])
					break
				}
			}
		}
		if p == body {
			break
		}
		cur = p
	}
	return out
}

// EnclosingIfConds returns the conditions of the enclosing if
// statements whose then-branch holds node: node executes only when
// each such condition held, so a bound in one of them dominates.
// Else-branches contribute nothing (they run when the condition is
// false).
func EnclosingIfConds(node ast.Node, parents map[ast.Node]ast.Node, body ast.Node) []ast.Expr {
	var out []ast.Expr
	cur := node
	for {
		p, ok := parents[cur]
		if !ok || p == nil {
			break
		}
		if iff, ok := p.(*ast.IfStmt); ok && cur == iff.Body && p != body && iff.Cond != nil {
			out = append(out, iff.Cond)
		}
		if p == body {
			break
		}
		cur = p
	}
	return out
}

// Spine returns the parts of n that execute whenever n is reached: for
// an if, its Init and Cond; for loops and switches, their controlling
// expressions; for plain statements, their expressions — never the
// bodies of nested conditionals, select clauses, or function literals.
// A comparison found by walking Spine of a dominating statement is a
// dominating comparison; one found only inside a skipped body is not.
func Spine(n ast.Node) []ast.Node {
	var out []ast.Node
	var expr func(ast.Node)
	var stmt func(ast.Stmt)

	expr = func(e ast.Node) {
		if e == nil {
			return
		}
		ast.Inspect(e, func(c ast.Node) bool {
			if c == nil {
				return true
			}
			if _, ok := c.(*ast.FuncLit); ok {
				return false
			}
			out = append(out, c)
			return true
		})
	}

	stmt = func(s ast.Stmt) {
		switch x := s.(type) {
		case *ast.IfStmt:
			out = append(out, x)
			stmt(x.Init)
			expr(x.Cond)
		case *ast.ForStmt:
			out = append(out, x)
			stmt(x.Init)
			expr(x.Cond)
			// x.Post is excluded: it runs only after a NORMAL
			// iteration, so an immediate break reaches later
			// statements without it.
		case *ast.RangeStmt:
			out = append(out, x)
			expr(x.X)
			// x.Key and x.Value are excluded: they are assigned (and
			// their expressions evaluated) only when the range yields,
			// so an empty range reaches later statements without them.
		case *ast.SwitchStmt:
			out = append(out, x)
			stmt(x.Init)
			expr(x.Tag)
		case *ast.TypeSwitchStmt:
			out = append(out, x)
			stmt(x.Init)
			stmt(x.Assign)
		case *ast.BlockStmt:
			out = append(out, x)
			for _, inner := range x.List {
				stmt(inner)
			}
		case *ast.AssignStmt:
			out = append(out, x)
			for _, e := range x.Lhs {
				expr(e)
			}
			for _, e := range x.Rhs {
				expr(e)
			}
		case *ast.ExprStmt:
			out = append(out, x)
			expr(x.X)
		case *ast.ReturnStmt:
			out = append(out, x)
			for _, e := range x.Results {
				expr(e)
			}
		case *ast.DeferStmt:
			out = append(out, x)
			expr(x.Call)
		case *ast.GoStmt:
			out = append(out, x)
			expr(x.Call)
		case *ast.SendStmt:
			out = append(out, x)
			expr(x.Chan)
			expr(x.Value)
		case *ast.IncDecStmt:
			out = append(out, x)
			expr(x.X)
		case *ast.DeclStmt:
			out = append(out, x)
			expr(x.Decl)
		case *ast.LabeledStmt:
			stmt(x.Stmt)
		case nil:
		default:
			out = append(out, s)
		}
	}

	if st, ok := n.(ast.Stmt); ok {
		stmt(st)
	} else {
		expr(n)
	}
	return out
}

// containsPath reports whether node sits inside st per the parent map,
// for when the path child is nested below statement level.
func containsPath(st ast.Node, node ast.Node, parents map[ast.Node]ast.Node) bool {
	for c := node; c != nil; {
		p, ok := parents[c]
		if !ok || p == nil {
			return false
		}
		if p == st {
			return true
		}
		c = p
	}
	return false
}
