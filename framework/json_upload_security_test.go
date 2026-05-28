package framework

import (
	"database/sql"
	"net/http"
	"testing"
)

// Image/File fields store URLs that flow into HTML attributes and HTTP
// responses later. The multipart path runs uploads through a sniffer;
// the JSON path stored whatever string the caller supplied, which is
// stored XSS the moment any view renders <img src=…>.

var dangerousMediaURLs = []string{
	"javascript:alert(1)",
	"JAVASCRIPT:alert(1)",
	"data:text/html,<svg/onload=1>",
	"vbscript:msgbox(1)",
	"file:///etc/passwd",
	"blob:https://evil.example/123",
	"../../../etc/passwd",
	"//evil.example/x.png",
	"mailto:attacker@example.com",
	"https://example.com/%0d%0aX-Test:1",
	"view-source:https://example.com",
}

func TestUpload_JSONCreateRejectsUnsafeURL(t *testing.T) {
	for _, payload := range dangerousMediaURLs {
		t.Run(payload, func(t *testing.T) {
			runUploadTest(t, func(t *testing.T, db *sql.DB, ta *TestApp, _ string) {
				resp := ta.Post("/posts", map[string]any{"title": "JSON", "avatar": payload})
				if resp.Status() == http.StatusCreated {
					t.Fatalf("Create accepted unsafe avatar URL %q; body=%s", payload, resp.Body())
				}
				var n int
				if err := db.QueryRow("SELECT COUNT(*) FROM posts").Scan(&n); err != nil {
					t.Fatalf("count: %v", err)
				}
				if n != 0 {
					t.Fatalf("rejected create still persisted a row (count=%d)", n)
				}
			})
		})
	}
}

func TestUpload_JSONUpdateRejectsUnsafeURL(t *testing.T) {
	for _, payload := range dangerousMediaURLs {
		t.Run(payload, func(t *testing.T) {
			forEachDialect(t, func(t *testing.T, db *sql.DB, _ Dialect) {
				seedUploadDB(t, db)
				if _, err := db.Exec("INSERT INTO posts(id, title, avatar) VALUES ($1, $2, $3)", "p1", "Original", "safe.png"); err != nil {
					t.Fatalf("seed: %v", err)
				}
				app, _ := uploadAppOnDB(t, db)
				ta := TestHarness(t, app)
				resp := ta.Put("/posts/p1", map[string]any{"title": "Updated", "avatar": payload})
				if resp.Status() == http.StatusOK {
					t.Fatalf("Update accepted unsafe avatar URL %q; body=%s", payload, resp.Body())
				}
				var avatar string
				if err := db.QueryRow("SELECT avatar FROM posts WHERE id = $1", "p1").Scan(&avatar); err != nil {
					t.Fatalf("read avatar: %v", err)
				}
				if avatar != "safe.png" {
					t.Fatalf("rejected update still mutated row (avatar=%q)", avatar)
				}
			})
		})
	}
}
