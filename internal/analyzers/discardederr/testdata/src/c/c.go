// Package c is a third discardederr positive with a different layout:
// a session method whose fetch refuses under load, plus the
// middle-position error as the documented negative.
package c

type store struct{}

// fetch returns (value, cached, error).
func (s *store) fetch(key string) (string, bool, error) { return "", false, nil }

// lookup returns (value, error, cached) with the error in the middle.
func lookup(key string) (string, error, bool) { return "", nil, false }

// cachedFetch keeps the value and the flag and drops the method's LAST
// error: a refused fetch reads as a cache miss.
func (s *store) cachedFetch(key string) string {
	v, _, _ := s.fetch(key) // want `assignment discards the error from fetch`
	return v
}

// cachedFetchChecked is the fix posture.
func (s *store) cachedFetchChecked(key string) (string, error) {
	v, _, err := s.fetch(key)
	if err != nil {
		return "", err
	}
	return v, nil
}

// middlePosition drops the error from a plain function with the error
// in the middle: silent since the narrowing (not a method, not last).
func middlePosition(key string) string {
	v, _, was := lookup(key)
	if !was {
		return ""
	}
	return v
}

// everythingDropped keeps nothing: a statement, not a hidden refusal.
func everythingDropped(key string) {
	_, _, _ = lookup(key)
}
