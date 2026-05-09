// Package expr is Kiln's tiny expression evaluator. It exists so the
// world IR can describe predicates, computed values, and conditions
// declaratively — without requiring an embedded Go interpreter.
//
// The grammar is a deliberately small subset:
//
//	expr     = or
//	or       = and ("||" and)*
//	and      = comp ("&&" comp)*
//	comp     = add (("==" | "!=" | "<" | ">" | "<=" | ">=") add)?
//	add      = mul (("+" | "-") mul)*
//	mul      = unary (("*" | "/" | "%") unary)*
//	unary    = ("!" | "-") unary | postfix
//	postfix  = primary ("." ident | "[" expr "]" | "(" args? ")")*
//	primary  = number | string | bool | null | ident | "(" expr ")" | "[" args? "]"
//
// Values are JSON-shaped: int64, float64, string, bool, nil, []any,
// map[string]any. Built-in functions live in Env (see DefaultEnv); the
// caller may register more.
package expr
