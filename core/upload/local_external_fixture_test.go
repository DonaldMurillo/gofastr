package upload_test

import (
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
)

func newTestLocalStorage(t testing.TB, base string) *upload.LocalStorage {
	t.Helper()
	store := upload.NewLocalStorage(base)
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close upload storage: %v", err)
		}
	})
	return store
}
