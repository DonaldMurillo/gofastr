package analyzers

import (
	"fmt"
	"go/ast"
	"go/token"
	"regexp"
	"strings"

	"github.com/DonaldMurillo/gofastr/core-ui/ownstyle"
	"github.com/DonaldMurillo/gofastr/framework/contracts"
)

var (
	rePhysicalCorner = regexp.MustCompile(`^border-(top|bottom)-(left|right)-radius$`)
	rePhysicalEdge   = regexp.MustCompile(`^(margin|padding|scroll-margin|scroll-padding|border)-(left|right)(-(?:width|style|color))?$`)
	rePhysicalWord   = regexp.MustCompile(`(?:^|\s)(left|right)(?:\s|$)`)
	reFourValue      = regexp.MustCompile(`^(?:margin|padding|inset|scroll-margin|scroll-padding|border-width|border-style|border-color)$`)
	reDirOverride    = regexp.MustCompile(`:dir\((?:rtl|ltr)\)`)
	reBackgroundPos  = regexp.MustCompile(`(?i)background-position`)
)

func cssTokens(v string) []string {
	v = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "!important"))
	var out []string
	depth, start := 0, -1
	for i := 0; i < len(v); i++ {
		switch c := v[i]; {
		case c == '(':
			depth++
		case c == ')':
			depth--
		case (c == ' ' || c == '\t') && depth == 0:
			if start >= 0 {
				out = append(out, v[start:i])
				start = -1
			}
			continue
		}
		if start < 0 {
			start = i
		}
	}
	if start >= 0 {
		out = append(out, v[start:])
	}
	return out
}

func physicalSide(prop, value string, bgMirrored bool) (lit, want string, ok bool) {
	v := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "!important"))
	switch prop {
	case "left", "right":
		if v == "50%" || v == "auto" || strings.HasPrefix(v, "=") || !strings.ContainsAny(v, "0123456789({") {
			return "", "", false
		}
		side := map[string]string{"left": "start", "right": "end"}[prop]
		return prop, "inset-inline-" + side, true
	case "text-align", "float", "clear":
		side := map[string]string{"left": "start", "right": "end"}[v]
		if side == "" {
			return "", "", false
		}
		if prop != "text-align" {
			side = "inline-" + side
		}
		return prop + ": " + v, prop + ": " + side, true
	case "background-position", "background-position-x":
		if bgMirrored || !rePhysicalWord.MatchString(v) {
			return "", "", false
		}
		return prop + ": " + v, "a `:dir(rtl)` rule that mirrors it", true
	}
	if m := rePhysicalEdge.FindStringSubmatch(prop); m != nil {
		side := map[string]string{"left": "start", "right": "end"}[m[2]]
		return prop, m[1] + "-inline-" + side + m[3], true
	}
	if m := rePhysicalCorner.FindStringSubmatch(prop); m != nil {
		block := map[string]string{"top": "start", "bottom": "end"}[m[1]]
		inline := map[string]string{"left": "start", "right": "end"}[m[2]]
		return prop, "border-" + block + "-" + inline + "-radius", true
	}
	if reFourValue.MatchString(prop) {
		if t := cssTokens(v); len(t) == 4 && t[1] != t[3] {
			return prop + ": " + v, prop + "-block and " + prop + "-inline", true
		}
		return "", "", false
	}
	if prop == "border-radius" && !strings.Contains(v, "/") {
		t := cssTokens(v)
		switch {
		case len(t) == 2 && t[0] != t[1],
			len(t) == 3 && (t[0] != t[1] || t[2] != t[1]),
			len(t) == 4 && (t[0] != t[1] || t[2] != t[3]):
			return prop + ": " + v, "the border-start-start/start-end/end-end/end-start-radius longhands", true
		}
	}
	return "", "", false
}

func checkGoPhysicalSides(p *contracts.Pass, rel string, body []byte, lines []string) []contracts.Diagnostic {
	file, ok := p.AST(rel)
	if !ok {
		return nil
	}
	positions := p.FileSet().File(file.Pos())
	css := make([]byte, len(body))
	for i, c := range body {
		css[i] = ' '
		if c == '\n' {
			css[i] = c
		}
	}
	var literalStarts []int
	var previousEnd int
	for node := range ast.Preorder(file) {
		if lit, ok := node.(*ast.BasicLit); ok && lit.Kind == token.STRING {
			start, end := positions.Offset(lit.Pos()), positions.Offset(lit.End())
			text := stripComments(string(body[start:end]))
			for i := start; i < end; i++ {
				css[i] = ' '
			}
			if previousEnd == 0 || strings.TrimSpace(stripComments(string(body[previousEnd:start]))) != "+" {
				literalStarts = append(literalStarts, start)
			}
			copy(css[start+1:], text[1:len(text)-1])
			previousEnd = end
		}
	}
	return scanPhysicalSides(rel, string(css), lines, physicalBuilderPairs(file, positions), literalStarts)
}

