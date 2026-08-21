package contracts

import (
	"fmt"
	"strings"
)

// Severity is how loudly a diagnostic lands. A config may move a rule
// either way along this list; what the ordering buys is that every move
// *down* from the catalog default is a relaxation, which the report
// names out loud (see [Config.Relaxations]), turning a check off is a
// decision the whole team gets to see, not a line in a YAML file.
type Severity int

const (
	// SeverityOff suppresses the rule entirely. Only reachable through
	// configuration: no rule declares it.
	SeverityOff Severity = iota
	// SeverityInfo reports without affecting the exit code.
	SeverityInfo
	// SeverityWarn reports and affects the exit code only under
	// `--severity=warn` (or `strict: true` in the AI section).
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

// ParseSeverity resolves a config or flag value. `warning` and `err` are
// accepted alongside the canonical spellings; anything else is an error
// rather than a silent default, because a typo'd severity that quietly
// means "error" would make a relaxation look applied when it is not.
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

// sarifLevel maps a severity onto the SARIF 2.1.0 result level
// vocabulary. SARIF has no "off": a suppressed diagnostic is dropped
// before it reaches the writer.
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
