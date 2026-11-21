package storage_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/storage"
)

func newTestLocalStorage(t testing.TB, base string, options ...storage.LocalOption) *storage.LocalStorage {
	t.Helper()
	store := storage.NewLocalStorage(base, options...)
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close local storage: %v", err)
		}
	})
	return store
}
