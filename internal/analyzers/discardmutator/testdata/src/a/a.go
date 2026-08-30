package a

import "net/http"

type SessionStore struct{}

func (s *SessionStore) Delete(tok string) error      { return nil }
func (s *SessionStore) Revoke(tok string) error      { return nil }
func (s *SessionStore) MarkRevoked(tok string) error { return nil }

type tokenRegistry struct{}

func (tokenRegistry) Purge(id string) error             { return nil }
func (tokenRegistry) Expire(id string) (bool, error)    { return false, nil }
func (tokenRegistry) Invalidate(id string) (int, error) { return 0, nil }

type cacheStore struct{}

func (cacheStore) Reset() error { return nil }

type mgr struct{ sessions *SessionStore }

func (m *mgr) SessionStore() *SessionStore { return m.sessions }

type core struct{ mgr mgr }

// Seed mirror (battery/auth logout): chained receiver, discard inside a
// loop, Redirect after the loop.
func (c *core) logoutForm(w http.ResponseWriter, r *http.Request) {
	for _, tok := range []string{"a", "b"} {
		_ = c.mgr.SessionStore().Delete(tok) // want `discardmutator: discarded result of c\.mgr\.SessionStore\(\)\.Delete is followed by a success response`
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (c *core) logoutJSON(w http.ResponseWriter) {
	_ = c.mgr.SessionStore().Delete("t") // want `discardmutator: discarded result of c\.mgr\.SessionStore\(\)\.Delete is followed by a success response`
	w.WriteHeader(http.StatusNoContent)
}

func revoke(store *SessionStore, w http.ResponseWriter) {
	_ = store.Revoke("t") // want `discardmutator: discarded result of store\.Revoke is followed by a success response`
	w.WriteHeader(http.StatusOK)
}

func markRevoked(sessions *SessionStore, w http.ResponseWriter) {
	_ = sessions.MarkRevoked("t") // want `discardmutator: discarded result of sessions\.MarkRevoked is followed by a success response`
	w.Write([]byte("ok"))
}

// Bare-statement discard: the error falls on the floor implicitly.
func purge(registry tokenRegistry, w http.ResponseWriter) {
	registry.Purge("id") // want `discardmutator: discarded result of registry\.Purge is followed by a success response`
	w.WriteHeader(http.StatusAccepted)
}

func expire(cache tokenRegistry, w http.ResponseWriter) {
	_, _ = cache.Expire("id") // want `discardmutator: discarded result of cache\.Expire is followed by a success response`
	w.Write([]byte("{}"))
}

func invalidate(registry tokenRegistry, w http.ResponseWriter) {
	_, _ = registry.Invalidate("id") // want `discardmutator: discarded result of registry\.Invalidate is followed by a success response`
	w.WriteHeader(http.StatusOK)
}

func reset(cacheStore cacheStore, w http.ResponseWriter) {
	cacheStore.Reset() // want `discardmutator: discarded result of cacheStore\.Reset is followed by a success response`
	w.WriteHeader(http.StatusOK)
}
