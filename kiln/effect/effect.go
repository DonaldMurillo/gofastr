// Package effect runs declarative actions described by world.Action.
//
// Effects are the runtime side of Kiln's "no Go code from the agent"
// stance: every behavior the agent can author — validators, hooks,
// computed defaults, audit emits — is one of a closed set of action
// kinds whose parameters are themselves expressions. effect.Run is the
// single dispatcher.
package effect

import (
	"context"
	"fmt"

	"github.com/DonaldMurillo/gofastr/kiln/expr"
	"github.com/DonaldMurillo/gofastr/kiln/world"
)

// Scope is the runtime context an action sees. Entity is the data being
// operated on (typically map[string]any for create/update, string id for
// delete). Ctx exposes request-derived values (user, tenant, request).
// Audit and Emit are hooks the host wires for ActionAudit / ActionEmitEvent.
type Scope struct {
	Entity any
	Ctx    map[string]any
	Result any
	Audit  func(AuditRecord)
	Emit   func(EventRecord)
}

// AuditRecord is what ActionAudit produces.
type AuditRecord struct {
	Channel string
	Message string
}

// EventRecord is what ActionEmitEvent produces.
type EventRecord struct {
	Topic string
	Data  any
}

// Run dispatches an action. Errors from the action propagate; for
// validate-style actions, the error is the validation message.
func Run(ctx context.Context, a world.Action, scope Scope) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	switch a.Kind {
	case "", world.ActionNoop:
		return nil
	case world.ActionValidate:
		return runValidate(a, scope)
	case world.ActionSetField:
		return runSetField(a, scope)
	case world.ActionAudit:
		return runAudit(a, scope)
	case world.ActionEmitEvent:
		return runEmit(a, scope)
	default:
		return fmt.Errorf("effect: unknown action kind %q", a.Kind)
	}
}

// RunHook gates a hook on its Condition (if any) and then runs its Action.
// Returns nil if the condition is false (no error, no side effect).
func RunHook(ctx context.Context, h *world.Hook, scope Scope) error {
	if h == nil {
		return nil
	}
	if h.Condition != "" {
		ok, err := expr.EvalBool(h.Condition, exprScope(scope), nil)
		if err != nil {
			return fmt.Errorf("hook %q condition: %w", h.ID, err)
		}
		if !ok {
			return nil
		}
	}
	return Run(ctx, h.Action, scope)
}

// --- internals ---------------------------------------------------------

func exprScope(s Scope) expr.Scope {
	out := expr.MapScope{}
	if s.Entity != nil {
		out["entity"] = s.Entity
	}
	if s.Ctx != nil {
		out["ctx"] = s.Ctx
	}
	if s.Result != nil {
		out["result"] = s.Result
	}
	return out
}

func paramString(a world.Action, key string) string {
	v, ok := a.Params[key]
	if !ok || v == nil {
		return ""
	}
	s, _ := v.(string)
	return s
}

func runValidate(a world.Action, s Scope) error {
	src := paramString(a, "expression")
	msg := paramString(a, "message")
	if src == "" {
		return fmt.Errorf("validate: missing expression")
	}
	ok, err := expr.EvalBool(src, exprScope(s), nil)
	if err != nil {
		return fmt.Errorf("validate: %w", err)
	}
	if !ok {
		if msg == "" {
			msg = "validation failed: " + src
		}
		return fmt.Errorf("%s", msg)
	}
	return nil
}

func runSetField(a world.Action, s Scope) error {
	field := paramString(a, "field")
	if field == "" {
		return fmt.Errorf("set_field: missing field")
	}
	target, ok := s.Entity.(map[string]any)
	if !ok {
		return fmt.Errorf("set_field: entity is %T, want map[string]any", s.Entity)
	}
	if lit, ok := a.Params["value_literal"]; ok {
		target[field] = lit
		return nil
	}
	src := paramString(a, "value")
	if src == "" {
		return fmt.Errorf("set_field: missing value or value_literal")
	}
	v, err := evalExpr(src, s)
	if err != nil {
		return fmt.Errorf("set_field: %w", err)
	}
	target[field] = v
	return nil
}

func runAudit(a world.Action, s Scope) error {
	if s.Audit == nil {
		return nil
	}
	channel := paramString(a, "channel")
	src := paramString(a, "message")
	msg := ""
	if src != "" {
		v, err := evalExpr(src, s)
		if err != nil {
			return fmt.Errorf("audit: %w", err)
		}
		msg = fmt.Sprint(v)
	}
	s.Audit(AuditRecord{Channel: channel, Message: msg})
	return nil
}

func runEmit(a world.Action, s Scope) error {
	if s.Emit == nil {
		return nil
	}
	topic := paramString(a, "topic")
	if topic == "" {
		return fmt.Errorf("emit_event: missing topic")
	}
	src := paramString(a, "data")
	var data any
	if src != "" {
		v, err := evalExpr(src, s)
		if err != nil {
			return fmt.Errorf("emit_event: %w", err)
		}
		data = v
	}
	s.Emit(EventRecord{Topic: topic, Data: data})
	return nil
}

func evalExpr(src string, s Scope) (any, error) {
	e, err := expr.Compile(src)
	if err != nil {
		return nil, err
	}
	return e.Eval(exprScope(s), nil)
}
