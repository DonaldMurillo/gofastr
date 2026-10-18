package storage

import "testing"

func newTestLocalStorage(t testing.TB, base string, options ...LocalOption) *LocalStorage {
	t.Helper()
	store := NewLocalStorage(base, options...)
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close local storage: %v", err)
		}
	})
	return store
}
