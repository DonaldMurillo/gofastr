// Package teststorage owns temporary local storage handles for test fixtures.
package teststorage

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

// New registers Close after the temporary directory was registered for cleanup.
func New(t testing.TB, dir string) *upload.LocalStorage {
	t.Helper()
	store := upload.NewLocalStorage(dir)
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close local storage: %v", err)
		}
	})
	return store
}
