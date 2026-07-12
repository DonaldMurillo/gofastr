package auth

import "context"

// UpdateRoles stubs for test-only UserStore implementors. Adding
// UpdateRoles to the UserStore interface requires every implementor to
// have the method; these stubs keep the test suite compiling. The
// in-memory stores that track users (memoryUserStore, e2eFullStore,
// linkingStore, linkingUserStore) get real implementations; the rest
// get no-op stubs.

// --- memoryUserStore (manager_test.go) ---

func (s *memoryUserStore) UpdateRoles(_ context.Context, userID string, roles []string) error {
	e, ok := s.byID[userID]
	if !ok {
		return ErrUserNotFound
	}
	if bu, ok := e.user.(*BasicUser); ok {
		bu.Roles = roles
	} else {
		e.user = &BasicUser{ID: e.user.GetID(), Email: e.user.GetEmail(), Roles: roles}
	}
	return nil
}

// --- e2eFullStore (e2e_happy_path_test.go) ---

func (s *e2eFullStore) UpdateRoles(_ context.Context, userID string, roles []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.byID[userID]
	if !ok {
		return ErrUserNotFound
	}
	if bu, ok := e.user.(*BasicUser); ok {
		bu.Roles = roles
	} else {
		e.user = &BasicUser{ID: e.user.GetID(), Email: e.user.GetEmail(), Roles: roles}
	}
	return nil
}

// --- linkingStore (accounts_test.go) ---

func (s *linkingStore) UpdateRoles(_ context.Context, userID string, roles []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byID[userID]
	if !ok {
		return ErrUserNotFound
	}
	if bu, ok := u.(*BasicUser); ok {
		bu.Roles = roles
	} else {
		s.byID[userID] = &BasicUser{ID: u.GetID(), Email: u.GetEmail(), Roles: roles}
	}
	return nil
}

// --- linkingUserStore (oauth2_test.go) ---

func (s *linkingUserStore) UpdateRoles(_ context.Context, userID string, roles []string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	u, ok := s.byID[userID]
	if !ok {
		return ErrUserNotFound
	}
	if bu, ok := u.(*BasicUser); ok {
		bu.Roles = roles
	} else {
		s.byID[userID] = &BasicUser{ID: u.GetID(), Email: u.GetEmail(), Roles: roles}
	}
	return nil
}

// --- staticUserStore (apitoken_test.go) ---

func (s *staticUserStore) UpdateRoles(_ context.Context, _ string, _ []string) error {
	return nil
}

// --- flakyUserStore (dberror_test.go) ---

func (s *flakyUserStore) UpdateRoles(_ context.Context, _ string, _ []string) error {
	return s.err
}

// --- fakeUserStore (session_middleware_observability_test.go) ---

func (f *fakeUserStore) UpdateRoles(_ context.Context, _ string, _ []string) error {
	return nil
}

// --- recordingLister (users_test.go) ---

func (r *recordingLister) UpdateRoles(_ context.Context, _ string, _ []string) error {
	return nil
}
