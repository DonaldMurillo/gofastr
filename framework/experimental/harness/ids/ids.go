package ids

import (
	"errors"
	"fmt"

	"github.com/DonaldMurillo/gofastr/framework/experimental/harness/internal/ulid"
)

// Prefixes of the ID types, joined to a ULID with "_".
const (
	PrefixSession = "sess"
	PrefixLog     = "log"
	PrefixCall    = "call"
	PrefixToken   = "tok"
	PrefixClient  = "cli"
)

// Typed ID strings, one Go type per prefix.
type (
	SessionID string
	LogID     string
	CallID    string
	JTI       string
	ClientID  string
)

// NewSessionID returns a fresh SessionID.
func NewSessionID() SessionID { return SessionID(ulid.MustNewPrefixed(PrefixSession)) }

// NewLogID returns a fresh LogID.
func NewLogID() LogID { return LogID(ulid.MustNewPrefixed(PrefixLog)) }

// NewCallID returns a fresh CallID.
func NewCallID() CallID { return CallID(ulid.MustNewPrefixed(PrefixCall)) }

// NewJTI returns a fresh token ID.
func NewJTI() JTI { return JTI(ulid.MustNewPrefixed(PrefixToken)) }

// NewClientID returns a fresh ClientID.
func NewClientID() ClientID { return ClientID(ulid.MustNewPrefixed(PrefixClient)) }

// ValidSession reports whether s is a syntactically valid SessionID.
func ValidSession(s SessionID) bool { return validPrefixed(string(s), PrefixSession) }

// ValidLog reports whether s is a syntactically valid LogID.
func ValidLog(s LogID) bool { return validPrefixed(string(s), PrefixLog) }

// ValidCall reports whether s is a syntactically valid CallID.
func ValidCall(s CallID) bool { return validPrefixed(string(s), PrefixCall) }

// ValidJTI reports whether s is a syntactically valid JTI.
func ValidJTI(s JTI) bool { return validPrefixed(string(s), PrefixToken) }

// ValidClient reports whether s is a syntactically valid ClientID.
func ValidClient(s ClientID) bool { return validPrefixed(string(s), PrefixClient) }

func validPrefixed(s, want string) bool {
	prefix, _, err := ulid.SplitPrefixed(s)
	if err != nil {
		return false
	}
	return prefix == want
}

// ParseSession parses a SessionID string and validates the prefix.
func ParseSession(s string) (SessionID, error) {
	if err := parseExpect(s, PrefixSession); err != nil {
		return "", err
	}
	return SessionID(s), nil
}

// ParseLog parses a LogID string and validates the prefix.
func ParseLog(s string) (LogID, error) {
	if err := parseExpect(s, PrefixLog); err != nil {
		return "", err
	}
	return LogID(s), nil
}

// ParseCall parses a CallID string and validates the prefix.
func ParseCall(s string) (CallID, error) {
	if err := parseExpect(s, PrefixCall); err != nil {
		return "", err
	}
	return CallID(s), nil
}

// ParseJTI parses a JTI string and validates the prefix.
func ParseJTI(s string) (JTI, error) {
	if err := parseExpect(s, PrefixToken); err != nil {
		return "", err
	}
	return JTI(s), nil
}

// ParseClient parses a ClientID string and validates the prefix.
func ParseClient(s string) (ClientID, error) {
	if err := parseExpect(s, PrefixClient); err != nil {
		return "", err
	}
	return ClientID(s), nil
}

func parseExpect(s, want string) error {
	prefix, _, err := ulid.SplitPrefixed(s)
	if err != nil {
		return fmt.Errorf("ids: %w", err)
	}
	if prefix != want {
		return fmt.Errorf("ids: prefix %q, want %q", prefix, want)
	}
	return nil
}

// ErrInvalidPrefix is returned when a parsed ID has the wrong prefix.
var ErrInvalidPrefix = errors.New("ids: invalid prefix")
