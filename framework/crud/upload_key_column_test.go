package crud

import (
	"bytes"
	"context"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
)

// An uploaded file's column holds its storage key, not its URL form. The
// two differ for a name that keeps '#' or '%': the column is what
// provenance, erasure and the screens' link builder all read as a key.
func TestUploadColumnHoldsStorageKey(t *testing.T) {
	for _, name := range []string{"shot#1.png", "100%.png"} {
		ch, db := covUploadHandler(t)
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("photo", name)
		_, _ = fw.Write(pngBytes())
		mw.Close()
		req := withTestUser(httptest.NewRequest("POST", "/media", &buf), "u1")
		req.Header.Set("Content-Type", mw.FormDataContentType())
		rec := httptest.NewRecorder()
		ch.Create()(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("upload %q = %d, body=%s", name, rec.Code, rec.Body.String())
		}
		got := decodeSingleResponse(t, rec.Body.Bytes())
		var stored string
		if err := db.QueryRow("SELECT photo FROM media WHERE id = ?", got["id"]).Scan(&stored); err != nil {
			t.Fatal(err)
		}
		if ok, err := ch.Storage.Exists(context.Background(), stored); err != nil || !ok {
			t.Errorf("upload %q: column %q names no stored object (%v, %v)", name, stored, ok, err)
		}
	}
}
