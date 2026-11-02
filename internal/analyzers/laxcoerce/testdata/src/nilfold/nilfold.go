// Package nilfold is battery/auth's token scan before 8b19113f: coerceTime
// answers time.Time{} for NULL and for a value it cannot read, and
// timePtrFromRaw turns that zero into nil, so an unreadable revoked_at
// read as "not revoked".
package nilfold

import "time"

type APIToken struct {
	ExpiresAt  *time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
	DisabledAt time.Time
	Name       string
}

func parseTimeFlex(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// The coercer: case nil and the after-switch return are the same zero.
func coerceTime(src any) time.Time {
	switch v := src.(type) {
	case time.Time:
		return v
	case string:
		return parseTimeFlex(v)
	case nil:
		return time.Time{}
	}
	return time.Time{}
}

// The wrapper: hands its own any straight to the coercer.
func timePtrFromRaw(src any) *time.Time {
	t := coerceTime(src)
	if t.IsZero() {
		return nil
	}
	return &t
}

// The fix: nil is split off first; a non-NULL value it cannot read is set.
func timePtrFailClosed(src any) *time.Time {
	if src == nil {
		return nil
	}
	if t := coerceTime(src); !t.IsZero() {
		return &t
	}
	epoch := time.Unix(0, 0).UTC()
	return &epoch
}

func scanPreFix(expiresRaw, lastUsedRaw, revokedRaw any) *APIToken {
	var t APIToken
	t.ExpiresAt = timePtrFromRaw(expiresRaw) // want `timePtrFromRaw answers the same zero for NULL and for a value it cannot read, so an unreadable ExpiresAt reads as unset`
	t.LastUsedAt = timePtrFromRaw(lastUsedRaw)
	t.RevokedAt = timePtrFromRaw(revokedRaw) // want `unreadable RevokedAt reads as unset`
	return &t
}

func scanFixed(expiresRaw, lastUsedRaw, revokedRaw any) *APIToken {
	var t APIToken
	t.ExpiresAt = timePtrFailClosed(expiresRaw)
	t.LastUsedAt = timePtrFromRaw(lastUsedRaw)
	t.RevokedAt = timePtrFailClosed(revokedRaw)
	return &t
}

// The composite-literal sink, straight from the coercer.
func literal(raw any) APIToken {
	return APIToken{DisabledAt: coerceTime(raw)} // want `coerceTime answers the same zero for NULL`
}

// A coercer whose own name says it reads a fail-open fact fires at the switch.
func revokedAt(src any) time.Time {
	switch v := src.(type) { // want `revokedAt answers the same zero for NULL`
	case time.Time:
		return v
	case nil:
		return time.Time{}
	default:
		return time.Time{}
	}
}

// Near miss: the unknown arm answers something else (a sentinel), so
// unreadable and NULL stay apart.
func expiresAtStrict(src any) time.Time {
	switch v := src.(type) {
	case time.Time:
		return v
	case nil:
		return time.Time{}
	default:
		return time.Unix(0, 0)
	}
}

func useStrict(raw any) APIToken {
	return APIToken{DisabledAt: expiresAtStrict(raw)}
}

// Near miss: a switch with no case nil folds nothing it names.
func coerceName(src any) string {
	switch v := src.(type) {
	case string:
		return v
	}
	return ""
}

func useName(raw any) APIToken {
	return APIToken{Name: coerceName(raw)}
}

type Session struct{ ExpiresAt time.Time }

// Near miss: a value-typed ExpiresAt holds year 1 for the folded zero,
// which now.After(exp) reads as expired: the fold fails closed there.
func session(raw any) Session {
	return Session{ExpiresAt: coerceTime(raw)}
}
