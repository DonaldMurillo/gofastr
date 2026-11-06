package themecmd

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestThemeWriteRetriesAfterFailure(t *testing.T) {
	server := newTestServer(t)
	write := func() int {
		request := httptest.NewRequest(http.MethodPost, "http://127.0.0.1:0/__theme/writeback", nil)
		request.Header.Set("Authorization", "Bearer "+server.Token)
		request.Header.Set("Origin", server.Origins[0])
		response := httptest.NewRecorder()
		server.ServeHTTP(response, request)
		return response.Code
	}
	if status := write(); status != http.StatusOK {
		t.Fatalf("first write: %d", status)
	}
	original, err := os.ReadFile(server.OutPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(server.OutPath, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(server.OutPath, 0o644) })
	for _, color := range []string{"#B91C1C", "#166534"} {
		if _, err := server.applyToken("color-primary", color); err != nil {
			t.Fatal(err)
		}
		if status := write(); status != http.StatusInternalServerError {
			t.Fatalf("read-only write: %d", status)
		}
	}
	unchanged, err := os.ReadFile(server.OutPath)
	if err != nil || !bytes.Equal(original, unchanged) {
		t.Fatalf("failed write changed disk: %v", err)
	}
	if err := os.Chmod(server.OutPath, 0o644); err != nil {
		t.Fatal(err)
	}
	if status := write(); status != http.StatusOK {
		t.Fatalf("retry after repairing permissions: %d", status)
	}
	updated, err := os.ReadFile(server.OutPath)
	if err != nil || !bytes.Contains(updated, []byte("#166534")) {
		t.Fatalf("retry did not save the working theme: %v", err)
	}
}
