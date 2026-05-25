package rest

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DonaldMurillo/gofastr/framework/harness/control/auth"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/multiplex"
	"github.com/DonaldMurillo/gofastr/framework/harness/control/resources"
	"github.com/DonaldMurillo/gofastr/framework/harness/ids"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool"
	"github.com/DonaldMurillo/gofastr/framework/harness/tool/builtins"
)

func newServer(t *testing.T) *Server {
	t.Helper()
	secret, _ := auth.GenerateSecret()
	return &Server{
		Mux:         multiplex.New(),
		Catalog:     resources.NewCatalog(),
		Encoder:     auth.NewEncoder(secret),
		Revocations: auth.NewRevocationList(),
		Features:    []string{"rest"},
	}
}

func TestHandshakeNoAuth(t *testing.T) {
	s := newServer(t)
	req := httptest.NewRequest("GET", "/v1/handshake", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var hs map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &hs); err != nil {
		t.Fatal(err)
	}
	if hs["protocol_version"] == "" {
		t.Error("missing protocol_version")
	}
}

func TestHealthNoAuth(t *testing.T) {
	s := newServer(t)
	req := httptest.NewRequest("GET", "/v1/health", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestSessionsRequiresToken(t *testing.T) {
	s := newServer(t)
	req := httptest.NewRequest("GET", "/v1/sessions", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestSessionsWithToken(t *testing.T) {
	s := newServer(t)
	tok, err := s.Encoder.Encode(auth.Claims{
		Sessions:      nil,
		IdentityClass: 0,
		ExpiresAt:     0,
	})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest("GET", "/v1/sessions", nil)
	req.Header.Set("X-Harness-Token", tok)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("status = %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestInvalidHostRejected(t *testing.T) {
	s := newServer(t)
	s.AllowedHosts = []string{"127.0.0.1:8421"}
	req := httptest.NewRequest("GET", "/v1/handshake", nil)
	req.Host = "evil.com"
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestInvalidOriginRejected(t *testing.T) {
	s := newServer(t)
	s.AllowedOrigins = []string{"http://localhost:8421"}
	req := httptest.NewRequest("GET", "/v1/handshake", nil)
	req.Header.Set("Origin", "http://attacker.com")
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Errorf("status = %d, want 403", rec.Code)
	}
}

func TestSlashCommandsCatalog(t *testing.T) {
	s := newServer(t)
	tok, _ := s.Encoder.Encode(auth.Claims{ExpiresAt: 0})
	req := httptest.NewRequest("GET", "/v1/slash-commands", nil)
	req.Header.Set("X-Harness-Token", tok)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "help") {
		t.Errorf("body missing help: %q", rec.Body.String())
	}
}

// keep imports honest in case of future test paths
var (
	_ = context.Background
	_ = bytes.NewReader
)

// TestSessionTasksEndpoint TDD: writing a TaskList snapshot for a
// session should be readable via GET /v1/sessions/<id>/tasks.
// Failing before the endpoint is wired; passing after.
func TestSessionTasksEndpoint(t *testing.T) {
	s := newServer(t)
	sess := ids.NewSessionID()
	defer builtins.ResetTasks(sess)

	// Stash a known plan in the per-session task store.
	tl := builtins.TaskList{}
	ctxWithSess := tool.WithSession(context.Background(), sess)
	raw, _ := json.Marshal(map[string]any{"tasks": []builtins.TaskItem{
		{Content: "audit imports", Status: "completed"},
		{Content: "fix race in bus", Status: "in_progress", ActiveForm: "Fixing race"},
		{Content: "write changelog", Status: "pending"},
	}})
	if _, err := tl.Run(ctxWithSess,
		tool.ToolCall{Name: "TaskList", Input: raw}, nil); err != nil {
		t.Fatal(err)
	}

	// Mint a token bound to this session.
	tok, err := s.Encoder.Encode(auth.Claims{
		Ver:           auth.VerCurrent,
		JTI:           ids.NewJTI(),
		Sessions:      []ids.SessionID{sess},
		IdentityClass: 0,
		ExpiresAt:     0,
	})
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest("GET", "/v1/sessions/"+string(sess)+"/tasks", nil)
	req.Header.Set("X-Harness-Token", tok)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	var body struct {
		Tasks []builtins.TaskItem `json:"tasks"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("parse: %v body=%s", err, rec.Body.String())
	}
	if len(body.Tasks) != 3 {
		t.Fatalf("got %d tasks, want 3: %+v", len(body.Tasks), body.Tasks)
	}
	if body.Tasks[1].Status != "in_progress" || body.Tasks[1].ActiveForm != "Fixing race" {
		t.Errorf("in-progress task lost fields: %+v", body.Tasks[1])
	}
}

func TestSessionTasksEndpointRequiresToken(t *testing.T) {
	s := newServer(t)
	sess := ids.NewSessionID()
	req := httptest.NewRequest("GET", "/v1/sessions/"+string(sess)+"/tasks", nil)
	rec := httptest.NewRecorder()
	s.Handler().ServeHTTP(rec, req)
	if rec.Code != 401 {
		t.Errorf("status = %d, want 401 for missing token", rec.Code)
	}
}
