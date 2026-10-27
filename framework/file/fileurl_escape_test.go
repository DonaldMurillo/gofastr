package file_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/core/upload"
	"github.com/DonaldMurillo/gofastr/framework/file"
	"github.com/DonaldMurillo/gofastr/internal/teststorage"
)

// FileField.URL is a URL path: a name with '#', '?' or '%' in it is
// escaped there, so a browser fetching it reaches the stored object
// instead of reading the tail as a fragment or query. StorageRef stays
// the literal key the object was saved under.
func TestFileFieldURLEscapesName(t *testing.T) {
	store := teststorage.New(t, t.TempDir())
	mux := http.NewServeMux()
	mux.Handle("/uploads/{key...}", upload.ServeHandler(store))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	for _, name := range []string{"report#2026.txt", "what?now.txt", "100%.txt", "a%2Fb.txt"} {
		body := "contents of " + name
		ff, err := file.ProcessFileField(context.Background(), store, strings.NewReader(body), name, "posts", "attachment")
		if err != nil {
			t.Fatalf("ProcessFileField(%q): %v", name, err)
		}
		if want := strings.TrimSuffix(upload.SanitizeFilename(name), ".txt"); !strings.Contains(ff.StorageRef, want+"_") {
			t.Errorf("StorageRef %q is not the literal sanitized name %q", ff.StorageRef, want)
		}
		if ok, err := store.Exists(context.Background(), ff.StorageRef); err != nil || !ok {
			t.Errorf("object not stored under StorageRef %q: %v, %v", ff.StorageRef, ok, err)
		}
		u, err := url.Parse(ff.URL)
		if err != nil {
			t.Fatalf("URL %q does not parse: %v", ff.URL, err)
		}
		if u.Fragment != "" || u.RawQuery != "" || u.Path != ff.StorageRef {
			t.Errorf("URL %q reads as path %q, query %q, fragment %q; want the path %q",
				ff.URL, u.Path, u.RawQuery, u.Fragment, ff.StorageRef)
		}
		if err := ff.Validate(); err != nil {
			t.Errorf("Validate(%q): %v", name, err)
		}
		res, err := http.Get(srv.URL + "/uploads/" + ff.URL)
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if res.StatusCode != http.StatusOK || string(got) != body {
			t.Errorf("GET /uploads/%s = %d %q, want 200 %q", ff.URL, res.StatusCode, got, body)
		}
	}
}
