package a

import "context"

// A test that blanks pending to set up a session is not a login path.
func setup(m *manager) *Session {
	sess, _, _ := m.MintSession(context.Background(), "u")
	return sess
}