func physicalBuilderPairs(file *ast.File, positions *token.File) map[int][]cssDeclaration {
	type builderCall struct {
		scope ast.Node
		list  ast.Node
		call  *ast.CallExpr
		recv  ast.Expr
		first int
	}
	type receiver struct {
		scope ast.Node
		name  string
	}
	type ruleCall struct {
		end  token.Pos
		list ast.Node
		dir  bool
	}
	ruleReceivers := make(map[receiver][]ruleCall)
	var calls []builderCall
	ast.PreorderStack(file, nil, func(node ast.Node, stack []ast.Node) bool {
		recv, method, call, ok := selectorCall(node)
		if !ok {
			return true
		}
		scope := cssBuilderScope(file, stack)
		var first int
		switch method {
		case "Rule":
			if id, ok := recv.(*ast.Ident); ok {
				key := receiver{scope, id.Name}
				list := builderStatementList(call, stack)
				ruleReceivers[key] = append(ruleReceivers[key], ruleCall{call.End(), list, dirSelectorArg(call, false)})
			}
			return true
		case "Set":
		case "Pseudo", "Child":
			first = 1
		default:
			return true
		}
		calls = append(calls, builderCall{scope, builderStatementList(call, stack), call, recv, first})
		return true
	})
	pairs := make(map[int][]cssDeclaration)
	for _, candidate := range calls {
		inDir, chained := styleRuleChain(candidate.recv)
		if id, isIdent := candidate.recv.(*ast.Ident); isIdent && !chained {
			rules, known := ruleReceivers[receiver{candidate.scope, id.Name}]
			if !known {
				continue
			}
			var latest token.Pos
			for _, rule := range rules {
				if rule.end <= candidate.call.Pos() && rule.end > latest {
					latest = rule.end
					inDir = rule.dir && rule.list != nil && rule.list == candidate.list
				}
			}
		} else if !chained {
			continue
		}
		if candidate.first == 1 {
			inDir = dirSelectorArg(candidate.call, inDir)
		}
		if inDir {
			continue
		}
		args := candidate.call.Args
		for i := candidate.first; i+1 < len(args); i += 2 {
			name, nameOK := stringLit(args[i])
			value, valueOK := stringLit(args[i+1])
			if nameOK && valueOK {
				line := positions.PositionFor(args[i].Pos(), false).Line
				pairs[line] = append(pairs[line], cssDeclaration{property: strings.ToLower(name), value: value})
			}
		}
	}
	return pairs
}

func builderStatementList(call *ast.CallExpr, stack []ast.Node) ast.Node {
	var child ast.Node = call
	for i := len(stack) - 1; i > 0; i-- {
		switch parent := stack[i].(type) {
		case *ast.SelectorExpr:
			if parent.X != child {
				return nil
			}
		case *ast.CallExpr:
			if parent.Fun != child {
				return nil
			}
		case *ast.ParenExpr:
		case *ast.ExprStmt:
			switch stack[i-1].(type) {
			case *ast.BlockStmt, *ast.CaseClause, *ast.CommClause:
				return stack[i-1]
			}
			return nil
		default:
			return nil
		}
		child = stack[i]
	}
	return nil
}

func dirSelectorArg(call *ast.CallExpr, inherited bool) bool {
	if len(call.Args) == 0 {
		return false
	}
	selector, ok := stringLit(call.Args[0])
	if !ok || (!inherited && !reDirOverride.MatchString(selector)) {
		return false
	}
	return selectorsHaveDir(ownstyle.Tokenize(selector), inherited, false)
}

func selectorsHaveDir(tokens []ownstyle.Token, inherited, rtlOnly bool) bool {
	depth, directional := 0, inherited
	for i, t := range tokens {
		switch t.Type {
		case ownstyle.TokenFunction, ownstyle.TokenOpenParen, ownstyle.TokenOpenSquare:
			if depth == 0 && t.Text == "dir(" && i > 0 && i+2 < len(tokens) &&
				tokens[i-1].Type == ownstyle.TokenColon && tokens[i-1].End == t.Start &&
				(tokens[i+1].Text == "rtl" || !rtlOnly && tokens[i+1].Text == "ltr") &&
				tokens[i+2].Type == ownstyle.TokenCloseParen {
				directional = true
			}
			depth++
		case ownstyle.TokenCloseParen, ownstyle.TokenCloseSquare:
			depth--
		case ownstyle.TokenComma:
			if depth == 0 {
				if !directional {
					return false
				}
				directional = false
			}
		}
	}
	return directional && depth == 0
}

