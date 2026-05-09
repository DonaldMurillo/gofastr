package expr

import (
	"errors"
	"fmt"
)

// Scope resolves named variables to runtime values.
type Scope interface {
	Lookup(name string) (any, bool)
}

// MapScope is a Scope backed by a map.
type MapScope map[string]any

// Lookup implements Scope.
func (m MapScope) Lookup(name string) (any, bool) {
	if m == nil {
		return nil, false
	}
	v, ok := m[name]
	return v, ok
}

// Func is a user-callable function.
type Func func(args []any) (any, error)

// Env holds built-in and user-registered functions.
type Env struct {
	Functions map[string]Func
}

// Register adds or replaces a function in env.
func (e *Env) Register(name string, fn Func) {
	if e.Functions == nil {
		e.Functions = map[string]Func{}
	}
	e.Functions[name] = fn
}

// Sentinel errors built-in functions return for arity / type mistakes.
var (
	ErrArity = errors.New("expr: wrong number of arguments")
	ErrType  = errors.New("expr: wrong argument type")
)

// Expression is a compiled, ready-to-evaluate expression tree.
type Expression struct {
	root node
	src  string
}

// Source returns the original source string the expression was compiled from.
func (e *Expression) Source() string { return e.src }

// Compile parses src into an Expression. The expression can then be Eval'd
// repeatedly with different scopes.
func Compile(src string) (*Expression, error) {
	if src == "" {
		return nil, fmt.Errorf("expr: empty source")
	}
	tokens, err := lex(src)
	if err != nil {
		return nil, err
	}
	p := &parser{tokens: tokens}
	root, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tokEOF {
		return nil, fmt.Errorf("expr: unexpected token %q at end", p.peek().value)
	}
	return &Expression{root: root, src: src}, nil
}

// Eval evaluates the compiled expression with the given scope and env.
// A nil env means DefaultEnv. A nil scope means an empty scope.
func (e *Expression) Eval(scope Scope, env *Env) (any, error) {
	if env == nil {
		env = DefaultEnv()
	}
	if scope == nil {
		scope = MapScope{}
	}
	return e.root.eval(scope, env)
}

// EvalBool compiles and evaluates src, asserting the result is a bool.
func EvalBool(src string, scope Scope, env *Env) (bool, error) {
	e, err := Compile(src)
	if err != nil {
		return false, err
	}
	v, err := e.Eval(scope, env)
	if err != nil {
		return false, err
	}
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("expr: expected bool result, got %T", v)
	}
	return b, nil
}
