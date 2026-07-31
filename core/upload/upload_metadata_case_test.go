package upload

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestMetadataJSONUsesCamelCase pins the wire shape of the upload response:
// every key is camelCase, matching the rest of the framework. The rest of the
// framework emits camelCase JSON; the upload Metadata struct must not be the
// one snake_case outlier.
func TestMetadataJSONUsesCamelCase(t *testing.T) {
	handler := Handler(Config{Storage: NewLocalStorage(tmpDir(t))})
	body, contentType := newMultipartBody(t, "report.txt", "hello world")
	req := httptest.NewRequest(http.MethodPost, "/upload", body)
	req.Header.Set("Content-Type", contentType)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("upload failed: %d %s", rec.Code, rec.Body.String())
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(rec.Body.Bytes(), &raw); err != nil {
		t.Fatalf("decode metadata: %v\nbody: %s", err, rec.Body.String())
	}
	for _, key := range []string{"originalName", "size", "mimeType", "uploadedAt", "key"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("metadata JSON missing camelCase key %q; body: %s", key, rec.Body.String())
		}
	}
	for _, old := range []string{"original_name", "mime_type", "uploaded_at"} {
		if _, ok := raw[old]; ok {
			t.Errorf("metadata JSON still uses snake_case key %q; body: %s", old, rec.Body.String())
		}
	}
}