// styleRuleChain reports whether recv chains back to a Rule call and
// whether that Rule's selector is a :dir() override.
func styleRuleChain(recv ast.Expr) (inDir, ok bool) {
	for {
		next, method, call, isCall := selectorCall(recv)
		if !isCall {
			return false, false
		}
		switch method {
		case "Rule":
			return dirSelectorArg(call, false), true
		case "Set", "Pseudo", "Child", "End":
			recv = next
		default:
			return false, false
		}
	}
}

func cssBuilderScope(file *ast.File, stack []ast.Node) ast.Node {
	for i := len(stack) - 1; i >= 0; i-- {
		switch stack[i].(type) {
		case *ast.FuncDecl, *ast.FuncLit:
			return stack[i]
		}
	}
	return file
}

func scanPhysicalSides(rel, css string, lines []string, pairs map[int][]cssDeclaration, literalStarts []int) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	directions := cssDirections(css, literalStarts)
	bgMirrored := false
	if directions != nil {
		for _, match := range reBackgroundPos.FindAllStringIndex(css, -1) {
			if directions[match[0]]&rtlScope != 0 {
				bgMirrored = true
				break
			}
		}
	}
	offset := 0
	for i, line := range strings.Split(css, "\n") {
		var inDir []cssDirection
		if directions != nil {
			inDir = directions[offset : offset+len(line)]
		}
		decls := appendCSSDeclarations(nil, line, ":", reDeclCSS)
		out = append(out, checkPhysicalSides(rel, i+1, decls, lines[i], bgMirrored, inDir)...)
		out = append(out, checkPhysicalSides(rel, i+1, pairs[i+1], lines[i], bgMirrored, nil)...)
		offset += len(line) + 1
	}
	return out
}

func checkPhysicalSides(rel string, lineNo int, decls []cssDeclaration, orig string, bgMirrored bool, inDir []cssDirection) []contracts.Diagnostic {
	var out []contracts.Diagnostic
	for _, decl := range decls {
		if decl.offset < len(inDir) && inDir[decl.offset]&dirScope != 0 {
			continue
		}
		lit, want, ok := physicalSide(decl.property, decl.value, bgMirrored)
		if !ok {
			continue
		}
		out = append(out, contracts.Diagnostic{
			RuleID: contracts.RulePhysicalSide,
			File:   rel,
			Line:   lineNo,
			Message: fmt.Sprintf("%s pins a physical side, which stays put under <html dir=\"rtl\">; write %s so the kit mirrors for right-to-left readers",
				lit, want),
			Snippet: strings.TrimSpace(orig),
		})
	}
	return out
}

type cssDirection uint8

const (
	dirScope cssDirection = 1 << iota
	rtlScope
)

func cssDirections(css string, literalStarts []int) []cssDirection {
	if !reDirOverride.MatchString(css) {
		return nil
	}
	directions := make([]cssDirection, len(css))
	start := 0
	for _, end := range literalStarts {
		markCSSDirections(css[start:end], directions[start:end])
		start = end
	}
	markCSSDirections(css[start:], directions[start:])
	return directions
}

func markCSSDirections(css string, directions []cssDirection) {
	if !reDirOverride.MatchString(css) {
		return
	}
	withoutBOM := strings.TrimPrefix(css, "\ufeff")
	directions = directions[len(css)-len(withoutBOM):]
	tokens := ownstyle.Tokenize(withoutBOM)
	scopes := []cssDirection{0}
	start, selectorStart, depth := 0, 0, 0
	fill := func(end int) {
		if scope := scopes[len(scopes)-1]; scope != 0 {
			for i := start; i < end; i++ {
				directions[i] = scope
			}
		}
	}
	for i, token := range tokens {
		switch token.Type {
		case ownstyle.TokenFunction, ownstyle.TokenOpenParen, ownstyle.TokenOpenSquare:
			depth++
		case ownstyle.TokenCloseParen, ownstyle.TokenCloseSquare:
			depth = max(depth-1, 0)
		case ownstyle.TokenSemicolon:
			if depth == 0 {
				selectorStart = i + 1
			}
		case ownstyle.TokenOpenCurly, ownstyle.TokenCloseCurly:
			if depth != 0 {
				continue
			}
			fill(token.Start)
			if token.Type == ownstyle.TokenOpenCurly {
				scope := scopes[len(scopes)-1]
				selector := tokens[selectorStart:i]
				if scope&dirScope == 0 && selectorsHaveDir(selector, false, false) {
					scope |= dirScope
				}
				if scope&rtlScope == 0 && selectorsHaveDir(selector, false, true) {
					scope |= rtlScope
				}
				scopes = append(scopes, scope)
			} else if len(scopes) > 1 {
				scopes = scopes[:len(scopes)-1]
			}
			start, selectorStart = token.End, i+1
		}
	}
	fill(len(directions))
}
