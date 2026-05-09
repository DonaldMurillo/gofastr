package expr

import "fmt"

func (n *literalNode) eval(_ Scope, _ *Env) (any, error) { return n.value, nil }

func (n *identNode) eval(scope Scope, _ *Env) (any, error) {
	v, ok := scope.Lookup(n.name)
	if !ok {
		return nil, fmt.Errorf("expr: undefined identifier %q", n.name)
	}
	return v, nil
}

func (n *memberNode) eval(scope Scope, env *Env) (any, error) {
	target, err := n.target.eval(scope, env)
	if err != nil {
		return nil, err
	}
	m, ok := target.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("expr: cannot access field %q on %T", n.field, target)
	}
	v, exists := m[n.field]
	if !exists {
		return nil, fmt.Errorf("expr: missing field %q", n.field)
	}
	return v, nil
}

func (n *indexNode) eval(scope Scope, env *Env) (any, error) {
	target, err := n.target.eval(scope, env)
	if err != nil {
		return nil, err
	}
	idx, err := n.index.eval(scope, env)
	if err != nil {
		return nil, err
	}
	switch t := target.(type) {
	case []any:
		i, ok := toInt(idx)
		if !ok {
			return nil, fmt.Errorf("expr: list index must be int, got %T", idx)
		}
		if i < 0 || int(i) >= len(t) {
			return nil, fmt.Errorf("expr: index %d out of range", i)
		}
		return t[i], nil
	case map[string]any:
		key, ok := idx.(string)
		if !ok {
			return nil, fmt.Errorf("expr: map key must be string, got %T", idx)
		}
		v, exists := t[key]
		if !exists {
			return nil, fmt.Errorf("expr: missing key %q", key)
		}
		return v, nil
	default:
		return nil, fmt.Errorf("expr: cannot index %T", target)
	}
}

func (n *callNode) eval(scope Scope, env *Env) (any, error) {
	fn, ok := env.Functions[n.name]
	if !ok {
		return nil, fmt.Errorf("expr: undefined function %q", n.name)
	}
	args := make([]any, len(n.args))
	for i, a := range n.args {
		v, err := a.eval(scope, env)
		if err != nil {
			return nil, err
		}
		args[i] = v
	}
	return fn(args)
}

func (n *unaryNode) eval(scope Scope, env *Env) (any, error) {
	v, err := n.expr.eval(scope, env)
	if err != nil {
		return nil, err
	}
	switch n.op {
	case "!":
		b, ok := v.(bool)
		if !ok {
			return nil, fmt.Errorf("expr: ! requires bool, got %T", v)
		}
		return !b, nil
	case "-":
		switch x := v.(type) {
		case int64:
			return -x, nil
		case float64:
			return -x, nil
		}
		return nil, fmt.Errorf("expr: - requires number, got %T", v)
	case "+":
		return v, nil
	}
	return nil, fmt.Errorf("expr: unknown unary %q", n.op)
}

func (n *binaryNode) eval(scope Scope, env *Env) (any, error) {
	// Short-circuit boolean ops before evaluating right.
	if n.op == "&&" || n.op == "||" {
		l, err := n.left.eval(scope, env)
		if err != nil {
			return nil, err
		}
		lb, ok := l.(bool)
		if !ok {
			return nil, fmt.Errorf("expr: %s requires bool, got %T", n.op, l)
		}
		if n.op == "&&" && !lb {
			return false, nil
		}
		if n.op == "||" && lb {
			return true, nil
		}
		r, err := n.right.eval(scope, env)
		if err != nil {
			return nil, err
		}
		rb, ok := r.(bool)
		if !ok {
			return nil, fmt.Errorf("expr: %s requires bool, got %T", n.op, r)
		}
		return rb, nil
	}

	l, err := n.left.eval(scope, env)
	if err != nil {
		return nil, err
	}
	r, err := n.right.eval(scope, env)
	if err != nil {
		return nil, err
	}

	switch n.op {
	case "+", "-", "*", "/", "%":
		return arith(n.op, l, r)
	case "==":
		return equals(l, r), nil
	case "!=":
		return !equals(l, r), nil
	case "<", ">", "<=", ">=":
		return compare(n.op, l, r)
	}
	return nil, fmt.Errorf("expr: unknown binary %q", n.op)
}

