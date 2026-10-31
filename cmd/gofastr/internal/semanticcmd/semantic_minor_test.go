package semanticcmd

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/DonaldMurillo/gofastr/battery/semantic"
	"github.com/DonaldMurillo/gofastr/cmd/gofastr/internal/clitest"
)

// A local query or stats beside a running `semantic watch` snapshotted
// and truncated the watcher's WAL on Close (2026-10-27 bug bash B37).
// Both open the index read-only now: they see the writer's records and
// leave its files alone.
func TestLocalQueryBesideWriter(t *testing.T) {
	covT_embedSandbox(t)
	w, err := openLocalIndex(false)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Add(context.Background(), semantic.Document{ID: "d", Text: "authentication middleware"}); err != nil {
		t.Fatal(err)
	}
	walPath := filepath.Join(localSnapshotDir(), "store.wal")
	before, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatal(err)
	}
	out := clitest.CovT_capStdout(t, func() { embedQuery([]string{"authentication"}) })
	if !strings.Contains(out, `"doc_id": "d"`) {
		t.Errorf("query beside the writer missed its document: %s", out)
	}
	out = clitest.CovT_capStdout(t, func() { embedStats() })
	if !strings.Contains(out, `"docs": 1`) {
		t.Errorf("stats beside the writer: %s", out)
	}
	after, err := os.ReadFile(walPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("query/stats changed the writer's WAL: %d bytes -> %d", len(before), len(after))
	}
}

// A mount with a token answered 401 to every remote query and stats call:
// the CLI read no token and sent no Authorization header (2026-10-27 bug
// bash B39).
func TestRemoteCallsSendToken(t *testing.T) {
	t.Setenv("GOFASTR_SEMANTIC_TOKEN", "tok-1") // not-a-secret: test fixture
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer tok-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"hits":[]}`))
	}))
	defer srv.Close()
	if _, err := remoteQuery(srv.URL, semantic.Query{Text: "x"}); err != nil {
		t.Fatalf("remoteQuery: %v", err)
	}
	if _, err := remoteGet(srv.URL + "/semantic/stats"); err != nil {
		t.Fatalf("remoteGet: %v", err)
	}
}

// Without the variable no Authorization header goes out.
func TestRemoteCallsOmitUnsetToken(t *testing.T) {
	t.Setenv("GOFASTR_SEMANTIC_TOKEN", "")
	var sent atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["Authorization"]; ok {
			sent.Store(true)
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if _, err := remoteGet(srv.URL + "/semantic/stats"); err != nil {
		t.Fatalf("remoteGet: %v", err)
	}
	if sent.Load() {
		t.Fatal("an Authorization header went out with no token set")
	}
}

// The token goes to GOFASTR_URL only: a redirect is refused, so a peer
// cannot forward the credential to a host the user never named.
func TestRemoteCallsRefuseRedirect(t *testing.T) {
	t.Setenv("GOFASTR_SEMANTIC_TOKEN", "tok-1") // not-a-secret: test fixture
	var reached atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reached.Store(true)
		_, _ = w.Write([]byte(`{"hits":[]}`))
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	defer srv.Close()
	if _, err := remoteQuery(srv.URL, semantic.Query{Text: "x"}); err == nil {
		t.Error("remoteQuery followed a redirect")
	}
	if _, err := remoteGet(srv.URL + "/semantic/stats"); err == nil {
		t.Error("remoteGet followed a redirect")
	}
	if reached.Load() {
		t.Fatal("a redirect reached another host")
	}
}
