package framework

import (
	"bufio"
	"database/sql"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/DonaldMurillo/gofastr/core/schema"
	_ "github.com/mattn/go-sqlite3"
)

// An SSE subscriber must outlive the request timeout and still receive
// events published after the deadline would have fired. This is the
// app-level pin for the one-request-lifetime policy: streams shed the
// deadline at their first flush.
func TestSSEOutlivesRequestTimeout(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	app := NewApp(
		WithDB(db),
		WithConfig(AppConfig{RequestTimeout: 300 * time.Millisecond}),
	)
	app.Entity("posts", EntityConfig{
		Exposure: &ExposureConfig{Public: true},
		Fields:   []schema.Field{{Name: "title", Type: schema.String}},
	})

	addr, stop := startOnRandomPort(t, app)
	defer stop()

	resp, err := http.Get("http://" + addr + "/posts/_events")
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("subscribe status = %d", resp.StatusCode)
	}
	r := bufio.NewReader(resp.Body)
	if _, err := r.ReadString('\n'); err != nil {
		t.Fatalf("first line: %v", err)
	}

	// Sit past the request timeout, then publish.
	time.Sleep(900 * time.Millisecond)
	post, err := http.Post("http://"+addr+"/posts", "application/json",
		strings.NewReader(`{"title":"late"}`))
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	post.Body.Close()
	if post.StatusCode != http.StatusCreated {
		t.Fatalf("create status = %d", post.StatusCode)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("stream died before delivering the event: %v", err)
		}
		if strings.Contains(line, "late") {
			return
		}
	}
	t.Fatal("event never arrived on the long-lived stream")
}

// DisableRequestTimeout documents itself as the opt-out for SSE and long
// uploads; the server-level read/write timeouts must follow it, or the
// opt-out is a lie past 60 seconds.
func TestDisableTimeoutRelaxesServer(t *testing.T) {
	app := NewApp(WithConfig(AppConfig{DisableRequestTimeout: true}))

	_, stop := startOnRandomPort(t, app)
	defer stop()

	app.serverMu.Lock()
	srv := app.server
	app.serverMu.Unlock()
	if srv.ReadTimeout != 0 || srv.WriteTimeout != 0 {
		t.Fatalf("read/write timeouts = %v/%v, want 0/0 when request timeout is disabled",
			srv.ReadTimeout, srv.WriteTimeout)
	}
	if srv.ReadHeaderTimeout == 0 || srv.IdleTimeout == 0 {
		t.Fatal("header/idle timeouts must stay set — they bound the connection, not the stream")
	}
}
