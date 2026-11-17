package contracts

import (
	"fmt"
	"strings"
)

// Severity is how loudly a diagnostic lands, ordered off < info < warn < error.
type Severity int

const (
	// SeverityOff suppresses the rule; only config can set it.
	SeverityOff Severity = iota
	// SeverityInfo reports without affecting the exit code.
	SeverityInfo
	// SeverityWarn fails the run only when FailOn is warn or lower.
	SeverityWarn
	// SeverityError fails the verify run.
	SeverityError
)

var severityNames = map[Severity]string{
	SeverityOff:   "off",
	SeverityInfo:  "info",
	SeverityWarn:  "warn",
	SeverityError: "error",
}

func (s Severity) String() string {
	if n, ok := severityNames[s]; ok {
		return n
	}
	return fmt.Sprintf("severity(%d)", int(s))
}

// ParseSeverity resolves a config or flag value, accepting aliases such as
// `warning`, `err` and `false`. Anything else is an error.
func ParseSeverity(s string) (Severity, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "off", "none", "disabled", "false":
		return SeverityOff, nil
	case "info", "note", "hint":
		return SeverityInfo, nil
	case "warn", "warning":
		return SeverityWarn, nil
	case "error", "err", "fail", "true":
		return SeverityError, nil
	}
	return SeverityError, fmt.Errorf("unknown severity %q: use off, info, warn, or error", s)
}

// MarshalText makes Severity round-trip through JSON as its name.
func (s Severity) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// UnmarshalText parses the name form written by MarshalText.
func (s *Severity) UnmarshalText(b []byte) error {
	parsed, err := ParseSeverity(string(b))
	if err != nil {
		return err
	}
	*s = parsed
	return nil
}

func (s Severity) sarifLevel() string {
	switch s {
	case SeverityError:
		return "error"
	case SeverityWarn:
		return "warning"
	default:
		return "note"
	}
}