func (n *listNode) eval(scope Scope, env *Env) (any, error) {
	out := make([]any, len(n.items))
	for i, it := range n.items {
		v, err := it.eval(scope, env)
		if err != nil {
			return nil, err
		}
		out[i] = v
	}
	return out, nil
}

// --- helpers -----------------------------------------------------------

func arith(op string, l, r any) (any, error) {
	if op == "+" {
		ls, lok := l.(string)
		rs, rok := r.(string)
		if lok && rok {
			return ls + rs, nil
		}
	}
	li, lf, lOK := numerics(l)
	ri, rf, rOK := numerics(r)
	if !lOK || !rOK {
		return nil, fmt.Errorf("expr: %s requires numbers, got %T and %T", op, l, r)
	}
	if li != nil && ri != nil {
		a, b := *li, *ri
		switch op {
		case "+":
			return a + b, nil
		case "-":
			return a - b, nil
		case "*":
			return a * b, nil
		case "/":
			if b == 0 {
				return nil, fmt.Errorf("expr: division by zero")
			}
			return a / b, nil
		case "%":
			if b == 0 {
				return nil, fmt.Errorf("expr: modulo by zero")
			}
			return a % b, nil
		}
	}
	a, b := lf, rf
	switch op {
	case "+":
		return a + b, nil
	case "-":
		return a - b, nil
	case "*":
		return a * b, nil
	case "/":
		if b == 0 {
			return nil, fmt.Errorf("expr: division by zero")
		}
		return a / b, nil
	case "%":
		return nil, fmt.Errorf("expr: %% requires integers")
	}
	return nil, fmt.Errorf("expr: unknown arith %q", op)
}

// numerics returns (intPtr, float, ok). If the value is a Go int64 we set
// intPtr; if it's a float64 we set only float; both ints and floats fall
// back to float.
func numerics(v any) (*int64, float64, bool) {
	switch n := v.(type) {
	case int64:
		f := float64(n)
		return &n, f, true
	case int:
		x := int64(n)
		f := float64(n)
		return &x, f, true
	case float64:
		return nil, n, true
	case float32:
		return nil, float64(n), true
	}
	return nil, 0, false
}

func equals(l, r any) bool {
	if l == nil || r == nil {
		return l == nil && r == nil
	}
	li, lf, lOK := numerics(l)
	ri, rf, rOK := numerics(r)
	if lOK && rOK {
		if li != nil && ri != nil {
			return *li == *ri
		}
		return lf == rf
	}
	return l == r
}

func compare(op string, l, r any) (any, error) {
	li, lf, lOK := numerics(l)
	ri, rf, rOK := numerics(r)
	if lOK && rOK {
		var a, b float64
		if li != nil && ri != nil {
			a, b = float64(*li), float64(*ri)
		} else {
			a, b = lf, rf
		}
		switch op {
		case "<":
			return a < b, nil
		case ">":
			return a > b, nil
		case "<=":
			return a <= b, nil
		case ">=":
			return a >= b, nil
		}
	}
	ls, lok := l.(string)
	rs, rok := r.(string)
	if lok && rok {
		switch op {
		case "<":
			return ls < rs, nil
		case ">":
			return ls > rs, nil
		case "<=":
			return ls <= rs, nil
		case ">=":
			return ls >= rs, nil
		}
	}
	return nil, fmt.Errorf("expr: cannot compare %T %s %T", l, op, r)
}

func toInt(v any) (int64, bool) {
	switch n := v.(type) {
	case int64:
		return n, true
	case int:
		return int64(n), true
	case float64:
		if float64(int64(n)) == n {
			return int64(n), true
		}
	}
	return 0, false
}
