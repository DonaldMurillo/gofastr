package uinodev1

import (
	"encoding/json"
	"fmt"
)

// truncateErrInput clips s to at most maxErrInputLen bytes for safe
// inclusion in an error message. It never panics on multi-byte rune
// boundaries: it may split a rune, but the bytes themselves are valid
// UTF-8 only because the input was; the truncation marker makes the
// partial-rune visible. We prefer byte-level truncation here because
// attacker payloads are often not valid UTF-8 and the cap is a hard
// byte budget, not a rune budget.
func truncateErrInput(s string) string {
	if len(s) <= maxErrInputLen {
		return s
	}
	return s[:maxErrInputLen] + "...(truncated)"
}

// validationErr is the concrete error type every validator path returns.
// It always carries a stable Op (the high-level check that failed) plus
// a short human-facing reason. The reason never echoes more than
// maxErrInputLen bytes of attacker-controlled input.
type validationErr struct {
	op     string
	reason string
}

func (e *validationErr) Error() string {
	if e.op == "" {
		return "uinodev1: " + e.reason
	}
	return "uinodev1: " + e.op + ": " + e.reason
}

func errf(op, format string, args ...any) error {
	return &validationErr{op: op, reason: fmt.Sprintf(format, args...)}
}

// cap error helpers -------------------------------------------------------

func errCapExceeded(field string, got, max int) error {
	return errf("cap", "%s exceeded: got %d, max %d", field, got, max)
}

// per-component validation helpers ---------------------------------------

func errRequired(field string) error {
	return errf("required", "%s is required", field)
}

func errRequiredIndex(field string, idx int) error {
	return errf("required", "%s[%d] missing required field", field, idx)
}

func errBadEnum(field, value string) error {
	return errf("enum", "%s=%q is not a permitted value", field, truncateErrInput(value))
}

func errBadRange(field string, value, lo, hi any) error {
	return errf("range", "%s=%v out of range [%v,%v]", field, value, lo, hi)
}

func errTooLong(field string, got, max int) error {
	return errf("cap", "%s too long: got %d bytes, max %d", field, got, max)
}

func errBadURL(field, value string) error {
	return errf("url", "%s=%q is not a host-relative same-origin path", field, truncateErrInput(value))
}

func errUnknownComponent(name string) error {
	return errf("component", "unknown component %q (not in the ui.node.v1 closed enum)", truncateErrInput(name))
}

func errActionRefShape(reason string) error {
	return errf("action_ref", "%s", reason)
}

func errChildPolicy(comp Component) error {
	return errf("children", "component %q does not accept children", string(comp))
}

func errButtonNeedsActionRef() error {
	return errf("action_ref", "button requires an action_ref")
}

func errLinkNeedsToOrActionRef() error {
	return errf("action_ref", "link requires either props.to or action_ref (exactly one)")
}

func errActionRefOnWrongComponent(comp Component) error {
	return errf("action_ref", "action_ref not permitted on component %q (only button/link)", string(comp))
}

func errEmpty() error {
	return errf("input", "empty input")
}

func errRootNotObject() error {
	return errf("input", "root must be a JSON object")
}

func errNullNode() error {
	return errf("input", "null node encountered")
}

func errTrailingJSON() error {
	return errf("input", "unexpected trailing JSON after root object")
}

func errDupKey(key string) error {
	return errf("input", "duplicate JSON key %q", truncateErrInput(key))
}

func errDecode(underlying error) error {
	if underlying == nil {
		return nil
	}
	// Truncate any attacker-controlled content embedded in the message.
	msg := underlying.Error()
	if len(msg) <= maxErrInputLen*2 {
		return &validationErr{op: "decode", reason: msg}
	}
	return &validationErr{op: "decode", reason: truncateErrInput(msg)}
}

// asJSONError extracts the underlying json error type so callers can
// branch on it (used internally; not part of the public API).
func asJSONError(err error) (je *json.UnmarshalTypeError, ok bool) {
	if err == nil {
		return nil, false
	}
	if ue, uok := err.(*json.UnmarshalTypeError); uok {
		return ue, true
	}
	return nil, false
}
