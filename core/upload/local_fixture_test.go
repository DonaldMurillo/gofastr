package upload

import "testing"

func newTestLocalStorage(t testing.TB, base string) *LocalStorage {
	t.Helper()
	store := NewLocalStorage(base)
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close upload storage: %v", err)
		}
	})
	return store
}
